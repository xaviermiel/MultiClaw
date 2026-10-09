package spending

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/bn"
)

// ConsumeFromQueue consumes up to amount from the front of q (oldest first)
// and returns copies of the consumed portions plus whatever couldn't be
// consumed. Expired entries reaching the front are dropped, with expiry
// measured from the event's timestamp: an entry is expired when its
// timestamp is < eventTimestamp - windowDuration. Port of consumeFromQueue.
func ConsumeFromQueue(q *Queue, amount *big.Int, eventTimestamp, windowDuration int64) (consumed []AcquiredEntry, remaining *big.Int) {
	remaining = bn.Copy(amount)
	expiryThreshold := eventTimestamp - windowDuration

	for remaining.Sign() > 0 && len(*q) > 0 {
		entry := (*q)[0]

		// Skip expired entries (they shouldn't be consumed as acquired)
		if entry.OriginalTimestamp < expiryThreshold {
			*q = (*q)[1:]
			continue
		}

		if entry.Amount.Cmp(remaining) <= 0 {
			// Consume entire entry
			consumed = append(consumed, AcquiredEntry{Amount: bn.Copy(entry.Amount), OriginalTimestamp: entry.OriginalTimestamp})
			remaining = bn.Sub(remaining, entry.Amount)
			*q = (*q)[1:]
		} else {
			// Partial consumption
			consumed = append(consumed, AcquiredEntry{Amount: bn.Copy(remaining), OriginalTimestamp: entry.OriginalTimestamp})
			(*q)[0].Amount = bn.Sub(entry.Amount, remaining)
			remaining = bn.Zero()
		}
	}
	return consumed, remaining
}

// AddToQueue appends amount with its original timestamp; non-positive
// amounts are ignored. Port of addToQueue.
func AddToQueue(q *Queue, amount *big.Int, originalTimestamp int64) {
	if amount.Sign() <= 0 {
		return
	}
	*q = append(*q, AcquiredEntry{Amount: bn.Copy(amount), OriginalTimestamp: originalTimestamp})
}

// ValidQueueBalance sums the entries that haven't expired at
// currentTimestamp (timestamp >= currentTimestamp - windowDuration).
// Port of getValidQueueBalance.
func ValidQueueBalance(q Queue, currentTimestamp, windowDuration int64) *big.Int {
	expiryThreshold := currentTimestamp - windowDuration
	total := bn.Zero()
	for _, e := range q {
		if e.OriginalTimestamp >= expiryThreshold {
			total = bn.Add(total, e.Amount)
		}
	}
	return total
}

// PruneExpiredEntries removes every expired entry, wherever it is in the
// (possibly unsorted) queue, keeping the survivors' order.
// Port of pruneExpiredEntries.
func PruneExpiredEntries(q *Queue, currentTimestamp, windowDuration int64) {
	expiryThreshold := currentTimestamp - windowDuration
	kept := (*q)[:0]
	for _, e := range *q {
		if e.OriginalTimestamp >= expiryThreshold {
			kept = append(kept, e)
		}
	}
	*q = kept
}

// TokenValueUSD returns amount's USD value (18 decimals) using the cache, or
// false when the token has no (or a zero) price. Port of getTokenValueUSD,
// except that 10^decimals is exact for every decimals value.
func TokenValueUSD(token common.Address, amount *big.Int, cache TokenPriceCache) (*big.Int, bool) {
	info, ok := cache[token]
	if !ok || info.PriceUSD == nil || info.PriceUSD.Sign() == 0 {
		return nil, false
	}
	// value = amount * price / 10^decimals
	return bn.Quo(bn.Mul(amount, info.PriceUSD), bn.Pow10(uint(info.Decimals))), true
}

func sumAmounts(entries []AcquiredEntry) *big.Int {
	total := bn.Zero()
	for _, e := range entries {
		total = bn.Add(total, e.Amount)
	}
	return total
}
