package ethx

import (
	"context"
	"fmt"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	"github.com/ethereum/go-ethereum/node"
)

// revertingCode is runtime code that reverts with the 4-byte selector as data:
// PUSH4 selector, PUSH1 0xe0, SHL, PUSH1 0, MSTORE, PUSH1 4, PUSH1 0, REVERT.
func revertingCode(selector []byte) []byte {
	code := append([]byte{0x63}, selector...)
	return append(code, 0x60, 0xe0, 0x1b, 0x60, 0x00, 0x52, 0x60, 0x04, 0x60, 0x00, 0xfd)
}

// freePort returns a TCP port that was free a moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

const simulatedChainID = 1337

// newSimulatedChain starts a geth simulated chain served over HTTP JSON-RPC
// and dials it with a FallbackClient.
func newSimulatedChain(t *testing.T, alloc types.GenesisAlloc) (*simulated.Backend, *FallbackClient) {
	t.Helper()
	port := freePort(t)
	sim := simulated.NewBackend(alloc, func(n *node.Config, _ *ethconfig.Config) {
		n.HTTPHost, n.HTTPPort, n.HTTPModules = "127.0.0.1", port, []string{"eth"}
	})
	t.Cleanup(func() { _ = sim.Close() })
	client, err := Dial(context.Background(), []string{fmt.Sprintf("http://127.0.0.1:%d", port)}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return sim, client
}

// TestTxSenderOnASimulatedChain sends real signed transactions to a geth
// simulated chain served over HTTP JSON-RPC, through FallbackClient: they
// must be accepted with consecutive nonces and mined, and a revert must be
// explained by its replay.
func TestTxSenderOnASimulatedChain(t *testing.T) {
	ctx := context.Background()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	from := crypto.PubkeyToAddress(key.PublicKey)
	moduleErr := argumentlessModuleError(t)
	reverter := common.HexToAddress("0x00000000000000000000000000000000000000ee")

	sim, client := newSimulatedChain(t, types.GenesisAlloc{
		from:     {Balance: new(big.Int).Mul(big.NewInt(1e18), big.NewInt(10))},
		reverter: {Code: revertingCode(moduleErr.ID[:4])},
	})
	if err := client.CheckChainID(ctx, simulatedChainID); err != nil {
		t.Fatal(err)
	}
	if err := client.CheckChainID(ctx, testChainID); err == nil {
		t.Fatal("CheckChainID accepted the wrong chain")
	}
	sender, err := NewTxSender(ctx, client, key, simulatedChainID, 500_000,
		SenderOptions{PollInterval: 10 * time.Millisecond, ReceiptTimeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}

	var sent []*SentTx
	for i := range 3 {
		tx, err := sender.Send(ctx, recipient, []byte{byte(i), 0xca, 0xfe})
		if err != nil {
			t.Fatal(err)
		}
		sent = append(sent, tx)
	}
	failing, err := sender.Send(ctx, reverter, []byte{0xde, 0xad, 0xbe, 0xef})
	if err != nil {
		t.Fatal(err)
	}
	sim.Commit()

	for i, tx := range sent {
		receipt, err := sender.Wait(ctx, tx)
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Status != types.ReceiptStatusSuccessful || tx.Nonce != uint64(i) {
			t.Fatalf("tx %d: status %d nonce %d", i, receipt.Status, tx.Nonce)
		}
	}
	receipt, err := sender.Wait(ctx, failing)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Status != types.ReceiptStatusFailed {
		t.Fatalf("the call to the reverting contract succeeded")
	}
	if got := RevertReason(ctx, client, failing, receipt); got != moduleErr.Name {
		t.Fatalf("revert reason %q, want %q", got, moduleErr.Name)
	}
	if n, err := client.PendingNonceAt(ctx, from); err != nil || n != 4 {
		t.Fatalf("account nonce %d (%v), want 4", n, err)
	}
}

// TestTxSenderRecoversFromADroppedTransaction drops a sent transaction from
// the simulated chain's pool. geth queues the next one behind the nonce gap
// without an error; once the dropped one is not mined in time, the sender
// takes its nonce again, and both transactions are mined.
func TestTxSenderRecoversFromADroppedTransaction(t *testing.T) {
	ctx := context.Background()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	from := crypto.PubkeyToAddress(key.PublicKey)
	sim, client := newSimulatedChain(t, types.GenesisAlloc{
		from: {Balance: new(big.Int).Mul(big.NewInt(1e18), big.NewInt(10))},
	})
	sender, err := NewTxSender(ctx, client, key, simulatedChainID, 500_000,
		SenderOptions{PollInterval: 10 * time.Millisecond, ReceiptTimeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	lost, err := sender.Send(ctx, recipient, []byte{1})
	if err != nil {
		t.Fatal(err)
	}
	sim.Rollback() // the node drops it
	queued, err := sender.Send(ctx, recipient, []byte{2})
	if err != nil {
		t.Fatal(err)
	}
	sim.Commit()
	if _, err := sender.Wait(ctx, lost); err == nil {
		t.Fatal("the dropped transaction was mined")
	}

	refill, err := sender.Send(ctx, recipient, []byte{3})
	if err != nil {
		t.Fatal(err)
	}
	sim.Commit()
	for _, tx := range []*SentTx{refill, queued} {
		receipt, err := sender.Wait(ctx, tx)
		if err != nil || receipt.Status != types.ReceiptStatusSuccessful {
			t.Fatalf("nonce %d: receipt %+v (%v)", tx.Nonce, receipt, err)
		}
	}
	if refill.Nonce != lost.Nonce || queued.Nonce != lost.Nonce+1 {
		t.Fatalf("nonces: lost %d, queued %d, refill %d", lost.Nonce, queued.Nonce, refill.Nonce)
	}
	if n, err := client.NonceAt(ctx, from, nil); err != nil || n != 2 {
		t.Fatalf("account nonce %d (%v), want 2", n, err)
	}
}
