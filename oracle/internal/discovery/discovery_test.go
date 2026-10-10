package discovery

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts/contractstest"
)

var (
	registry = common.HexToAddress("0x5e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e")
	moduleA  = common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	moduleB  = common.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	fallback = common.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")
	oracle   = common.HexToAddress("0x763072E0FDa74Eecab3e60BF5BC5b8A46866be7E")
)

type logs []string

func (l *logs) logf(format string, args ...any) { *l = append(*l, fmt.Sprintf(format, args...)) }

func registryWith(modules ...common.Address) *contractstest.Fake {
	if modules == nil {
		modules = []common.Address{}
	}
	return contractstest.New().Return(registry, &contracts.RegistryABI, "getActiveModules", modules)
}

// policyRegistry is a registry with access policies: the oracle serves
// only `served` of its active modules `active`.
func policyRegistry(active, served []common.Address) *contractstest.Fake {
	return contractstest.New().
		Return(registry, &contracts.RegistryABI, "getActiveModules", active).
		On(registry, &contracts.RegistryABI, "getModulesServedBy", func(args []any) ([]any, error) {
			if args[0].(common.Address) != oracle {
				return []any{active}, nil // another oracle's (open) policy
			}
			return []any{served}, nil
		})
}

// unreachable fails every call like an endpoint that is down.
type unreachable struct{}

func (unreachable) CallContract(context.Context, ethereum.CallMsg, *big.Int) ([]byte, error) {
	return nil, errors.New("dial tcp: connection refused")
}

func ptr(a common.Address) *common.Address { return &a }

func TestActiveModules(t *testing.T) {
	broken := contractstest.New() // getActiveModules reverts
	cases := []struct {
		name             string
		caller           contracts.Caller
		registry, module *common.Address
		want             []common.Address
		wantErr, wantLog string
	}{
		{name: "no registry", caller: broken, module: ptr(fallback), want: []common.Address{fallback}},
		{name: "neither configured", caller: broken, wantErr: "neither REGISTRY_ADDRESS nor MODULE_ADDRESS"},
		{
			name: "registry without access policies", caller: registryWith(moduleA, moduleB), registry: ptr(registry), module: ptr(fallback),
			want: []common.Address{moduleA, moduleB}, wantLog: "Found 2 active modules in registry",
		},
		{
			name: "registry with access policies", caller: policyRegistry([]common.Address{moduleA, moduleB}, []common.Address{moduleB}),
			registry: ptr(registry), want: []common.Address{moduleB}, wantLog: "Found 1 active modules in registry served by this oracle",
		},
		{
			name: "access policy serving nothing falls back", caller: policyRegistry([]common.Address{moduleA}, []common.Address{}),
			registry: ptr(registry), module: ptr(fallback), want: []common.Address{fallback}, wantLog: "Registry returned no active modules, falling back",
		},
		{
			// A transport failure, or a revert with data, is not a registry
			// without policies: the other modules must not be served instead.
			name: "unreachable registry", caller: unreachable{}, registry: ptr(registry),
			wantErr: "connection refused",
		},
		{
			name: "access policy read reverting with data", registry: ptr(registry),
			caller:  registryWith(moduleA).Revert(registry, &contracts.RegistryABI, "getModulesServedBy", []byte{0x4e, 0x48, 0x7b, 0x71}),
			wantErr: "query registry " + registry.Hex(),
		},
		{
			name: "empty registry falls back", caller: registryWith(), registry: ptr(registry), module: ptr(fallback),
			want: []common.Address{fallback}, wantLog: "Registry returned no active modules, falling back",
		},
		{
			name: "empty registry without fallback", caller: registryWith(), registry: ptr(registry),
			want: nil, wantLog: "Registry returned no active modules",
		},
		{
			name: "unreadable registry falls back", caller: broken, registry: ptr(registry), module: ptr(fallback),
			want: []common.Address{fallback}, wantLog: "Error querying registry, falling back to single module",
		},
		{
			name: "unreadable registry without fallback", caller: broken, registry: ptr(registry),
			wantErr: "query registry " + registry.Hex(),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var l logs
			got, err := ActiveModules(context.Background(), tc.caller, tc.registry, tc.module, oracle, l.logf)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil || !slices.Equal(got, tc.want) {
				t.Fatalf("modules %v (%v), want %v", got, err, tc.want)
			}
			if tc.wantLog != "" && !slices.ContainsFunc(l, func(s string) bool { return strings.HasPrefix(s, tc.wantLog) }) {
				t.Fatalf("logs %q, want %q", l, tc.wantLog)
			}
		})
	}
}

func TestAuthorized(t *testing.T) {
	fake := contractstest.New().
		Return(moduleA, &contracts.ModuleABI, "authorizedOracle", oracle).
		Return(moduleB, &contracts.ModuleABI, "authorizedOracle", common.HexToAddress("0x1234")).
		Return(fallback, &contracts.ModuleABI, "authorizedOracle", common.Address{})
	unreadable := common.HexToAddress("0xdddd")
	ctx := context.Background()

	var l logs
	if !Authorized(ctx, fake, moduleA, oracle, l.logf) || len(l) != 0 {
		t.Fatalf("our module was skipped: %q", l)
	}
	for _, module := range []common.Address{moduleB, fallback, unreadable} {
		var l logs
		if Authorized(ctx, fake, module, oracle, l.logf) {
			t.Fatalf("module %s accepted", module.Hex())
		}
		if len(l) != 1 || !strings.Contains(l[0], module.Hex()) {
			t.Fatalf("logs %q", l)
		}
	}
	l = nil
	Authorized(ctx, fake, fallback, oracle, l.logf)
	want := "Skipping module " + fallback.Hex() + " — authorized oracle is 0x0000000000000000000000000000000000000000, not us (" + oracle.Hex() + ")"
	if len(l) != 1 || l[0] != want {
		t.Fatalf("log %q, want %q", l, want)
	}
}
