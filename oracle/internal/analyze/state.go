package analyze

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
)

// functionKind is who may call a module function (a test checks it against
// the Solidity).
type functionKind int

const (
	ownerFunction      functionKind = iota // onlyOwner
	subaccountFunction                     // a subaccount with a role
	oracleFunction                         // onlyOracle
	anyoneFunction                         // initialize: anyone, once
)

func kindOf(name string) functionKind {
	switch name {
	case "executeOnProtocol", "executeOnProtocolWithValue", "transferToken":
		return subaccountFunction
	case "updateSafeValue", "updateSpendingAllowance", "updateAcquiredBalance", "batchUpdate":
		return oracleFunction
	case "initialize":
		return anyoneFunction
	}
	return ownerFunction
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

// number reads a module view returning a uint256.
func (r reader) number(ctx context.Context, module common.Address, method string, args ...any) (*big.Int, error) {
	vals, err := r.call(ctx, module, &contracts.ModuleABI, method, args...)
	if err != nil {
		return nil, err
	}
	v, ok := vals[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected %T", vals[0])
	}
	return v, nil
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

// moduleTarget is what the analyzer knows of the module a transaction
// calls: where its state is read, and its Safe.
type moduleTarget struct {
	read  reader // at the block before the transaction, or the latest
	title string
	// unread is why the block before could not be read.
	unread string
	safe   common.Address
	// err is set when the target does not answer as a module.
	err error
}

// locate reads the module's Safe at the block before the transaction, or at
// the latest block when that block cannot be read. A target that answers
// is taken to be a module.
func (a *Analyzer) locate(ctx context.Context, an *analysis) {
	m := &an.module
	m.read = reader{a.b, an.before()}
	m.title = fmt.Sprintf("MODULE STATE AT BLOCK %d (before the transaction)", an.before())
	m.safe, m.err = contracts.NewModule(*an.to(), a.b).At(m.read.block).Avatar(ctx)
	if m.err == nil || ethx.IsExecutionError(m.err) {
		return
	}
	if safe, err := contracts.NewModule(*an.to(), a.b).Avatar(ctx); err == nil {
		m.read.block, m.unread, m.safe, m.err = nil, oneLine(m.err), safe, nil
		m.title = "MODULE STATE NOW (the block before the transaction could not be read)"
	}
}

// state reports the module state behind the error, at the block before the
// transaction, or now when that block cannot be read.
func (a *Analyzer) state(ctx context.Context, an *analysis) {
	if an.method == nil {
		return // not a module call
	}
	m := &an.module
	if m.err != nil {
		an.add(m.title, field{"", "The target does not answer as a DeFiInteractorModule: " + oneLine(m.err)})
		return
	}
	r, module := m.read, *an.to()
	var fields []field
	add := func(key, value string) { fields = append(fields, field{key, value}) }
	if m.unread != "" {
		add(fmt.Sprintf("Block %d", an.before()), "unreadable: "+m.unread)
	}
	add("Safe", m.safe.Hex())
	// The module's own errors explain themselves with its state.
	errName := ""
	if an.revert != nil && an.revert.source == moduleError && an.raisedByModule() {
		errName = an.revert.name
	}
	// who names an address that is the sender or the Safe.
	who := func(addr common.Address) string {
		switch addr {
		case an.tx.From:
			return addr.Hex() + " (the sender)"
		case m.safe:
			return addr.Hex() + " (the Safe)"
		}
		return addr.Hex()
	}
	kind := kindOf(an.method.RawName)
	// The subaccount: the sender, or the one an oracle or owner function is
	// about.
	sub := an.tx.From
	if kind != subaccountFunction && len(an.method.Inputs) > 0 && an.method.Inputs[0].Name == "subAccount" {
		sub = an.args[0].(common.Address)
	}
	oracle, err := contracts.NewModule(module, a.b).At(r.block).AuthorizedOracle(ctx)
	oracleless := err == nil && oracle == (common.Address{})

	switch kind {
	case subaccountFunction:
		if oracleless {
			add("Oracle", "none: the module is oracleless (no spending allowance; maxSpendingUSD caps each window)")
		} else {
			add("Spending allowance", view(ctx, r, module, usd, "getSpendingAllowance", sub))
			add("Last oracle update", view(ctx, r, module, an.since, "lastOracleUpdate", sub))
		}
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
			fields = append(fields, a.spending(ctx, r, module, m.safe, sub, *an.inner.spends)...)
			if oracleless { // checked against what is left of the window's cap
				fields = append(fields, a.windowLeft(ctx, an, r, module, sub)...)
			}
		}
	case oracleFunction:
		switch {
		case err != nil:
			add("Authorized oracle", "unavailable: "+oneLine(err))
		case oracleless:
			add("Authorized oracle", "none: the module is oracleless")
		default:
			add("Authorized oracle", who(oracle))
		}
	case ownerFunction:
		add("Owner", view(ctx, r, module, who, "owner"))
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
		if !oracleless { // the window's Safe value is unused without an oracle
			add("Window Safe value", view(ctx, r, module, usd, "windowSafeValue", sub))
		}
		add("Spent in the window", view(ctx, r, module, usd, "cumulativeSpent", sub))
		add("Subaccount limits", a.limits(ctx, module, r.block, sub))
	case "ExceedsOracleAcquiredBudget":
		// The budget is maxOracleAcquiredBps of the Safe value at the start
		// of the spending window, or of the current one before any spending.
		add("Grant window start", view(ctx, r, module, an.since, "acquiredGrantWindowStart", sub))
		add("Granted in the window", view(ctx, r, module, usd, "cumulativeOracleGrantedUSD", sub))
		switch v, err := r.number(ctx, module, "windowSafeValue", sub); {
		case err != nil:
			add("Window Safe value", "unavailable: "+oneLine(err))
		case v.Sign() == 0:
			add("Window Safe value", "0 USD: no spending window yet, so the budget is based on the Safe value")
			if sv, err := contracts.NewModule(module, a.b).At(r.block).GetSafeValue(ctx); err == nil {
				add("Safe value", usd(sv.TotalValueUSD))
			} else {
				add("Safe value", "unavailable: "+oneLine(err))
			}
		default:
			add("Window Safe value", usd(v))
		}
		add("Max oracle acquired budget", view(ctx, r, module, bps, "maxOracleAcquiredBps"))
	case "EnforcedPause":
		add("Paused", view(ctx, r, module, yesNo, "paused"))
	}
	an.add(m.title, fields...)
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

// windowLeft shows what is left of an oracleless subaccount's spending cap
// in its window, which the module checks approvals against.
func (a *Analyzer) windowLeft(ctx context.Context, an *analysis, r reader, module, sub common.Address) []field {
	start, err := r.number(ctx, module, "windowStart", sub)
	if err != nil {
		return []field{{"Window start", "unavailable: " + oneLine(err)}}
	}
	spent, err := r.number(ctx, module, "cumulativeSpent", sub)
	if err != nil {
		return []field{{"Spent in the window", "unavailable: " + oneLine(err)}}
	}
	l, err := contracts.NewModule(module, a.b).At(r.block).GetSubAccountLimits(ctx, sub)
	if err != nil {
		return []field{{"Subaccount limits", "unavailable: " + oneLine(err)}}
	}
	fields := []field{
		{"Window start", an.since(start)},
		{"Spent in the window", usd(spent)},
		{"Subaccount limits", formatLimits(l)},
	}
	// As the module does: a window that ended before the block is unspent.
	end := new(big.Int).Add(start, l.WindowDuration)
	if start.Sign() != 0 && !an.time.IsZero() && big.NewInt(an.time.Unix()).Cmp(end) > 0 {
		spent = new(big.Int)
		fields[1].value += " (the window had ended: nothing is spent in the next one)"
	}
	left := new(big.Int)
	if l.MaxSpendingUSD.Cmp(spent) > 0 {
		left.Sub(l.MaxSpendingUSD, spent)
	}
	return append(fields, field{"Left in the window", usd(left)})
}

// maxTime is the last second time.Time formats with a 4-digit year.
var maxTime = time.Date(9999, 12, 31, 23, 59, 59, 0, time.UTC).Unix()

// since shows a timestamp and how long before the transaction's block it
// was ("never" for 0).
func (an *analysis) since(ts *big.Int) string {
	if ts.Sign() == 0 {
		return "never"
	}
	if !ts.IsInt64() || ts.Int64() > maxTime {
		return ts.String()
	}
	s := time.Unix(ts.Int64(), 0).UTC().Format(time.RFC3339)
	if !an.time.IsZero() {
		if d := an.time.Unix() - ts.Int64(); d >= 0 {
			s += fmt.Sprintf(" (%s before the block)", duration(d))
		} else {
			s += fmt.Sprintf(" (%s after the block)", duration(-d))
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
	return formatLimits(l)
}

func formatLimits(l contracts.SubAccountLimits) string {
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
	return duration(v.Int64())
}

// duration shows a number of seconds as a time.Duration, or in seconds
// beyond its range (about 292 years).
func duration(s int64) string {
	if s > math.MaxInt64/int64(time.Second) || s < math.MinInt64/int64(time.Second) {
		return strconv.FormatInt(s, 10) + "s"
	}
	return (time.Duration(s) * time.Second).String()
}
