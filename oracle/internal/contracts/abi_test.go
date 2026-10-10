package contracts

import (
	"maps"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

// Every method the oracle calls, with its exact signature and output types.
// If a contract change breaks one of these, update the oracle together with
// the ABI (`make abi`).
func TestABIDriftGuardMethods(t *testing.T) {
	cases := []struct {
		abi     *abi.ABI
		name    string
		sig     string
		outputs string
	}{
		{&ModuleABI, "getSafeValue", "getSafeValue()", "uint256,uint256,uint256"},
		{&ModuleABI, "getSubAccountLimits", "getSubAccountLimits(address)", "uint256,uint256,uint256"},
		{&ModuleABI, "getSpendingAllowance", "getSpendingAllowance(address)", "uint256"},
		{&ModuleABI, "getAcquiredBalance", "getAcquiredBalance(address,address)", "uint256"},
		{&ModuleABI, "allowanceVersion", "allowanceVersion(address)", "uint256"},
		{&ModuleABI, "acquiredBalanceVersion", "acquiredBalanceVersion(address,address)", "uint256"},
		{&ModuleABI, "avatar", "avatar()", "address"},
		{&ModuleABI, "authorizedOracle", "authorizedOracle()", "address"},
		{&ModuleABI, "getSubaccountsByRole", "getSubaccountsByRole(uint16)", "address[]"},
		{&ModuleABI, "getTokenBalances", "getTokenBalances(address[])", "uint256[]"},
		{&ModuleABI, "batchUpdate", "batchUpdate(address,uint256,uint256,address[],uint256[],uint256[])", ""},
		{&ModuleABI, "updateSafeValue", "updateSafeValue(uint256)", ""},
		{&ModuleABI, "DEFI_EXECUTE_ROLE", "DEFI_EXECUTE_ROLE()", "uint16"},
		{&ModuleABI, "DEFI_TRANSFER_ROLE", "DEFI_TRANSFER_ROLE()", "uint16"},
		{&ModuleABI, "DEFI_REPAY_ROLE", "DEFI_REPAY_ROLE()", "uint16"},
		{&RegistryABI, "getActiveModules", "getActiveModules()", "address[]"},
		{&RegistryABI, "getModulesServedBy", "getModulesServedBy(address)", "address[]"},
		{&LegacyModuleABI, "absoluteMaxSpendingBps", "absoluteMaxSpendingBps()", "uint256"},
		{&ERC20ABI, "balanceOf", "balanceOf(address)", "uint256"},
		{&ERC20ABI, "decimals", "decimals()", "uint8"},
		{&ERC20ABI, "symbol", "symbol()", "string"},
		{&ChainlinkABI, "latestRoundData", "latestRoundData()", "uint80,int256,uint256,uint256,uint80"},
		{&ChainlinkABI, "decimals", "decimals()", "uint8"},
		{&ERC4626ABI, "convertToAssets", "convertToAssets(uint256)", "uint256"},
		{&UniswapV2PairABI, "totalSupply", "totalSupply()", "uint256"},
		{&UniswapV2PairABI, "getReserves", "getReserves()", "uint112,uint112,uint32"},
	}
	for _, c := range cases {
		m, ok := c.abi.Methods[c.name]
		if !ok {
			t.Errorf("%s missing from ABI", c.name)
			continue
		}
		if m.Sig != c.sig {
			t.Errorf("%s signature = %s, want %s", c.name, m.Sig, c.sig)
		}
		outs := make([]string, len(m.Outputs))
		for i, o := range m.Outputs {
			outs[i] = o.Type.String()
		}
		if got := strings.Join(outs, ","); got != c.outputs {
			t.Errorf("%s outputs = %s, want %s", c.name, got, c.outputs)
		}
	}

	// The legacy getter must stay out of the current module ABI: its absence
	// is what makes the allowance cap probe skip on new deployments.
	if _, ok := ModuleABI.Methods["absoluteMaxSpendingBps"]; ok {
		t.Error("absoluteMaxSpendingBps reappeared in the module ABI; revisit the legacy cap probe")
	}
}

func TestABIDriftGuardEvents(t *testing.T) {
	cases := []struct {
		name    string
		sig     string
		indexed []bool
	}{
		{"ProtocolExecution", "ProtocolExecution(address,address,uint8,address[],uint256[],address[],uint256[],uint256)", []bool{true, true, false, false, false, false, false, false}},
		{"TransferExecuted", "TransferExecuted(address,address,address,uint256,uint256)", []bool{true, true, true, false, false}},
		{"AcquiredBalanceUpdated", "AcquiredBalanceUpdated(address,address,uint256)", []bool{true, true, false}},
		{"SubAccountLimitsSet", "SubAccountLimitsSet(address,uint256,uint256,uint256)", []bool{true, false, false, false}},
	}
	for _, c := range cases {
		ev, ok := ModuleABI.Events[c.name]
		if !ok {
			t.Errorf("event %s missing", c.name)
			continue
		}
		if ev.Sig != c.sig {
			t.Errorf("%s signature = %s, want %s", c.name, ev.Sig, c.sig)
		}
		if ev.ID != crypto.Keccak256Hash([]byte(c.sig)) {
			t.Errorf("%s topic mismatch", c.name)
		}
		for i, in := range ev.Inputs {
			if i < len(c.indexed) && in.Indexed != c.indexed[i] {
				t.Errorf("%s input %d indexed = %v", c.name, i, in.Indexed)
			}
		}
	}
}

func TestPinnedSelectors(t *testing.T) {
	pins := map[string]string{
		"batchUpdate":     "0x1eed40fa",
		"updateSafeValue": "0xe70d0840",
	}
	for name, want := range pins {
		if got := hexutil.Encode(ModuleABI.Methods[name].ID); got != want {
			t.Errorf("%s selector = %s, want %s", name, got, want)
		}
	}
}

// TestProtocolCallSelectors pins the analyzer's protocol calls to the
// selectors the module's parsers recognize (AaveV3Parser, UniswapV3Parser),
// plus ERC20 and Aave borrow, and the token errors to OpenZeppelin v5's.
func TestProtocolCallSelectors(t *testing.T) {
	want := map[string]string{
		"approve(address,uint256)":                                                            "0x095ea7b3",
		"transfer(address,uint256)":                                                           "0xa9059cbb",
		"transferFrom(address,address,uint256)":                                               "0x23b872dd",
		"supply(address,uint256,address,uint16)":                                              "0x617ba037",
		"withdraw(address,uint256,address)":                                                   "0x69328dec",
		"repay(address,uint256,uint256,address)":                                              "0x573ade81",
		"borrow(address,uint256,uint256,uint16,address)":                                      "0xa415bcad",
		"exactInputSingle((address,address,uint24,address,uint256,uint256,uint256,uint160))":  "0x414bf389",
		"exactInput((bytes,address,uint256,uint256,uint256))":                                 "0xc04b8d59",
		"exactOutputSingle((address,address,uint24,address,uint256,uint256,uint256,uint160))": "0xdb3e2198",
		"exactOutput((bytes,address,uint256,uint256,uint256))":                                "0xf28c0498",
		"exactInputSingle((address,address,uint24,address,uint256,uint256,uint160))":          "0x04e45aaf",
		"exactInput((bytes,address,uint256,uint256))":                                         "0xb858183f",
		"exactOutputSingle((address,address,uint24,address,uint256,uint256,uint160))":         "0x5023b4df",
		"exactOutput((bytes,address,uint256,uint256))":                                        "0x09b81346",
	}
	got := map[string]string{}
	for _, m := range ProtocolCallsABI.Methods {
		got[m.Sig] = hexutil.Encode(m.ID)
	}
	if !maps.Equal(got, want) {
		t.Errorf("protocol call selectors %v, want %v", got, want)
	}

	errs := map[string]string{
		"ERC20InsufficientBalance":   "0xe450d38c",
		"ERC20InsufficientAllowance": "0xfb8f41b2",
		"SafeERC20FailedOperation":   "0x5274afe7",
	}
	if len(CommonErrorsABI.Errors) != len(errs) {
		t.Errorf("%d common errors, want %d", len(CommonErrorsABI.Errors), len(errs))
	}
	for name, sel := range errs {
		if e, ok := CommonErrorsABI.Errors[name]; !ok || hexutil.Encode(e.ID[:4]) != sel {
			t.Errorf("%s selector = %x, want %s", name, e.ID[:4], sel)
		}
	}
}

func TestOperationTypeMatchesContractEnum(t *testing.T) {
	// The event's opType is the contract enum; its ABI type is uint8.
	if typ := ModuleABI.Events["ProtocolExecution"].Inputs[2]; typ.Type.String() != "uint8" {
		t.Fatalf("opType ABI type = %s", typ.Type)
	}
	want := map[OperationType]string{
		OpUnknown: "UNKNOWN", OpSwap: "SWAP", OpDeposit: "DEPOSIT", OpWithdraw: "WITHDRAW",
		OpClaim: "CLAIM", OpApprove: "APPROVE", OpRepay: "REPAY", 9: "OperationType(9)",
	}
	for op, s := range want {
		if op.String() != s {
			t.Errorf("%d.String() = %s, want %s", op, op.String(), s)
		}
	}
}

func TestErrorTablesLoaded(t *testing.T) {
	for _, name := range []string{"ExceedsAllowanceCap", "ExceedsOracleAcquiredBudget", "LengthMismatch", "OnlyAuthorizedOracle", "StalePortfolioValue"} {
		if _, ok := ModuleABI.Errors[name]; !ok {
			t.Errorf("module error %s missing", name)
		}
	}
	for _, name := range []string{"InvalidCalldata", "InvalidPath", "UnsupportedSelector", "UnsupportedV4ExactOutMultiHop"} {
		if _, ok := ParserErrorsABI.Errors[name]; !ok {
			t.Errorf("parser error %s missing", name)
		}
	}
}

func TestAddressTopic(t *testing.T) {
	a := common.HexToAddress("0x763072E0FDa74Eecab3e60BF5BC5b8A46866be7E")
	topic := AddressTopic(a)
	if topicAddress(topic) != a || topic[0] != 0 || topic[11] != 0 {
		t.Fatalf("topic = %s", topic.Hex())
	}
}
