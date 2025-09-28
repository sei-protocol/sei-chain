#!/usr/bin/env node
import { existsSync, readFileSync } from "fs";
import process from "process";
import { ethers } from "ethers";

const SOLO_PRECOMPILE_ADDRESS = "0x000000000000000000000000000000000000100C";
const SOLO_ABI = [
  {
    inputs: [{ internalType: "bytes", name: "payload", type: "bytes" }],
    name: "claim",
    outputs: [{ internalType: "bool", name: "response", type: "bool" }],
    stateMutability: "nonpayable",
    type: "function",
  },
  {
    inputs: [{ internalType: "bytes", name: "payload", type: "bytes" }],
    name: "claimSpecific",
    outputs: [{ internalType: "bool", name: "response", type: "bool" }],
    stateMutability: "nonpayable",
    type: "function",
  },
];

const FLAG_WITH_VALUE = new Set([
  "payload",
  "rpc-url",
  "gas-limit",
  "gas-price",
  "max-fee-per-gas",
  "max-priority-fee-per-gas",
  "nonce",
  "chain-id",
]);

const BOOLEAN_FLAGS = new Set(["claim-specific", "dry-run", "no-wait"]);

function printHelp(exitCode = 0) {
  console.log(`Usage: node deploy/msg_claim.mjs --payload <hex|file> [options]\n\n` +
    `Required:\n` +
    `  --payload <value>          Hex string or path to file with Cosmos-signed payload.\n\n` +
    `When broadcasting (default behaviour):\n` +
    `  --rpc-url <url>            HTTP RPC endpoint for Sei EVM.\n` +
    `  PRIVATE_KEY env var        Private key used to sign the transaction.\n\n` +
    `Optional flags:\n` +
    `  --claim-specific           Call claimSpecific(bytes) instead of claim(bytes).\n` +
    `  --gas-limit <value>        Gas limit to use (default 750000).\n` +
    `  --gas-price <gwei>         Legacy gas price in gwei.\n` +
    `  --max-fee-per-gas <gwei>   EIP-1559 maxFeePerGas in gwei (requires --max-priority-fee-per-gas).\n` +
    `  --max-priority-fee-per-gas <gwei>  EIP-1559 tip in gwei.\n` +
    `  --nonce <value>            Override the nonce instead of querying RPC.\n` +
    `  --chain-id <value>         Chain ID hint when connecting to RPC.\n` +
    `  --dry-run                  Do not broadcast; print transaction skeleton instead.\n` +
    `  --no-wait                  Do not wait for transaction confirmation.\n` +
    `  -h, --help                 Show this message.\n`);
  process.exit(exitCode);
}

function parseArgs(argv) {
  const parsed = {};
  for (let i = 0; i < argv.length; i += 1) {
    const arg = argv[i];
    if (arg === "--help" || arg === "-h") {
      parsed.help = true;
      continue;
    }
    if (!arg.startsWith("--")) {
      throw new Error(`Unexpected positional argument '${arg}'.`);
    }
    const flag = arg.slice(2);
    if (BOOLEAN_FLAGS.has(flag)) {
      parsed[flag] = true;
      continue;
    }
    if (!FLAG_WITH_VALUE.has(flag)) {
      throw new Error(`Unknown flag --${flag}. Use --help to list supported options.`);
    }
    if (i + 1 >= argv.length) {
      throw new Error(`Flag --${flag} requires a value.`);
    }
    parsed[flag] = argv[i + 1];
    i += 1;
  }
  return parsed;
}

function ensure0x(hex) {
  return hex.startsWith("0x") ? hex : `0x${hex}`;
}

function normalizeHex(hex) {
  const prefixed = ensure0x(hex.trim());
  if (!/^0x[0-9a-fA-F]*$/.test(prefixed)) {
    throw new Error("Payload must be a hex string when provided inline.");
  }
  if (prefixed.length % 2 !== 0) {
    throw new Error("Payload hex must have an even number of characters.");
  }
  return prefixed;
}

function loadPayload(input) {
  if (existsSync(input)) {
    const raw = readFileSync(input);
    const asString = raw.toString().trim();
    if (asString.length > 0 && /^0x?[0-9a-fA-F]+$/.test(asString)) {
      return normalizeHex(asString);
    }
    return ensure0x(Buffer.from(raw).toString("hex"));
  }
  return normalizeHex(input);
}

function parseIntegerOption(value, flagName) {
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < 0) {
    throw new Error(`--${flagName} must be a non-negative integer.`);
  }
  return parsed;
}

function buildGasOverrides(options) {
  const overrides = {};
  const gasLimitSource = options["gas-limit"] ?? process.env.CLAIM_TX_GAS_LIMIT;
  if (gasLimitSource !== undefined) {
    overrides.gasLimit = BigInt(parseIntegerOption(gasLimitSource, "gas-limit"));
  } else {
    overrides.gasLimit = 750000n;
  }

  if (options["gas-price"] !== undefined) {
    overrides.gasPrice = ethers.parseUnits(options["gas-price"], "gwei");
  }

  const hasMaxFee = options["max-fee-per-gas"] !== undefined;
  const hasPriority = options["max-priority-fee-per-gas"] !== undefined;
  if (hasMaxFee || hasPriority) {
    if (!(hasMaxFee && hasPriority)) {
      throw new Error("Both --max-fee-per-gas and --max-priority-fee-per-gas must be set together.");
    }
    overrides.maxFeePerGas = ethers.parseUnits(options["max-fee-per-gas"], "gwei");
    overrides.maxPriorityFeePerGas = ethers.parseUnits(
      options["max-priority-fee-per-gas"],
      "gwei",
    );
    delete overrides.gasPrice;
  }

  if (options.nonce !== undefined) {
    overrides.nonce = parseIntegerOption(options.nonce, "nonce");
  }

  return overrides;
}

async function maybePopulateFees(overrides, provider) {
  if (("gasPrice" in overrides) || ("maxFeePerGas" in overrides) || !provider) {
    return overrides;
  }

  const feeData = await provider.getFeeData();
  if (feeData.maxFeePerGas != null && feeData.maxPriorityFeePerGas != null) {
    overrides.maxFeePerGas = feeData.maxFeePerGas;
    overrides.maxPriorityFeePerGas = feeData.maxPriorityFeePerGas;
  } else if (feeData.gasPrice != null) {
    overrides.gasPrice = feeData.gasPrice;
  } else {
    throw new Error(
      "RPC did not return usable fee data. Provide --gas-price or EIP-1559 flags explicitly.",
    );
  }
  return overrides;
}

async function main() {
  const args = parseArgs(process.argv.slice(2));
  if (args.help) {
    printHelp(0);
  }
  if (!args.payload) {
    console.error("Missing required --payload argument.\n");
    printHelp(1);
  }

  const payloadHex = loadPayload(args.payload);
  const method = args["claim-specific"] ? "claimSpecific" : "claim";
  const iface = new ethers.Interface(SOLO_ABI);
  const calldata = iface.encodeFunctionData(method, [payloadHex]);

  const overrides = buildGasOverrides(args);

  if (args["dry-run"]) {
    const summary = {
      to: SOLO_PRECOMPILE_ADDRESS,
      method,
      gasLimit: overrides.gasLimit?.toString?.() ?? undefined,
      feeFields: {
        gasPrice: overrides.gasPrice ? overrides.gasPrice.toString() : undefined,
        maxFeePerGas: overrides.maxFeePerGas ? overrides.maxFeePerGas.toString() : undefined,
        maxPriorityFeePerGas: overrides.maxPriorityFeePerGas
          ? overrides.maxPriorityFeePerGas.toString()
          : undefined,
      },
      nonce: overrides.nonce,
      data: calldata,
    };
    console.log(JSON.stringify(summary, null, 2));
    return;
  }

  const rpcUrl =
    args["rpc-url"] || process.env.SEI_EVM_RPC_URL || process.env.RPC_URL;
  if (!rpcUrl) {
    throw new Error("RPC URL is required when broadcasting. Provide --rpc-url or set SEI_EVM_RPC_URL.");
  }

  const chainId = args["chain-id"] !== undefined ? parseIntegerOption(args["chain-id"], "chain-id") : undefined;
  const provider = new ethers.JsonRpcProvider(rpcUrl, chainId);
  const privateKey = process.env.PRIVATE_KEY;
  if (!privateKey) {
    throw new Error("PRIVATE_KEY environment variable is required to sign the transaction.");
  }

  const wallet = new ethers.Wallet(privateKey, provider);
  await maybePopulateFees(overrides, provider);

  const txRequest = {
    to: SOLO_PRECOMPILE_ADDRESS,
    data: calldata,
    value: 0n,
    ...overrides,
  };

  console.log(`Submitting ${method} transaction from ${wallet.address}...`);
  const response = await wallet.sendTransaction(txRequest);
  console.log(`Transaction hash: ${response.hash}`);

  if (args["no-wait"]) {
    return;
  }

  const receipt = await response.wait();
  if (!receipt) {
    console.warn("Transaction submitted but no receipt was returned (provider may not support wait).");
    return;
  }
  console.log(`Status: ${receipt.status === 1 ? "SUCCESS" : "FAILED"}`);
  console.log(`Gas used: ${receipt.gasUsed?.toString() ?? "unknown"}`);
}

main().catch((err) => {
  console.error(err instanceof Error ? err.message : err);
  process.exit(1);
});
