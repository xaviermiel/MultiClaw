package analyze

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
)

// subaccountFunctions are the module functions subaccounts call.
var subaccountFunctions = map[string]bool{
	"executeOnProtocol": true, "executeOnProtocolWithValue": true, "transferToken": true,
}

// reader reads views at one block (nil: the latest).
type reader struct {
	b     Backend
	block *big.Int
}

// call calls method of the contract at `to`, described by a.
func (r reader) call(ctx context.Context, to common.Address, a *abi.ABI, method string, args ...any) ([]any, error) {
	data, err := a.Pack(method, args...)
	if err != nil {
		return nil, err
	}
	out, err := r.b.CallContract(ctx, ethereum.CallMsg{To: &to, Data: data}, r.block)
	if err != nil {
		return nil, err
	}
	return a.Unpack(method, out)
}

// view returns the single output of a module view, as a string formatted by
// format, or why it could not be read.
func view[T any](ctx context.Context, r reader, module common.Address, format func(T) string, method string, args ...any) string {
	vals, err := r.call(ctx, module, &contracts.ModuleABI, method, args...)
	if err != nil {
		return "unavailable: " + oneLine(err)
	}
	v, ok := vals[0].(T)
	if !ok {
		return fmt.Sprintf("unexpected %T", vals[0])
	}
	return format(v)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// state reports the module state behind the error, at the block before the
// transaction, or now when the node has no historical state.
func (a *Analyzer) state(ctx context.Context, an *analysis) {
	if an.method == nil {
		return // not a module call
	}
	module := *an.to()
	r := reader{a.b, an.before()}
	title := fmt.Sprintf("MODULE STATE AT BLOCK %d (before the transaction)", an.block-1)
	safe, err := contracts.NewModule(module, a.b).At(r.block).Avatar(ctx)
	if err != nil && !ethx.IsExecutionError(err) {
		r.block = nil
		title = "MODULE STATE NOW (the node has no state for the block before the transaction)"
		safe, err = contracts.NewModule(module, a.b).Avatar(ctx)
	}
	if err != nil {
		an.add(title, field{"", "The target does not answer as a DeFiInteractorModule: " + oneLine(err)})
		return
	}
	fields := []field{{"Safe", safe.Hex()}}
	add := func(key, value string) { fields = append(fields, field{key, value}) }
	errName := ""
	if an.revert != nil {
		errName = an.revert.name
	}
	// The subaccount: the sender, or the one an oracle function updates.
	sub := an.tx.From
	if !subaccountFunctions[an.method.RawName] && len(an.method.Inputs) > 0 && an.method.Inputs[0].Name == "subAccount" {
		sub = an.args[0].(common.Address)
	}

	if subaccountFunctions[an.method.RawName] {
		add("Spending allowance", view(ctx, r, module, usd, "getSpendingAllowance", sub))
		add("Last oracle update", view(ctx, r, module, an.since, "lastOracleUpdate", sub))
		switch errName {
		case "StaleOracleData":
			add("Max oracle age", view(ctx, r, module, maxAge, "maxOracleAge"))
		case "Unauthorized":
			for _, role := range []struct {
				name string
				id   uint16
			}{{"DEFI_EXECUTE_ROLE", contracts.DefiExecuteRole}, {"DEFI_TRANSFER_ROLE", contracts.DefiTransferRole}, {"DEFI_REPAY_ROLE", contracts.DefiRepayRole}} {
				add("Has "+role.name, view(ctx, r, module, yesNo, "hasRole", sub, role.id))
			}
		case "AddressNotAllowed":
			if an.inner != nil {
				add("Target whitelisted", view(ctx, r, module, yesNo, "allowedAddresses", sub, an.inner.target))
			}
		}
		if (errName == "SpenderNotAllowed" || errName == "ApprovalExceedsLimit") && an.inner != nil && an.inner.method != nil && an.inner.method.RawName == "approve" {
			add("Spender whitelisted", view(ctx, r, module, yesNo, "allowedAddresses", sub, an.inner.args[0].(common.Address)))
		}
		if (errName == "ApprovalExceedsLimit" || errName == "ExceedsSpendingLimit") && an.inner != nil && an.inner.spends != nil {
			fields = append(fields, a.spending(ctx, r, module, safe, sub, *an.inner.spends)...)
		}
	} else {
		oracle := view(ctx, r, module, func(o common.Address) string {
			if o == an.tx.From {
				return o.Hex() + " (the sender)"
			}
			return o.Hex()
		}, "authorizedOracle")
		add("Authorized oracle", oracle)
	}
	switch errName {
	case "StalePortfolioValue", "ExceedsAllowanceCap":
		if v, err := contracts.NewModule(module, a.b).At(r.block).GetSafeValue(ctx); err == nil {
			add("Safe value", usd(v.TotalValueUSD))
			add("Safe value updated", an.since(v.LastUpdated))
		} else {
			add("Safe value", "unavailable: "+oneLine(err))
		}
		add("Max Safe value age", view(ctx, r, module, maxAge, "maxSafeValueAge"))
		if errName == "ExceedsAllowanceCap" {
			add("Subaccount limits", a.limits(ctx, module, r.block, sub))
		}
	case "ExceedsCumulativeSpendingLimit":
		add("Window start", view(ctx, r, module, an.since, "windowStart", sub))
		add("Window Safe value", view(ctx, r, module, usd, "windowSafeValue", sub))
		add("Spent in the window", view(ctx, r, module, usd, "cumulativeSpent", sub))
		add("Subaccount limits", a.limits(ctx, module, r.block, sub))
	case "ExceedsOracleAcquiredBudget":
		// The budget is maxOracleAcquiredBps of the Safe value at the start
		// of the spending window (the current one before any spending).
		add("Grant window start", view(ctx, r, module, an.since, "acquiredGrantWindowStart", sub))
		add("Granted in the window", view(ctx, r, module, usd, "cumulativeOracleGrantedUSD", sub))
		add("Window Safe value", view(ctx, r, module, usd, "windowSafeValue", sub))
		add("Max oracle acquired budget", view(ctx, r, module, bps, "maxOracleAcquiredBps"))
	case "EnforcedPause":
		add("Paused", view(ctx, r, module, yesNo, "paused"))
	}
	an.add(title, fields...)
}

// spending shows what the module weighs a spent token against: its acquired
// balance, the Safe's balance, and the USD value of the rest.
func (a *Analyzer) spending(ctx context.Context, r reader, module, safe, sub common.Address, spent tokenAmount) []field {
	token := spent.token
	sym := a.symbol(ctx, token)
	switch {
	case token == (common.Address{}):
		sym = "ETH"
	case sym == "":
		sym = shortAddress(token)
	}
	fields := []field{{"Spends", a.amount(ctx, token, spent.amount)}}
	vals, err := r.call(ctx, module, &contracts.ModuleABI, "getAcquiredBalance", sub, token)
	if err != nil {
		return append(fields, field{"Acquired " + sym, "unavailable: " + oneLine(err)})
	}
	acquired := vals[0].(*big.Int)
	fields = append(fields, field{"Acquired " + sym, a.amount(ctx, token, acquired)})

	var balance *big.Int
	if token == (common.Address{}) {
		balance, err = a.b.BalanceAt(ctx, safe, r.block)
	} else {
		balance, err = contracts.NewERC20(token, a.b).At(r.block).BalanceOf(ctx, safe)
	}
	if err == nil {
		fields = append(fields, field{"Safe " + sym + " balance", a.amount(ctx, token, balance)})
	}

	// The module values what is not acquired with its own price feed.
	vals, err = r.call(ctx, module, &contracts.ModuleABI, "tokenPriceFeeds", token)
	if err != nil || vals[0].(common.Address) == (common.Address{}) {
		return append(fields, field{sym + " price", "no price feed set on the module"})
	}
	feed := contracts.NewPriceFeed(vals[0].(common.Address), a.b).At(r.block)
	round, err := feed.LatestRoundData(ctx)
	if err != nil {
		return append(fields, field{sym + " price", "unavailable: " + oneLine(err)})
	}
	decimals, err := feed.Decimals(ctx)
	if err != nil {
		return append(fields, field{sym + " price", "unavailable: " + oneLine(err)})
	}
	fields = append(fields, field{sym + " price", bn.FormatUnits(round.Answer, int(decimals)) + " USD"})
	if spent.amount.Cmp(maxUint256) != 0 && round.Answer.Sign() > 0 {
		rest := new(big.Int)
		if spent.amount.Cmp(acquired) > 0 {
			rest.Sub(spent.amount, acquired)
		}
		value := bn.MulDiv(rest, round.Answer, bn.Pow10(uint(a.tokenDecimals(ctx, token))))
		fields = append(fields, field{"USD value beyond the acquired balance", bn.FormatUnits(value, int(decimals)) + " USD"})
	}
	return fields
}

// since shows a timestamp and how long before the transaction's block it
// was ("never" for 0).
func (an *analysis) since(ts *big.Int) string {
	if ts.Sign() == 0 {
		return "never"
	}
	if !ts.IsInt64() {
		return ts.String()
	}
	t := time.Unix(ts.Int64(), 0).UTC()
	s := t.Format(time.RFC3339)
	if !an.time.IsZero() {
		if d := an.time.Sub(t); d >= 0 {
			s += fmt.Sprintf(" (%s before the block)", d)
		} else {
			s += fmt.Sprintf(" (%s after the block)", -d)
		}
	}
	return s
}

// limits shows a subaccount's spending limits.
func (a *Analyzer) limits(ctx context.Context, module common.Address, block *big.Int, sub common.Address) string {
	l, err := contracts.NewModule(module, a.b).At(block).GetSubAccountLimits(ctx, sub)
	if err != nil {
		return "unavailable: " + oneLine(err)
	}
	limit := bps(l.MaxSpendingBps) + " of the Safe value"
	if l.MaxSpendingUSD.Sign() > 0 {
		limit = usd(l.MaxSpendingUSD)
	}
	return limit + " per " + seconds(l.WindowDuration)
}

func bps(v *big.Int) string {
	return v.String() + " bps (" + bn.FormatUnits(v, 2) + "%)"
}

// maxAge shows a maximum age, warning about 0, which makes every value
// stale from the block after its update.
func maxAge(v *big.Int) string {
	if v.Sign() == 0 {
		return "0s: values are stale from the block after their update"
	}
	return seconds(v)
}

func seconds(v *big.Int) string {
	if !v.IsInt64() {
		return v.String() + "s"
	}
	return (time.Duration(v.Int64()) * time.Second).String()
}
