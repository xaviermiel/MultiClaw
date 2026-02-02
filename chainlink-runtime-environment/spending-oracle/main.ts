/**
 * Spending Oracle for DeFiInteractorModule
 *
 * Implements the Acquired Balance Model with FIFO tracking:
 * - Rolling 24h window tracking for spending
 * - FIFO queue for acquired balances with original timestamp preservation
 * - Deposit/withdrawal matching for acquired status with timestamp inheritance
 * - 24h expiry for acquired balances based on original acquisition timestamp
 * - Periodic allowance refresh via cron trigger
 * - Proper tracking of acquired balance usage (deductions)
 * - Stale on-chain balance clearing
 *
 * State Management:
 * Since CRE workflows are stateless, we query historical events from the chain
 * to reconstruct state on each invocation. This ensures verifiable, decentralized
 * state derivation.
 *
 * Key Design:
 * - FIFO queues track (amount, originalTimestamp) for each token
 * - When swapping/depositing, consumed acquired tokens' timestamps are inherited
 * - Mixed acquired/non-acquired inputs are proportionally split in outputs
 * - The contract reads acquired balances, oracle manages them
 * - Spending is one-way (no recovery on withdrawals)
 */

import {
	bytesToHex,
	type CronPayload,
	cre,
	encodeCallMsg,
	getNetwork,
	hexToBase64,
	LAST_FINALIZED_BLOCK_NUMBER,
	Runner,
	type Runtime,
	TxStatus,
} from '@chainlink/cre-sdk'
import {
	type Address,
	decodeAbiParameters,
	decodeFunctionResult,
	encodeFunctionData,
	keccak256,
	toHex,
	zeroAddress,
} from 'viem'
import { z } from 'zod'
import { DeFiInteractorModule, ModuleRegistry, OperationType } from '../contracts/abi'

// ============ Configuration Schema ============

const TokenSchema = z.object({
	address: z.string(),
	priceFeedAddress: z.string(),
	symbol: z.string(),
})

const configSchema = z.object({
	// Single module address (legacy/fallback mode)
	moduleAddress: z.string(),
	// Registry address for multi-module support (optional - if provided, overrides moduleAddress)
	registryAddress: z.string().optional(),
	chainSelectorName: z.string(),
	gasLimit: z.string(),
	proxyAddress: z.string(),
	tokens: z.array(TokenSchema),
	// Cron schedule for periodic allowance refresh (e.g., "*/5 * * * *" for every 5 minutes)
	refreshSchedule: z.string(),
	// Window duration in seconds (default 24 hours)
	windowDurationSeconds: z.number(),
	// How many blocks to look back for events (approximate 24h worth)
	// Ethereum: ~7200 blocks/day, Arbitrum: ~300000 blocks/day
	blocksToLookBack: z.number(),
})

type Config = z.infer<typeof configSchema>

// ============ Types ============

interface ProtocolExecutionEvent {
	subAccount: Address
	target: Address
	opType: OperationType
	tokensIn: Address[]     // Array of input tokens
	amountsIn: bigint[]     // Array of input amounts
	tokensOut: Address[]    // Array of output tokens
	amountsOut: bigint[]    // Array of output amounts
	spendingCost: bigint
	timestamp: bigint
	blockNumber: bigint
	logIndex: number
}

interface TransferExecutedEvent {
	subAccount: Address
	token: Address
	recipient: Address
	amount: bigint
	spendingCost: bigint
	timestamp: bigint
	blockNumber: bigint
	logIndex: number
}

interface DepositRecord {
	subAccount: Address
	target: Address
	tokenIn: Address
	amountIn: bigint
	remainingAmount: bigint  // Tracks how much of the deposit hasn't been withdrawn yet
	tokenOut: Address        // Output token received from deposit (e.g., aToken, LP token)
	amountOut: bigint        // Amount of output token received
	remainingOutputAmount: bigint  // Tracks how much output hasn't been consumed by withdrawal
	timestamp: bigint  // When the deposit happened
	originalAcquisitionTimestamp: bigint  // When the tokens were originally acquired (for FIFO inheritance)
}

// ============ Token Price Cache ============

interface TokenPriceInfo {
	priceUSD: bigint      // Price in USD with 18 decimals
	decimals: number      // Token decimals
}

type TokenPriceCache = Map<Address, TokenPriceInfo>

/**
 * FIFO queue entry for acquired balances
 * Tracks the original acquisition timestamp so tokens expire together
 * when swapped (output inherits input's original timestamp)
 */
interface AcquiredBalanceEntry {
	amount: bigint
	originalTimestamp: bigint  // When the tokens were originally acquired (for expiry calculation)
}

/**
 * FIFO queue for each token's acquired balance
 * Oldest entries are consumed first when spending
 */
type AcquiredBalanceQueue = AcquiredBalanceEntry[]

interface SubAccountState {
	spendingRecords: { amount: bigint; timestamp: bigint }[]
	depositRecords: DepositRecord[]
	totalSpendingInWindow: bigint
	// FIFO queues for acquired balances per token
	acquiredQueues: Map<Address, AcquiredBalanceQueue>
	// Final calculated acquired balances (sum of non-expired entries)
	acquiredBalances: Map<Address, bigint>
}

// ============ Event Signatures ============
// Note: Events no longer include timestamp parameter - contract uses block.timestamp

const PROTOCOL_EXECUTION_EVENT_SIG = keccak256(
	toHex('ProtocolExecution(address,address,uint8,address[],uint256[],address[],uint256[],uint256)')
)

const TRANSFER_EXECUTED_EVENT_SIG = keccak256(
	toHex('TransferExecuted(address,address,address,uint256,uint256)')
)

const ACQUIRED_BALANCE_UPDATED_EVENT_SIG = keccak256(
	toHex('AcquiredBalanceUpdated(address,address,uint256)')
)

// ============ Helper Functions ============

/**
 * Get the network configuration
 */
const getNetworkConfig = (runtime: Runtime<Config>) => {
	const isTestnet = runtime.config.chainSelectorName.includes('testnet') ||
		runtime.config.chainSelectorName.includes('sepolia')

	const network = getNetwork({
		chainFamily: 'evm',
		chainSelectorName: runtime.config.chainSelectorName,
		isTestnet,
	})

	if (!network) {
		throw new Error(`Network not found for: ${runtime.config.chainSelectorName}`)
	}

	return network
}

/**
 * Create an EVM client for contract calls
 */
const createEvmClient = (runtime: Runtime<Config>) => {
	const network = getNetworkConfig(runtime)
	return new cre.capabilities.EVMClient(network.chainSelector.selector)
}

const getCurrentBlockTimestamp = (): bigint => {
	return BigInt(Math.floor(Date.now() / 1000))
}

// ============ Retry Logic ============

/**
 * Retry an operation once on failure
 */
const retryOnce = <T>(
	runtime: Runtime<Config>,
	operation: () => T,
	operationName: string
): T => {
	try {
		return operation()
	} catch (firstError) {
		runtime.log(`${operationName} failed, retrying once: ${firstError}`)
		try {
			return operation()
		} catch (secondError) {
			runtime.log(`${operationName} failed after retry: ${secondError}`)
			throw secondError
		}
	}
}

// ============ Chainlink Price Feed ABI ============

const ChainlinkPriceFeedABI = [
	{
		inputs: [],
		name: 'latestRoundData',
		outputs: [
			{ name: 'roundId', type: 'uint80' },
			{ name: 'answer', type: 'int256' },
			{ name: 'startedAt', type: 'uint256' },
			{ name: 'updatedAt', type: 'uint256' },
			{ name: 'answeredInRound', type: 'uint80' },
		],
		stateMutability: 'view',
		type: 'function',
	},
	{
		inputs: [],
		name: 'decimals',
		outputs: [{ name: '', type: 'uint8' }],
		stateMutability: 'view',
		type: 'function',
	},
] as const

// ERC20 ABI for decimals
const ERC20DecimalsABI = [
	{
		inputs: [],
		name: 'decimals',
		outputs: [{ name: '', type: 'uint8' }],
		stateMutability: 'view',
		type: 'function',
	},
] as const

// ============ Token Price Functions ============

/**
 * Get price feed address for a token from config
 */
const getPriceFeedForToken = (runtime: Runtime<Config>, tokenAddress: Address): Address | null => {
	const tokenLower = tokenAddress.toLowerCase()
	const tokenConfig = runtime.config.tokens.find(t => t.address.toLowerCase() === tokenLower)
	if (tokenConfig?.priceFeedAddress) {
		return tokenConfig.priceFeedAddress as Address
	}
	return null
}

/**
 * Get token decimals from contract
 */
const getTokenDecimals = (runtime: Runtime<Config>, tokenAddress: Address): number => {
	const evmClient = createEvmClient(runtime)

	const callData = encodeFunctionData({
		abi: ERC20DecimalsABI,
		functionName: 'decimals',
	})

	try {
		const result = evmClient
			.callContract(runtime, {
				call: encodeCallMsg({
					from: zeroAddress,
					to: tokenAddress,
					data: callData,
				}),
				blockNumber: LAST_FINALIZED_BLOCK_NUMBER,
			})
			.result()

		if (!result.data || result.data.length === 0) {
			return 18
		}

		const decimals = decodeFunctionResult({
			abi: ERC20DecimalsABI,
			functionName: 'decimals',
			data: bytesToHex(result.data),
		})

		return Number(decimals)
	} catch (error) {
		runtime.log(`Error getting decimals for ${tokenAddress}: ${error}`)
		return 18
	}
}

/**
 * Get price from Chainlink price feed (normalized to 18 decimals)
 */
const getChainlinkPriceUSD = (runtime: Runtime<Config>, priceFeedAddress: Address): bigint => {
	const evmClient = createEvmClient(runtime)

	const callData = encodeFunctionData({
		abi: ChainlinkPriceFeedABI,
		functionName: 'latestRoundData',
	})

	try {
		const priceResult = evmClient
			.callContract(runtime, {
				call: encodeCallMsg({
					from: zeroAddress,
					to: priceFeedAddress,
					data: callData,
				}),
				blockNumber: LAST_FINALIZED_BLOCK_NUMBER,
			})
			.result()

		if (!priceResult.data || priceResult.data.length === 0) {
			return 0n
		}

		const [, answer] = decodeFunctionResult({
			abi: ChainlinkPriceFeedABI,
			functionName: 'latestRoundData',
			data: bytesToHex(priceResult.data),
		})

		// Get feed decimals
		const decimalsCallData = encodeFunctionData({
			abi: ChainlinkPriceFeedABI,
			functionName: 'decimals',
		})

		const decimalsResult = evmClient
			.callContract(runtime, {
				call: encodeCallMsg({
					from: zeroAddress,
					to: priceFeedAddress,
					data: decimalsCallData,
				}),
				blockNumber: LAST_FINALIZED_BLOCK_NUMBER,
			})
			.result()

		let feedDecimals = 8
		if (decimalsResult.data && decimalsResult.data.length > 0) {
			feedDecimals = Number(decodeFunctionResult({
				abi: ChainlinkPriceFeedABI,
				functionName: 'decimals',
				data: bytesToHex(decimalsResult.data),
			}))
		}

		// Normalize to 18 decimals
		const price18 = BigInt(answer) * BigInt(10 ** (18 - feedDecimals))
		return price18
	} catch (error) {
		runtime.log(`Error getting price from ${priceFeedAddress}: ${error}`)
		return 0n
	}
}

/**
 * Build a price cache for all tokens involved in events
 * Returns a map of token address -> { priceUSD (18 decimals), decimals }
 */
const buildTokenPriceCache = (runtime: Runtime<Config>, tokens: Set<Address>): TokenPriceCache => {
	const cache: TokenPriceCache = new Map()

	for (const token of tokens) {
		const tokenLower = token.toLowerCase() as Address
		const priceFeed = getPriceFeedForToken(runtime, tokenLower)

		if (!priceFeed) {
			// No price feed configured - skip
			continue
		}

		try {
			const priceUSD = getChainlinkPriceUSD(runtime, priceFeed)
			const decimals = getTokenDecimals(runtime, tokenLower)

			if (priceUSD > 0n) {
				cache.set(tokenLower, { priceUSD, decimals })
			}
		} catch (error) {
			runtime.log(`Error fetching price for ${token}: ${error}`)
		}
	}

	return cache
}

/**
 * Calculate USD value for a token amount using the price cache
 * Returns value in 18 decimals, or null if price not available
 */
const getTokenValueUSD = (
	token: Address,
	amount: bigint,
	priceCache: TokenPriceCache
): bigint | null => {
	const tokenLower = token.toLowerCase() as Address
	const priceInfo = priceCache.get(tokenLower)

	if (!priceInfo || priceInfo.priceUSD === 0n) {
		return null
	}

	// value = amount * price / 10^decimals
	// Both price and result are in 18 decimals
	return (amount * priceInfo.priceUSD) / BigInt(10 ** priceInfo.decimals)
}

// ============ Update Thresholds ============

// Only update if allowance INCREASED by more than this percentage
const ALLOWANCE_INCREASE_THRESHOLD_BPS = 200n // 2%

// Always update if last update was more than this many seconds ago
const MAX_STALENESS_SECONDS = 2700n // 45 minutes

// Track last update timestamp per subaccount (module:subaccount -> timestamp)
const lastUpdateTimestamp = new Map<string, bigint>()

/**
 * Get current acquired balance from contract for a specific module
 * This is the source of truth for what's currently available
 */
const getContractAcquiredBalanceForModule = (
	runtime: Runtime<Config>,
	moduleAddress: Address,
	subAccount: Address,
	token: Address,
): bigint => {
	const evmClient = createEvmClient(runtime)

	const callData = encodeFunctionData({
		abi: DeFiInteractorModule,
		functionName: 'getAcquiredBalance',
		args: [subAccount, token],
	})

	try {
		const result = evmClient
			.callContract(runtime, {
				call: encodeCallMsg({
					from: zeroAddress,
					to: moduleAddress,
					data: callData,
				}),
				blockNumber: LAST_FINALIZED_BLOCK_NUMBER,
			})
			.result()

		if (!result.data || result.data.length === 0) {
			return 0n
		}

		return decodeFunctionResult({
			abi: DeFiInteractorModule,
			functionName: 'getAcquiredBalance',
			data: bytesToHex(result.data),
		})
	} catch (error) {
		runtime.log(`Error getting acquired balance for module ${moduleAddress}: ${error}`)
		return 0n
	}
}


const getSubAccountLimitsForModule = (
	runtime: Runtime<Config>,
	moduleAddress: Address,
	subAccount: Address,
): { maxSpendingBps: bigint; windowDuration: bigint } => {
	const evmClient = createEvmClient(runtime)

	const callData = encodeFunctionData({
		abi: DeFiInteractorModule,
		functionName: 'getSubAccountLimits',
		args: [subAccount],
	})

	try {
		const result = evmClient
			.callContract(runtime, {
				call: encodeCallMsg({
					from: zeroAddress,
					to: moduleAddress,
					data: callData,
				}),
				blockNumber: LAST_FINALIZED_BLOCK_NUMBER,
			})
			.result()

		if (!result.data || result.data.length === 0) {
			return { maxSpendingBps: 500n, windowDuration: 86400n }
		}

		const [maxSpendingBps, windowDuration] = decodeFunctionResult({
			abi: DeFiInteractorModule,
			functionName: 'getSubAccountLimits',
			data: bytesToHex(result.data),
		})

		return { maxSpendingBps, windowDuration }
	} catch (error) {
		runtime.log(`Error getting subaccount limits: ${error}`)
		return { maxSpendingBps: 500n, windowDuration: 86400n }
	}
}


/**
 * Get Safe's total USD value from contract for a specific module
 */
const getSafeValueForModule = (runtime: Runtime<Config>, moduleAddress: Address): bigint => {
	const evmClient = createEvmClient(runtime)

	const callData = encodeFunctionData({
		abi: DeFiInteractorModule,
		functionName: 'getSafeValue',
	})

	try {
		const result = evmClient
			.callContract(runtime, {
				call: encodeCallMsg({
					from: zeroAddress,
					to: moduleAddress,
					data: callData,
				}),
				blockNumber: LAST_FINALIZED_BLOCK_NUMBER,
			})
			.result()

		if (!result.data || result.data.length === 0) {
			return 0n
		}

		const [totalValueUSD] = decodeFunctionResult({
			abi: DeFiInteractorModule,
			functionName: 'getSafeValue',
			data: bytesToHex(result.data),
		})

		return totalValueUSD
	} catch (error) {
		runtime.log(`Error getting safe value for module ${moduleAddress}: ${error}`)
		return 0n
	}
}


/**
 * Get all active modules from the registry
 * Returns array of module addresses, or falls back to single moduleAddress if no registry
 */
const getActiveModulesFromRegistry = (runtime: Runtime<Config>): Address[] => {
	// If no registry configured, use single module address (backwards compatibility)
	if (!runtime.config.registryAddress) {
		return [runtime.config.moduleAddress as Address]
	}

	const evmClient = createEvmClient(runtime)

	const callData = encodeFunctionData({
		abi: ModuleRegistry,
		functionName: 'getActiveModules',
	})

	try {
		const result = evmClient
			.callContract(runtime, {
				call: encodeCallMsg({
					from: zeroAddress,
					to: runtime.config.registryAddress as Address,
					data: callData,
				}),
				blockNumber: LAST_FINALIZED_BLOCK_NUMBER,
			})
			.result()

		if (!result.data || result.data.length === 0) {
			runtime.log('No active modules found in registry, falling back to moduleAddress')
			return [runtime.config.moduleAddress as Address]
		}

		const modules = decodeFunctionResult({
			abi: ModuleRegistry,
			functionName: 'getActiveModules',
			data: bytesToHex(result.data),
		}) as Address[]

		runtime.log(`Found ${modules.length} active modules in registry`)
		return modules
	} catch (error) {
		runtime.log(`Error querying registry: ${error}, falling back to moduleAddress`)
		return [runtime.config.moduleAddress as Address]
	}
}

/**
 * Get all subaccounts with DEFI_EXECUTE_ROLE for a specific module
 */
const getActiveSubaccountsForModule = (runtime: Runtime<Config>, moduleAddress: Address): Address[] => {
	const evmClient = createEvmClient(runtime)

	// DEFI_EXECUTE_ROLE = 1
	const callData = encodeFunctionData({
		abi: DeFiInteractorModule,
		functionName: 'getSubaccountsByRole',
		args: [1], // DEFI_EXECUTE_ROLE
	})

	try {
		const result = evmClient
			.callContract(runtime, {
				call: encodeCallMsg({
					from: zeroAddress,
					to: moduleAddress,
					data: callData,
				}),
				blockNumber: LAST_FINALIZED_BLOCK_NUMBER,
			})
			.result()

		if (!result.data || result.data.length === 0) {
			return []
		}

		return decodeFunctionResult({
			abi: DeFiInteractorModule,
			functionName: 'getSubaccountsByRole',
			data: bytesToHex(result.data),
		}) as Address[]
	} catch (error) {
		runtime.log(`Error getting subaccounts for module ${moduleAddress}: ${error}`)
		return []
	}
}


/**
 * Convert SDK BigInt (Uint8Array absVal) to native bigint
 */
const sdkBigIntToBigInt = (sdkBigInt: { absVal: Uint8Array; sign: bigint }): bigint => {
	// absVal is big-endian bytes representing the absolute value
	let result = 0n
	for (const byte of sdkBigInt.absVal) {
		result = (result << 8n) | BigInt(byte)
	}
	// Apply sign (negative if sign < 0)
	return sdkBigInt.sign < 0n ? -result : result
}

/**
 * Get current finalized block number from the chain
 */
const getCurrentBlockNumber = (runtime: Runtime<Config>): bigint => {
	const evmClient = createEvmClient(runtime)

	try {
		const headerResult = evmClient
			.headerByNumber(runtime, {
				blockNumber: LAST_FINALIZED_BLOCK_NUMBER,
			})
			.result()

		if (headerResult.header?.blockNumber) {
			// blockNumber is SDK BigInt type with Uint8Array absVal
			return sdkBigIntToBigInt(headerResult.header.blockNumber)
		}
		return 0n
	} catch (error) {
		runtime.log(`Error getting current block number: ${error}`)
		return 0n
	}
}

/**
 * Convert address to padded 32-byte hex string for topic filtering
 */
const addressToTopicBytes = (address: Address): string => {
	return '0x' + address.slice(2).toLowerCase().padStart(64, '0')
}

/**
 * Get block timestamp from block number
 */
const getBlockTimestamp = (
	runtime: Runtime<Config>,
	blockNumber: bigint,
): bigint => {
	const evmClient = createEvmClient(runtime)

	try {
		const headerResult = evmClient
			.headerByNumber(runtime, {
				blockNumber: { absVal: blockNumber.toString(), sign: '' },
			})
			.result()

		if (headerResult.header?.timestamp) {
			return sdkBigIntToBigInt(headerResult.header.timestamp)
		}
		// Fallback to current time if header fetch fails
		runtime.log(`Warning: Could not get timestamp for block ${blockNumber}, using current time`)
		return BigInt(Math.floor(Date.now() / 1000))
	} catch (error) {
		runtime.log(`Error getting block timestamp for ${blockNumber}: ${error}`)
		return BigInt(Math.floor(Date.now() / 1000))
	}
}

/**
 * Batch fetch block timestamps for multiple blocks
 * Returns a map of blockNumber -> timestamp
 */
const getBlockTimestamps = (
	runtime: Runtime<Config>,
	blockNumbers: bigint[],
): Map<bigint, bigint> => {
	const timestamps = new Map<bigint, bigint>()
	const uniqueBlocks = [...new Set(blockNumbers)]

	for (const blockNum of uniqueBlocks) {
		const timestamp = getBlockTimestamp(runtime, blockNum)
		timestamps.set(blockNum, timestamp)
	}

	return timestamps
}

/**
 * Query historical ProtocolExecution events for a specific module
 */
const queryHistoricalEventsForModule = (
	runtime: Runtime<Config>,
	moduleAddress: Address,
	subAccount?: Address,
): ProtocolExecutionEvent[] => {
	const evmClient = createEvmClient(runtime)
	const events: ProtocolExecutionEvent[] = []

	runtime.log(`Querying historical events for module ${moduleAddress} (last ${runtime.config.blocksToLookBack * 2} blocks)...`)

	try {
		const currentBlock = getCurrentBlockNumber(runtime)
		if (currentBlock === 0n) {
			runtime.log('Could not determine current block number')
			return events
		}

		const fromBlock = currentBlock - BigInt(runtime.config.blocksToLookBack * 2)
		runtime.log(`Block range: ${fromBlock} to ${currentBlock}`)

		const topics: Array<{ topic: string[] }> = [
			{ topic: [PROTOCOL_EXECUTION_EVENT_SIG] },
		]

		if (subAccount) {
			topics.push({ topic: [addressToTopicBytes(subAccount)] })
		}

		const logsResult = evmClient
			.filterLogs(runtime, {
				filterQuery: {
					addresses: [moduleAddress],
					topics: topics,
					fromBlock: { absVal: fromBlock.toString(), sign: '' },
					toBlock: { absVal: currentBlock.toString(), sign: '' },
				},
			})
			.result()

		if (!logsResult.logs || logsResult.logs.length === 0) {
			runtime.log('No historical events found')
			return events
		}

		runtime.log(`Found ${logsResult.logs.length} historical events`)

		const parsedEvents: Array<{ log: any; event: ProtocolExecutionEvent }> = []
		for (const log of logsResult.logs) {
			try {
				const event = parseProtocolExecutionEvent(log)
				parsedEvents.push({ log, event })
			} catch (error) {
				runtime.log(`Error parsing event: ${error}`)
			}
		}

		const blockNumbers = parsedEvents.map(p => p.event.blockNumber)
		const blockTimestamps = getBlockTimestamps(runtime, blockNumbers)

		for (const { event } of parsedEvents) {
			const actualTimestamp = blockTimestamps.get(event.blockNumber)
			if (actualTimestamp) {
				event.timestamp = actualTimestamp
			}
			events.push(event)
		}
	} catch (error) {
		runtime.log(`Error querying historical events: ${error}`)
	}

	return events
}


/**
 * Convert Uint8Array to hex string
 */
const uint8ArrayToHex = (arr: Uint8Array): `0x${string}` => {
	return ('0x' + Array.from(arr).map(b => b.toString(16).padStart(2, '0')).join('')) as `0x${string}`
}

/**
 * Extract address from 32-byte topic (last 20 bytes)
 */
const topicToAddress = (topic: Uint8Array | string): Address => {
	if (typeof topic === 'string') {
		// Handle string format (hex)
		return ('0x' + topic.slice(-40)) as Address
	}
	// Handle Uint8Array format (take last 20 bytes)
	const addressBytes = topic.slice(-20)
	return uint8ArrayToHex(addressBytes) as Address
}

/**
 * Parse ProtocolExecution event from log data
 * Handles both SDK Log type (Uint8Array) and JSON format (string)
 * Event format: ProtocolExecution(address indexed subAccount, address indexed target, uint8 opType, address tokenIn, uint256 amountIn, address[] tokensOut, uint256[] amountsOut, uint256 spendingCost)
 */
const parseProtocolExecutionEvent = (log: any): ProtocolExecutionEvent => {
	// Handle topics - SDK returns Uint8Array[], may also be string[]
	const topic1 = log.topics[1]
	const topic2 = log.topics[2]
	const subAccount = topicToAddress(topic1)
	const target = topicToAddress(topic2)

	// Handle data - SDK returns Uint8Array, may also be string
	const data = typeof log.data === 'string'
		? log.data as `0x${string}`
		: uint8ArrayToHex(log.data)

	// Decode the non-indexed parameters including arrays
	const decoded = decodeAbiParameters(
		[
			{ name: 'opType', type: 'uint8' },
			{ name: 'tokensIn', type: 'address[]' },
			{ name: 'amountsIn', type: 'uint256[]' },
			{ name: 'tokensOut', type: 'address[]' },
			{ name: 'amountsOut', type: 'uint256[]' },
			{ name: 'spendingCost', type: 'uint256' },
		],
		data,
	)

	// Handle blockNumber - SDK returns BigInt type with Uint8Array absVal
	let blockNumber = 0n
	if (log.blockNumber) {
		if (typeof log.blockNumber === 'bigint' || typeof log.blockNumber === 'number') {
			blockNumber = BigInt(log.blockNumber)
		} else if (log.blockNumber.absVal) {
			blockNumber = sdkBigIntToBigInt(log.blockNumber)
		}
	}

	// Handle logIndex - SDK may return number, BigInt, or SDK BigInt type
	let logIndex = 0
	if (log.logIndex !== undefined) {
		if (typeof log.logIndex === 'number') {
			logIndex = log.logIndex
		} else if (typeof log.logIndex === 'bigint') {
			logIndex = Number(log.logIndex)
		} else if (log.logIndex.absVal) {
			logIndex = Number(sdkBigIntToBigInt(log.logIndex))
		}
	}

	// Convert decoded arrays to proper types
	const tokensIn = (decoded[1] as readonly `0x${string}`[]).map(t => t as Address)
	const amountsIn = decoded[2] as readonly bigint[]
	const tokensOut = (decoded[3] as readonly `0x${string}`[]).map(t => t as Address)
	const amountsOut = decoded[4] as readonly bigint[]

	return {
		subAccount,
		target,
		opType: decoded[0] as OperationType,
		tokensIn: [...tokensIn],
		amountsIn: [...amountsIn],
		tokensOut: [...tokensOut],
		amountsOut: [...amountsOut],
		spendingCost: decoded[5],
		// Timestamp will be set from block header after parsing
		// Initialize with 0 to indicate it needs to be fetched
		timestamp: 0n,
		blockNumber,
		logIndex,
	}
}

/**
 * Parse TransferExecuted event from log data
 * Event: TransferExecuted(address indexed subAccount, address indexed token, address indexed recipient, uint256 amount, uint256 spendingCost)
 */
const parseTransferExecutedEvent = (log: any): TransferExecutedEvent => {
	// All 3 parameters are indexed (topics[1], topics[2], topics[3])
	const topic1 = log.topics[1]
	const topic2 = log.topics[2]
	const topic3 = log.topics[3]
	const subAccount = topicToAddress(topic1)
	const token = topicToAddress(topic2)
	const recipient = topicToAddress(topic3)

	// Handle data - contains amount, spendingCost (no timestamp)
	const data = typeof log.data === 'string'
		? log.data as `0x${string}`
		: uint8ArrayToHex(log.data)

	const decoded = decodeAbiParameters(
		[
			{ name: 'amount', type: 'uint256' },
			{ name: 'spendingCost', type: 'uint256' },
		],
		data,
	)

	// Handle blockNumber
	let blockNumber = 0n
	if (log.blockNumber) {
		if (typeof log.blockNumber === 'bigint' || typeof log.blockNumber === 'number') {
			blockNumber = BigInt(log.blockNumber)
		} else if (log.blockNumber.absVal) {
			blockNumber = sdkBigIntToBigInt(log.blockNumber)
		}
	}

	// Handle logIndex
	let logIndex = 0
	if (log.logIndex !== undefined) {
		if (typeof log.logIndex === 'number') {
			logIndex = log.logIndex
		} else if (typeof log.logIndex === 'bigint') {
			logIndex = Number(log.logIndex)
		} else if (log.logIndex.absVal) {
			logIndex = Number(sdkBigIntToBigInt(log.logIndex))
		}
	}

	return {
		subAccount,
		token,
		recipient,
		amount: decoded[0],
		spendingCost: decoded[1],
		// Timestamp will be set from block header after parsing
		// Initialize with 0 to indicate it needs to be fetched
		timestamp: 0n,
		blockNumber,
		logIndex,
	}
}

/**
 * Query historical TransferExecuted events for a specific module
 */
const queryTransferEventsForModule = (
	runtime: Runtime<Config>,
	moduleAddress: Address,
	subAccount?: Address,
): TransferExecutedEvent[] => {
	const evmClient = createEvmClient(runtime)
	const events: TransferExecutedEvent[] = []

	runtime.log(`Querying transfer events for module ${moduleAddress} (last ${runtime.config.blocksToLookBack * 2} blocks)...`)

	try {
		const currentBlock = getCurrentBlockNumber(runtime)
		if (currentBlock === 0n) {
			runtime.log('Could not determine current block number')
			return events
		}

		const fromBlock = currentBlock - BigInt(runtime.config.blocksToLookBack * 2)

		const topics: Array<{ topic: string[] }> = [
			{ topic: [TRANSFER_EXECUTED_EVENT_SIG] },
		]

		if (subAccount) {
			topics.push({ topic: [addressToTopicBytes(subAccount)] })
		}

		const logsResult = evmClient
			.filterLogs(runtime, {
				filterQuery: {
					addresses: [moduleAddress],
					topics: topics,
					fromBlock: { absVal: fromBlock.toString(), sign: '' },
					toBlock: { absVal: currentBlock.toString(), sign: '' },
				},
			})
			.result()

		if (!logsResult.logs || logsResult.logs.length === 0) {
			runtime.log('No transfer events found')
			return events
		}

		runtime.log(`Found ${logsResult.logs.length} transfer events`)

		const parsedEvents: Array<{ log: any; event: TransferExecutedEvent }> = []
		for (const log of logsResult.logs) {
			try {
				const event = parseTransferExecutedEvent(log)
				parsedEvents.push({ log, event })
			} catch (error) {
				runtime.log(`Error parsing transfer event: ${error}`)
			}
		}

		const blockNumbers = parsedEvents.map(p => p.event.blockNumber)
		const blockTimestamps = getBlockTimestamps(runtime, blockNumbers)

		for (const { event } of parsedEvents) {
			const actualTimestamp = blockTimestamps.get(event.blockNumber)
			if (actualTimestamp) {
				event.timestamp = actualTimestamp
			}
			events.push(event)
		}
	} catch (error) {
		runtime.log(`Error querying transfer events: ${error}`)
	}

	return events
}


/**
 * Query historical AcquiredBalanceUpdated events for a specific module to find all tokens
 * that have ever had acquired balance set for a subaccount.
 * This is used to detect and clear stale on-chain balances.
 */
const queryHistoricalAcquiredTokensForModule = (
	runtime: Runtime<Config>,
	moduleAddress: Address,
	subAccount: Address,
): Set<Address> => {
	const tokens = new Set<Address>()
	const evmClient = createEvmClient(runtime)

	try {
		// Query from a reasonable lookback - use extended range to catch all historical tokens
		const currentBlock = getCurrentBlockNumber(runtime)
		const fromBlock = currentBlock - BigInt(runtime.config.blocksToLookBack * 2)

		const topics: Array<{ topic: string[] }> = [
			{ topic: [ACQUIRED_BALANCE_UPDATED_EVENT_SIG] },
			{ topic: [addressToTopicBytes(subAccount)] },
		]

		const logsResult = evmClient
			.filterLogs(runtime, {
				filterQuery: {
					addresses: [moduleAddress],
					topics: topics,
					fromBlock: { absVal: fromBlock.toString(), sign: '' },
					toBlock: { absVal: currentBlock.toString(), sign: '' },
				},
			})
			.result()

		if (logsResult.logs && logsResult.logs.length > 0) {
			for (const log of logsResult.logs) {
				// topics[2] is the token address (indexed)
				const topic2 = log.topics[2]
				const token = topicToAddress(topic2)
				if (token) {
					tokens.add(token.toLowerCase() as Address)
				}
			}
		}
	} catch (error) {
		runtime.log(`Error querying historical acquired tokens for module ${moduleAddress}: ${error}`)
	}

	return tokens
}


// ============ FIFO Queue Helpers ============

/**
 * Consume tokens from a FIFO queue (oldest first)
 * Returns the entries consumed with their original timestamps
 * Only consumes non-expired entries based on the event timestamp
 */
const consumeFromQueue = (
	queue: AcquiredBalanceQueue,
	amount: bigint,
	eventTimestamp: bigint,
	windowDuration: bigint
): { consumed: AcquiredBalanceEntry[]; remaining: bigint } => {
	const consumed: AcquiredBalanceEntry[] = []
	let remaining = amount
	const expiryThreshold = eventTimestamp - windowDuration

	while (remaining > 0n && queue.length > 0) {
		const entry = queue[0]

		// Skip expired entries (they shouldn't be consumed as acquired)
		if (entry.originalTimestamp < expiryThreshold) {
			queue.shift()
			continue
		}

		if (entry.amount <= remaining) {
			// Consume entire entry
			consumed.push({ ...entry })
			remaining -= entry.amount
			queue.shift()
		} else {
			// Partial consumption
			consumed.push({ amount: remaining, originalTimestamp: entry.originalTimestamp })
			entry.amount -= remaining
			remaining = 0n
		}
	}

	return { consumed, remaining }
}

/**
 * Add tokens to a FIFO queue with the given original timestamp
 */
const addToQueue = (
	queue: AcquiredBalanceQueue,
	amount: bigint,
	originalTimestamp: bigint
): void => {
	if (amount <= 0n) return
	queue.push({ amount, originalTimestamp })
}

/**
 * Get total amount in queue that hasn't expired
 */
const getValidQueueBalance = (
	queue: AcquiredBalanceQueue,
	currentTimestamp: bigint,
	windowDuration: bigint
): bigint => {
	const expiryThreshold = currentTimestamp - windowDuration
	let total = 0n
	for (const entry of queue) {
		if (entry.originalTimestamp >= expiryThreshold) {
			total += entry.amount
		}
	}
	return total
}

/**
 * Remove expired entries from queue
 */
const pruneExpiredEntries = (
	queue: AcquiredBalanceQueue,
	currentTimestamp: bigint,
	windowDuration: bigint
): void => {
	const expiryThreshold = currentTimestamp - windowDuration
	while (queue.length > 0 && queue[0].originalTimestamp < expiryThreshold) {
		queue.shift()
	}
}

// ============ State Building ============

// Unified event type for chronological processing
type UnifiedEvent =
	| { type: 'protocol'; event: ProtocolExecutionEvent }
	| { type: 'transfer'; event: TransferExecutedEvent }

/**
 * Build state for a subaccount from historical events using FIFO queue model
 *
 * Key Design:
 * - FIFO queues track (amount, originalTimestamp) for each token
 * - When swapping/depositing, consumed acquired tokens' timestamps are inherited
 * - Mixed acquired/non-acquired inputs are proportionally split in outputs (USD-weighted if priceCache available)
 * - Deposits store originalAcquisitionTimestamp so withdrawals inherit correctly
 * - Deposits also track output tokens (e.g., aToken, LP token) for proper withdrawal matching
 */
const buildSubAccountState = (
	runtime: Runtime<Config>,
	events: ProtocolExecutionEvent[],
	transferEvents: TransferExecutedEvent[],
	subAccount: Address,
	currentTimestamp: bigint,
	subAccountWindowDuration?: bigint,
	priceCache?: TokenPriceCache,
): SubAccountState => {
	// Use per-subaccount window duration if provided, otherwise fall back to config
	const windowDuration = subAccountWindowDuration ?? BigInt(runtime.config.windowDurationSeconds)
	const windowStart = currentTimestamp - windowDuration

	const state: SubAccountState = {
		spendingRecords: [],
		depositRecords: [],
		totalSpendingInWindow: 0n,
		acquiredQueues: new Map(),
		acquiredBalances: new Map(),
	}

	// Filter events for this subaccount
	const filteredProtocol = events
		.filter(e => e.subAccount.toLowerCase() === subAccount.toLowerCase())

	const filteredTransfers = transferEvents
		.filter(e => e.subAccount.toLowerCase() === subAccount.toLowerCase())

	// Merge into unified event list and sort chronologically
	// This ensures transfers are processed in correct order relative to protocol events
	const unifiedEvents: UnifiedEvent[] = [
		...filteredProtocol.map(e => ({ type: 'protocol' as const, event: e })),
		...filteredTransfers.map(e => ({ type: 'transfer' as const, event: e })),
	].sort((a, b) => {
		const timestampDiff = Number(a.event.timestamp - b.event.timestamp)
		if (timestampDiff !== 0) return timestampDiff
		// Same timestamp: sort by block number, then log index
		const blockDiff = Number(a.event.blockNumber - b.event.blockNumber)
		if (blockDiff !== 0) return blockDiff
		return a.event.logIndex - b.event.logIndex
	})

	runtime.log(`Processing ${unifiedEvents.length} events for ${subAccount} (FIFO mode, ${filteredProtocol.length} protocol + ${filteredTransfers.length} transfers)`)

	// Track all tokens that ever had acquired balance (for cleanup)
	const tokensWithAcquiredHistory = new Set<Address>()

	// FIFO queues per token - tracks (amount, originalTimestamp)
	const acquiredQueues: Map<Address, AcquiredBalanceQueue> = new Map()

	// Helper to get or create queue
	const getQueue = (token: Address): AcquiredBalanceQueue => {
		const lower = token.toLowerCase() as Address
		if (!acquiredQueues.has(lower)) {
			acquiredQueues.set(lower, [])
		}
		return acquiredQueues.get(lower)!
	}

	// Process ALL events chronologically (unified protocol + transfer events)
	for (const unified of unifiedEvents) {
		if (unified.type === 'protocol') {
			const event = unified.event
			const isInWindow = event.timestamp >= windowStart

			// Track spending (only count if in window)
			if (event.opType === OperationType.SWAP || event.opType === OperationType.DEPOSIT) {
				if (isInWindow && event.spendingCost > 0n) {
					state.spendingRecords.push({
						amount: event.spendingCost,
						timestamp: event.timestamp,
					})
					state.totalSpendingInWindow += event.spendingCost
				}
			}

			// Handle input token consumption (FIFO) - do this before creating deposit record
			// so we can capture the original acquisition timestamp for deposits
			// Use event timestamp to determine expiry - tokens must be valid at the time of the event
			// NOTE: Now handles multiple input tokens (e.g., LP position minting uses 2 tokens)
			let consumedEntries: AcquiredBalanceEntry[] = []
			let totalAmountIn = 0n
			let totalValueInUSD = 0n       // USD value of all inputs (for weighted ratio)
			let consumedValueUSD = 0n      // USD value of consumed acquired tokens
			let hasAllPrices = true        // Whether we have prices for all input tokens
			if (event.opType === OperationType.SWAP || event.opType === OperationType.DEPOSIT) {
				// Process each input token
				for (let i = 0; i < event.tokensIn.length; i++) {
					const tokenIn = event.tokensIn[i]
					const amountIn = event.amountsIn[i]
					if (amountIn <= 0n) continue

					totalAmountIn += amountIn
					const tokenInLower = tokenIn.toLowerCase() as Address
					const inputQueue = getQueue(tokenInLower)
					const result = consumeFromQueue(inputQueue, amountIn, event.timestamp, windowDuration)
					consumedEntries.push(...result.consumed)
					tokensWithAcquiredHistory.add(tokenInLower)

					// Track USD values for weighted ratio calculation
					if (priceCache) {
						const inputValueUSD = getTokenValueUSD(tokenIn, amountIn, priceCache)
						if (inputValueUSD !== null) {
							totalValueInUSD += inputValueUSD
							// Calculate USD value of consumed portion for this token
							const consumedAmount = result.consumed.reduce((sum, e) => sum + e.amount, 0n)
							const consumedTokenValueUSD = getTokenValueUSD(tokenIn, consumedAmount, priceCache)
							if (consumedTokenValueUSD !== null) {
								consumedValueUSD += consumedTokenValueUSD
							}
						} else {
							hasAllPrices = false
						}
					}
				}
			}

			// Track deposits for withdrawal matching
			// Store the original acquisition timestamp so withdrawals inherit it correctly
			// For multi-token deposits (LP), create a record for each input/output token pair
			if (event.opType === OperationType.DEPOSIT) {
				// Find the oldest original timestamp from consumed acquired tokens
				// If no acquired tokens were consumed, use the deposit timestamp (it's new spending)
				let originalAcquisitionTimestamp = event.timestamp
				if (consumedEntries.length > 0) {
					originalAcquisitionTimestamp = consumedEntries.reduce(
						(oldest, entry) => entry.originalTimestamp < oldest ? entry.originalTimestamp : oldest,
						consumedEntries[0].originalTimestamp
					)
					runtime.log(`  DEPOSIT: storing original acquisition timestamp ${originalAcquisitionTimestamp} for future withdrawal`)
				}

				// Create a deposit record linking input token to output token
				// This allows us to consume the output token (e.g., aLINK) when withdrawing the input token (LINK)

				// For multi-token LP deposits (N inputs → 1 output), we need to divide the output
				// proportionally among input tokens to avoid double-counting remainingOutputAmount
				const validInputCount = event.tokensIn.filter((_, i) => event.amountsIn[i] > 0n).length
				const validOutputCount = event.tokensOut.filter((_, i) => event.amountsOut[i] > 0n).length
				const isMultiInputSingleOutput = validInputCount > 1 && event.tokensOut.length === 1
				const isSingleInputMultiOutput = validInputCount === 1 && validOutputCount > 1

				if (isSingleInputMultiOutput) {
					// Single input → multiple outputs: create a deposit record for each output
					// Divide the input amount equally among output records
					const tokenIn = event.tokensIn.find((_, i) => event.amountsIn[i] > 0n)!
					const amountIn = event.amountsIn.find(a => a > 0n)!
					const inputSharePerOutput = amountIn / BigInt(validOutputCount)

					for (let i = 0; i < event.tokensOut.length; i++) {
						const tokenOut = event.tokensOut[i]
						const amountOut = event.amountsOut[i]
						if (amountOut <= 0n) continue

						runtime.log(`  DEPOSIT: single-input multi-output detected, allocating ${inputSharePerOutput} ${tokenIn} to ${tokenOut} output (1/${validOutputCount} share)`)

						state.depositRecords.push({
							subAccount: event.subAccount,
							target: event.target,
							tokenIn: tokenIn,
							amountIn: inputSharePerOutput, // Each output gets proportional share of input
							remainingAmount: inputSharePerOutput,
							tokenOut: tokenOut,
							amountOut: amountOut,
							remainingOutputAmount: amountOut,
							timestamp: event.timestamp,
							originalAcquisitionTimestamp,
						})
					}
				} else {
					// Standard case: loop over inputs
					for (let i = 0; i < event.tokensIn.length; i++) {
						const tokenIn = event.tokensIn[i]
						const amountIn = event.amountsIn[i]
						if (amountIn <= 0n) continue

						// Find corresponding output token (same index if available, otherwise first output)
						const tokenOut = event.tokensOut[i] || event.tokensOut[0] || ('0x' as Address)
						let amountOut = event.amountsOut[i] || event.amountsOut[0] || 0n

						// For multi-input → single-output LP deposits, divide output equally among inputs
						// This prevents double-counting: if 2 tokens deposit into 1 LP, each record gets 50% of LP
						if (isMultiInputSingleOutput && amountOut > 0n) {
							amountOut = amountOut / BigInt(validInputCount)
							runtime.log(`  DEPOSIT: multi-input LP detected, allocating ${amountOut} ${tokenOut} to ${tokenIn} input (1/${validInputCount} share)`)
						}

						state.depositRecords.push({
							subAccount: event.subAccount,
							target: event.target,
							tokenIn: tokenIn,
							amountIn: amountIn,
							remainingAmount: amountIn,
							tokenOut: tokenOut,
							amountOut: amountOut,
							remainingOutputAmount: amountOut,
							timestamp: event.timestamp,
							originalAcquisitionTimestamp,
						})
					}
				}
			}

			// Handle output tokens (add to acquired queue) - iterate over tokensOut/amountsOut arrays
			// For SWAPs and DEPOSITs: proportionally split output between acquired (inherited timestamp) and new (current timestamp)
			// Uses USD-weighted ratios when priceCache is available for accurate multi-token handling
			// For WITHDRAW/CLAIM: output matched to deposits inherits their original acquisition timestamp

			if (event.opType === OperationType.SWAP || event.opType === OperationType.DEPOSIT) {
				// Process all output tokens in the array
				for (let i = 0; i < event.tokensOut.length; i++) {
					const tokenOut = event.tokensOut[i]
					const amountOut = event.amountsOut[i]
					if (amountOut <= 0n) continue

					const tokenOutLower = tokenOut.toLowerCase() as Address
					tokensWithAcquiredHistory.add(tokenOutLower)
					const outputQueue = getQueue(tokenOutLower)

					// Calculate how much of the input was acquired vs non-acquired
					const totalConsumed = consumedEntries.reduce((sum, e) => sum + e.amount, 0n)
					const fromNonAcquired = totalAmountIn - totalConsumed // Remaining came from original funds

					if (totalConsumed > 0n && fromNonAcquired > 0n) {
						// Mixed case: proportionally split the output
						// Acquired portion inherits timestamps proportionally, non-acquired portion is newly acquired

						// Use USD-weighted ratio if we have prices for all input tokens
						// This correctly handles multi-token inputs with different values (e.g., 1 WETH + 1000 USDC)
						let acquiredRatio: bigint
						let useUSDWeighting = false

						if (priceCache && hasAllPrices && totalValueInUSD > 0n) {
							// USD-weighted ratio: based on actual value, not raw amounts
							acquiredRatio = (consumedValueUSD * 10000n) / totalValueInUSD
							useUSDWeighting = true
						} else {
							// Fallback: amount-weighted ratio (original behavior)
							acquiredRatio = (totalConsumed * 10000n) / totalAmountIn
						}

						const outputFromAcquired = (amountOut * acquiredRatio) / 10000n
						const outputFromNonAcquired = amountOut - outputFromAcquired

						const opName = OperationType[event.opType]
						if (useUSDWeighting) {
							runtime.log(`  ${opName}: mixed input (USD-weighted) - ${consumedValueUSD} acquired + ${totalValueInUSD - consumedValueUSD} non-acquired (USD)`)
						} else {
							runtime.log(`  ${opName}: mixed input - ${totalConsumed} acquired + ${fromNonAcquired} non-acquired`)
						}

						// Proportionally split the acquired output among consumed entries by their amounts
						// Each consumed entry's portion of the output inherits that entry's timestamp
						for (const entry of consumedEntries) {
							const entryRatio = (entry.amount * 10000n) / totalConsumed
							const entryOutput = (outputFromAcquired * entryRatio) / 10000n
							if (entryOutput > 0n) {
								runtime.log(`    ${entryOutput} ${tokenOut} inherits timestamp ${entry.originalTimestamp}`)
								addToQueue(outputQueue, entryOutput, entry.originalTimestamp)
							}
						}

						runtime.log(`    ${outputFromNonAcquired} ${tokenOut} newly acquired at ${event.timestamp}`)
						addToQueue(outputQueue, outputFromNonAcquired, event.timestamp)
					} else if (totalConsumed > 0n) {
						// Entire input was acquired - output inherits timestamps proportionally from consumed entries
						const opName = OperationType[event.opType]

						// Proportionally split the output among consumed entries by their amounts
						// Each consumed entry's portion of the output inherits that entry's timestamp
						for (const entry of consumedEntries) {
							const entryRatio = (entry.amount * 10000n) / totalConsumed
							const entryOutput = (amountOut * entryRatio) / 10000n
							if (entryOutput > 0n) {
								runtime.log(`  ${opName}: ${entryOutput} ${tokenOut} inherits timestamp ${entry.originalTimestamp}`)
								addToQueue(outputQueue, entryOutput, entry.originalTimestamp)
							}
						}
					} else {
						// No acquired input - output is newly acquired
						const opName = OperationType[event.opType]
						runtime.log(`  ${opName}: ${amountOut} ${tokenOut} is newly acquired at ${event.timestamp}`)
						addToQueue(outputQueue, amountOut, event.timestamp)
					}
				}
			} else if (event.opType === OperationType.WITHDRAW || event.opType === OperationType.CLAIM) {
				// Process all output tokens in the array
				for (let i = 0; i < event.tokensOut.length; i++) {
					const tokenOut = event.tokensOut[i]
					const amountOut = event.amountsOut[i]
					if (amountOut <= 0n) continue

					const tokenOutLower = tokenOut.toLowerCase() as Address

					// Find matching deposits
					let remainingToMatch = amountOut

					// Track output tokens to consume from acquired queue (e.g., aLINK when withdrawing LINK)
					// We track the deposit reference so we can update remainingOutputAmount after actual queue consumption
					const outputTokensToConsume: { token: Address; amount: bigint; deposit: DepositRecord }[] = []

					// Track matched amounts per timestamp - each deposit portion inherits its own timestamp
					const matchedByTimestamp: { amount: bigint; timestamp: bigint }[] = []

					for (const deposit of state.depositRecords) {
						if (remainingToMatch <= 0n) break

						if (deposit.target.toLowerCase() === event.target.toLowerCase() &&
								deposit.subAccount.toLowerCase() === event.subAccount.toLowerCase() &&
								deposit.tokenIn.toLowerCase() === tokenOutLower &&
								deposit.remainingAmount > 0n) {

							const consumeAmount = remainingToMatch > deposit.remainingAmount
								? deposit.remainingAmount
								: remainingToMatch

							deposit.remainingAmount -= consumeAmount
							remainingToMatch -= consumeAmount

							// Calculate proportional output token consumption (e.g., aLINK)
							// If we're withdrawing 50% of the deposited amount, consume 50% of the output token
							// NOTE: We don't reduce remainingOutputAmount here - we do it after actual queue consumption
							// to handle cases where queue entries have expired
							if (deposit.tokenOut && deposit.tokenOut !== '0x' && deposit.remainingOutputAmount > 0n) {
								const ratio = (consumeAmount * 10000n) / deposit.amountIn
								const outputToConsume = (deposit.amountOut * ratio) / 10000n
								const maxConsume = outputToConsume > deposit.remainingOutputAmount
									? deposit.remainingOutputAmount
									: outputToConsume

								if (maxConsume > 0n) {
									outputTokensToConsume.push({
										token: deposit.tokenOut.toLowerCase() as Address,
										amount: maxConsume,
										deposit: deposit
									})
									runtime.log(`  ${OperationType[event.opType]} will consume up to ${maxConsume} ${deposit.tokenOut} (deposit output token)`)
								}
							}

							// Track this matched portion with its own timestamp (not the oldest across all deposits)
							// This ensures each deposit's portion inherits its correct original acquisition timestamp
							matchedByTimestamp.push({
								amount: consumeAmount,
								timestamp: deposit.originalAcquisitionTimestamp
							})

							runtime.log(`  ${OperationType[event.opType]} consuming ${consumeAmount} from deposit (original acquisition: ${deposit.originalAcquisitionTimestamp})`)
						}
					}

					// Consume the deposit's output tokens (e.g., aLINK) from the acquired queue
					// These tokens were added when depositing and should be removed when withdrawing
					// We update deposit.remainingOutputAmount based on actual consumption (not calculated)
					// to handle cases where queue entries have expired
					for (const { token, amount, deposit } of outputTokensToConsume) {
						const outputTokenQueue = getQueue(token)
						tokensWithAcquiredHistory.add(token)
						const { consumed } = consumeFromQueue(outputTokenQueue, amount, event.timestamp, windowDuration)
						const totalConsumedAmount = consumed.reduce((sum, e) => sum + e.amount, 0n)

						// Update deposit record with actual amount consumed (may be less than requested if expired)
						deposit.remainingOutputAmount -= totalConsumedAmount
						runtime.log(`  ${OperationType[event.opType]} consumed ${totalConsumedAmount} ${token} from acquired queue (deposit receipt token)`)
					}

					// Add each matched portion to the queue with its own inherited timestamp
					// This correctly preserves timestamp granularity from different deposits
					const totalMatched = matchedByTimestamp.reduce((sum, m) => sum + m.amount, 0n)
					if (totalMatched > 0n) {
						tokensWithAcquiredHistory.add(tokenOutLower)
						const outputQueue = getQueue(tokenOutLower)

						for (const { amount, timestamp } of matchedByTimestamp) {
							runtime.log(`  ${OperationType[event.opType]} matched: ${amount} inherits original timestamp ${timestamp}`)
							addToQueue(outputQueue, amount, timestamp)
						}
					}

					// Handle unmatched amount
					if (remainingToMatch > 0n) {
						if (event.opType === OperationType.CLAIM) {
							// CLAIM rewards should only be acquired if there's a matching deposit for this target
							// (i.e., the subaccount created the position that generates rewards)
							const hasMatchingDeposit = state.depositRecords.some(
								d => d.target.toLowerCase() === event.target.toLowerCase() &&
									 d.subAccount.toLowerCase() === event.subAccount.toLowerCase()
							)

							if (hasMatchingDeposit) {
								// Find the oldest deposit timestamp for this target to inherit
								const oldestDepositTimestamp = state.depositRecords
									.filter(d => d.target.toLowerCase() === event.target.toLowerCase() &&
												d.subAccount.toLowerCase() === event.subAccount.toLowerCase())
									.reduce((oldest, d) => d.originalAcquisitionTimestamp < oldest ? d.originalAcquisitionTimestamp : oldest,
											event.timestamp)

								tokensWithAcquiredHistory.add(tokenOutLower)
								const outputQueue = getQueue(tokenOutLower)
								runtime.log(`  CLAIM: ${remainingToMatch} ${tokenOut} is acquired (has deposit at target), inherits timestamp ${oldestDepositTimestamp}`)
								addToQueue(outputQueue, remainingToMatch, oldestDepositTimestamp)
							} else {
								// No matching deposit - claim is from multisig's position, not subaccount's
								runtime.log(`  CLAIM: ${remainingToMatch} ${tokenOut} NOT acquired (no matching deposit from subaccount)`)
							}
						} else {
							// Unmatched WITHDRAW - the LP/receipt tokens weren't acquired by subaccount
							// This means either: external aTokens sent to Safe, or deposit was outside window/by multisig
							// In either case, the withdrawn tokens belong to the multisig, not subaccount
							runtime.log(`  WITHDRAW unmatched: ${remainingToMatch} ${tokenOut} NOT acquired (no matching deposit from subaccount)`)
						}
					}
				}
			}
		} else {
			// Transfer event
			const transfer = unified.event
			const isInWindow = transfer.timestamp >= windowStart
			const tokenLower = transfer.token.toLowerCase() as Address

			if (isInWindow && transfer.spendingCost > 0n) {
				state.spendingRecords.push({
					amount: transfer.spendingCost,
					timestamp: transfer.timestamp,
				})
				state.totalSpendingInWindow += transfer.spendingCost
			}

			if (transfer.amount > 0n) {
				const queue = getQueue(tokenLower)
				consumeFromQueue(queue, transfer.amount, transfer.timestamp, windowDuration)
				tokensWithAcquiredHistory.add(tokenLower)
			}
		}
	}

	// Calculate final acquired balances (only non-expired entries count)
	for (const token of tokensWithAcquiredHistory) {
		const queue = acquiredQueues.get(token) || []

		// Prune expired entries
		pruneExpiredEntries(queue, currentTimestamp, windowDuration)

		// Sum remaining valid balance
		const validBalance = getValidQueueBalance(queue, currentTimestamp, windowDuration)
		// Only store non-zero balances - stale balances are cleared via queryHistoricalAcquiredTokens
		if (validBalance > 0n) {
			state.acquiredBalances.set(token, validBalance)
		}

		runtime.log(`  Token ${token}: acquired balance = ${validBalance}`)
	}

	// Store queues in state for potential debugging
	state.acquiredQueues = acquiredQueues

	runtime.log(`State built: spending=${state.totalSpendingInWindow}, acquired tokens=${state.acquiredBalances.size}`)

	return state
}

/**
 * Calculate new spending allowance for a subaccount on a specific module
 */
const calculateSpendingAllowanceForModule = (
	runtime: Runtime<Config>,
	moduleAddress: Address,
	subAccount: Address,
	state: SubAccountState,
): bigint => {
	const safeValue = getSafeValueForModule(runtime, moduleAddress)
	const { maxSpendingBps } = getSubAccountLimitsForModule(runtime, moduleAddress, subAccount)

	// maxSpending = safeValue * maxSpendingBps / 10000
	const maxSpending = (safeValue * maxSpendingBps) / 10000n

	// newAllowance = maxSpending - spendingUsed
	const newAllowance = maxSpending > state.totalSpendingInWindow
		? maxSpending - state.totalSpendingInWindow
		: 0n

	runtime.log(`Allowance: safeValue=${safeValue}, maxBps=${maxSpendingBps}, max=${maxSpending}, spent=${state.totalSpendingInWindow}, new=${newAllowance}`)

	return newAllowance
}


/**
 * Get current on-chain spending allowance for a specific module
 */
const getOnChainSpendingAllowanceForModule = (
	runtime: Runtime<Config>,
	moduleAddress: Address,
	subAccount: Address,
): bigint => {
	const evmClient = createEvmClient(runtime)

	const callData = encodeFunctionData({
		abi: DeFiInteractorModule,
		functionName: 'getSpendingAllowance',
		args: [subAccount],
	})

	try {
		const result = evmClient
			.callContract(runtime, {
				call: encodeCallMsg({
					from: zeroAddress,
					to: moduleAddress,
					data: callData,
				}),
				blockNumber: LAST_FINALIZED_BLOCK_NUMBER,
			})
			.result()

		if (!result.data || result.data.length === 0) {
			return 0n
		}

		return decodeFunctionResult({
			abi: DeFiInteractorModule,
			functionName: 'getSpendingAllowance',
			data: bytesToHex(result.data),
		})
	} catch (error) {
		runtime.log(`Error getting on-chain spending allowance for module ${moduleAddress}: ${error}`)
		return 0n
	}
}


/**
 * Push batch update to contract for a specific module (skips if no meaningful changes)
 *
 * Update logic:
 * - Always update if acquired balances changed
 * - Always update if allowance went down (any decrease)
 * - Update if allowance went up by more than 2% (ALLOWANCE_INCREASE_THRESHOLD_BPS)
 * - Update if stale for more than 45 minutes (MAX_STALENESS_SECONDS)
 */
const pushBatchUpdateForModule = (
	runtime: Runtime<Config>,
	moduleAddress: Address,
	subAccount: Address,
	newAllowance: bigint,
	acquiredBalances: Map<Address, bigint>,
): string | null => {
	const evmClient = createEvmClient(runtime)

	// Get current on-chain allowance
	const onChainAllowance = getOnChainSpendingAllowanceForModule(runtime, moduleAddress, subAccount)
	const currentTimestamp = getCurrentBlockTimestamp()

	// Check staleness
	const subAccountKey = `${moduleAddress}:${subAccount}`.toLowerCase()
	const lastUpdate = lastUpdateTimestamp.get(subAccountKey) || 0n
	const timeSinceUpdate = currentTimestamp - lastUpdate
	const isStale = timeSinceUpdate > MAX_STALENESS_SECONDS

	// Check allowance direction
	const allowanceDecreased = newAllowance < onChainAllowance
	const allowanceIncreased = newAllowance > onChainAllowance

	// Check if increase exceeds threshold
	// Also consider any increase from 0 as significant
	let significantIncrease = false
	if (allowanceIncreased) {
		if (onChainAllowance > 0n) {
			const increaseAmount = newAllowance - onChainAllowance
			const threshold = (onChainAllowance * ALLOWANCE_INCREASE_THRESHOLD_BPS) / 10000n
			significantIncrease = increaseAmount > threshold
		} else {
			significantIncrease = newAllowance > 0n
		}
	}

	// Determine if allowance update is needed based on rules:
	// 1. Allowance went down (any decrease) -> always update
	// 2. Allowance went up by more than threshold -> update
	// 3. Stale for more than MAX_STALENESS_SECONDS -> update
	const allowanceChanged = allowanceDecreased || significantIncrease || isStale

	if (!allowanceChanged && !allowanceDecreased && !significantIncrease) {
		if (allowanceIncreased) {
			const increaseAmount = newAllowance - onChainAllowance
			const increaseBps = onChainAllowance > 0n ? (increaseAmount * 10000n) / onChainAllowance : 0n
			runtime.log(`  Allowance increase within threshold: ${onChainAllowance} -> ${newAllowance} (+${increaseBps}bps, threshold: ${ALLOWANCE_INCREASE_THRESHOLD_BPS}bps)`)
		}
	}

	// Check if any acquired balances changed
	const tokens: Address[] = []
	const balances: bigint[] = []
	let acquiredChanged = false

	// First, add all tokens from calculated acquired balances
	for (const [token, newBalance] of acquiredBalances) {
		const onChainBalance = getContractAcquiredBalanceForModule(runtime, moduleAddress, subAccount, token)
		if (newBalance !== onChainBalance) {
			acquiredChanged = true
		}
		tokens.push(token)
		balances.push(newBalance)
	}

	// Also check for tokens that have on-chain balance but aren't in calculated map
	// These need to be cleared to 0 (e.g., tokens that aged out or had incorrect matching)
	const historicalTokens = queryHistoricalAcquiredTokensForModule(runtime, moduleAddress, subAccount)
	for (const token of historicalTokens) {
		if (!acquiredBalances.has(token)) {
			const onChainBalance = getContractAcquiredBalanceForModule(runtime, moduleAddress, subAccount, token)
			if (onChainBalance > 0n) {
				runtime.log(`  Clearing stale acquired balance for ${token}: ${onChainBalance} -> 0`)
				acquiredChanged = true
				tokens.push(token)
				balances.push(0n)
			}
		}
	}

	// Skip if no changes needed
	if (!allowanceChanged && !acquiredChanged) {
		runtime.log(`Skipping batch update - no changes needed:`)
		runtime.log(`  Allowance: ${onChainAllowance} -> ${newAllowance} (no decrease, increase <${ALLOWANCE_INCREASE_THRESHOLD_BPS / 100n}%)`)
		runtime.log(`  Staleness: ${timeSinceUpdate}s (max: ${MAX_STALENESS_SECONDS}s)`)
		runtime.log(`  Acquired tokens: ${tokens.length} (no changes)`)
		return null
	}

	// Log reason for update
	const reasons: string[] = []
	if (acquiredChanged) reasons.push('acquired changed')
	if (allowanceDecreased) reasons.push('allowance decreased')
	if (significantIncrease) reasons.push(`allowance increased >${ALLOWANCE_INCREASE_THRESHOLD_BPS / 100n}%`)
	if (isStale) reasons.push(`stale (${timeSinceUpdate}s > ${MAX_STALENESS_SECONDS}s)`)

	runtime.log(`Pushing batch update for module ${moduleAddress}: subAccount=${subAccount}`)
	runtime.log(`  Reason: ${reasons.join(', ')}`)
	runtime.log(`  Allowance: ${onChainAllowance} -> ${newAllowance}`)
	runtime.log(`  Tokens: ${tokens.length}`)

	const callData = encodeFunctionData({
		abi: DeFiInteractorModule,
		functionName: 'batchUpdate',
		args: [subAccount, newAllowance, tokens, balances],
	})

	const reportResponse = runtime
		.report({
			encodedPayload: hexToBase64(callData),
			encoderName: 'evm',
			signingAlgo: 'ecdsa',
			hashingAlgo: 'keccak256',
		})
		.result()

	const resp = evmClient
		.writeReport(runtime, {
			receiver: runtime.config.proxyAddress,
			report: reportResponse,
			gasConfig: {
				gasLimit: runtime.config.gasLimit,
			},
		})
		.result()

	if (resp.txStatus !== TxStatus.SUCCESS) {
		throw new Error(`Failed to push batch update: ${resp.errorMessage || resp.txStatus}`)
	}

	// Update last update timestamp on success
	lastUpdateTimestamp.set(subAccountKey, currentTimestamp)

	const txHash = bytesToHex(resp.txHash || new Uint8Array(32))
	runtime.log(`Batch update complete. TxHash: ${txHash}`)
	return txHash
}


// ============ Event Handler ============

/**
 * Handle ProtocolExecution event
 * Triggered on each new protocol interaction
 * Note: Event trigger only monitors config.moduleAddress, so we use it explicitly
 */
const onProtocolExecution = (runtime: Runtime<Config>, payload: any): string => {
	runtime.log('=== Spending Oracle: ProtocolExecution Event ===')

	const moduleAddress = runtime.config.moduleAddress as Address

	try {
		const log = payload.log
		if (!log || !log.topics || log.topics.length < 3) {
			runtime.log('Invalid event log format')
			return 'Invalid event'
		}

		const newEvent = parseProtocolExecutionEvent(log)

		// Fetch actual block timestamp for the new event
		const actualTimestamp = getBlockTimestamp(runtime, newEvent.blockNumber)
		newEvent.timestamp = actualTimestamp

		const currentTimestamp = getCurrentBlockTimestamp()

		runtime.log(`New event: ${OperationType[newEvent.opType]} by ${newEvent.subAccount}`)
		runtime.log(`  Module: ${moduleAddress}`)
		runtime.log(`  Block: ${newEvent.blockNumber}, Timestamp: ${newEvent.timestamp}`)
		runtime.log(`  TokensIn: [${newEvent.tokensIn.join(', ')}]`)
		runtime.log(`  AmountsIn: [${newEvent.amountsIn.map(a => a.toString()).join(', ')}]`)
		runtime.log(`  TokensOut: [${newEvent.tokensOut.join(', ')}]`)
		runtime.log(`  AmountsOut: [${newEvent.amountsOut.map(a => a.toString()).join(', ')}]`)
		runtime.log(`  SpendingCost: ${newEvent.spendingCost}`)

		// Query historical events (both protocol executions and transfers)
		const historicalEvents = retryOnce(runtime, () => queryHistoricalEventsForModule(runtime, moduleAddress, newEvent.subAccount), 'queryHistoricalEventsForModule')
		const transferEvents = retryOnce(runtime, () => queryTransferEventsForModule(runtime, moduleAddress, newEvent.subAccount), 'queryTransferEventsForModule')

		// Add the new event (deduplicate by blockNumber + logIndex which uniquely identifies each log)
		const allEvents = [...historicalEvents]
		const isDuplicate = allEvents.some(e =>
			e.blockNumber === newEvent.blockNumber &&
			e.logIndex === newEvent.logIndex
		)
		if (!isDuplicate) {
			allEvents.push(newEvent)
		}

		// Collect all tokens from events for price cache
		const tokensInEvents = new Set<Address>()
		for (const e of allEvents) {
			for (const t of e.tokensIn) tokensInEvents.add(t.toLowerCase() as Address)
			for (const t of e.tokensOut) tokensInEvents.add(t.toLowerCase() as Address)
		}

		// Build price cache for USD-weighted ratio calculations
		const priceCache = buildTokenPriceCache(runtime, tokensInEvents)
		runtime.log(`Built price cache for ${priceCache.size} tokens`)

		// Get per-subaccount window duration
		const { windowDuration } = getSubAccountLimitsForModule(runtime, moduleAddress, newEvent.subAccount)

		// Build state from all events using per-subaccount window duration
		const state = buildSubAccountState(runtime, allEvents, transferEvents, newEvent.subAccount, currentTimestamp, windowDuration, priceCache)

		// Calculate new spending allowance
		const newAllowance = calculateSpendingAllowanceForModule(runtime, moduleAddress, newEvent.subAccount, state)

		// Push update to contract
		const txHash = pushBatchUpdateForModule(runtime, moduleAddress, newEvent.subAccount, newAllowance, state.acquiredBalances)

		runtime.log(`=== Event Processing Complete ===`)
		return txHash || 'Skipped - no changes'
	} catch (error) {
		runtime.log(`Error processing event: ${error}`)
		return `Error: ${error}`
	}
}

// ============ Cron Handler ============

/**
 * Process a single module's subaccounts for cron refresh
 * Uses module-aware functions to properly support multi-module operation
 */
const processModuleSubaccounts = (
	runtime: Runtime<Config>,
	moduleAddress: Address,
	currentTimestamp: bigint,
): string[] => {
	const results: string[] = []

	// Get subaccounts for this module
	const subaccounts = retryOnce(runtime, () => getActiveSubaccountsForModule(runtime, moduleAddress), 'getActiveSubaccountsForModule')
	runtime.log(`Found ${subaccounts.length} active subaccounts for module ${moduleAddress}`)

	if (subaccounts.length === 0) {
		return [`${moduleAddress}: No subaccounts`]
	}

	// Query events for this module
	const allEvents = retryOnce(runtime, () => queryHistoricalEventsForModule(runtime, moduleAddress), 'queryHistoricalEventsForModule')
	const allTransfers = retryOnce(runtime, () => queryTransferEventsForModule(runtime, moduleAddress), 'queryTransferEventsForModule')

	// Collect all tokens from events for price cache
	const tokensInEvents = new Set<Address>()
	for (const e of allEvents) {
		for (const t of e.tokensIn) tokensInEvents.add(t.toLowerCase() as Address)
		for (const t of e.tokensOut) tokensInEvents.add(t.toLowerCase() as Address)
	}

	// Build price cache for USD-weighted ratio calculations
	const priceCache = buildTokenPriceCache(runtime, tokensInEvents)
	runtime.log(`Built price cache for ${priceCache.size} tokens`)

	// Process each subaccount
	for (const subAccount of subaccounts) {
		try {
			runtime.log(`Processing subaccount: ${subAccount}`)

			// Get per-subaccount window duration
			const { windowDuration } = getSubAccountLimitsForModule(runtime, moduleAddress, subAccount)

			// Build state for this subaccount with priceCache for USD-weighted ratios
			const state = buildSubAccountState(runtime, allEvents, allTransfers, subAccount, currentTimestamp, windowDuration, priceCache)

			// Calculate new spending allowance using module-aware functions
			const newAllowance = calculateSpendingAllowanceForModule(runtime, moduleAddress, subAccount, state)

			// Push update to contract using module-aware function
			const txHash = pushBatchUpdateForModule(runtime, moduleAddress, subAccount, newAllowance, state.acquiredBalances)
			results.push(`${moduleAddress}/${subAccount}: ${txHash || 'Skipped'}`)
		} catch (error) {
			runtime.log(`Error processing ${subAccount}: ${error}`)
			results.push(`${moduleAddress}/${subAccount}: Error - ${error}`)
		}
	}

	return results
}

/**
 * Periodic refresh of spending allowances
 * Runs every 5 minutes to update allowances as old spending expires
 * Supports multi-module operation via registry if configured
 */
const onCronRefresh = (runtime: Runtime<Config>, _payload: CronPayload): string => {
	runtime.log('=== Spending Oracle: Periodic Refresh ===')

	try {
		const currentTimestamp = getCurrentBlockTimestamp()

		// Get all active modules from registry (or single module if no registry)
		const modules = retryOnce(runtime, () => getActiveModulesFromRegistry(runtime), 'getActiveModulesFromRegistry')
		runtime.log(`Processing ${modules.length} module(s)`)

		if (modules.length === 0) {
			runtime.log('No active modules found')
			return 'No modules'
		}

		const allResults: string[] = []

		// Process each module
		for (const moduleAddress of modules) {
			runtime.log(`\n--- Processing module: ${moduleAddress} ---`)
			try {
				const moduleResults = processModuleSubaccounts(runtime, moduleAddress, currentTimestamp)
				allResults.push(...moduleResults)
			} catch (error) {
				runtime.log(`Error processing module ${moduleAddress}: ${error}`)
				allResults.push(`${moduleAddress}: Error - ${error}`)
			}
		}

		runtime.log(`\n=== Periodic Refresh Complete ===`)
		return allResults.join('; ')
	} catch (error) {
		runtime.log(`Error in periodic refresh: ${error}`)
		return `Error: ${error}`
	}
}

// ============ Transfer Event Handler ============

/**
 * Handle TransferExecuted event
 * Triggered on each token transfer from the Safe
 * Note: Event trigger only monitors config.moduleAddress, so we use it explicitly
 */
const onTransferExecuted = (runtime: Runtime<Config>, payload: any): string => {
	runtime.log('=== Spending Oracle: TransferExecuted Event ===')

	const moduleAddress = runtime.config.moduleAddress as Address

	try {
		const log = payload.log
		if (!log || !log.topics || log.topics.length < 4) {
			runtime.log('Invalid transfer event log format')
			return 'Invalid event'
		}

		const newTransfer = parseTransferExecutedEvent(log)

		// Fetch actual block timestamp for the new event
		const actualTimestamp = getBlockTimestamp(runtime, newTransfer.blockNumber)
		newTransfer.timestamp = actualTimestamp

		const currentTimestamp = getCurrentBlockTimestamp()

		runtime.log(`New transfer: ${newTransfer.amount} of ${newTransfer.token} to ${newTransfer.recipient}`)
		runtime.log(`  Module: ${moduleAddress}`)
		runtime.log(`  Block: ${newTransfer.blockNumber}, Timestamp: ${newTransfer.timestamp}`)
		runtime.log(`  SpendingCost: ${newTransfer.spendingCost}`)

		// Query historical events (both protocol executions and transfers)
		const historicalEvents = retryOnce(runtime, () => queryHistoricalEventsForModule(runtime, moduleAddress, newTransfer.subAccount), 'queryHistoricalEventsForModule')
		const transferEvents = retryOnce(runtime, () => queryTransferEventsForModule(runtime, moduleAddress, newTransfer.subAccount), 'queryTransferEventsForModule')

		// Add the new transfer event (deduplicate by blockNumber + logIndex)
		const allTransfers = [...transferEvents]
		const isDuplicate = allTransfers.some(e =>
			e.blockNumber === newTransfer.blockNumber &&
			e.logIndex === newTransfer.logIndex
		)
		if (!isDuplicate) {
			allTransfers.push(newTransfer)
		}

		// Collect all tokens from events for price cache
		const tokensInEvents = new Set<Address>()
		for (const e of historicalEvents) {
			for (const t of e.tokensIn) tokensInEvents.add(t.toLowerCase() as Address)
			for (const t of e.tokensOut) tokensInEvents.add(t.toLowerCase() as Address)
		}
		tokensInEvents.add(newTransfer.token.toLowerCase() as Address)

		// Build price cache for USD-weighted ratio calculations
		const priceCache = buildTokenPriceCache(runtime, tokensInEvents)
		runtime.log(`Built price cache for ${priceCache.size} tokens`)

		// Get per-subaccount window duration
		const { windowDuration } = getSubAccountLimitsForModule(runtime, moduleAddress, newTransfer.subAccount)

		// Build state from all events using per-subaccount window duration
		const state = buildSubAccountState(runtime, historicalEvents, allTransfers, newTransfer.subAccount, currentTimestamp, windowDuration, priceCache)

		// Calculate new spending allowance
		const newAllowance = calculateSpendingAllowanceForModule(runtime, moduleAddress, newTransfer.subAccount, state)

		// Push update to contract
		const txHash = pushBatchUpdateForModule(runtime, moduleAddress, newTransfer.subAccount, newAllowance, state.acquiredBalances)

		runtime.log(`=== Transfer Event Processing Complete ===`)
		return txHash || 'Skipped - no changes'
	} catch (error) {
		runtime.log(`Error processing transfer event: ${error}`)
		return `Error: ${error}`
	}
}

// ============ Workflow Initialization ============

const initWorkflow = (config: Config) => {
	const network = getNetwork({
		chainFamily: 'evm',
		chainSelectorName: config.chainSelectorName,
		isTestnet: config.chainSelectorName.includes('testnet') || config.chainSelectorName.includes('sepolia'),
	})

	if (!network) {
		throw new Error(`Network not found: ${config.chainSelectorName}`)
	}

	const evmClient = new cre.capabilities.EVMClient(network.chainSelector.selector)
	const cronTrigger = new cre.capabilities.CronCapability()

	return [
		// Event trigger: Process each ProtocolExecution event
		// logTrigger uses topics array where topics[0] contains event signatures
		cre.handler(
			evmClient.logTrigger({
				addresses: [config.moduleAddress],
				topics: [{ values: [PROTOCOL_EXECUTION_EVENT_SIG] }],
			}),
			onProtocolExecution,
		),
		// Event trigger: Process each TransferExecuted event
		cre.handler(
			evmClient.logTrigger({
				addresses: [config.moduleAddress],
				topics: [{ values: [TRANSFER_EXECUTED_EVENT_SIG] }],
			}),
			onTransferExecuted,
		),
		// Cron trigger: Periodic refresh of spending allowances
		cre.handler(
			cronTrigger.trigger({
				schedule: config.refreshSchedule,
			}),
			onCronRefresh,
		),
	]
}

export async function main() {
	const runner = await Runner.newRunner<Config>({
		configSchema,
	})
	await runner.run(initWorkflow)
}

main()
