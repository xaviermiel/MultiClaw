package spendingoracle

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
	"github.com/xaviermiel/MultiClaw/oracle/internal/config"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts/contractstest"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
)

const (
	now          = 1_700_000_000
	headBlock    = 1000
	blockSeconds = 2
)

var (
	module   = common.HexToAddress("0x4d0D0000000000000000000000000000000000e1")
	registry = common.HexToAddress("0x5e90000000000000000000000000000000000001")
	oracle   = common.HexToAddress("0x763072E0FDa74Eecab3e60BF5BC5b8A46866be7E")
	subA     = common.HexToAddress("0xAAaa0000000000000000000000000000000000a1")
	subB     = common.HexToAddress("0xbbBB0000000000000000000000000000000000b2")
	subC     = common.HexToAddress("0xcCCc0000000000000000000000000000000000c3")
	target   = common.HexToAddress("0x7a76000000000000000000000000000000000001")
	tokenX   = common.HexToAddress("0x1111111111111111111111111111111111111111")
	tokenY   = common.HexToAddress("0x2222222222222222222222222222222222222222")
	tokenZ   = common.HexToAddress("0x3333333333333333333333333333333333333333")
)

func e18(v int64) *big.Int { return bn.Mul(big.NewInt(v), bn.Precision) }

// blockTime is the fake chain's timestamp of block n: the head is `now`.
func blockTime(n uint64) uint64 { return uint64(now) - (headBlock-n)*blockSeconds }

// fakeChain is an in-memory chain: eth_call through contractstest.Fake, plus
// a head, block infos and logs.
type fakeChain struct {
	*contractstest.Fake

	mu         sync.Mutex
	head       uint64
	hashSalt   map[uint64]string // changes a block's hash (a reorg)
	logs       []types.Log
	nextIndex  uint
	queries    []ethereum.FilterQuery
	failLogs   func(q ethereum.FilterQuery) error
	blockReads int
}

func newFakeChain() *fakeChain {
	return &fakeChain{Fake: contractstest.New(), head: headBlock, hashSalt: map[uint64]string{}}
}

func (c *fakeChain) BlockNumber(context.Context) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.head, nil
}

func (c *fakeChain) BlockInfo(_ context.Context, n uint64) (ethx.BlockInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.blockReads++
	if n > c.head {
		return ethx.BlockInfo{}, fmt.Errorf("block %d not found", n)
	}
	hash := crypto.Keccak256Hash([]byte(fmt.Sprintf("block %d %s", n, c.hashSalt[n])))
	return ethx.BlockInfo{Number: n, Hash: hash, Timestamp: blockTime(n)}, nil
}

func (c *fakeChain) FilterLogs(_ context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.queries = append(c.queries, q)
	if c.failLogs != nil {
		if err := c.failLogs(q); err != nil {
			return nil, err
		}
	}
	var out []types.Log
	for _, l := range c.logs {
		if l.BlockNumber < q.FromBlock.Uint64() || l.BlockNumber > q.ToBlock.Uint64() {
			continue
		}
		if len(q.Addresses) > 0 && !slices.Contains(q.Addresses, l.Address) {
			continue
		}
		match := true
		for i, want := range q.Topics {
			if len(want) > 0 && (i >= len(l.Topics) || !slices.Contains(want, l.Topics[i])) {
				match = false
				break
			}
		}
		if match {
			out = append(out, l)
		}
	}
	// In chain order, as nodes return them.
	slices.SortStableFunc(out, func(a, b types.Log) int {
		return cmp.Or(cmp.Compare(a.BlockNumber, b.BlockNumber), cmp.Compare(a.Index, b.Index))
	})
	return out, nil
}

func (c *fakeChain) addLog(l types.Log) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextIndex++
	l.Index = c.nextIndex // unique, increasing within each block
	c.logs = append(c.logs, l)
}

// removeLogs drops the logs drop matches (blocks replaced by a reorg).
func (c *fakeChain) removeLogs(drop func(types.Log) bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.logs = slices.DeleteFunc(c.logs, drop)
}

func (c *fakeChain) setHead(n uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.head = n
}

func (c *fakeChain) reorg(blocks ...uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, n := range blocks {
		c.hashSalt[n] = "reorged"
	}
}

func eventData(t *testing.T, name string, vals ...any) []byte {
	t.Helper()
	data, err := contracts.ModuleABI.Events[name].Inputs.NonIndexed().Pack(vals...)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func topic(a common.Address) common.Hash { return contracts.AddressTopic(a) }

func protocolLog(t *testing.T, sub common.Address, op contracts.OperationType, in []common.Address, amountsIn []*big.Int,
	out []common.Address, amountsOut []*big.Int, cost *big.Int, block uint64) types.Log {
	return types.Log{
		Address:     module,
		Topics:      []common.Hash{contracts.ProtocolExecutionTopic, topic(sub), topic(target)},
		Data:        eventData(t, "ProtocolExecution", uint8(op), in, amountsIn, out, amountsOut, cost),
		BlockNumber: block,
	}
}

func transferLog(t *testing.T, sub, token common.Address, amount, cost *big.Int, block uint64) types.Log {
	return types.Log{
		Address:     module,
		Topics:      []common.Hash{contracts.TransferExecutedTopic, topic(sub), topic(token), topic(target)},
		Data:        eventData(t, "TransferExecuted", amount, cost),
		BlockNumber: block,
	}
}

func limitsSetLog(t *testing.T, sub common.Address, block uint64) types.Log {
	return types.Log{
		Address:     module,
		Topics:      []common.Hash{contracts.SubAccountLimitsSetTopic, topic(sub)},
		Data:        eventData(t, "SubAccountLimitsSet", big.NewInt(500), big.NewInt(0), big.NewInt(86400)),
		BlockNumber: block,
	}
}

func acquiredLog(t *testing.T, sub, token common.Address, balance *big.Int, block uint64) types.Log {
	return types.Log{
		Address:     module,
		Topics:      []common.Hash{contracts.AcquiredBalanceUpdatedTopic, topic(sub), topic(token)},
		Data:        eventData(t, "AcquiredBalanceUpdated", balance),
		BlockNumber: block,
	}
}

// moduleState is the on-chain state of the test module.
type moduleState struct {
	mu               sync.Mutex
	safeValue        *big.Int
	limits           [3]*big.Int // maxSpendingBps, maxSpendingUSD, windowDuration
	roles            map[uint16][]common.Address
	allowance        map[common.Address]*big.Int
	acquired         map[[2]common.Address]*big.Int
	absoluteMaxBps   *big.Int // nil: the getter doesn't exist (new implementation)
	allowanceVersion int64
}

func newModuleState() *moduleState {
	return &moduleState{
		safeValue: e18(10_000),
		limits:    [3]*big.Int{big.NewInt(1000), big.NewInt(0), big.NewInt(86400)}, // 10% per day
		roles:     map[uint16][]common.Address{},
		allowance: map[common.Address]*big.Int{},
		acquired:  map[[2]common.Address]*big.Int{},
	}
}

// install registers the module's getters on the chain.
func (m *moduleState) install(c *fakeChain, at common.Address) {
	abi := &contracts.ModuleABI
	get := func(f func() []any) contractstest.Handler {
		return func([]any) ([]any, error) {
			m.mu.Lock()
			defer m.mu.Unlock()
			return f(), nil
		}
	}
	c.Return(at, abi, "authorizedOracle", oracle)
	c.On(at, abi, "getSafeValue", get(func() []any { return []any{m.safeValue, big.NewInt(now - 60), big.NewInt(1)} }))
	c.On(at, abi, "getSubAccountLimits", get(func() []any { return []any{m.limits[0], m.limits[1], m.limits[2]} }))
	c.On(at, abi, "getSubaccountsByRole", func(args []any) ([]any, error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		accounts := m.roles[args[0].(uint16)]
		if accounts == nil {
			accounts = []common.Address{}
		}
		return []any{accounts}, nil
	})
	c.On(at, abi, "getSpendingAllowance", func(args []any) ([]any, error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if v, ok := m.allowance[args[0].(common.Address)]; ok {
			return []any{v}, nil
		}
		return []any{big.NewInt(0)}, nil
	})
	c.On(at, abi, "allowanceVersion", get(func() []any { return []any{big.NewInt(m.allowanceVersion)} }))
	c.On(at, abi, "getAcquiredBalance", func(args []any) ([]any, error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if v, ok := m.acquired[[2]common.Address{args[0].(common.Address), args[1].(common.Address)}]; ok {
			return []any{v}, nil
		}
		return []any{big.NewInt(0)}, nil
	})
	c.On(at, abi, "acquiredBalanceVersion", func(args []any) ([]any, error) {
		// Distinct per token, to check the versions follow their tokens.
		return []any{new(big.Int).SetBytes(args[1].(common.Address).Bytes()[19:])}, nil
	})
	c.On(at, &contracts.LegacyModuleABI, "absoluteMaxSpendingBps", func([]any) ([]any, error) {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.absoluteMaxBps == nil {
			return nil, &contractstest.RevertError{}
		}
		return []any{m.absoluteMaxBps}, nil
	})
}

// sentBatch is a decoded batchUpdate.
type sentBatch struct {
	module, subAccount common.Address
	allowanceVersion   *big.Int
	newAllowance       *big.Int
	tokens             []common.Address
	tokenVersions      []*big.Int
	balances           []*big.Int
}

// fakeSender decodes the batchUpdates it is given; receipts succeed unless
// revert says the batch reverts, or waitErr fails the wait (e.g. a receipt
// timeout). onWait runs before each wait.
type fakeSender struct {
	t       *testing.T
	mu      sync.Mutex
	batches []sentBatch
	waited  int
	revert  func(sentBatch) bool
	waitErr error
	onWait  func()
}

func (f *fakeSender) Address() common.Address { return oracle }

func (f *fakeSender) Send(_ context.Context, to common.Address, data []byte) (*ethx.SentTx, error) {
	vals, err := contracts.ModuleABI.Methods["batchUpdate"].Inputs.Unpack(data[4:])
	if err != nil {
		f.t.Fatalf("not a batchUpdate: %v", err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batches = append(f.batches, sentBatch{
		module: to, subAccount: vals[0].(common.Address), allowanceVersion: vals[1].(*big.Int),
		newAllowance: vals[2].(*big.Int), tokens: vals[3].([]common.Address),
		tokenVersions: vals[4].([]*big.Int), balances: vals[5].([]*big.Int),
	})
	n := len(f.batches)
	return &ethx.SentTx{Hash: common.BigToHash(big.NewInt(int64(0xabc0000 + n))), From: oracle, To: to, Data: data, Nonce: uint64(n)}, nil
}

func (f *fakeSender) Wait(_ context.Context, tx *ethx.SentTx) (*types.Receipt, error) {
	if f.onWait != nil {
		f.onWait()
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.waited++
	if f.waitErr != nil {
		return nil, f.waitErr
	}
	b := f.batches[tx.Nonce-1]
	status := types.ReceiptStatusSuccessful
	if f.revert != nil && f.revert(b) {
		status = types.ReceiptStatusFailed
	}
	return &types.Receipt{Status: status, BlockNumber: big.NewInt(headBlock + 1), GasUsed: 50_000}, nil
}

func (f *fakeSender) sent() []sentBatch {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.batches)
}

func (f *fakeSender) subAccounts() []common.Address {
	var out []common.Address
	for _, b := range f.sent() {
		out = append(out, b.subAccount)
	}
	return out
}

// logs collects log lines (from several goroutines).
type logs struct {
	mu    sync.Mutex
	lines []string
}

func (l *logs) logf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logs) has(s string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.ContainsFunc(l.lines, func(line string) bool { return strings.Contains(line, s) })
}

// index returns the position of the first line containing s, or -1.
func (l *logs) index(s string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.IndexFunc(l.lines, func(line string) bool { return strings.Contains(line, s) })
}

func (l *logs) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = nil
}

func (l *logs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

type testEnv struct {
	t      *testing.T
	chain  *fakeChain
	module *moduleState
	sender *fakeSender
	logs   *logs
	svc    *Service
}

// newTestEnv is a single-module oracle (MODULE_ADDRESS) on a fresh chain.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	env := &testEnv{t: t, chain: newFakeChain(), module: newModuleState(), sender: &fakeSender{t: t}, logs: &logs{}}
	env.module.install(env.chain, module)
	m := module
	cfg := &config.Config{
		Chain:                             config.Chain{},
		ModuleAddress:                     &m,
		PollInterval:                      10 * time.Millisecond,
		BlocksToLookBack:                  100,
		ConfirmationBlocks:                2,
		MaxBlocksPerQuery:                 50,
		MaxHistoricalBlocks:               500,
		AllowanceIncreaseThresholdBps:     big.NewInt(200),
		SpendingOracleMaxStalenessSeconds: 2700,
	}
	env.svc = New(cfg, env.chain, env.sender, env.logs.logf)
	env.svc.Now = func() time.Time { return time.Unix(now, 0) }
	return env
}

func (env *testEnv) wantLogs(want ...string) {
	env.t.Helper()
	for _, w := range want {
		if !env.logs.has(w) {
			env.t.Errorf("logs lack %q:\n%s", w, env.logs)
		}
	}
}

var errRPC = errors.New("rpc: rate limited")
