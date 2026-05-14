export const AgentVaultFactoryAbi = [
  {
    type: "constructor",
    inputs: [
      {
        name: "_initialOwner",
        type: "address",
        internalType: "address",
      },
      {
        name: "_registry",
        type: "address",
        internalType: "address",
      },
      {
        name: "_presetRegistry",
        type: "address",
        internalType: "address",
      },
      {
        name: "_implementation",
        type: "address",
        internalType: "address",
      },
    ],
    stateMutability: "nonpayable",
  },
  {
    type: "function",
    name: "computeModuleAddress",
    inputs: [
      {
        name: "safe",
        type: "address",
        internalType: "address",
      },
    ],
    outputs: [
      {
        name: "predicted",
        type: "address",
        internalType: "address",
      },
    ],
    stateMutability: "view",
  },
  {
    type: "function",
    name: "deployVault",
    inputs: [
      {
        name: "config",
        type: "tuple",
        internalType: "struct AgentVaultFactory.VaultConfig",
        components: [
          {
            name: "safe",
            type: "address",
            internalType: "address",
          },
          {
            name: "oracle",
            type: "address",
            internalType: "address",
          },
          {
            name: "agentAddress",
            type: "address",
            internalType: "address",
          },
          {
            name: "roleId",
            type: "uint16",
            internalType: "uint16",
          },
          {
            name: "maxSpendingBps",
            type: "uint256",
            internalType: "uint256",
          },
          {
            name: "maxSpendingUSD",
            type: "uint256",
            internalType: "uint256",
          },
          {
            name: "windowDuration",
            type: "uint256",
            internalType: "uint256",
          },
          {
            name: "allowedProtocols",
            type: "address[]",
            internalType: "address[]",
          },
          {
            name: "parserProtocols",
            type: "address[]",
            internalType: "address[]",
          },
          {
            name: "parserAddresses",
            type: "address[]",
            internalType: "address[]",
          },
          {
            name: "selectors",
            type: "bytes4[]",
            internalType: "bytes4[]",
          },
          {
            name: "selectorTypes",
            type: "uint8[]",
            internalType: "uint8[]",
          },
          {
            name: "priceFeedTokens",
            type: "address[]",
            internalType: "address[]",
          },
          {
            name: "priceFeedAddresses",
            type: "address[]",
            internalType: "address[]",
          },
          {
            name: "recipientWhitelistEnabled",
            type: "bool",
            internalType: "bool",
          },
          {
            name: "allowedRecipients",
            type: "address[]",
            internalType: "address[]",
          },
        ],
      },
    ],
    outputs: [
      {
        name: "module",
        type: "address",
        internalType: "address",
      },
    ],
    stateMutability: "nonpayable",
  },
  {
    type: "function",
    name: "deployVaultFromPreset",
    inputs: [
      {
        name: "safe",
        type: "address",
        internalType: "address",
      },
      {
        name: "oracle",
        type: "address",
        internalType: "address",
      },
      {
        name: "agentAddress",
        type: "address",
        internalType: "address",
      },
      {
        name: "presetId",
        type: "uint256",
        internalType: "uint256",
      },
      {
        name: "priceFeedTokens",
        type: "address[]",
        internalType: "address[]",
      },
      {
        name: "priceFeedAddresses",
        type: "address[]",
        internalType: "address[]",
      },
      {
        name: "allowedRecipients",
        type: "address[]",
        internalType: "address[]",
      },
    ],
    outputs: [
      {
        name: "module",
        type: "address",
        internalType: "address",
      },
    ],
    stateMutability: "nonpayable",
  },
  {
    type: "function",
    name: "getDeployedModules",
    inputs: [
      {
        name: "safe",
        type: "address",
        internalType: "address",
      },
    ],
    outputs: [
      {
        name: "",
        type: "address[]",
        internalType: "address[]",
      },
    ],
    stateMutability: "view",
  },
  {
    type: "function",
    name: "getNonce",
    inputs: [
      {
        name: "safe",
        type: "address",
        internalType: "address",
      },
    ],
    outputs: [
      {
        name: "",
        type: "uint256",
        internalType: "uint256",
      },
    ],
    stateMutability: "view",
  },
  {
    type: "function",
    name: "implementation",
    inputs: [],
    outputs: [
      {
        name: "",
        type: "address",
        internalType: "address",
      },
    ],
    stateMutability: "view",
  },
  {
    type: "function",
    name: "owner",
    inputs: [],
    outputs: [
      {
        name: "",
        type: "address",
        internalType: "address",
      },
    ],
    stateMutability: "view",
  },
  {
    type: "function",
    name: "presetRegistry",
    inputs: [],
    outputs: [
      {
        name: "",
        type: "address",
        internalType: "contract PresetRegistry",
      },
    ],
    stateMutability: "view",
  },
  {
    type: "function",
    name: "registry",
    inputs: [],
    outputs: [
      {
        name: "",
        type: "address",
        internalType: "contract IModuleRegistry",
      },
    ],
    stateMutability: "view",
  },
  {
    type: "function",
    name: "renounceOwnership",
    inputs: [],
    outputs: [],
    stateMutability: "nonpayable",
  },
  {
    type: "function",
    name: "setImplementation",
    inputs: [
      {
        name: "_implementation",
        type: "address",
        internalType: "address",
      },
    ],
    outputs: [],
    stateMutability: "nonpayable",
  },
  {
    type: "function",
    name: "setPresetRegistry",
    inputs: [
      {
        name: "_presetRegistry",
        type: "address",
        internalType: "address",
      },
    ],
    outputs: [],
    stateMutability: "nonpayable",
  },
  {
    type: "function",
    name: "setRegistry",
    inputs: [
      {
        name: "_registry",
        type: "address",
        internalType: "address",
      },
    ],
    outputs: [],
    stateMutability: "nonpayable",
  },
  {
    type: "function",
    name: "transferOwnership",
    inputs: [
      {
        name: "newOwner",
        type: "address",
        internalType: "address",
      },
    ],
    outputs: [],
    stateMutability: "nonpayable",
  },
  {
    type: "function",
    name: "vaultNonce",
    inputs: [
      {
        name: "",
        type: "address",
        internalType: "address",
      },
    ],
    outputs: [
      {
        name: "",
        type: "uint256",
        internalType: "uint256",
      },
    ],
    stateMutability: "view",
  },
  {
    type: "event",
    name: "AgentVaultCreated",
    inputs: [
      {
        name: "safe",
        type: "address",
        indexed: true,
        internalType: "address",
      },
      {
        name: "agentAddress",
        type: "address",
        indexed: true,
        internalType: "address",
      },
      {
        name: "module",
        type: "address",
        indexed: false,
        internalType: "address",
      },
      {
        name: "presetId",
        type: "uint256",
        indexed: false,
        internalType: "uint256",
      },
    ],
    anonymous: false,
  },
  {
    type: "event",
    name: "ImplementationUpdated",
    inputs: [
      {
        name: "oldImplementation",
        type: "address",
        indexed: true,
        internalType: "address",
      },
      {
        name: "newImplementation",
        type: "address",
        indexed: true,
        internalType: "address",
      },
    ],
    anonymous: false,
  },
  {
    type: "event",
    name: "OwnershipTransferred",
    inputs: [
      {
        name: "previousOwner",
        type: "address",
        indexed: true,
        internalType: "address",
      },
      {
        name: "newOwner",
        type: "address",
        indexed: true,
        internalType: "address",
      },
    ],
    anonymous: false,
  },
  {
    type: "event",
    name: "PresetRegistryUpdated",
    inputs: [
      {
        name: "oldPresetRegistry",
        type: "address",
        indexed: true,
        internalType: "address",
      },
      {
        name: "newPresetRegistry",
        type: "address",
        indexed: true,
        internalType: "address",
      },
    ],
    anonymous: false,
  },
  {
    type: "event",
    name: "RegistryUpdated",
    inputs: [
      {
        name: "oldRegistry",
        type: "address",
        indexed: true,
        internalType: "address",
      },
      {
        name: "newRegistry",
        type: "address",
        indexed: true,
        internalType: "address",
      },
    ],
    anonymous: false,
  },
  {
    type: "error",
    name: "ArrayLengthMismatch",
    inputs: [],
  },
  {
    type: "error",
    name: "FailedDeployment",
    inputs: [],
  },
  {
    type: "error",
    name: "ImplementationNotSet",
    inputs: [],
  },
  {
    type: "error",
    name: "InsufficientBalance",
    inputs: [
      {
        name: "balance",
        type: "uint256",
        internalType: "uint256",
      },
      {
        name: "needed",
        type: "uint256",
        internalType: "uint256",
      },
    ],
  },
  {
    type: "error",
    name: "InvalidAddress",
    inputs: [],
  },
  {
    type: "error",
    name: "InvalidConfig",
    inputs: [],
  },
  {
    type: "error",
    name: "OwnableInvalidOwner",
    inputs: [
      {
        name: "owner",
        type: "address",
        internalType: "address",
      },
    ],
  },
  {
    type: "error",
    name: "OwnableUnauthorizedAccount",
    inputs: [
      {
        name: "account",
        type: "address",
        internalType: "address",
      },
    ],
  },
  {
    type: "error",
    name: "PresetRegistryNotSet",
    inputs: [],
  },
  {
    type: "error",
    name: "SafeAlreadyHasModule",
    inputs: [
      {
        name: "safe",
        type: "address",
        internalType: "address",
      },
      {
        name: "existingModule",
        type: "address",
        internalType: "address",
      },
    ],
  },
] as const;
