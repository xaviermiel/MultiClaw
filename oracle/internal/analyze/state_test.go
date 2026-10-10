package analyze

import (
	"context"
	"math/big"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
)

// kindOf must say who may call each state-changing module function as the
// contract's modifiers do (onlyOwner, onlyOracle; the subaccount functions
// check roles in their bodies).
func TestFunctionKindsMatchTheSolidity(t *testing.T) {
	modifiers := map[string]string{}
	comment := regexp.MustCompile(`(?s)/\*.*?\*/|//[^\n]*`)
	header := regexp.MustCompile(`(?s)function\s+(\w+)\s*\([^)]*\)([^{;]*)[{;]`)
	for _, path := range []string{"../../../src/DeFiInteractorModule.sol", "../../../src/base/Module.sol"} {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("contract sources not found: %v", err)
		}
		for _, m := range header.FindAllSubmatch(comment.ReplaceAll(src, nil), -1) {
			modifiers[string(m[1])] += string(m[2])
		}
	}
	only := regexp.MustCompile(`\bonly(Owner|Oracle)\b`)
	checked := 0
	for name, m := range contracts.ModuleABI.Methods {
		if m.IsConstant() {
			continue
		}
		mods, ok := modifiers[name]
		if !ok {
			t.Errorf("%s: not found in the sources", name)
			continue
		}
		want := subaccountFunction
		switch only.FindString(mods) {
		case "onlyOwner":
			want = ownerFunction
		case "onlyOracle":
			want = oracleFunction
		default:
			if name == "initialize" {
				want = anyoneFunction
			}
		}
		if got := kindOf(name); got != want {
			t.Errorf("%s: kind %d, want %d (modifiers %q)", name, got, want, mods)
		}
		checked++
	}
	if checked < 25 {
		t.Fatalf("only %d functions checked", checked)
	}
}

func TestDurations(t *testing.T) {
	for _, tc := range []struct {
		seconds *big.Int
		want    string
	}{
		{big.NewInt(3600), "1h0m0s"},
		{big.NewInt(9_223_372_036), "2562047h47m16s"}, // time.Duration's last second
		{big.NewInt(9_223_372_037), "9223372037s"},
		{new(big.Int).Lsh(big.NewInt(1), 255), new(big.Int).Lsh(big.NewInt(1), 255).String() + "s"},
	} {
		if got := seconds(tc.seconds); got != tc.want {
			t.Errorf("seconds(%s) = %q, want %q", tc.seconds, got, tc.want)
		}
	}

	an := &analysis{time: time.Unix(int64(blockTime), 0)}
	for _, tc := range []struct {
		ts   *big.Int
		want string
	}{
		{big.NewInt(0), "never"},
		{new(big.Int).SetUint64(blockTime - 90), "2025-10-09T23:34:35Z (1m30s before the block)"},
		{big.NewInt(maxTime), "9999-12-31T23:59:59Z (251642247834s after the block)"}, // beyond time.Duration
		{big.NewInt(maxTime + 1), "253402300800"},
	} {
		if got := an.since(tc.ts); got != tc.want {
			t.Errorf("since(%s) = %q, want %q", tc.ts, got, tc.want)
		}
	}
	if got := an.deadline(big.NewInt(1)); got != "1970-01-01T00:00:01Z (passed 488903h36m4s before the block)" {
		t.Errorf("deadline(1) = %q", got)
	}
	if got := an.deadline(big.NewInt(1e12)); got != "1000000000000" {
		t.Errorf("deadline(1e12) = %q", got)
	}
}

// An oracleless subaccount's approval is checked against what is left of
// its window's cap, all of it once the window has ended.
func TestWindowLeft(t *testing.T) {
	n := newFakeNode()
	m := &contracts.ModuleABI
	n.Return(module, m, "cumulativeSpent", e18(80))
	n.Return(module, m, "getSubAccountLimits", big.NewInt(0), e18(100), big.NewInt(86400))
	an := &analysis{time: time.Unix(int64(blockTime), 0)}
	a := New(n, chainFor(t), Options{})
	for _, tc := range []struct {
		start uint64
		left  string
	}{
		{blockTime - 3600, "20 USD"},
		{blockTime - 86400, "20 USD"},  // ends at the block: still the window
		{blockTime - 86401, "100 USD"}, // ended: nothing spent in the next one
		{0, "20 USD"},
	} {
		n.Return(module, m, "windowStart", new(big.Int).SetUint64(tc.start))
		fields := a.windowLeft(context.Background(), an, reader{n, nil}, module, sub)
		if got := fields[len(fields)-1]; got.key != "Left in the window" || got.value != tc.left {
			t.Errorf("window from %d: %v", tc.start, fields)
		}
	}
}

// A contract answering avatar() is taken to be a module, read now when the
// node cannot read the block before.
func TestAnswersAsModule(t *testing.T) {
	n := newFakeNode()
	installModule(n)
	n.noHistory = true
	an := &analysis{block: block, tx: rpcTx{To: &safe}}
	a := New(n, chainFor(t), Options{})
	if !a.answersAsModule(context.Background(), an, module) || a.answersAsModule(context.Background(), an, common.Address{1}) {
		t.Fatal("wrong answer")
	}
}

func TestEVMErrors(t *testing.T) {
	for msg, want := range map[string]help{
		"out of gas":                        outOfGasHelp,
		"insufficient balance for transfer": insufficientETHHelp,
		"invalid opcode: INVALID":           evmHelp,
	} {
		if r := evmError(msg); r.title() != msg || r.help != want || r.source != evmFailure {
			t.Errorf("%s: %q %v", msg, r.title(), r.help)
		}
	}
}
