package ethx

import (
	"context"
	"errors"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/rpc"
)

// IsExecutionError reports whether err is a deterministic execution failure
// (an EVM revert), which no other endpoint would answer differently.
func IsExecutionError(err error) bool {
	if err == nil {
		return false
	}
	// Revert data is a hex string; some providers attach JSON objects as
	// "data" to rate-limit errors, which must stay retryable.
	var de rpc.DataError
	if errors.As(err, &de) {
		if s, ok := de.ErrorData().(string); ok && strings.HasPrefix(s, "0x") {
			return true
		}
	}
	var re rpc.Error
	if errors.As(err, &re) && re.ErrorCode() == 3 { // geth: execution reverted
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "execution reverted")
}

// Deterministic eth_sendRawTransaction failures. The tx sender handles them
// (nonce resync, duplicate detection), so the client must not retry them.
var (
	// alreadyKnown: the node already has this exact transaction.
	alreadyKnown = []string{"already known", "alreadyknown", "known transaction"}
	// nonceRejections: the nonce is used up, or ahead of the account's.
	nonceRejections = []string{
		"nonce too low",
		"nonce too high",
		"nonce has already been used",
		"replacement transaction underpriced",
	}
	otherTxPoolRejections = []string{
		"transaction underpriced",
		"insufficient funds",
		"intrinsic gas too low",
		"exceeds block gas limit",
		"max fee per gas less than block base fee",
		"fee cap less than block base fee",
	}
)

func errorContains(err error, needles []string) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, s := range needles {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// IsTxPoolRejection reports whether err is a deterministic transaction
// rejection from the node's tx pool.
func IsTxPoolRejection(err error) bool {
	return errorContains(err, alreadyKnown) || errorContains(err, nonceRejections) ||
		errorContains(err, otherTxPoolRejections)
}

// IsRetryable reports whether err may succeed on a retry or another endpoint.
// Deterministic answers (reverts, tx-pool rejections, "not found" for
// receipts/transactions) and caller cancellation are not retryable;
// everything else (HTTP/network errors, rate limits, node-side errors) is,
// and counts toward the endpoint's failure tally.
func IsRetryable(err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, context.Canceled):
		return false
	case errors.Is(err, ethereum.NotFound):
		return false
	case IsExecutionError(err), IsTxPoolRejection(err):
		return false
	}
	return true
}
