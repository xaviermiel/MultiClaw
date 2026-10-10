package spending

import (
	"fmt"
	"math/big"
	"slices"
	"strings"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/logx"
)

// withdrawalRatioPrecision is the basis-point precision the TS used when
// burning deposit receipt tokens on withdrawal (unlike the 1e18 used for
// every other ratio). Kept as-is: it affects on-chain acquired balances.
var withdrawalRatioPrecision = bn.BPS

// unifiedEvent is a protocol or transfer event in the merged, sorted stream.
type unifiedEvent struct {
	protocol *ProtocolExecutionEvent
	transfer *TransferExecutedEvent
}

func (u unifiedEvent) key() (timestamp, block, logIndex int64) {
	if u.protocol != nil {
		return u.protocol.Timestamp, u.protocol.BlockNumber, u.protocol.LogIndex
	}
	return u.transfer.Timestamp, u.transfer.BlockNumber, u.transfer.LogIndex
}

// BuildSubAccountState replays subAccount's events chronologically and
// returns its in-window spending and acquired balances as of
// currentTimestamp. Port of buildSubAccountState (spending-oracle.ts).
//
// priceCache may be nil (no USD weighting). logf receives the same progress
// messages the TS logged.
func BuildSubAccountState(
	events []ProtocolExecutionEvent,
	transferEvents []TransferExecutedEvent,
	subAccount common.Address,
	currentTimestamp int64,
	windowDuration int64,
	priceCache TokenPriceCache,
	logf logx.Logf,
) *SubAccountState {
	if logf == nil {
		logf = logx.Discard
	}
	windowStart := currentTimestamp - windowDuration

	state := &SubAccountState{
		TotalSpendingInWindow: bn.Zero(),
		AcquiredQueues:        NewOrderedMap[common.Address, *Queue](),
		AcquiredBalances:      NewOrderedMap[common.Address, *big.Int](),
	}

	// Filter events for this subaccount, then merge into a unified list
	// sorted chronologically (stable, like Array.prototype.sort): by
	// timestamp, then block number, then log index. This ensures transfers
	// are processed in correct order relative to protocol events.
	var unified []unifiedEvent
	protocolCount, transferCount := 0, 0
	for i := range events {
		if events[i].SubAccount == subAccount {
			unified = append(unified, unifiedEvent{protocol: &events[i]})
			protocolCount++
		}
	}
	for i := range transferEvents {
		if transferEvents[i].SubAccount == subAccount {
			unified = append(unified, unifiedEvent{transfer: &transferEvents[i]})
			transferCount++
		}
	}
	slices.SortStableFunc(unified, func(a, b unifiedEvent) int {
		at, ab, al := a.key()
		bt, bb, bl := b.key()
		switch {
		case at != bt:
			return cmpInt64(at, bt)
		case ab != bb:
			return cmpInt64(ab, bb)
		default:
			return cmpInt64(al, bl)
		}
	})

	logf("Processing %d events for %s (FIFO mode, %d protocol + %d transfers)",
		len(unified), lower(subAccount), protocolCount, transferCount)

	// Track all tokens that ever had acquired balance (for cleanup)
	tokensWithAcquiredHistory := NewOrderedMap[common.Address, struct{}]()

	// FIFO queues per token - tracks (amount, originalTimestamp)
	acquiredQueues := state.AcquiredQueues
	getQueue := func(token common.Address) *Queue {
		q, ok := acquiredQueues.Get(token)
		if !ok {
			q = &Queue{}
			acquiredQueues.Set(token, q)
		}
		return q
	}
	markHistory := func(token common.Address) { tokensWithAcquiredHistory.Set(token, struct{}{}) }

	for _, u := range unified {
		if u.transfer != nil {
			processTransfer(state, u.transfer, windowStart, windowDuration, getQueue, markHistory)
			continue
		}
		processProtocolEvent(state, u.protocol, windowStart, windowDuration, priceCache, getQueue, markHistory, logf)
	}

	// Calculate final acquired balances (only non-expired entries count)
	for _, token := range tokensWithAcquiredHistory.Keys() {
		queue, ok := acquiredQueues.Get(token)
		if !ok {
			queue = &Queue{}
		}
		PruneExpiredEntries(queue, currentTimestamp, windowDuration)

		validBalance := ValidQueueBalance(*queue, currentTimestamp, windowDuration)
		if validBalance.Sign() > 0 {
			state.AcquiredBalances.Set(token, validBalance)
		}
		logf("  Token %s: acquired balance = %s", lower(token), validBalance)
	}

	logf("State built: spending=%s, acquired tokens=%d", state.TotalSpendingInWindow, state.AcquiredBalances.Len())
	return state
}

func processTransfer(
	state *SubAccountState,
	transfer *TransferExecutedEvent,
	windowStart, windowDuration int64,
	getQueue func(common.Address) *Queue,
	markHistory func(common.Address),
) {
	isInWindow := transfer.Timestamp >= windowStart

	if isInWindow && transfer.SpendingCost.Sign() > 0 {
		state.SpendingRecords = append(state.SpendingRecords, SpendingRecord{Amount: bn.Copy(transfer.SpendingCost), Timestamp: transfer.Timestamp})
		state.TotalSpendingInWindow = bn.Add(state.TotalSpendingInWindow, transfer.SpendingCost)
	}

	if transfer.Amount.Sign() > 0 {
		ConsumeFromQueue(getQueue(transfer.Token), transfer.Amount, transfer.Timestamp, windowDuration)
		markHistory(transfer.Token)
	}
}

func processProtocolEvent(
	state *SubAccountState,
	event *ProtocolExecutionEvent,
	windowStart, windowDuration int64,
	priceCache TokenPriceCache,
	getQueue func(common.Address) *Queue,
	markHistory func(common.Address),
	logf logx.Logf,
) {
	isInWindow := event.Timestamp >= windowStart
	isSwapOrDeposit := event.OpType == contracts.OpSwap || event.OpType == contracts.OpDeposit
	isWithdrawOrClaim := event.OpType == contracts.OpWithdraw || event.OpType == contracts.OpClaim

	if (isSwapOrDeposit || isWithdrawOrClaim) &&
		(len(event.TokensIn) != len(event.AmountsIn) || len(event.TokensOut) != len(event.AmountsOut)) {
		// The event decoder rejects these, so they never get here. (The TS
		// didn't check: depending on the shape, it threw a TypeError or went
		// on with what it could read.) Other op types are ignored entirely,
		// as in the TS.
		panic(fmt.Sprintf("malformed event at block %d log %d: token/amount length mismatch", event.BlockNumber, event.LogIndex))
	}

	// Track spending (only count if in window)
	if isSwapOrDeposit && isInWindow && event.SpendingCost.Sign() > 0 {
		state.SpendingRecords = append(state.SpendingRecords, SpendingRecord{Amount: bn.Copy(event.SpendingCost), Timestamp: event.Timestamp})
		state.TotalSpendingInWindow = bn.Add(state.TotalSpendingInWindow, event.SpendingCost)
	}

	// Handle input token consumption (FIFO) - before creating deposit records
	// so deposits can capture the original acquisition timestamp. Expiry is
	// judged at the event's timestamp. Handles multiple input tokens (e.g.
	// LP position minting uses 2 tokens).
	var consumedEntries []AcquiredEntry
	totalAmountIn := bn.Zero()
	totalValueInUSD := bn.Zero()  // USD value of all inputs (for weighted ratio)
	consumedValueUSD := bn.Zero() // USD value of consumed acquired tokens
	hasAllPrices := true          // whether we have prices for all input tokens
	if isSwapOrDeposit {
		for i, tokenIn := range event.TokensIn {
			amountIn := event.AmountsIn[i]
			if amountIn.Sign() <= 0 {
				continue
			}
			totalAmountIn = bn.Add(totalAmountIn, amountIn)
			consumed, _ := ConsumeFromQueue(getQueue(tokenIn), amountIn, event.Timestamp, windowDuration)
			consumedEntries = append(consumedEntries, consumed...)
			markHistory(tokenIn)

			// Track USD values for weighted ratio calculation
			if priceCache != nil {
				if inputValueUSD, ok := TokenValueUSD(tokenIn, amountIn, priceCache); ok {
					totalValueInUSD = bn.Add(totalValueInUSD, inputValueUSD)
					// USD value of the consumed portion of this token
					if consumedTokenValueUSD, ok := TokenValueUSD(tokenIn, sumAmounts(consumed), priceCache); ok {
						consumedValueUSD = bn.Add(consumedValueUSD, consumedTokenValueUSD)
					}
				} else {
					hasAllPrices = false
				}
			}
		}
	}
	useUSDWeighting := priceCache != nil && hasAllPrices && totalValueInUSD.Sign() > 0

	if event.OpType == contracts.OpDeposit {
		createDepositRecords(state, event, consumedEntries, totalAmountIn, totalValueInUSD, consumedValueUSD, useUSDWeighting, priceCache, logf)
	}

	switch {
	case isSwapOrDeposit:
		addSwapOrDepositOutputs(event, consumedEntries, totalAmountIn, totalValueInUSD, consumedValueUSD, useUSDWeighting, getQueue, markHistory, logf)
	case isWithdrawOrClaim:
		matchWithdrawOrClaim(state, event, windowDuration, getQueue, markHistory, logf)
	}
}

// createDepositRecords tracks deposits for withdrawal matching, storing the
// original acquisition timestamp so withdrawals inherit it. Mixed
// acquired/non-acquired inputs get separate records so the acquired portion
// keeps its inherited timestamp and the rest gets the event timestamp.
func createDepositRecords(
	state *SubAccountState,
	event *ProtocolExecutionEvent,
	consumedEntries []AcquiredEntry,
	totalAmountIn, totalValueInUSD, consumedValueUSD *big.Int,
	useUSDWeighting bool,
	priceCache TokenPriceCache,
	logf logx.Logf,
) {
	totalConsumedForDeposit := sumAmounts(consumedEntries)
	fromNonAcquiredForDeposit := bn.Sub(totalAmountIn, totalConsumedForDeposit)
	isMixed := totalConsumedForDeposit.Sign() > 0 && fromNonAcquiredForDeposit.Sign() > 0

	// The oldest original timestamp among consumed acquired tokens (for the
	// acquired portion).
	acquiredTimestamp := event.Timestamp
	if len(consumedEntries) > 0 {
		acquiredTimestamp = consumedEntries[0].OriginalTimestamp
		for _, e := range consumedEntries {
			if e.OriginalTimestamp < acquiredTimestamp {
				acquiredTimestamp = e.OriginalTimestamp
			}
		}
		logf("  DEPOSIT: acquired portion will inherit timestamp %d", acquiredTimestamp)
	}
	// Timestamp for single (unsplit) records.
	singleRecordTimestamp := event.Timestamp
	if totalConsumedForDeposit.Sign() > 0 {
		singleRecordTimestamp = acquiredTimestamp
	}

	validInputCount := countPositive(event.AmountsIn)
	validOutputCount := countPositive(event.AmountsOut)
	// For multi-token LP deposits (N inputs -> 1 output), the output is
	// divided among inputs to avoid double-counting remainingOutputAmount.
	isMultiInputSingleOutput := validInputCount > 1 && len(event.TokensOut) == 1
	isSingleInputMultiOutput := validInputCount == 1 && validOutputCount > 1

	// USD-weighted (or amount-weighted) acquired ratio, for mixed deposits.
	acquiredRatioForDeposit := bn.Zero()
	if totalAmountIn.Sign() > 0 {
		if useUSDWeighting {
			acquiredRatioForDeposit = bn.MulDiv(consumedValueUSD, bn.Precision, totalValueInUSD)
		} else {
			acquiredRatioForDeposit = bn.MulDiv(totalConsumedForDeposit, bn.Precision, totalAmountIn)
		}
	}

	push := func(tokenIn common.Address, amountIn *big.Int, tokenOut common.Address, hasTokenOut bool, amountOut *big.Int, originalTimestamp int64) {
		state.DepositRecords = append(state.DepositRecords, &DepositRecord{
			SubAccount:                   event.SubAccount,
			Target:                       event.Target,
			TokenIn:                      tokenIn,
			AmountIn:                     bn.Copy(amountIn),
			RemainingAmount:              bn.Copy(amountIn),
			TokenOut:                     tokenOut,
			HasTokenOut:                  hasTokenOut,
			AmountOut:                    bn.Copy(amountOut),
			RemainingOutputAmount:        bn.Copy(amountOut),
			Timestamp:                    event.Timestamp,
			OriginalAcquisitionTimestamp: originalTimestamp,
		})
	}
	// pushSplit creates the acquired and non-acquired records of a mixed
	// deposit (each only if its input share is positive).
	pushSplit := func(tokenIn common.Address, amountIn *big.Int, tokenOut common.Address, hasTokenOut bool, amountOut *big.Int, acquiredLog, nonAcquiredLog string) {
		acquiredIn := bn.MulDiv(amountIn, acquiredRatioForDeposit, bn.Precision)
		nonAcquiredIn := bn.Sub(amountIn, acquiredIn)
		acquiredOut := bn.MulDiv(amountOut, acquiredRatioForDeposit, bn.Precision)
		nonAcquiredOut := bn.Sub(amountOut, acquiredOut)
		if acquiredIn.Sign() > 0 {
			logf(acquiredLog, acquiredIn, tokenIn.Hex(), acquiredOut, outName(tokenOut, hasTokenOut), acquiredTimestamp)
			push(tokenIn, acquiredIn, tokenOut, hasTokenOut, acquiredOut, acquiredTimestamp)
		}
		if nonAcquiredIn.Sign() > 0 {
			logf(nonAcquiredLog, nonAcquiredIn, tokenIn.Hex(), nonAcquiredOut, outName(tokenOut, hasTokenOut), event.Timestamp)
			push(tokenIn, nonAcquiredIn, tokenOut, hasTokenOut, nonAcquiredOut, event.Timestamp) // non-acquired = new spending
		}
	}

	if isSingleInputMultiOutput {
		// Single input -> multiple outputs: one deposit record per output,
		// with the input allocated by output value (USD), not by count.
		firstValid := slices.IndexFunc(event.AmountsIn, func(a *big.Int) bool { return a.Sign() > 0 })
		tokenIn := event.TokensIn[firstValid]
		amountIn := event.AmountsIn[firstValid]

		totalOutputValueUSD := bn.Zero()
		outputValuesUSD := make([]*big.Int, len(event.TokensOut))
		for i, amountOut := range event.AmountsOut {
			outputValuesUSD[i] = bn.Zero()
			if amountOut.Sign() <= 0 {
				continue
			}
			if priceCache != nil {
				if v, ok := TokenValueUSD(event.TokensOut[i], amountOut, priceCache); ok {
					outputValuesUSD[i] = v
					totalOutputValueUSD = bn.Add(totalOutputValueUSD, v)
				}
			}
		}

		allocatedInput := bn.Zero()
		for i, tokenOut := range event.TokensOut {
			amountOut := event.AmountsOut[i]
			if amountOut.Sign() <= 0 {
				continue
			}

			// This output's share of the input.
			var inputShare *big.Int
			switch remainingOutputs := countPositive(event.AmountsOut[i:]); {
			case remainingOutputs == 1:
				// Last output gets remainder to prevent dust loss
				inputShare = bn.Sub(amountIn, allocatedInput)
			case totalOutputValueUSD.Sign() > 0 && outputValuesUSD[i].Sign() > 0:
				// USD-weighted share
				inputShare = bn.MulDiv(amountIn, outputValuesUSD[i], totalOutputValueUSD)
			default:
				// Fallback: equal division
				inputShare = bn.Quo(amountIn, big.NewInt(int64(validOutputCount)))
			}
			allocatedInput = bn.Add(allocatedInput, inputShare)

			logf("  DEPOSIT: single-input multi-output, allocating %s %s to %s (value-weighted)", inputShare, tokenIn.Hex(), tokenOut.Hex())

			if isMixed {
				pushSplit(tokenIn, inputShare, tokenOut, true, amountOut,
					"    Acquired portion: %s %s -> %s %s, timestamp %d",
					"    Non-acquired portion: %s %s -> %s %s, timestamp %d")
			} else {
				// All acquired or all non-acquired - single record
				push(tokenIn, inputShare, tokenOut, true, amountOut, singleRecordTimestamp)
			}
		}
		return
	}

	// Standard case: loop over inputs
	for i, tokenIn := range event.TokensIn {
		amountIn := event.AmountsIn[i]
		if amountIn.Sign() <= 0 {
			continue
		}

		// Corresponding output token: same index if available, otherwise the
		// first output (JS: tokensOut[i] || tokensOut[0] || "0x" and
		// amountsOut[i] || amountsOut[0] || 0n, where 0n is falsy).
		tokenOut, hasTokenOut := common.Address{}, false
		switch {
		case i < len(event.TokensOut):
			tokenOut, hasTokenOut = event.TokensOut[i], true
		case len(event.TokensOut) > 0:
			tokenOut, hasTokenOut = event.TokensOut[0], true
		}
		amountOut := bn.Zero()
		switch {
		case i < len(event.AmountsOut) && event.AmountsOut[i].Sign() != 0:
			amountOut = event.AmountsOut[i]
		case len(event.AmountsOut) > 0 && event.AmountsOut[0].Sign() != 0:
			amountOut = event.AmountsOut[0]
		}

		// Multi-input -> single-output LP deposits: divide the output equally
		// among inputs to prevent double-counting.
		if isMultiInputSingleOutput && amountOut.Sign() > 0 {
			amountOut = bn.Quo(amountOut, big.NewInt(int64(validInputCount)))
			logf("  DEPOSIT: multi-input LP detected, allocating %s %s to %s input (1/%d share)",
				amountOut, outName(tokenOut, hasTokenOut), tokenIn.Hex(), validInputCount)
		}

		// For mixed acquired/non-acquired, split each input with the overall
		// acquired ratio (an approximation: consumedEntries spans all inputs).
		if isMixed {
			pushSplit(tokenIn, amountIn, tokenOut, hasTokenOut, amountOut,
				"  DEPOSIT: acquired portion %s %s -> %s %s, timestamp %d",
				"  DEPOSIT: non-acquired portion %s %s -> %s %s, timestamp %d")
		} else {
			push(tokenIn, amountIn, tokenOut, hasTokenOut, amountOut, singleRecordTimestamp)
		}
	}
}

// addSwapOrDepositOutputs adds SWAP/DEPOSIT outputs to their acquired
// queues: the acquired share of the output inherits the consumed entries'
// timestamps (split proportionally), the rest is newly acquired.
func addSwapOrDepositOutputs(
	event *ProtocolExecutionEvent,
	consumedEntries []AcquiredEntry,
	totalAmountIn, totalValueInUSD, consumedValueUSD *big.Int,
	useUSDWeighting bool,
	getQueue func(common.Address) *Queue,
	markHistory func(common.Address),
	logf logx.Logf,
) {
	opName := event.OpType.String()
	totalConsumed := sumAmounts(consumedEntries)
	fromNonAcquired := bn.Sub(totalAmountIn, totalConsumed) // remaining came from original funds

	// distribute splits amount across the consumed entries by their amounts,
	// each portion inheriting that entry's timestamp; the last entry gets the
	// remainder so no dust is lost.
	distribute := func(outputQueue *Queue, amount *big.Int, tokenOut common.Address, logFormat string) {
		allocated := bn.Zero()
		for idx, entry := range consumedEntries {
			var entryOutput *big.Int
			if idx == len(consumedEntries)-1 {
				entryOutput = bn.Sub(amount, allocated)
			} else {
				entryRatio := bn.MulDiv(entry.Amount, bn.Precision, totalConsumed)
				entryOutput = bn.MulDiv(amount, entryRatio, bn.Precision)
			}
			if entryOutput.Sign() > 0 {
				if logFormat == "mixed" {
					logf("    %s %s inherits timestamp %d", entryOutput, tokenOut.Hex(), entry.OriginalTimestamp)
				} else {
					logf("  %s: %s %s inherits timestamp %d", opName, entryOutput, tokenOut.Hex(), entry.OriginalTimestamp)
				}
				AddToQueue(outputQueue, entryOutput, entry.OriginalTimestamp)
				allocated = bn.Add(allocated, entryOutput)
			}
		}
	}

	for i, tokenOut := range event.TokensOut {
		amountOut := event.AmountsOut[i]
		if amountOut.Sign() <= 0 {
			continue
		}
		markHistory(tokenOut)
		outputQueue := getQueue(tokenOut)

		switch {
		case totalConsumed.Sign() > 0 && fromNonAcquired.Sign() > 0:
			// Mixed case: proportionally split the output. Use a USD-weighted
			// ratio when every input is priced (handles e.g. 1 WETH + 1000
			// USDC), else the amount-weighted ratio.
			var acquiredRatio *big.Int
			if useUSDWeighting {
				acquiredRatio = bn.MulDiv(consumedValueUSD, bn.Precision, totalValueInUSD)
				logf("  %s: mixed input (USD-weighted) - $%s acquired + $%s non-acquired",
					opName, bn.FormatUnits(consumedValueUSD, 18), bn.FormatUnits(bn.Sub(totalValueInUSD, consumedValueUSD), 18))
			} else {
				acquiredRatio = bn.MulDiv(totalConsumed, bn.Precision, totalAmountIn)
				logf("  %s: mixed input - %s acquired + %s non-acquired", opName, totalConsumed, fromNonAcquired)
			}
			outputFromAcquired := bn.MulDiv(amountOut, acquiredRatio, bn.Precision)
			outputFromNonAcquired := bn.Sub(amountOut, outputFromAcquired)

			distribute(outputQueue, outputFromAcquired, tokenOut, "mixed")

			logf("    %s %s newly acquired at %d", outputFromNonAcquired, tokenOut.Hex(), event.Timestamp)
			AddToQueue(outputQueue, outputFromNonAcquired, event.Timestamp)

		case totalConsumed.Sign() > 0:
			// Entire input was acquired - output inherits timestamps
			// proportionally from consumed entries.
			distribute(outputQueue, amountOut, tokenOut, "full")

		default:
			// No acquired input - output is newly acquired
			logf("  %s: %s %s is newly acquired at %d", opName, amountOut, tokenOut.Hex(), event.Timestamp)
			AddToQueue(outputQueue, amountOut, event.Timestamp)
		}
	}
}

// matchWithdrawOrClaim matches WITHDRAW/CLAIM outputs against deposit records
// (oldest first): matched amounts inherit each deposit's original acquisition
// timestamp, and the deposit's receipt tokens are burned from their queue.
// An unmatched CLAIM remainder is acquired only if the subaccount has a
// deposit at the same target; an unmatched WITHDRAW remainder never is.
func matchWithdrawOrClaim(
	state *SubAccountState,
	event *ProtocolExecutionEvent,
	windowDuration int64,
	getQueue func(common.Address) *Queue,
	markHistory func(common.Address),
	logf logx.Logf,
) {
	opName := event.OpType.String()

	type outputToConsume struct {
		token   common.Address
		amount  *big.Int
		deposit *DepositRecord
	}
	type matchedPortion struct {
		amount    *big.Int
		timestamp int64
	}

	for i, tokenOut := range event.TokensOut {
		amountOut := event.AmountsOut[i]
		if amountOut.Sign() <= 0 {
			continue
		}

		remainingToMatch := bn.Copy(amountOut)
		// Receipt tokens to consume from the acquired queue (e.g. aLINK when
		// withdrawing LINK). remainingOutputAmount is reduced after the actual
		// queue consumption, since queue entries may have expired.
		var toConsume []outputToConsume
		// Each matched portion keeps its own deposit's timestamp.
		var matched []matchedPortion

		for _, deposit := range state.DepositRecords {
			if remainingToMatch.Sign() <= 0 {
				break
			}
			if deposit.Target != event.Target || deposit.SubAccount != event.SubAccount ||
				deposit.TokenIn != tokenOut || deposit.RemainingAmount.Sign() <= 0 {
				continue
			}

			consumeAmount := bn.Min(remainingToMatch, deposit.RemainingAmount)
			deposit.RemainingAmount = bn.Sub(deposit.RemainingAmount, consumeAmount)
			remainingToMatch = bn.Sub(remainingToMatch, consumeAmount)

			// Proportional receipt-token consumption: withdrawing 50% of the
			// deposited amount consumes 50% of its output token.
			if deposit.HasTokenOut && deposit.RemainingOutputAmount.Sign() > 0 {
				ratio := bn.MulDiv(consumeAmount, withdrawalRatioPrecision, deposit.AmountIn)
				outputAmount := bn.MulDiv(deposit.AmountOut, ratio, withdrawalRatioPrecision)
				maxConsume := bn.Min(outputAmount, deposit.RemainingOutputAmount)
				if maxConsume.Sign() > 0 {
					toConsume = append(toConsume, outputToConsume{token: deposit.TokenOut, amount: maxConsume, deposit: deposit})
					logf("  %s will consume up to %s %s (deposit output token)", opName, maxConsume, deposit.TokenOut.Hex())
				}
			}

			matched = append(matched, matchedPortion{amount: consumeAmount, timestamp: deposit.OriginalAcquisitionTimestamp})
			logf("  %s consuming %s from deposit (original acquisition: %d)", opName, consumeAmount, deposit.OriginalAcquisitionTimestamp)
		}

		// Burn the deposits' receipt tokens from the acquired queue, reducing
		// remainingOutputAmount by what was actually consumed.
		for _, c := range toConsume {
			queue := getQueue(c.token)
			markHistory(c.token)
			consumed, _ := ConsumeFromQueue(queue, c.amount, event.Timestamp, windowDuration)
			totalConsumed := sumAmounts(consumed)
			c.deposit.RemainingOutputAmount = bn.Sub(c.deposit.RemainingOutputAmount, totalConsumed)
			logf("  %s consumed %s %s from acquired queue (deposit receipt token)", opName, totalConsumed, lower(c.token))
		}

		// Add each matched portion with its own inherited timestamp.
		totalMatched := bn.Zero()
		for _, m := range matched {
			totalMatched = bn.Add(totalMatched, m.amount)
		}
		if totalMatched.Sign() > 0 {
			markHistory(tokenOut)
			outputQueue := getQueue(tokenOut)
			for _, m := range matched {
				logf("  %s matched: %s inherits original timestamp %d", opName, m.amount, m.timestamp)
				AddToQueue(outputQueue, m.amount, m.timestamp)
			}
		}

		if remainingToMatch.Sign() <= 0 {
			continue
		}
		if event.OpType != contracts.OpClaim {
			// Unmatched WITHDRAW: the receipt tokens weren't acquired by the
			// subaccount (external aTokens, or a deposit outside the
			// window/by the multisig), so the output belongs to the multisig.
			logf("  WITHDRAW unmatched: %s %s NOT acquired (no matching deposit from subaccount)", remainingToMatch, tokenOut.Hex())
			continue
		}

		// CLAIM rewards are acquired only if the subaccount has a deposit at
		// this target (it created the position that generates them), and
		// inherit the oldest such deposit's timestamp.
		hasMatchingDeposit := false
		oldestDepositTimestamp := event.Timestamp
		for _, d := range state.DepositRecords {
			if d.Target == event.Target && d.SubAccount == event.SubAccount {
				hasMatchingDeposit = true
				if d.OriginalAcquisitionTimestamp < oldestDepositTimestamp {
					oldestDepositTimestamp = d.OriginalAcquisitionTimestamp
				}
			}
		}
		if !hasMatchingDeposit {
			logf("  CLAIM: %s %s NOT acquired (no matching deposit from subaccount)", remainingToMatch, tokenOut.Hex())
			continue
		}
		markHistory(tokenOut)
		outputQueue := getQueue(tokenOut)
		logf("  CLAIM: %s %s is acquired (has deposit at target), inherits timestamp %d", remainingToMatch, tokenOut.Hex(), oldestDepositTimestamp)
		AddToQueue(outputQueue, remainingToMatch, oldestDepositTimestamp)
	}
}

func countPositive(xs []*big.Int) int {
	n := 0
	for _, x := range xs {
		if x.Sign() > 0 {
			n++
		}
	}
	return n
}

func cmpInt64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// lower renders an address in lowercase hex, as the TS did for addresses
// that came from event topics or its lowercased maps.
func lower(a common.Address) string { return strings.ToLower(a.Hex()) }

// outName renders a deposit output token, "0x" when there is none (as TS).
func outName(token common.Address, ok bool) string {
	if !ok {
		return "0x"
	}
	return token.Hex()
}
