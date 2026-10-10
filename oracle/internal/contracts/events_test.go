package contracts

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	evSub    = common.HexToAddress("0x4573A0C9428e56B0d4EEfcb43DCd99299BBD2e6c")
	evTarget = common.HexToAddress("0xaAaAaAaaAaAaAaaAaAAAAAAAAaaaAaAaAaaAaaAa")
	evTokA   = common.HexToAddress("0x1111111111111111111111111111111111111111")
	evTokB   = common.HexToAddress("0x2222222222222222222222222222222222222222")
	evRecip  = common.HexToAddress("0x5555555555555555555555555555555555555555")
)

func packEventData(t *testing.T, name string, vals ...any) []byte {
	t.Helper()
	data, err := ModuleABI.Events[name].Inputs.NonIndexed().Pack(vals...)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func ints(xs ...int64) []*big.Int {
	out := make([]*big.Int, len(xs))
	for i, x := range xs {
		out[i] = big.NewInt(x)
	}
	return out
}

func TestDecodeProtocolExecution(t *testing.T) {
	l := types.Log{
		Topics:      []common.Hash{ProtocolExecutionTopic, AddressTopic(evSub), AddressTopic(evTarget)},
		Data:        packEventData(t, "ProtocolExecution", uint8(OpDeposit), []common.Address{evTokA, evTokB}, ints(100, 200), []common.Address{evTokB}, ints(150), big.NewInt(42)),
		BlockNumber: 1234,
		Index:       7,
	}
	ev, err := DecodeProtocolExecution(l)
	if err != nil {
		t.Fatal(err)
	}
	if ev.SubAccount != evSub || ev.Target != evTarget || ev.OpType != OpDeposit ||
		len(ev.TokensIn) != 2 || ev.TokensIn[1] != evTokB || ev.AmountsIn[1].Int64() != 200 ||
		len(ev.TokensOut) != 1 || ev.AmountsOut[0].Int64() != 150 || ev.SpendingCost.Int64() != 42 ||
		ev.BlockNumber != 1234 || ev.LogIndex != 7 {
		t.Fatalf("decoded = %+v", ev)
	}
}

func TestDecodeProtocolExecutionRejectsLengthMismatch(t *testing.T) {
	l := types.Log{
		Topics: []common.Hash{ProtocolExecutionTopic, AddressTopic(evSub), AddressTopic(evTarget)},
		Data:   packEventData(t, "ProtocolExecution", uint8(OpSwap), []common.Address{evTokA, evTokB}, ints(100), []common.Address{}, ints(), big.NewInt(0)),
	}
	if _, err := DecodeProtocolExecution(l); err == nil || !strings.Contains(err.Error(), "tokensIn.length (2) !== amountsIn.length (1)") {
		t.Fatalf("err = %v", err)
	}
	l.Data = packEventData(t, "ProtocolExecution", uint8(OpSwap), []common.Address{}, ints(), []common.Address{evTokA}, ints(1, 2), big.NewInt(0))
	if _, err := DecodeProtocolExecution(l); err == nil || !strings.Contains(err.Error(), "tokensOut.length (1) !== amountsOut.length (2)") {
		t.Fatalf("err = %v", err)
	}
}

func TestDecodeRejectsForeignOrTruncatedLogs(t *testing.T) {
	good := types.Log{
		Topics: []common.Hash{TransferExecutedTopic, AddressTopic(evSub), AddressTopic(evTokA), AddressTopic(evRecip)},
		Data:   packEventData(t, "TransferExecuted", big.NewInt(1), big.NewInt(2)),
	}
	if _, err := DecodeProtocolExecution(good); err == nil {
		t.Error("a TransferExecuted log must not decode as ProtocolExecution")
	}
	truncated := good
	truncated.Data = good.Data[:32]
	if _, err := DecodeTransferExecuted(truncated); err == nil {
		t.Error("truncated data must fail to decode")
	}
	missingTopic := good
	missingTopic.Topics = good.Topics[:3]
	if _, err := DecodeTransferExecuted(missingTopic); err == nil {
		t.Error("missing indexed topic must fail")
	}
}

func TestDecodeTransferExecuted(t *testing.T) {
	l := types.Log{
		Topics:      []common.Hash{TransferExecutedTopic, AddressTopic(evSub), AddressTopic(evTokA), AddressTopic(evRecip)},
		Data:        packEventData(t, "TransferExecuted", big.NewInt(50), big.NewInt(5)),
		BlockNumber: 99,
		Index:       3,
	}
	ev, err := DecodeTransferExecuted(l)
	if err != nil {
		t.Fatal(err)
	}
	if ev.SubAccount != evSub || ev.Token != evTokA || ev.Recipient != evRecip ||
		ev.Amount.Int64() != 50 || ev.SpendingCost.Int64() != 5 || ev.BlockNumber != 99 || ev.LogIndex != 3 {
		t.Fatalf("decoded = %+v", ev)
	}
}

func TestDecodeAcquiredBalanceUpdatedAndLimitsSet(t *testing.T) {
	l := types.Log{
		Topics:      []common.Hash{AcquiredBalanceUpdatedTopic, AddressTopic(evSub), AddressTopic(evTokB)},
		Data:        packEventData(t, "AcquiredBalanceUpdated", big.NewInt(77)),
		BlockNumber: 5,
	}
	ev, err := DecodeAcquiredBalanceUpdated(l)
	if err != nil || ev.SubAccount != evSub || ev.Token != evTokB || ev.NewBalance.Int64() != 77 || ev.BlockNumber != 5 {
		t.Fatalf("AcquiredBalanceUpdated = %+v, %v", ev, err)
	}

	limits := types.Log{
		Topics: []common.Hash{SubAccountLimitsSetTopic, AddressTopic(evSub)},
		Data:   packEventData(t, "SubAccountLimitsSet", big.NewInt(500), big.NewInt(0), big.NewInt(86400)),
	}
	if sub, err := SubAccountFromLimitsSet(limits); err != nil || sub != evSub {
		t.Fatalf("SubAccountFromLimitsSet = %s, %v", sub.Hex(), err)
	}
}
