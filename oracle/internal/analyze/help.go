package analyze

// help explains an error: what it means and what to do about it.
type help struct {
	description string
	solution    string
}

// errorHelp explains every custom error of the module and its parsers (a
// test keeps it complete) and the OpenZeppelin token errors.
var errorHelp = map[string]help{
	// Permissions and routing.
	"Unauthorized": {
		"The sender lacks the role this call needs: DEFI_EXECUTE_ROLE for protocol operations, DEFI_REPAY_ROLE for repayments, DEFI_TRANSFER_ROLE for transferToken.",
		"Grant the role to the subaccount (grantRole), or send from a subaccount that has it.",
	},
	"AddressNotAllowed": {
		"The protocol address is not whitelisted for this subaccount.",
		"Whitelist it with setAllowedAddresses(subAccount, [target], true).",
	},
	"SpenderNotAllowed": {
		"The spender of the approve is not whitelisted for this subaccount.",
		"Whitelist the spender with setAllowedAddresses before approving it.",
	},
	"RecipientNotAllowed": {
		"The subaccount has a recipient whitelist, and this recipient is not on it.",
		"Add the recipient with setAllowedRecipients, or transfer to an allowed one.",
	},
	"UnknownSelector": {
		"The module has no operation type registered for this function selector.",
		"Register it with registerSelector(selector, opType), or call a supported function.",
	},
	"NoParserRegistered": {
		"No calldata parser is registered for this protocol, so the module cannot tell what the call spends.",
		"Register one with registerParser(protocol, parser).",
	},
	"InvalidRecipient": {
		"The protocol call sends its output to an address other than the Safe.",
		"Make the Safe the recipient (or onBehalfOf) of the protocol call.",
	},
	"EnforcedPause": {
		"The module is paused.",
		"The owner must call unpause().",
	},
	"ExpectedPause": {
		"unpause() was called while the module was not paused.",
		"Nothing to do: the module is not paused.",
	},
	"ReentrancyGuardReentrantCall": {
		"A call re-entered the module while another module call was running.",
		"The protocol called back into the module, which the module forbids.",
	},
	"AlreadyInitialized": {
		"The module is already initialized.",
		"A module is initialized once, when it is deployed.",
	},

	// Oracle state.
	"StaleOracleData": {
		"The oracle has not updated this subaccount within maxOracleAge, or never has.",
		"Check that the spending oracle is running and is this module's authorized oracle; operations resume after its next update.",
	},
	"StalePortfolioValue": {
		"The Safe value stored in the module is older than maxSafeValueAge, or was never set.",
		"The Safe value oracle must call updateSafeValue: check that it is running and is this module's authorized oracle.",
	},
	"OnlyAuthorizedOracle": {
		"Only the module's authorized oracle can call this function.",
		"Send from the authorizedOracle address, or have the owner change it with setAuthorizedOracle.",
	},
	"InvalidOracleAddress": {
		"The oracle address is invalid. Current modules do not raise this error.",
		"Check the oracle address being set.",
	},

	// Spending.
	"ExceedsSpendingLimit": {
		"The USD value of the tokens spent, beyond their acquired balances, is above the subaccount's spending allowance.",
		"Spend less or use acquired tokens, or wait for the oracle to raise the allowance as older spending leaves the window.",
	},
	"ExceedsCumulativeSpendingLimit": {
		"The window's total spending would go above the subaccount's cap (maxSpendingBps of the Safe value at the window start, or maxSpendingUSD).",
		"Wait for the window to end, or spend less.",
	},
	"ApprovalExceedsLimit": {
		"The approved amount beyond the token's acquired balance is worth more than the spending allowance (without an oracle: than what is left of the window's cap).",
		"Approve less, such as the exact amount of the next operation, or wait for the allowance to grow.",
	},
	"ApprovalFailed": {
		"The Safe's approve call to the token failed.",
		"Check the token: some require setting the allowance to 0 before changing it.",
	},
	"TransactionFailed": {
		"The protocol call the Safe made reverted.",
		"See the underlying error: insufficient balance, slippage, an expired deadline...",
	},
	"ModuleTransactionFailed": {
		"The Safe refused to execute the module's transaction.",
		"Check that the module is enabled on the Safe.",
	},

	// Oracle writes.
	"ExceedsAllowanceCap": {
		"The oracle tried to set an allowance above the subaccount's maximum (maxSpendingBps of the Safe value, or maxSpendingUSD).",
		"The Safe value or the limits the oracle used are out of date: update the Safe value, or check the subaccount's limits.",
	},
	"ExceedsOracleAcquiredBudget": {
		"The acquired balances the oracle granted in this window exceed maxOracleAcquiredBps of the Safe value.",
		"Wait for the window to end. If it keeps happening, check the oracle for a compromise.",
	},
	"LengthMismatch": {
		"Array lengths differ (batchUpdate's tokens, versions and balances, or a parser's tokens and amounts), or the approve calldata is too short.",
		"Pass arrays of equal length, and a complete approve(spender, amount) call.",
	},

	// Prices.
	"NoPriceFeedSet": {
		"The module has no Chainlink price feed for a token the operation spends.",
		"The owner must set one with setTokenPriceFeed(token, feed).",
	},
	"InvalidPrice": {
		"The token's Chainlink feed returned a price of 0 or less.",
		"Check the feed set for the token (tokenPriceFeeds).",
	},
	"StalePriceFeed": {
		"The token's Chainlink price is older than maxPriceFeedAge, or its round is incomplete.",
		"Check that the feed is still updated on this network, or raise maxPriceFeedAge (setMaxPriceFeedAge).",
	},
	"InvalidPriceFeed": {
		"The price feed address is zero.",
		"Pass the feed's address.",
	},

	// Owner configuration.
	"InvalidLimitConfiguration": {
		"A limit is out of bounds: maxSpendingBps above 10000 or a window under 1 hour (setSubAccountLimits), maxOracleAge or maxSafeValueAge under 10 minutes, or maxPriceFeedAge under 1 hour.",
		"Use values within those bounds.",
	},
	"BothLimitModesSet": {
		"setSubAccountLimits got both maxSpendingBps and maxSpendingUSD.",
		"Set exactly one of them, and the other to 0.",
	},
	"NeitherLimitModeSet": {
		"setSubAccountLimits got neither maxSpendingBps nor maxSpendingUSD.",
		"Set exactly one of them.",
	},
	"OraclelessRequiresUSDMode": {
		"Without an oracle the module cannot value the Safe, so spending limits must be in USD.",
		"Use maxSpendingUSD, or set an authorized oracle first.",
	},
	"SubAccountHasBPSLimits": {
		"The module cannot drop its oracle while this subaccount has BPS limits.",
		"Move the subaccount to USD limits (setSubAccountLimits) first.",
	},
	"ExceedsMaxBps": {
		"A basis-point value is above 10000 (100%).",
		"Use at most 10000.",
	},
	"CannotRegisterUnknown": {
		"A selector cannot be registered with the UNKNOWN operation type.",
		"Use SWAP, DEPOSIT, WITHDRAW, CLAIM, APPROVE or REPAY.",
	},
	"CannotBeSubaccount": {
		"The Safe, the module and the authorized oracle cannot be subaccounts.",
		"Use another address.",
	},
	"CannotBeOracle": {
		"The Safe, the module and the subaccounts cannot be the oracle.",
		"Use another address.",
	},
	"CannotWhitelistCoreAddress": {
		"The Safe and the module cannot be whitelisted as targets or recipients.",
		"Remove them from the list.",
	},
	"CannotRegisterParserForCoreAddress": {
		"Parsers cannot be registered for the Safe or the module.",
		"Register parsers for protocol addresses only.",
	},
	"InvalidAddress": {
		"An address argument is zero.",
		"Pass a non-zero address.",
	},

	// Parsers.
	"UnsupportedSelector": {
		"The protocol's parser does not support this function.",
		"Use a function the parser supports, or register a parser that supports it.",
	},
	"InvalidCalldata": {
		"The protocol's parser could not decode the calldata.",
		"Check how the protocol call is encoded.",
	},
	"InvalidPath": {
		"The swap path is shorter than one token.",
		"Encode the path as token, fee, token, ...",
	},
	"UnsupportedV4ExactOutMultiHop": {
		"Multi-hop exact-output Uniswap V4 swaps are not supported.",
		"Use an exact-input swap, or single-hop exact-output swaps.",
	},

	// OpenZeppelin v5 token errors.
	"ERC20InsufficientBalance": {
		"The sender holds fewer tokens than the transfer needs.",
		"Check the sender's balance of the token (the Safe's, for a call the module makes).",
	},
	"ERC20InsufficientAllowance": {
		"The spender's allowance is below the amount it tried to take.",
		"Approve the protocol for at least this amount first.",
	},
	"SafeERC20FailedOperation": {
		"A token transfer or approve failed, or returned false.",
		"Check the token's balance, allowance and transfer rules.",
	},
}

// reasonHelp explains common revert strings (Error(string) reasons).
var reasonHelp = map[string]help{
	"ERC20: insufficient allowance": {
		"The spender's allowance is below the amount it tried to take.",
		"Approve the protocol for at least this amount first.",
	},
	"ERC20: transfer amount exceeds balance": {
		"The sender holds fewer tokens than the transfer needs.",
		"Check the sender's balance of the token (the Safe's, for a call the module makes).",
	},
	// Uniswap V3 periphery.
	"STF": {
		"The router could not take the input token from the Safe (safeTransferFrom failed).",
		"Check the Safe's balance of the input token and its allowance to the router.",
	},
	"Too little received": {
		"The swap would return less than amountOutMinimum (slippage).",
		"Retry with a lower minimum, or when the price moves back.",
	},
	"Too much requested": {
		"The swap would need more input than amountInMaximum (slippage).",
		"Retry with a higher maximum, or when the price moves back.",
	},
	"Transaction too old": {
		"The swap's deadline had passed when it was mined.",
		"Use a later deadline.",
	},
}

// panicCodes names Solidity's Panic(uint256) codes.
var panicCodes = map[uint64]string{
	0x00: "generic compiler panic",
	0x01: "assert failed",
	0x11: "arithmetic overflow or underflow",
	0x12: "division or modulo by zero",
	0x21: "invalid enum value",
	0x22: "invalid storage byte array",
	0x31: "pop() on an empty array",
	0x32: "array index out of bounds",
	0x41: "too much memory allocated",
	0x51: "call to an uninitialized function",
}

var (
	panicHelp = help{
		"The contract hit a Solidity panic.",
		"An unexpected state, or a bug, in the contract that reverted.",
	}
	outOfGasHelp = help{
		"The transaction ran out of gas.",
		"Send it again with a higher gas limit (GAS_LIMIT for oracle transactions).",
	}
	unknownHelp = help{
		"The error is not one the analyzer knows.",
		"Look up the selector (e.g. on a 4byte directory) or in the reverting contract's source.",
	}
)
