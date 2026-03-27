/**
 * Enable a DeFiInteractorModule on a Safe
 *
 * Builds and executes a Safe transaction calling enableModule(moduleAddress).
 * Works for 1/1 Safes where the signer is the sole owner.
 *
 * Usage: npx tsx src/enable-module.ts
 */

import { createPublicClient, createWalletClient, http, encodeFunctionData, keccak256, encodeAbiParameters, parseAbiParameters, toBytes, concat } from "viem";
import { privateKeyToAccount } from "viem/accounts";
import { baseSepolia } from "viem/chains";
import dotenv from "dotenv";

dotenv.config({ path: "../bot/.env" });

// ── Config ────────────────────────────────────────────────────────────────────
const SAFE_ADDRESS   = "0x6c9410Fcdedda7a0dA572eB613b1ad5372592BB7" as const;
const MODULE_ADDRESS = "0xc0e4351BF07c6ae11caF04f356c88997Ef97b25c" as const;
const RPC_URL        = "https://sepolia.base.org";

// ── EIP-712 typehashes ────────────────────────────────────────────────────────
const DOMAIN_SEPARATOR_TYPEHASH = keccak256(
  toBytes("EIP712Domain(uint256 chainId,address verifyingContract)")
);

const SAFE_TX_TYPEHASH = keccak256(
  toBytes("SafeTx(address to,uint256 value,bytes data,uint8 operation,uint256 safeTxGas,uint256 baseGas,uint256 gasPrice,address gasToken,address refundReceiver,uint256 nonce)")
);

function getSafeTxHash(
  chainId: bigint,
  safe: `0x${string}`,
  to: `0x${string}`,
  data: `0x${string}`,
  nonce: bigint
): `0x${string}` {
  const domainSeparator = keccak256(
    encodeAbiParameters(
      parseAbiParameters("bytes32, uint256, address"),
      [DOMAIN_SEPARATOR_TYPEHASH, chainId, safe]
    )
  );

  const dataHash = keccak256(data);

  const safeTxHash = keccak256(
    encodeAbiParameters(
      parseAbiParameters("bytes32, address, uint256, bytes32, uint8, uint256, uint256, uint256, address, address, uint256"),
      [SAFE_TX_TYPEHASH, to, 0n, dataHash, 0, 0n, 0n, 0n, "0x0000000000000000000000000000000000000000", "0x0000000000000000000000000000000000000000", nonce]
    )
  );

  return keccak256(concat(["0x1901", domainSeparator, safeTxHash]));
}

async function main() {
  const privateKey = process.env.AGENT_PRIVATE_KEY as `0x${string}`;
  if (!privateKey) throw new Error("AGENT_PRIVATE_KEY not set");

  const account = privateKeyToAccount(privateKey);
  console.log("Signer:", account.address);
  console.log("Safe:  ", SAFE_ADDRESS);
  console.log("Module:", MODULE_ADDRESS);

  const publicClient = createPublicClient({ chain: baseSepolia, transport: http(RPC_URL) });
  const walletClient = createWalletClient({ account, chain: baseSepolia, transport: http(RPC_URL) });

  // Check module isn't already enabled
  const isEnabled = await publicClient.readContract({
    address: SAFE_ADDRESS,
    abi: [{ name: "isModuleEnabled", type: "function", inputs: [{ type: "address" }], outputs: [{ type: "bool" }], stateMutability: "view" }],
    functionName: "isModuleEnabled",
    args: [MODULE_ADDRESS],
  });

  if (isEnabled) {
    console.log("Module already enabled.");
    return;
  }

  // Get current Safe nonce
  const nonce = await publicClient.readContract({
    address: SAFE_ADDRESS,
    abi: [{ name: "nonce", type: "function", inputs: [], outputs: [{ type: "uint256" }], stateMutability: "view" }],
    functionName: "nonce",
  });
  console.log("Safe nonce:", nonce.toString());

  // Build enableModule calldata
  const data = encodeFunctionData({
    abi: [{ name: "enableModule", type: "function", inputs: [{ name: "module", type: "address" }], outputs: [], stateMutability: "nonpayable" }],
    functionName: "enableModule",
    args: [MODULE_ADDRESS],
  });

  // Build and sign Safe tx hash
  const chainId = BigInt(baseSepolia.id);
  const txHash = getSafeTxHash(chainId, SAFE_ADDRESS, SAFE_ADDRESS, data, nonce);
  console.log("Safe tx hash:", txHash);

  const signature = await account.sign({ hash: txHash });

  // Execute Safe transaction
  const hash = await walletClient.writeContract({
    address: SAFE_ADDRESS,
    abi: [{
      name: "execTransaction",
      type: "function",
      inputs: [
        { name: "to", type: "address" },
        { name: "value", type: "uint256" },
        { name: "data", type: "bytes" },
        { name: "operation", type: "uint8" },
        { name: "safeTxGas", type: "uint256" },
        { name: "baseGas", type: "uint256" },
        { name: "gasPrice", type: "uint256" },
        { name: "gasToken", type: "address" },
        { name: "refundReceiver", type: "address" },
        { name: "signatures", type: "bytes" },
      ],
      outputs: [{ type: "bool" }],
      stateMutability: "payable",
    }],
    functionName: "execTransaction",
    args: [SAFE_ADDRESS, 0n, data, 0, 0n, 0n, 0n, "0x0000000000000000000000000000000000000000", "0x0000000000000000000000000000000000000000", signature],
  });

  console.log("Tx sent:", hash);
  const receipt = await publicClient.waitForTransactionReceipt({ hash });
  console.log("Status:", receipt.status === "success" ? "✓ success" : "✗ failed");

  // Verify
  const nowEnabled = await publicClient.readContract({
    address: SAFE_ADDRESS,
    abi: [{ name: "isModuleEnabled", type: "function", inputs: [{ type: "address" }], outputs: [{ type: "bool" }], stateMutability: "view" }],
    functionName: "isModuleEnabled",
    args: [MODULE_ADDRESS],
  });
  console.log("Module enabled:", nowEnabled);
}

main().catch(console.error);
