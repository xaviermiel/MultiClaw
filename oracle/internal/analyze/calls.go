package analyze

import (
	"context"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
)

// innerFields describes a protocol call, naming tokens and formatting
// amounts with their decimals.
func (a *Analyzer) innerFields(ctx context.Context, an *analysis, c *innerCall) []field {
	fields := []field{{"Target", a.label(ctx, c.target)}}
	if c.value != nil && c.value.Sign() > 0 {
		fields = append(fields, field{"Value", bn.FormatUnits(c.value, 18) + " ETH"})
	}
	if len(c.data) == 0 {
		return fields // a plain ETH transfer
	}
	if c.method == nil {
		fields = append(fields, field{"Function", fmt.Sprintf("unknown, selector %s", hexutil.Encode(c.data[:min(4, len(c.data))]))})
		return append(fields, words(c.data)...)
	}
	fields = append(fields, field{"Function", c.method.Sig})
	args := c.args
	addr := func(i int) common.Address { return args[i].(common.Address) }
	num := func(i int) *big.Int { return args[i].(*big.Int) }
	arg := func(key, value string) { fields = append(fields, field{"  " + key, value}) }

	switch c.method.RawName {
	case "approve":
		arg("spender", a.label(ctx, addr(0)))
		arg("amount", a.amount(ctx, c.target, num(1)))
	case "transfer":
		arg("to", a.label(ctx, addr(0)))
		arg("amount", a.amount(ctx, c.target, num(1)))
	case "transferFrom":
		arg("from", a.label(ctx, addr(0)))
		arg("to", a.label(ctx, addr(1)))
		arg("amount", a.amount(ctx, c.target, num(2)))
	case "supply":
		arg("asset", a.label(ctx, addr(0)))
		arg("amount", a.amount(ctx, addr(0), num(1)))
		arg("onBehalfOf", a.label(ctx, addr(2)))
	case "withdraw":
		arg("asset", a.label(ctx, addr(0)))
		arg("amount", a.amount(ctx, addr(0), num(1)))
		arg("to", a.label(ctx, addr(2)))
	case "repay":
		arg("asset", a.label(ctx, addr(0)))
		arg("amount", a.amount(ctx, addr(0), num(1)))
		arg("interestRateMode", rateMode(num(2)))
		arg("onBehalfOf", a.label(ctx, addr(3)))
	case "borrow":
		arg("asset", a.label(ctx, addr(0)))
		arg("amount", a.amount(ctx, addr(0), num(1)))
		arg("interestRateMode", rateMode(num(2)))
		arg("onBehalfOf", a.label(ctx, addr(4)))
	case "exactInputSingle", "exactOutputSingle":
		p := args[0]
		in, out := tuple[common.Address](p, "tokenIn"), tuple[common.Address](p, "tokenOut")
		arg("tokenIn", a.label(ctx, in))
		arg("tokenOut", a.label(ctx, out))
		arg("fee", feePercent(tuple[*big.Int](p, "fee")))
		arg("recipient", a.label(ctx, tuple[common.Address](p, "recipient")))
		if d := tuple[*big.Int](p, "deadline"); d != nil {
			arg("deadline", an.deadline(d))
		}
		if c.method.RawName == "exactInputSingle" {
			arg("amountIn", a.amount(ctx, in, tuple[*big.Int](p, "amountIn")))
			arg("amountOutMinimum", a.amount(ctx, out, tuple[*big.Int](p, "amountOutMinimum")))
		} else {
			arg("amountOut", a.amount(ctx, out, tuple[*big.Int](p, "amountOut")))
			arg("amountInMaximum", a.amount(ctx, in, tuple[*big.Int](p, "amountInMaximum")))
		}
	case "exactInput", "exactOutput":
		p := args[0]
		path := tuple[[]byte](p, "path")
		tokens, fees, ok := swapPath(path)
		if !ok {
			arg("path", shortBytes(path)+" (malformed)")
		} else {
			if c.method.RawName == "exactOutput" { // encoded from the output token
				slices.Reverse(tokens)
				slices.Reverse(fees)
			}
			arg("path", a.route(ctx, tokens, fees))
		}
		arg("recipient", a.label(ctx, tuple[common.Address](p, "recipient")))
		if d := tuple[*big.Int](p, "deadline"); d != nil {
			arg("deadline", an.deadline(d))
		}
		first, last := common.Address{}, common.Address{}
		if ok {
			first, last = tokens[0], tokens[len(tokens)-1]
		}
		if c.method.RawName == "exactInput" {
			arg("amountIn", a.amount(ctx, first, tuple[*big.Int](p, "amountIn")))
			arg("amountOutMinimum", a.amount(ctx, last, tuple[*big.Int](p, "amountOutMinimum")))
		} else {
			arg("amountOut", a.amount(ctx, last, tuple[*big.Int](p, "amountOut")))
			arg("amountInMaximum", a.amount(ctx, first, tuple[*big.Int](p, "amountInMaximum")))
		}
	}
	return fields
}

// route shows a swap path from the input token: "USDC -(0.05%)-> WETH".
func (a *Analyzer) route(ctx context.Context, tokens []common.Address, fees []*big.Int) string {
	var b strings.Builder
	for i, t := range tokens {
		if i > 0 {
			fmt.Fprintf(&b, " -(%s)-> ", feePercent(fees[i-1]))
		}
		if s := a.symbol(ctx, t); s != "" {
			b.WriteString(s)
		} else {
			b.WriteString(t.Hex())
		}
	}
	return b.String()
}

// deadline shows a swap deadline, and whether it had passed when the
// transaction was mined.
func (an *analysis) deadline(d *big.Int) string {
	if !d.IsInt64() {
		return d.String()
	}
	t := time.Unix(d.Int64(), 0).UTC()
	s := t.Format(time.RFC3339)
	if !an.time.IsZero() && t.Before(an.time) {
		s += fmt.Sprintf(" (passed %s before the block)", an.time.Sub(t))
	}
	return s
}

// feePercent shows a Uniswap fee in hundredths of a basis point as a
// percentage.
func feePercent(fee *big.Int) string {
	if fee == nil {
		return "?"
	}
	return bn.FormatUnits(fee, 4) + "%"
}

func rateMode(m *big.Int) string {
	switch m.Int64() {
	case 1:
		return "1 (stable)"
	case 2:
		return "2 (variable)"
	}
	return m.String()
}
