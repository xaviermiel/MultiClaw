package contracts

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ProtocolExecutionLog is a decoded ProtocolExecution event (without the block
// timestamp, which the spending oracle fetches separately).
type ProtocolExecutionLog struct {
	SubAccount   common.Address
	Target       common.Address
	OpType       OperationType
	TokensIn     []common.Address
	AmountsIn    []*big.Int
	TokensOut    []common.Address
	AmountsOut   []*big.Int
	SpendingCost *big.Int
	BlockNumber  uint64
	BlockHash    common.Hash
	LogIndex     uint
}

// TransferExecutedLog is a decoded TransferExecuted event.
type TransferExecutedLog struct {
	SubAccount   common.Address
	Token        common.Address
	Recipient    common.Address
	Amount       *big.Int
	SpendingCost *big.Int
	BlockNumber  uint64
	BlockHash    common.Hash
	LogIndex     uint
}

// AcquiredBalanceUpdatedLog is a decoded AcquiredBalanceUpdated event.
type AcquiredBalanceUpdatedLog struct {
	SubAccount  common.Address
	Token       common.Address
	NewBalance  *big.Int
	BlockNumber uint64
}

func checkTopics(l types.Log, topic0 common.Hash, n int, name string) error {
	if len(l.Topics) != n || l.Topics[0] != topic0 {
		return fmt.Errorf("log %s:%d is not a %s event", l.TxHash.Hex(), l.Index, name)
	}
	return nil
}

// DecodeProtocolExecution decodes a ProtocolExecution log. Like the TS parser
// it rejects events whose token/amount arrays have different lengths.
func DecodeProtocolExecution(l types.Log) (ProtocolExecutionLog, error) {
	if err := checkTopics(l, ProtocolExecutionTopic, 3, "ProtocolExecution"); err != nil {
		return ProtocolExecutionLog{}, err
	}
	vals, err := ModuleABI.Events["ProtocolExecution"].Inputs.NonIndexed().Unpack(l.Data)
	if err != nil {
		return ProtocolExecutionLog{}, fmt.Errorf("decode ProtocolExecution data: %w", err)
	}
	ev := ProtocolExecutionLog{
		SubAccount:   topicAddress(l.Topics[1]),
		Target:       topicAddress(l.Topics[2]),
		OpType:       OperationType(vals[0].(uint8)),
		TokensIn:     vals[1].([]common.Address),
		AmountsIn:    vals[2].([]*big.Int),
		TokensOut:    vals[3].([]common.Address),
		AmountsOut:   vals[4].([]*big.Int),
		SpendingCost: vals[5].(*big.Int),
		BlockNumber:  l.BlockNumber,
		BlockHash:    l.BlockHash,
		LogIndex:     l.Index,
	}
	if len(ev.TokensIn) != len(ev.AmountsIn) {
		return ProtocolExecutionLog{}, fmt.Errorf("malformed event: tokensIn.length (%d) !== amountsIn.length (%d)", len(ev.TokensIn), len(ev.AmountsIn))
	}
	if len(ev.TokensOut) != len(ev.AmountsOut) {
		return ProtocolExecutionLog{}, fmt.Errorf("malformed event: tokensOut.length (%d) !== amountsOut.length (%d)", len(ev.TokensOut), len(ev.AmountsOut))
	}
	return ev, nil
}

// DecodeTransferExecuted decodes a TransferExecuted log.
func DecodeTransferExecuted(l types.Log) (TransferExecutedLog, error) {
	if err := checkTopics(l, TransferExecutedTopic, 4, "TransferExecuted"); err != nil {
		return TransferExecutedLog{}, err
	}
	vals, err := ModuleABI.Events["TransferExecuted"].Inputs.NonIndexed().Unpack(l.Data)
	if err != nil {
		return TransferExecutedLog{}, fmt.Errorf("decode TransferExecuted data: %w", err)
	}
	return TransferExecutedLog{
		SubAccount:   topicAddress(l.Topics[1]),
		Token:        topicAddress(l.Topics[2]),
		Recipient:    topicAddress(l.Topics[3]),
		Amount:       vals[0].(*big.Int),
		SpendingCost: vals[1].(*big.Int),
		BlockNumber:  l.BlockNumber,
		BlockHash:    l.BlockHash,
		LogIndex:     l.Index,
	}, nil
}

// DecodeAcquiredBalanceUpdated decodes an AcquiredBalanceUpdated log.
func DecodeAcquiredBalanceUpdated(l types.Log) (AcquiredBalanceUpdatedLog, error) {
	if err := checkTopics(l, AcquiredBalanceUpdatedTopic, 3, "AcquiredBalanceUpdated"); err != nil {
		return AcquiredBalanceUpdatedLog{}, err
	}
	vals, err := ModuleABI.Events["AcquiredBalanceUpdated"].Inputs.NonIndexed().Unpack(l.Data)
	if err != nil {
		return AcquiredBalanceUpdatedLog{}, fmt.Errorf("decode AcquiredBalanceUpdated data: %w", err)
	}
	return AcquiredBalanceUpdatedLog{
		SubAccount:  topicAddress(l.Topics[1]),
		Token:       topicAddress(l.Topics[2]),
		NewBalance:  vals[0].(*big.Int),
		BlockNumber: l.BlockNumber,
	}, nil
}

// SubAccountFromLimitsSet returns the subaccount of a SubAccountLimitsSet log
// (the spending oracle only needs to know which subaccount changed).
func SubAccountFromLimitsSet(l types.Log) (common.Address, error) {
	if err := checkTopics(l, SubAccountLimitsSetTopic, 2, "SubAccountLimitsSet"); err != nil {
		return common.Address{}, err
	}
	return topicAddress(l.Topics[1]), nil
}

// AddressTopic encodes an address as an indexed-topic filter value.
func AddressTopic(a common.Address) common.Hash {
	return common.BytesToHash(a.Bytes())
}
