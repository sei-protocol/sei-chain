import { readFile, stat, writeFile } from "fs/promises";
import { basename } from "path";
import { ethers } from "ethers";
import type { Network } from "ethers";

const SOLO_PRECOMPILE_ADDRESS = "0x000000000000000000000000000000000000100C";
const SOLO_ABI = [
  "function claim(bytes payload) external returns (bool)",
  "function claimSpecific(bytes payload) external returns (bool)",
];

type FeeOptions =
  | { type: "legacy"; gasPrice: bigint }
  | { type: "eip1559"; maxFeePerGas: bigint; maxPriorityFeePerGas: bigint }
  | { type: "auto" };

type ParsedArgs = {
  payloadSource: string;
  claimSpecific: boolean;
  rpcUrl: string;
  gasLimit?: bigint;
  nonce?: number;
  chainId?: number;
  waitForReceipt: boolean;
  broadcast: boolean;
  outputPath?: string;
  fees: FeeOptions;
};

function printUsage(): void {
  const scriptName = basename(process.argv[1] ?? "msg_claim.ts");
  console.log(`Usage: ${scriptName} --payload <hex-or-path> [options]\n\n` +
    "Options:\n" +
    "  --payload <hex-or-path>      Hex string (with or without 0x) or path to payload file.\n" +
    "  --claim-specific             Use claimSpecific(bytes) instead of claim(bytes).\n" +
    "  --rpc-url <url>              Sei EVM RPC endpoint (defaults to SEI_EVM_RPC_URL).\n" +
    "  --chain-id <id>              Override the chain ID used in the transaction.\n" +
    "  --gas-limit <gas>            Gas limit to include in the request.\n" +
    "  --gas-price <gwei>           Legacy gas price in gwei.\n" +
    "  --max-fee-per-gas <gwei>     EIP-1559 maxFeePerGas in gwei (requires max-priority flag).\n" +
    "  --max-priority-fee-per-gas <gwei>  EIP-1559 maxPriorityFeePerGas in gwei.\n" +
    "  --nonce <value>              Explicit nonce to use.\n" +
    "  --output <path>              Write a JSON summary of the signed transaction.\n" +
    "  --no-broadcast               Sign the transaction but do not send it (alias: --dry-run).\n" +
    "  --no-wait                    Do not wait for the receipt after broadcasting.\n" +
    "  -h, --help                   Show this message.\n");
}

function expectValue(args: string[], index: number, flag: string): string {
  if (index >= args.length) {
    throw new Error(`Flag ${flag} requires a value.`);
  }
  return args[index];
}

function parseInteger(value: string, flag: string): number {
  if (!/^\d+$/.test(value)) {
    throw new Error(`Flag ${flag} expects a non-negative integer.`);
  }
  return Number.parseInt(value, 10);
}

function parseBigInt(value: string, flag: string): bigint {
  if (!/^\d+$/.test(value)) {
    throw new Error(`Flag ${flag} expects a non-negative integer.`);
  }
  return BigInt(value);
}

function parseGwei(value: string, flag: string): bigint {
  if (!/^\d+(?:\.\d+)?$/.test(value)) {
    throw new Error(`Flag ${flag} expects a decimal number.`);
  }
  return ethers.parseUnits(value, "gwei");
}

function parseArgs(raw: string[]): ParsedArgs {
  const options: Partial<ParsedArgs> = {
    claimSpecific: false,
    waitForReceipt: true,
    broadcast: true,
    fees: { type: "auto" },
  };

  let gasPriceGwei: string | undefined;
  let maxFeeGwei: string | undefined;
  let maxPriorityGwei: string | undefined;

  for (let i = 0; i < raw.length; i += 1) {
    const token = raw[i];
    switch (token) {
      case "--payload": {
        const value = expectValue(raw, ++i, token);
        options.payloadSource = value;
        break;
      }
      case "--claim-specific": {
        options.claimSpecific = true;
        break;
      }
      case "--rpc-url": {
        const value = expectValue(raw, ++i, token);
        options.rpcUrl = value;
        break;
      }
      case "--chain-id": {
        const value = expectValue(raw, ++i, token);
        options.chainId = parseInteger(value, token);
        break;
      }
      case "--gas-limit": {
        const value = expectValue(raw, ++i, token);
        options.gasLimit = parseBigInt(value, token);
        break;
      }
      case "--gas-price": {
        gasPriceGwei = expectValue(raw, ++i, token);
        break;
      }
      case "--max-fee-per-gas": {
        maxFeeGwei = expectValue(raw, ++i, token);
        break;
      }
      case "--max-priority-fee-per-gas": {
        maxPriorityGwei = expectValue(raw, ++i, token);
        break;
      }
      case "--nonce": {
        const value = expectValue(raw, ++i, token);
        options.nonce = parseInteger(value, token);
        break;
      }
      case "--output": {
        const value = expectValue(raw, ++i, token);
        options.outputPath = value;
        break;
      }
      case "--no-broadcast":
      case "--dry-run": {
        options.broadcast = false;
        break;
      }
      case "--no-wait": {
        options.waitForReceipt = false;
        break;
      }
      case "-h":
      case "--help": {
        printUsage();
        process.exit(0);
      }
      default: {
        throw new Error(`Unrecognized flag: ${token}`);
      }
    }
  }

  if (!options.payloadSource) {
    throw new Error("--payload is required.");
  }

  if (!options.rpcUrl) {
    const envRpc = process.env.SEI_EVM_RPC_URL;
    if (!envRpc) {
      throw new Error("--rpc-url is required when SEI_EVM_RPC_URL is not set.");
    }
    options.rpcUrl = envRpc;
  }

  if (maxFeeGwei !== undefined || maxPriorityGwei !== undefined) {
    if (!maxFeeGwei || !maxPriorityGwei) {
      throw new Error("Both --max-fee-per-gas and --max-priority-fee-per-gas must be set.");
    }
    options.fees = {
      type: "eip1559",
      maxFeePerGas: parseGwei(maxFeeGwei, "--max-fee-per-gas"),
      maxPriorityFeePerGas: parseGwei(maxPriorityGwei, "--max-priority-fee-per-gas"),
    };
  } else if (gasPriceGwei !== undefined) {
    options.fees = { type: "legacy", gasPrice: parseGwei(gasPriceGwei, "--gas-price") };
  }

  return options as ParsedArgs;
}

async function loadPayload(source: string): Promise<Uint8Array> {
  try {
    const fileInfo = await stat(source);
    if (fileInfo.isFile()) {
      const fileContent = await readFile(source);
      const trimmed = fileContent.toString("utf8").trim();
      if (/^(?:0x)?[0-9a-fA-F]+$/.test(trimmed)) {
        const hex = trimmed.startsWith("0x") || trimmed.startsWith("0X") ? trimmed : `0x${trimmed}`;
        return ethers.getBytes(hex);
      }
      return new Uint8Array(fileContent);
    }
  } catch (error: unknown) {
    // Treat as inline payload if file lookup fails.
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") {
      throw error;
    }
  }

  const normalized = source.trim();
  if (/^(?:0x)?[0-9a-fA-F]+$/.test(normalized)) {
    const hex = normalized.startsWith("0x") || normalized.startsWith("0X") ? normalized : `0x${normalized}`;
    return ethers.getBytes(hex);
  }
  throw new Error("Payload must be a hex string or an existing file.");
}

async function resolveFeeFields(options: ParsedArgs, provider: ethers.JsonRpcProvider): Promise<FeeOptions> {
  if (options.fees.type !== "auto") {
    return options.fees;
  }

  const feeData = await provider.getFeeData();
  if (feeData.maxFeePerGas !== null && feeData.maxPriorityFeePerGas !== null) {
    return {
      type: "eip1559",
      maxFeePerGas: feeData.maxFeePerGas,
      maxPriorityFeePerGas: feeData.maxPriorityFeePerGas,
    };
  }
  if (feeData.gasPrice !== null) {
    return { type: "legacy", gasPrice: feeData.gasPrice };
  }
  throw new Error("RPC endpoint did not return usable fee data; specify gas flags manually.");
}

async function main(): Promise<void> {
  const args = parseArgs(process.argv.slice(2));
  const privateKey = process.env.PRIVATE_KEY;
  if (!privateKey) {
    throw new Error("PRIVATE_KEY environment variable is required.");
  }

  const payload = await loadPayload(args.payloadSource);
  const provider = new ethers.JsonRpcProvider(args.rpcUrl);

  let network: Network;
  try {
    network = await provider.getNetwork();
  } catch (error) {
    throw new Error(`Unable to connect to RPC at ${args.rpcUrl}.`);
  }

  const wallet = new ethers.Wallet(privateKey, provider);
  const contract = new ethers.Contract(SOLO_PRECOMPILE_ADDRESS, SOLO_ABI, wallet);
  const methodName = args.claimSpecific ? "claimSpecific" : "claim";
  const populated = await contract[methodName].populateTransaction(payload);

  if (args.chainId !== undefined) {
    populated.chainId = BigInt(args.chainId);
  } else if (populated.chainId === undefined) {
    populated.chainId = network.chainId;
  }

  if (args.gasLimit !== undefined) {
    populated.gasLimit = args.gasLimit;
  }

  if (args.nonce !== undefined) {
    populated.nonce = args.nonce;
  } else if (populated.nonce === undefined) {
    populated.nonce = await provider.getTransactionCount(wallet.address);
  }

  const feeFields = await resolveFeeFields(args, provider);
  if (feeFields.type === "legacy") {
    populated.gasPrice = feeFields.gasPrice;
    populated.type = 0;
  } else {
    populated.maxFeePerGas = feeFields.maxFeePerGas;
    populated.maxPriorityFeePerGas = feeFields.maxPriorityFeePerGas;
    populated.type = 2;
  }

  const signed = await wallet.signTransaction(populated);
  const transactionHash = ethers.keccak256(signed);
  console.log("Raw transaction:", signed);
  console.log("Transaction hash:", transactionHash);

  const summary = {
    function: methodName,
    raw_transaction: signed,
    transaction_hash: transactionHash,
    from: wallet.address,
    to: SOLO_PRECOMPILE_ADDRESS,
    nonce: populated.nonce !== undefined ? populated.nonce.toString() : undefined,
    chain_id: populated.chainId !== undefined ? populated.chainId.toString() : undefined,
    gas_limit: populated.gasLimit !== undefined ? populated.gasLimit.toString() : undefined,
    fee_fields:
      feeFields.type === "legacy"
        ? { type: "legacy", gas_price: feeFields.gasPrice.toString() }
        : {
            type: "eip1559",
            max_fee_per_gas: feeFields.maxFeePerGas.toString(),
            max_priority_fee_per_gas: feeFields.maxPriorityFeePerGas.toString(),
          },
    claim_specific: args.claimSpecific,
  };

  if (args.outputPath) {
    await writeFile(args.outputPath, `${JSON.stringify(summary, null, 2)}\n`);
    console.log(`Transaction summary written to ${args.outputPath}`);
  }

  if (!args.broadcast) {
    console.log("Broadcast skipped (--no-broadcast provided).");
    return;
  }

  const response = await wallet.sendTransaction(populated);
  console.log("Broadcasted transaction:", response.hash);

  if (!args.waitForReceipt) {
    return;
  }

  const receipt = await response.wait();
  console.log("Receipt status:", receipt?.status);
  if (receipt?.gasUsed !== undefined) {
    console.log("Gas used:", receipt.gasUsed.toString());
  }
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : error);
  process.exit(1);
});
