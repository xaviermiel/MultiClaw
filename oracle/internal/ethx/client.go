// Package ethx is the oracle's Ethereum client layer: a multi-endpoint RPC
// client with retries and failover, raw block info, chunked log queries, and
// the transaction sender.
package ethx

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/xaviermiel/MultiClaw/oracle/internal/logx"
)

// Options tunes a FallbackClient. Zero values select the defaults.
type Options struct {
	// Logf receives failure and rotation messages.
	Logf logx.Logf
	// MaxFailures is how many consecutive failures make an endpoint unhealthy
	// and move the preferred endpoint on (default 3, as in the TS client).
	MaxFailures int
	// Retries is the number of extra attempts per call (default 3, viem's
	// retryCount; -1 disables retries). A call always tries every endpoint
	// at least once.
	Retries int
	// RetryDelay is the backoff base once every endpoint has been tried;
	// it doubles per retry (default 150ms, viem's retryDelay).
	RetryDelay time.Duration
	// AttemptTimeout bounds each request (default 10s, viem's http timeout).
	AttemptTimeout time.Duration
}

func (o *Options) setDefaults() {
	if o.Logf == nil {
		o.Logf = logx.Discard
	}
	if o.MaxFailures <= 0 {
		o.MaxFailures = 3
	}
	if o.Retries < 0 {
		o.Retries = 0
	} else if o.Retries == 0 {
		o.Retries = 3
	}
	if o.RetryDelay <= 0 {
		o.RetryDelay = 150 * time.Millisecond
	}
	if o.AttemptTimeout <= 0 {
		o.AttemptTimeout = 10 * time.Second
	}
}

// maxRetryDelay caps the exponential backoff.
const maxRetryDelay = 2 * time.Second

type endpoint struct {
	index int
	rpc   *rpc.Client
	eth   *ethclient.Client
	// urls are the spellings of the endpoint URL an error message may quote,
	// and safeURL what replaces them.
	urls    []string
	safeURL string
}

func newEndpoint(index int, raw string, rc *rpc.Client) *endpoint {
	ep := &endpoint{index: index, rpc: rc, eth: ethclient.NewClient(rc), urls: []string{raw}, safeURL: "<RPC endpoint>"}
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		ep.safeURL = u.Scheme + "://" + u.Host
		if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
			ep.safeURL += "/***"
		}
		ep.urls = append(ep.urls, u.String(), u.Redacted())
		if _, ok := u.User.Password(); ok { // net/http's own masking
			ep.urls = append(ep.urls, strings.Replace(u.String(), u.User.String()+"@", u.User.Username()+":***@", 1))
		}
	}
	return ep
}

// redact hides the endpoint URL, which usually embeds a provider API key, in
// err's message: go-ethereum's transport errors quote the request URL.
// errors.Is and errors.As still see err.
func (ep *endpoint) redact(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, u := range ep.urls {
		if u != ep.safeURL && strings.Contains(msg, u) {
			msg = strings.ReplaceAll(msg, u, ep.safeURL)
		}
	}
	if msg == err.Error() {
		return err
	}
	return &redactedError{err: err, msg: msg}
}

// redactedError is an error whose message hides an endpoint URL.
type redactedError struct {
	err error
	msg string
}

func (e *redactedError) Error() string { return e.msg }
func (e *redactedError) Unwrap() error { return e.err }

// FallbackClient spreads calls over several RPC endpoints (the Go port of the
// TS RpcClientManager).
//
// Each call starts at the preferred endpoint and moves to the next endpoint on
// every retryable failure. Once all endpoints have been tried it backs off
// exponentially, like viem's built-in retries. Each endpoint has a failure
// tally (reset on success); when the preferred endpoint reaches MaxFailures
// the preference moves to the next healthy one, and when none is healthy all
// tallies reset. Deterministic errors (reverts, tx-pool rejections, not
// found) return immediately and don't count against the endpoint.
type FallbackClient struct {
	opts      Options
	endpoints []*endpoint

	mu        sync.Mutex
	preferred int
	failures  []int
}

// Dial connects to the given endpoint URLs (the first is preferred).
func Dial(ctx context.Context, urls []string, opts Options) (*FallbackClient, error) {
	if len(urls) == 0 {
		return nil, errors.New("at least one RPC URL is required")
	}
	opts.setDefaults()
	c := &FallbackClient{opts: opts, failures: make([]int, len(urls))}
	for i, u := range urls {
		rc, err := rpc.DialContext(ctx, u)
		if err != nil {
			c.Close()
			return nil, fmt.Errorf("dial RPC endpoint %d: %w", i, newEndpoint(i, u, nil).redact(err))
		}
		c.endpoints = append(c.endpoints, newEndpoint(i, u, rc))
	}
	opts.Logf("Initialized RPC client manager with %d endpoint(s)", len(urls))
	return c, nil
}

// Close closes all endpoint connections.
func (c *FallbackClient) Close() {
	for _, ep := range c.endpoints {
		ep.rpc.Close()
	}
}

// do runs fn with retries and failover. op names the call in logs.
func (c *FallbackClient) do(ctx context.Context, op string, fn func(ctx context.Context, ep *endpoint) error) error {
	n := len(c.endpoints)
	attempts := 1 + c.opts.Retries
	if attempts < n {
		attempts = n
	}

	c.mu.Lock()
	start := c.preferred
	c.mu.Unlock()

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt >= n { // every endpoint tried: back off before reusing one
			delay := c.opts.RetryDelay << (attempt - n)
			if delay > maxRetryDelay || delay <= 0 {
				delay = maxRetryDelay
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}

		ep := c.endpoints[(start+attempt)%n]
		actx, cancel := context.WithTimeout(ctx, c.opts.AttemptTimeout)
		err := ep.redact(fn(actx, ep))
		cancel()

		if err == nil {
			c.reportSuccess(ep.index)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err() // caller gave up; not the endpoint's fault
		}
		if !IsRetryable(err) {
			return err
		}
		lastErr = err
		c.opts.Logf("%s failed on RPC endpoint %d: %v", op, ep.index, err)
		c.reportFailure(ep.index)
	}
	return lastErr
}

func (c *FallbackClient) reportSuccess(i int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures[i] = 0
}

func (c *FallbackClient) reportFailure(i int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.failures[i]++
	if i != c.preferred || c.failures[i] < c.opts.MaxFailures {
		return
	}
	old := c.preferred
	n := len(c.endpoints)
	for step := 1; step <= n; step++ {
		next := (old + step) % n
		if c.failures[next] < c.opts.MaxFailures {
			c.preferred = next
			if next != old {
				c.opts.Logf("RPC endpoint %d exceeded failure threshold, rotated to endpoint %d", old, next)
			}
			return
		}
	}
	c.opts.Logf("All RPC endpoints unhealthy, resetting failure counts")
	for j := range c.failures {
		c.failures[j] = 0
	}
	c.preferred = 0
}

// Preferred returns the index of the currently preferred endpoint.
func (c *FallbackClient) Preferred() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.preferred
}

// ============ Read methods ============

// ChainID returns eth_chainId.
func (c *FallbackClient) ChainID(ctx context.Context) (*big.Int, error) {
	var out *big.Int
	err := c.do(ctx, "getChainId", func(ctx context.Context, ep *endpoint) (err error) {
		out, err = ep.eth.ChainID(ctx)
		return err
	})
	return out, err
}

// BlockNumber returns eth_blockNumber.
func (c *FallbackClient) BlockNumber(ctx context.Context) (uint64, error) {
	var out uint64
	err := c.do(ctx, "getBlockNumber", func(ctx context.Context, ep *endpoint) (err error) {
		out, err = ep.eth.BlockNumber(ctx)
		return err
	})
	return out, err
}

// CallContract executes eth_call (implements contracts.Caller).
func (c *FallbackClient) CallContract(ctx context.Context, msg ethereum.CallMsg, block *big.Int) ([]byte, error) {
	var out []byte
	err := c.do(ctx, "call", func(ctx context.Context, ep *endpoint) (err error) {
		out, err = ep.eth.CallContract(ctx, msg, block)
		return err
	})
	return out, err
}

// FilterLogs executes eth_getLogs.
func (c *FallbackClient) FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error) {
	var out []types.Log
	err := c.do(ctx, "getLogs", func(ctx context.Context, ep *endpoint) (err error) {
		out, err = ep.eth.FilterLogs(ctx, q)
		return err
	})
	return out, err
}

// BalanceAt returns the native balance of addr (block nil = latest).
func (c *FallbackClient) BalanceAt(ctx context.Context, addr common.Address, block *big.Int) (*big.Int, error) {
	var out *big.Int
	err := c.do(ctx, "getBalance", func(ctx context.Context, ep *endpoint) (err error) {
		out, err = ep.eth.BalanceAt(ctx, addr, block)
		return err
	})
	return out, err
}

// PendingNonceAt returns the pending-state nonce of addr.
func (c *FallbackClient) PendingNonceAt(ctx context.Context, addr common.Address) (uint64, error) {
	var out uint64
	err := c.do(ctx, "getTransactionCount", func(ctx context.Context, ep *endpoint) (err error) {
		out, err = ep.eth.PendingNonceAt(ctx, addr)
		return err
	})
	return out, err
}

// NonceAt returns the nonce of addr at block (nil = latest): the number of
// its mined transactions.
func (c *FallbackClient) NonceAt(ctx context.Context, addr common.Address, block *big.Int) (uint64, error) {
	var out uint64
	err := c.do(ctx, "getTransactionCount", func(ctx context.Context, ep *endpoint) (err error) {
		out, err = ep.eth.NonceAt(ctx, addr, block)
		return err
	})
	return out, err
}

// SuggestGasTipCap returns eth_maxPriorityFeePerGas.
func (c *FallbackClient) SuggestGasTipCap(ctx context.Context) (*big.Int, error) {
	var out *big.Int
	err := c.do(ctx, "maxPriorityFeePerGas", func(ctx context.Context, ep *endpoint) (err error) {
		out, err = ep.eth.SuggestGasTipCap(ctx)
		return err
	})
	return out, err
}

// SuggestGasPrice returns eth_gasPrice.
func (c *FallbackClient) SuggestGasPrice(ctx context.Context) (*big.Int, error) {
	var out *big.Int
	err := c.do(ctx, "gasPrice", func(ctx context.Context, ep *endpoint) (err error) {
		out, err = ep.eth.SuggestGasPrice(ctx)
		return err
	})
	return out, err
}

// SendTransaction broadcasts a signed transaction. A transport failure falls
// over to the next endpoint with the same signed bytes, which is idempotent;
// tx-pool rejections (e.g. "already known") are returned to the caller.
func (c *FallbackClient) SendTransaction(ctx context.Context, tx *types.Transaction) error {
	return c.do(ctx, "sendRawTransaction", func(ctx context.Context, ep *endpoint) error {
		return ep.eth.SendTransaction(ctx, tx)
	})
}

// TransactionReceipt returns the receipt of hash, or ethereum.NotFound while
// the transaction is pending.
func (c *FallbackClient) TransactionReceipt(ctx context.Context, hash common.Hash) (*types.Receipt, error) {
	var out *types.Receipt
	err := c.do(ctx, "getTransactionReceipt", func(ctx context.Context, ep *endpoint) (err error) {
		out, err = ep.eth.TransactionReceipt(ctx, hash)
		return err
	})
	return out, err
}

// TransactionByHash returns the transaction with the given hash and whether
// it is still pending, or ethereum.NotFound.
func (c *FallbackClient) TransactionByHash(ctx context.Context, hash common.Hash) (tx *types.Transaction, isPending bool, err error) {
	err = c.do(ctx, "getTransaction", func(ctx context.Context, ep *endpoint) (err error) {
		tx, isPending, err = ep.eth.TransactionByHash(ctx, hash)
		return err
	})
	return tx, isPending, err
}

// CallRPC calls a JSON-RPC method that has no typed wrapper (e.g.
// debug_traceTransaction), with the client's fallbacks and retries, and
// decodes its result into result (a null result sets a pointer to nil).
func (c *FallbackClient) CallRPC(ctx context.Context, result any, method string, args ...any) error {
	return c.do(ctx, method, func(ctx context.Context, ep *endpoint) error {
		return ep.rpc.CallContext(ctx, result, method, args...)
	})
}

// CheckChainID verifies that every endpoint serves chain want. Endpoints that
// don't answer are only logged (they may be down for now), but at least one
// must answer.
func (c *FallbackClient) CheckChainID(ctx context.Context, want uint64) error {
	answered := 0
	for _, ep := range c.endpoints {
		actx, cancel := context.WithTimeout(ctx, c.opts.AttemptTimeout)
		got, err := ep.eth.ChainID(actx)
		cancel()
		if err = ep.redact(err); err != nil {
			c.opts.Logf("Could not check the chain ID of RPC endpoint %d: %v", ep.index, err)
			continue
		}
		answered++
		if !got.IsUint64() || got.Uint64() != want {
			return fmt.Errorf("RPC endpoint %d serves chain %s, not the configured chain %d", ep.index, got, want)
		}
	}
	if answered == 0 {
		return errors.New("no RPC endpoint answered eth_chainId")
	}
	return nil
}
