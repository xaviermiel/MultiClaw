package ethx

import (
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

// BlockInfo is the subset of a block header the oracle needs.
type BlockInfo struct {
	Number    uint64
	Hash      common.Hash
	Timestamp uint64
	// BaseFee is nil on chains without EIP-1559.
	BaseFee *big.Int
}

// rpcBlock decodes only the fields we use from eth_getBlockByNumber. The hash
// is taken from the RPC response, as viem does: recomputing it from header
// fields (types.Header.Hash) breaks whenever a chain adds header fields this
// geth version doesn't know, and full block decoding (BlockByNumber) fails on
// OP-stack deposit transactions.
type rpcBlock struct {
	Number    *hexutil.Uint64 `json:"number"`
	Hash      *common.Hash    `json:"hash"`
	Timestamp *hexutil.Uint64 `json:"timestamp"`
	BaseFee   *hexutil.Big    `json:"baseFeePerGas"`
}

func (b *rpcBlock) info(tag string) (BlockInfo, error) {
	if b == nil {
		return BlockInfo{}, fmt.Errorf("block %s not found", tag)
	}
	if b.Number == nil || b.Hash == nil || b.Timestamp == nil {
		return BlockInfo{}, fmt.Errorf("block %s: missing number/hash/timestamp", tag)
	}
	info := BlockInfo{Number: uint64(*b.Number), Hash: *b.Hash, Timestamp: uint64(*b.Timestamp)}
	if b.BaseFee != nil {
		info.BaseFee = b.BaseFee.ToInt()
	}
	return info, nil
}

// BlockInfo returns number, hash, timestamp and base fee of block n.
func (c *FallbackClient) BlockInfo(ctx context.Context, n uint64) (BlockInfo, error) {
	return c.blockInfo(ctx, hexutil.EncodeUint64(n), fmt.Sprintf("getBlock(%d)", n))
}

// LatestBlockInfo returns the latest block's info.
func (c *FallbackClient) LatestBlockInfo(ctx context.Context) (BlockInfo, error) {
	return c.blockInfo(ctx, "latest", "getBlock(latest)")
}

func (c *FallbackClient) blockInfo(ctx context.Context, tag, op string) (BlockInfo, error) {
	var info BlockInfo
	err := c.do(ctx, op, func(ctx context.Context, ep *endpoint) error {
		var raw *rpcBlock
		if err := ep.rpc.CallContext(ctx, &raw, "eth_getBlockByNumber", tag, false); err != nil {
			return err
		}
		var err error
		info, err = raw.info(tag)
		return err // a missing block may just be a lagging endpoint: retryable
	})
	return info, err
}
