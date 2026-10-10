// Package spending is the Go port of the TS spending oracle.
//
// The pure core (this file, fifo.go, state.go) rebuilds a subaccount's
// rolling-window spending and acquired balances from ProtocolExecution and
// TransferExecuted events. It is a 1:1 port of buildSubAccountState and its
// FIFO helpers in the TS oracle's spending-oracle.ts, including their quirks:
// every value it produces can end up on-chain via batchUpdate.
//
// Amounts are *big.Int and are treated as immutable (see package bn): stored
// values are replaced, never mutated in place. Timestamps, block numbers and
// log indexes are int64; callers must keep them (and the window duration)
// below 2^62 in magnitude so differences cannot overflow.
package spending

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
)

// TokenPriceInfo is a token's USD price (18 decimals) and its decimals.
type TokenPriceInfo struct {
	PriceUSD *big.Int
	Decimals uint8
}

// TokenPriceCache maps tokens to prices. A nil cache means "no cache"
// (TS: undefined); a non-nil empty cache is "a cache without prices".
type TokenPriceCache map[common.Address]TokenPriceInfo

// ProtocolExecutionEvent is a ProtocolExecution log with its block timestamp.
type ProtocolExecutionEvent struct {
	SubAccount   common.Address
	Target       common.Address
	OpType       contracts.OperationType
	TokensIn     []common.Address
	AmountsIn    []*big.Int
	TokensOut    []common.Address
	AmountsOut   []*big.Int
	SpendingCost *big.Int
	Timestamp    int64
	BlockNumber  int64
	LogIndex     int64
}

// TransferExecutedEvent is a TransferExecuted log with its block timestamp.
type TransferExecutedEvent struct {
	SubAccount   common.Address
	Token        common.Address
	Recipient    common.Address
	Amount       *big.Int
	SpendingCost *big.Int
	Timestamp    int64
	BlockNumber  int64
	LogIndex     int64
}

// DepositRecord links a deposit's input token to its output (receipt) token
// so later withdrawals can be matched to it.
type DepositRecord struct {
	SubAccount common.Address
	Target     common.Address
	TokenIn    common.Address
	AmountIn   *big.Int
	// RemainingAmount is how much of the deposit hasn't been withdrawn yet.
	RemainingAmount *big.Int
	// TokenOut is only meaningful when HasTokenOut is set. The TS used the
	// string "0x" for "no output token", which is distinct from the zero
	// address (a deposit can legitimately output 0x000...0).
	TokenOut    common.Address
	HasTokenOut bool
	AmountOut   *big.Int
	// RemainingOutputAmount is how much output hasn't been consumed by
	// withdrawals.
	RemainingOutputAmount *big.Int
	Timestamp             int64
	// OriginalAcquisitionTimestamp is inherited by withdrawn tokens.
	OriginalAcquisitionTimestamp int64
}

// AcquiredEntry is one FIFO queue entry: an amount and the timestamp at which
// it was originally acquired (inherited through swaps and deposits).
type AcquiredEntry struct {
	Amount            *big.Int
	OriginalTimestamp int64
}

// Queue is a token's FIFO queue of acquired amounts. It is not necessarily
// sorted by timestamp (inherited entries are appended after newer ones).
type Queue []AcquiredEntry

// SpendingRecord is one in-window spending charge.
type SpendingRecord struct {
	Amount    *big.Int
	Timestamp int64
}

// SubAccountState is the result of BuildSubAccountState.
type SubAccountState struct {
	SpendingRecords       []SpendingRecord
	DepositRecords        []*DepositRecord
	TotalSpendingInWindow *big.Int
	// AcquiredQueues holds a queue (possibly empty) for every token touched,
	// in first-touch order.
	AcquiredQueues *OrderedMap[common.Address, *Queue]
	// AcquiredBalances holds the non-expired acquired balance of every token
	// with a positive balance, in first-touch order (this order becomes the
	// token order of batchUpdate).
	AcquiredBalances *OrderedMap[common.Address, *big.Int]
}
