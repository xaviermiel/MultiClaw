// Package contracts holds the ABIs the oracle uses and typed helpers around
// them: read calls, event decoding, calldata for oracle writes, and revert
// decoding.
//
// abi/generated/ is extracted from the Foundry build with `make abi` (module,
// registry and parser errors). abi/manual/ holds minimal hand-written ABIs for
// external contracts (ERC20, Chainlink, ERC4626, Uniswap V2 pairs), for the
// protocol calls and token errors the transaction analyzer decodes, and for
// the legacy absoluteMaxSpendingBps() getter that only old module deployments
// have.
package contracts

import (
	"bytes"
	"embed"
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

//go:embed abi/generated/*.json abi/manual/*.json
var abiFS embed.FS

// Parsed ABIs.
var (
	ModuleABI       = mustLoad("abi/generated/DeFiInteractorModule.json")
	RegistryABI     = mustLoad("abi/generated/ModuleRegistry.json")
	ParserErrorsABI = mustLoad("abi/generated/ParserErrors.json")

	ERC20ABI         = mustLoad("abi/manual/ERC20.json")
	ChainlinkABI     = mustLoad("abi/manual/ChainlinkAggregator.json")
	ERC4626ABI       = mustLoad("abi/manual/ERC4626Vault.json")
	UniswapV2PairABI = mustLoad("abi/manual/UniswapV2Pair.json")
	LegacyModuleABI  = mustLoad("abi/manual/LegacyModule.json")

	// ProtocolCallsABI has the ERC20, Aave V3 Pool and Uniswap V3 router
	// (SwapRouter and SwapRouter02) calls a subaccount sends through
	// executeOnProtocol; CommonErrorsABI the OpenZeppelin v5 token errors.
	ProtocolCallsABI = mustLoad("abi/manual/ProtocolCalls.json")
	CommonErrorsABI  = mustLoad("abi/manual/CommonErrors.json")
)

// Event topics (topic0) of the module events the oracle reads.
var (
	ProtocolExecutionTopic      = ModuleABI.Events["ProtocolExecution"].ID
	TransferExecutedTopic       = ModuleABI.Events["TransferExecuted"].ID
	AcquiredBalanceUpdatedTopic = ModuleABI.Events["AcquiredBalanceUpdated"].ID
	SubAccountLimitsSetTopic    = ModuleABI.Events["SubAccountLimitsSet"].ID
)

// Role IDs used to discover subaccounts (DeFiInteractorModule constants).
const (
	DefiExecuteRole  uint16 = 1
	DefiTransferRole uint16 = 2
	DefiRepayRole    uint16 = 3
)

// OperationType mirrors DeFiInteractorModule.OperationType.
type OperationType uint8

// Operation types. REPAY is missing from the TS abi.ts enum; the spending
// state machine ignores it either way.
const (
	OpUnknown  OperationType = 0
	OpSwap     OperationType = 1
	OpDeposit  OperationType = 2
	OpWithdraw OperationType = 3
	OpClaim    OperationType = 4
	OpApprove  OperationType = 5
	OpRepay    OperationType = 6
)

var opNames = [...]string{"UNKNOWN", "SWAP", "DEPOSIT", "WITHDRAW", "CLAIM", "APPROVE", "REPAY"}

func (o OperationType) String() string {
	if int(o) < len(opNames) {
		return opNames[o]
	}
	return fmt.Sprintf("OperationType(%d)", uint8(o))
}

func mustLoad(path string) abi.ABI {
	raw, err := abiFS.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("contracts: read %s: %v", path, err))
	}
	parsed, err := abi.JSON(bytes.NewReader(raw))
	if err != nil {
		panic(fmt.Sprintf("contracts: parse %s: %v", path, err))
	}
	return parsed
}

// topicAddress extracts the address stored in an indexed address topic.
func topicAddress(topic common.Hash) common.Address {
	return common.BytesToAddress(topic[12:])
}
