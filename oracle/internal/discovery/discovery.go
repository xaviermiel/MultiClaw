// Package discovery finds the modules this oracle serves. It ports the TS
// getActiveModules and isAuthorizedForModule, which both services shared.
package discovery

import (
	"context"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/xaviermiel/MultiClaw/oracle/internal/contracts"
	"github.com/xaviermiel/MultiClaw/oracle/internal/ethx"
	"github.com/xaviermiel/MultiClaw/oracle/internal/logx"
)

// ActiveModules returns the modules to serve: the registry's active modules
// that the oracle's access policy covers (all of them unless the oracle
// restricted itself to the modules it approved and those of deployers it
// trusts), or the configured module when there is no registry. A registry
// deployed before access policies lacks getModulesServedBy, and every active
// module is served, as in the TS. As in the TS, an empty or unreadable
// registry falls back to the configured module. Without one, an empty
// registry yields no modules and a failed read is an error (the TS carried on
// with an undefined module address).
func ActiveModules(ctx context.Context, c contracts.Caller, registry, module *common.Address, oracle common.Address, logf logx.Logf) ([]common.Address, error) {
	if registry == nil {
		if module == nil {
			return nil, errors.New("neither REGISTRY_ADDRESS nor MODULE_ADDRESS is set")
		}
		return []common.Address{*module}, nil
	}
	reg := contracts.NewRegistry(*registry, c)
	modules, err := reg.ModulesServedBy(ctx, oracle)
	policies := err == nil
	if _, hasData := contracts.RevertData(err); ethx.IsExecutionError(err) && !hasData {
		// A revert without data is how a registry without access policies
		// answers. Any other failure is an error: a restricted oracle must
		// not end up serving every module.
		modules, err = reg.GetActiveModules(ctx)
	}
	switch {
	case err != nil && module != nil:
		logf("Error querying registry, falling back to single module: %v", err)
		return []common.Address{*module}, nil
	case err != nil:
		return nil, fmt.Errorf("query registry %s: %w", registry.Hex(), err)
	case len(modules) == 0 && module != nil:
		logf("Registry returned no active modules, falling back to config.moduleAddress")
		return []common.Address{*module}, nil
	case len(modules) == 0:
		logf("Registry returned no active modules")
		return nil, nil
	}
	if policies {
		logf("Found %d active modules in registry served by this oracle", len(modules))
	} else {
		logf("Found %d active modules in registry", len(modules))
	}
	return modules, nil
}

// Authorized reports whether oracle is module's authorizedOracle. Modules of
// other oracles, oracleless modules (authorizedOracle is zero) and modules
// whose authorizedOracle can't be read are skipped with a log line.
func Authorized(ctx context.Context, c contracts.Caller, module, oracle common.Address, logf logx.Logf) bool {
	authorized, err := contracts.NewModule(module, c).AuthorizedOracle(ctx)
	if err != nil {
		logf("Error checking authorizedOracle for %s, skipping: %v", module.Hex(), err)
		return false
	}
	if authorized != oracle {
		logf("Skipping module %s — authorized oracle is %s, not us (%s)", module.Hex(), authorized.Hex(), oracle.Hex())
		return false
	}
	return true
}
