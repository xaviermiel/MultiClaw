package spendingoracle

import (
	"context"
	"slices"
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// maxBlockHashes is how many block hashes are kept for reorg detection.
const maxBlockHashes = 1000

// blockHashes remembers the hash of every block events were read from (the
// newest maxBlockHashes of them). It is safe for concurrent use.
type blockHashes struct {
	mu     sync.Mutex
	hashes map[uint64]common.Hash
}

func newBlockHashes() *blockHashes { return &blockHashes{hashes: map[uint64]common.Hash{}} }

func (b *blockHashes) record(n uint64, h common.Hash) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.hashes[n] = h
	if extra := len(b.hashes) - maxBlockHashes; extra > 0 {
		for _, old := range b.sortedLocked()[:extra] {
			delete(b.hashes, old)
		}
	}
}

// newest returns up to k recorded block numbers, newest first.
func (b *blockHashes) newest(k int) []uint64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	nums := b.sortedLocked()
	slices.Reverse(nums)
	return nums[:min(k, len(nums))]
}

func (b *blockHashes) hash(n uint64) common.Hash {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.hashes[n]
}

// forgetFrom drops the blocks from n up.
func (b *blockHashes) forgetFrom(n uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for num := range b.hashes {
		if num >= n {
			delete(b.hashes, num)
		}
	}
}

func (b *blockHashes) sortedLocked() []uint64 {
	nums := make([]uint64, 0, len(b.hashes))
	for n := range b.hashes {
		nums = append(nums, n)
	}
	slices.Sort(nums)
	return nums
}

// checkReorg compares the newest min(2*CONFIRMATION_BLOCKS, n) recorded block
// hashes with the chain's. On a mismatch it forgets the blocks from the
// oldest mismatching one up and returns the block before it, to poll again
// from there. (The TS stopped at the newest mismatch, so unwinding a reorg
// of several blocks took one poll per block.) A block that can't be read is
// assumed unchanged, as in the TS.
func (s *Service) checkReorg(ctx context.Context) (rewindTo uint64, found bool) {
	var oldest uint64
	for _, n := range s.blocks.newest(int(2 * s.cfg.ConfirmationBlocks)) {
		info, err := s.chain.BlockInfo(ctx, n)
		if err != nil {
			s.logf("Error checking block %d for reorg: %v", n, err)
			continue
		}
		if expected := s.blocks.hash(n); info.Hash != expected {
			s.logf("Reorg detected at block %d: expected %s, got %s", n, expected.Hex(), info.Hash.Hex())
			oldest, found = n, true
		}
	}
	if !found {
		return 0, false
	}
	s.blocks.forgetFrom(oldest)
	if oldest == 0 {
		return 0, true
	}
	return oldest - 1, true
}
