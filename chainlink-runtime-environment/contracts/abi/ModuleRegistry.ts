/**
 * ModuleRegistry ABI - minimal interface for CRE workflow
 */
export const ModuleRegistry = [
	{
		inputs: [],
		name: 'getActiveModules',
		outputs: [{ name: 'modules', type: 'address[]', internalType: 'address[]' }],
		stateMutability: 'view',
		type: 'function',
	},
	{
		inputs: [
			{ name: 'offset', type: 'uint256', internalType: 'uint256' },
			{ name: 'limit', type: 'uint256', internalType: 'uint256' },
		],
		name: 'getActiveModulesPaginated',
		outputs: [
			{ name: 'modules', type: 'address[]', internalType: 'address[]' },
			{ name: 'total', type: 'uint256', internalType: 'uint256' },
		],
		stateMutability: 'view',
		type: 'function',
	},
	{
		inputs: [],
		name: 'getActiveModuleCount',
		outputs: [{ name: 'count', type: 'uint256', internalType: 'uint256' }],
		stateMutability: 'view',
		type: 'function',
	},
	{
		inputs: [{ name: 'safe', type: 'address', internalType: 'address' }],
		name: 'getModuleForSafe',
		outputs: [{ name: 'module', type: 'address', internalType: 'address' }],
		stateMutability: 'view',
		type: 'function',
	},
	{
		inputs: [{ name: 'module', type: 'address', internalType: 'address' }],
		name: 'isRegistered',
		outputs: [{ name: 'registered', type: 'bool', internalType: 'bool' }],
		stateMutability: 'view',
		type: 'function',
	},
	{
		inputs: [{ name: 'module', type: 'address', internalType: 'address' }],
		name: 'moduleInfo',
		outputs: [
			{
				name: 'info',
				type: 'tuple',
				internalType: 'struct IModuleRegistry.ModuleInfo',
				components: [
					{ name: 'safeAddress', type: 'address', internalType: 'address' },
					{ name: 'authorizedOracle', type: 'address', internalType: 'address' },
					{ name: 'deployedAt', type: 'uint256', internalType: 'uint256' },
					{ name: 'isActive', type: 'bool', internalType: 'bool' },
				],
			},
		],
		stateMutability: 'view',
		type: 'function',
	},
] as const
