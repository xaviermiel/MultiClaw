package ethx

import (
	"context"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
)

// TestLiveRPC is an opt-in smoke test against a real node:
//
//	ORACLE_LIVE_RPC_URL=https://sepolia.base.org go test -run TestLiveRPC -v ./internal/ethx
func TestLiveRPC(t *testing.T) {
	url := os.Getenv("ORACLE_LIVE_RPC_URL")
	if url == "" {
		t.Skip("set ORACLE_LIVE_RPC_URL to run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	c, err := Dial(ctx, []string{url}, Options{Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	chainID, err := c.ChainID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	head, err := c.BlockNumber(ctx)
	if err != nil {
		t.Fatal(err)
	}
	info, err := c.BlockInfo(ctx, head-5)
	if err != nil {
		t.Fatal(err)
	}
	latest, err := c.LatestBlockInfo(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("chain %s head %d; block %d hash %s ts %d baseFee %s; latest %d",
		chainID, head, info.Number, info.Hash.Hex(), info.Timestamp, info.BaseFee, latest.Number)
	if info.Number != head-5 || info.Timestamp == 0 || latest.BaseFee == nil {
		t.Fatalf("unexpected block info %+v / %+v", info, latest)
	}

	// Informational: does geth's locally recomputed header hash agree with
	// the node's? (BlockInfo never relies on it.)
	eth, err := ethclient.DialContext(ctx, url)
	if err == nil {
		if h, err := eth.HeaderByNumber(ctx, new(big.Int).SetUint64(info.Number)); err == nil && h.Hash() != info.Hash {
			t.Logf("note: recomputed header hash %s differs from RPC hash", h.Hash().Hex())
		}
	}
}
