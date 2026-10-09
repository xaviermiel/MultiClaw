package spending

// Golden parity with the TS implementation: scripts/golden/gen.ts ran every
// case in testdata/golden through the real buildSubAccountState and recorded
// the state it produced (the script header documents the format). The Go
// port must produce the identical state, or panic where the TS threw.
//
// `make golden` regenerates the vectors; `make golden-check` verifies that
// the committed ones are what the current TS produces.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
)

type goldenFile struct {
	Family    string       `json:"family"`
	Addresses []string     `json:"addresses"`
	Cases     []goldenCase `json:"cases"`
}

type goldenCase struct {
	Name  string `json:"name"`
	Input struct {
		SubAccount       int                `json:"subAccount"`
		CurrentTimestamp int64              `json:"currentTimestamp"`
		WindowDuration   int64              `json:"windowDuration"`
		PriceCache       *[]json.RawMessage `json:"priceCache"` // nil: no cache
		Events           []json.RawMessage  `json:"events"`
		Transfers        []json.RawMessage  `json:"transfers"`
	} `json:"input"`
	Want   json.RawMessage `json:"want"`
	Throws string          `json:"throws"`
}

// goldenAddresses resolves the vectors' address indexes.
type goldenAddresses struct {
	byIndex []common.Address
	// canonical maps an address to the index outputs use for it: the index
	// of its first (lowercase) spelling.
	canonical map[common.Address]int
}

func newGoldenAddresses(spellings []string) (*goldenAddresses, error) {
	g := &goldenAddresses{canonical: map[common.Address]int{}}
	for i, s := range spellings {
		if !common.IsHexAddress(s) {
			return nil, fmt.Errorf("invalid address %q", s)
		}
		a := common.HexToAddress(s)
		g.byIndex = append(g.byIndex, a)
		if _, ok := g.canonical[a]; !ok {
			g.canonical[a] = i
		}
	}
	return g, nil
}

func (g *goldenAddresses) at(i int) (common.Address, error) {
	if i < 0 || i >= len(g.byIndex) {
		return common.Address{}, fmt.Errorf("address index %d out of range", i)
	}
	return g.byIndex[i], nil
}

func (g *goldenAddresses) list(idx []int) ([]common.Address, error) {
	out := make([]common.Address, len(idx))
	for i, x := range idx {
		a, err := g.at(x)
		if err != nil {
			return nil, err
		}
		out[i] = a
	}
	return out, nil
}

// ref is how outputs name an address; -1 (never a valid index) for an
// address the vectors don't know.
func (g *goldenAddresses) ref(a common.Address) any {
	if i, ok := g.canonical[a]; ok {
		return i
	}
	return -1
}

// tuple unmarshals a JSON array into fields, element by element.
func tuple(raw json.RawMessage, fields ...any) error {
	var elems []json.RawMessage
	if err := json.Unmarshal(raw, &elems); err != nil {
		return err
	}
	if len(elems) != len(fields) {
		return fmt.Errorf("%s: want %d elements, got %d", raw, len(fields), len(elems))
	}
	for i, f := range fields {
		if err := json.Unmarshal(elems[i], f); err != nil {
			return fmt.Errorf("%s: element %d: %w", raw, i, err)
		}
	}
	return nil
}

func parseAmounts(xs []string) ([]*big.Int, error) {
	out := make([]*big.Int, len(xs))
	for i, x := range xs {
		v, err := bn.Parse(x)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

type goldenInput struct {
	events    []ProtocolExecutionEvent
	transfers []TransferExecutedEvent
	sub       common.Address
	now       int64
	window    int64
	cache     TokenPriceCache
}

func decodeGoldenInput(g *goldenAddresses, c *goldenCase) (*goldenInput, error) {
	in := &goldenInput{now: c.Input.CurrentTimestamp, window: c.Input.WindowDuration}
	var err error
	if in.sub, err = g.at(c.Input.SubAccount); err != nil {
		return nil, err
	}
	if c.Input.PriceCache != nil {
		in.cache = TokenPriceCache{}
		for _, raw := range *c.Input.PriceCache {
			var (
				token    int
				price    string
				decimals uint8
			)
			if err := tuple(raw, &token, &price, &decimals); err != nil {
				return nil, err
			}
			a, err := g.at(token)
			if err != nil {
				return nil, err
			}
			p, err := bn.Parse(price)
			if err != nil {
				return nil, err
			}
			in.cache[a] = TokenPriceInfo{PriceUSD: p, Decimals: decimals}
		}
	}
	for _, raw := range c.Input.Events {
		var (
			sub, target, op       int
			tokensIn, tokensOut   []int
			amountsIn, amountsOut []string
			cost                  string
			ev                    ProtocolExecutionEvent
		)
		if err := tuple(raw, &sub, &target, &op, &tokensIn, &amountsIn, &tokensOut, &amountsOut,
			&cost, &ev.Timestamp, &ev.BlockNumber, &ev.LogIndex); err != nil {
			return nil, err
		}
		ev.OpType = contracts.OperationType(op)
		if ev.SubAccount, err = g.at(sub); err != nil {
			return nil, err
		}
		if ev.Target, err = g.at(target); err != nil {
			return nil, err
		}
		if ev.TokensIn, err = g.list(tokensIn); err != nil {
			return nil, err
		}
		if ev.TokensOut, err = g.list(tokensOut); err != nil {
			return nil, err
		}
		if ev.AmountsIn, err = parseAmounts(amountsIn); err != nil {
			return nil, err
		}
		if ev.AmountsOut, err = parseAmounts(amountsOut); err != nil {
			return nil, err
		}
		if ev.SpendingCost, err = bn.Parse(cost); err != nil {
			return nil, err
		}
		in.events = append(in.events, ev)
	}
	for _, raw := range c.Input.Transfers {
		var (
			sub, token, recipient int
			amount, cost          string
			tr                    TransferExecutedEvent
		)
		if err := tuple(raw, &sub, &token, &recipient, &amount, &cost,
			&tr.Timestamp, &tr.BlockNumber, &tr.LogIndex); err != nil {
			return nil, err
		}
		if tr.SubAccount, err = g.at(sub); err != nil {
			return nil, err
		}
		if tr.Token, err = g.at(token); err != nil {
			return nil, err
		}
		if tr.Recipient, err = g.at(recipient); err != nil {
			return nil, err
		}
		if tr.Amount, err = bn.Parse(amount); err != nil {
			return nil, err
		}
		if tr.SpendingCost, err = bn.Parse(cost); err != nil {
			return nil, err
		}
		in.transfers = append(in.transfers, tr)
	}
	return in, nil
}

// renderState lays a state out like the vectors' "want" (see gen.ts),
// naming addresses with ref.
func renderState(s *SubAccountState, ref func(common.Address) any) map[string]any {
	spending := []any{}
	for _, r := range s.SpendingRecords {
		spending = append(spending, []any{r.Amount.String(), r.Timestamp})
	}
	deposits := []any{}
	for _, d := range s.DepositRecords {
		var tokenOut any // null is the TS "0x": no output token
		if d.HasTokenOut {
			tokenOut = ref(d.TokenOut)
		}
		deposits = append(deposits, []any{
			ref(d.SubAccount), ref(d.Target), ref(d.TokenIn),
			d.AmountIn.String(), d.RemainingAmount.String(),
			tokenOut, d.AmountOut.String(), d.RemainingOutputAmount.String(),
			d.Timestamp, d.OriginalAcquisitionTimestamp,
		})
	}
	queues := []any{}
	for _, token := range s.AcquiredQueues.Keys() {
		q, _ := s.AcquiredQueues.Get(token)
		entries := []any{}
		for _, e := range *q {
			entries = append(entries, []any{e.Amount.String(), e.OriginalTimestamp})
		}
		queues = append(queues, []any{ref(token), entries})
	}
	balances := []any{}
	for _, token := range s.AcquiredBalances.Keys() {
		b, _ := s.AcquiredBalances.Get(token)
		balances = append(balances, []any{ref(token), b.String()})
	}
	return map[string]any{
		"spendingRecords":       spending,
		"totalSpendingInWindow": s.TotalSpendingInWindow.String(),
		"depositRecords":        deposits,
		"acquiredQueues":        queues,
		"acquiredBalances":      balances,
	}
}

// jsonSections canonicalizes a JSON object section by section.
func jsonSections(raw []byte) (map[string]string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(obj))
	for k, v := range obj {
		b, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		out[k] = string(b)
	}
	return out, nil
}

// sprintfLogf formats every log message, so the log calls run with real
// arguments, and drops it.
func sprintfLogf(format string, args ...any) { _ = fmt.Sprintf(format, args...) }

// buildRecovering runs BuildSubAccountState and returns what it panicked
// with, if it did.
func buildRecovering(in *goldenInput) (s *SubAccountState, panicked any) {
	defer func() { panicked = recover() }()
	return BuildSubAccountState(in.events, in.transfers, in.sub, in.now, in.window, in.cache, sprintfLogf), nil
}

func TestGoldenParity(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("testdata", "golden", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) == 0 {
		t.Fatal("no golden vectors in testdata/golden (make golden)")
	}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var gf goldenFile
		if err := json.Unmarshal(raw, &gf); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if len(gf.Cases) == 0 {
			t.Fatalf("%s has no cases", path)
		}
		addresses, err := newGoldenAddresses(gf.Addresses)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for i := range gf.Cases {
			c := &gf.Cases[i]
			t.Run(c.Name, func(t *testing.T) { checkGoldenCase(t, addresses, c) })
		}
	}
}

func checkGoldenCase(t *testing.T, addresses *goldenAddresses, c *goldenCase) {
	in, err := decodeGoldenInput(addresses, c)
	if err != nil {
		t.Fatalf("decoding input: %v", err)
	}
	state, panicked := buildRecovering(in)
	if c.Throws != "" {
		if panicked == nil {
			t.Fatalf("TS threw %q, Go returned a state", c.Throws)
		}
		return
	}
	if panicked != nil {
		t.Fatalf("panic: %v", panicked)
	}

	want, err := jsonSections(c.Want)
	if err != nil {
		t.Fatalf("decoding want: %v", err)
	}
	rendered, err := json.Marshal(renderState(state, addresses.ref))
	if err != nil {
		t.Fatal(err)
	}
	got, err := jsonSections(rendered)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	for k := range got {
		if _, ok := want[k]; !ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		if want[k] != got[k] {
			t.Errorf("%s:\n  TS: %s\n  Go: %s", k, want[k], got[k])
		}
	}
}
