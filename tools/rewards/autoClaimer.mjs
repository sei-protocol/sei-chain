#!/usr/bin/env node
import { ethers } from "ethers";
import { buildSigner, createDistributorContract, mapOutstandingRewards, sleep } from "./lib/distributorClient.mjs";

const USAGE = `
Usage: node autoClaimer.mjs [options]

Required environment variables:
  RPC_URL        JSON-RPC endpoint (HTTP)
  PRIVATE_KEY    Hex encoded private key for the executor wallet

Options:
  --distributor <address>   Override reward distributor address
  --user <address>          Claim for a specific address (defaults to signer address)
  --mode <borrow|supply|both>
                            Reward streams to claim (default: both)
  --loop                    Continuously poll for rewards
  --interval <seconds>      Poll interval when looping (default: 60)
  --gas-limit <number>      Gas limit hint for disburse transactions
  --dry-run                 Print actions without sending transactions
  --help                    Display this message
`;

function parseArgs() {
  const args = process.argv.slice(2);
  const config = {
    distributor: process.env.REWARD_DISTRIBUTOR,
    user: process.env.REWARD_USER,
    mode: "both",
    loop: false,
    interval: 60,
    dryRun: false,
  };

  for (let i = 0; i < args.length; ) {
    const arg = args[i];
    switch (arg) {
      case "--help":
      case "-h":
        console.log(USAGE);
        process.exit(0);
        break;
      case "--distributor":
        if (!args[i + 1]) {
          throw new Error("--distributor requires an address value");
        }
        config.distributor = args[i + 1];
        i += 2;
        break;
      case "--user":
        if (!args[i + 1]) {
          throw new Error("--user requires an address value");
        }
        config.user = args[i + 1];
        i += 2;
        break;
      case "--mode":
        if (!args[i + 1]) {
          throw new Error("--mode requires a value");
        }
        config.mode = args[i + 1];
        i += 2;
        break;
      case "--loop":
        config.loop = true;
        i += 1;
        break;
      case "--interval":
        if (!args[i + 1]) {
          throw new Error("--interval requires a numeric value");
        }
        config.interval = Number(args[i + 1]);
        i += 2;
        break;
      case "--gas-limit":
        if (!args[i + 1]) {
          throw new Error("--gas-limit requires a numeric value");
        }
        config.gasLimit = BigInt(args[i + 1]);
        if (config.gasLimit <= 0n) {
          throw new Error("--gas-limit must be positive");
        }
        i += 2;
        break;
      case "--dry-run":
        config.dryRun = true;
        i += 1;
        break;
      default:
        if (arg.startsWith("-")) {
          throw new Error(`Unknown flag: ${arg}`);
        }
        i += 1;
        break;
    }
  }

  if (!config.distributor) {
    throw new Error("Missing reward distributor address. Provide --distributor or set REWARD_DISTRIBUTOR env var.");
  }

  if (!ethers.isAddress(config.distributor)) {
    throw new Error(`Invalid distributor address provided: ${config.distributor}`);
  }

  if (config.user && !ethers.isAddress(config.user)) {
    throw new Error(`Invalid user address provided: ${config.user}`);
  }

  if (!config.mode || !["borrow", "supply", "both"].includes(config.mode)) {
    throw new Error("--mode must be one of: borrow, supply, both");
  }

  if (Number.isNaN(config.interval) || config.interval <= 0) {
    throw new Error("--interval must be a positive number of seconds");
  }

  return config;
}

async function disburseRewards({ distributor, user, rewards, mode, gasLimit, dryRun }) {
  const shouldClaimBorrow = mode === "borrow" || mode === "both";
  const shouldClaimSupply = mode === "supply" || mode === "both";

  const txs = [];
  for (const reward of rewards) {
    const amount = BigInt(reward.amount);
    if (amount === 0n) continue;

    const label = reward.isBorrowReward ? "borrow" : "supply";
    if ((label === "borrow" && !shouldClaimBorrow) || (label === "supply" && !shouldClaimSupply)) {
      continue;
    }

    if (dryRun) {
      console.log(`ℹ️  Dry run: would claim ${ethers.formatUnits(amount)} from ${reward.tToken} (${label})`);
      continue;
    }

    const method = reward.isBorrowReward
      ? "disburseBorrowerRewards"
      : "disburseSupplierRewards";

    console.log(`🚀 Claiming ${label} rewards for ${user} from ${reward.tToken}...`);
    const args = [reward.tToken, user, true];
    if (gasLimit) {
      args.push({ gasLimit });
    }
    const tx = await distributor[method](...args);
    txs.push(tx.wait());
  }

  await Promise.all(txs);
  if (!dryRun && txs.length === 0) {
    console.log("No claimable rewards for the selected mode.");
  }
}

async function main() {
  const config = parseArgs();
  const signer = buildSigner({});
  const account = config.user ?? (await signer.getAddress());
  const distributor = createDistributorContract(signer, config.distributor);

  do {
    const outstanding = await distributor.getOutstandingRewardsForUser(account);
    const rewards = mapOutstandingRewards(outstanding);

    if (rewards.length === 0) {
      console.log("No rewards returned by distributor.");
    }

    await disburseRewards({
      distributor,
      user: account,
      rewards,
      mode: config.mode,
      gasLimit: config.gasLimit,
      dryRun: config.dryRun,
    });

    if (!config.loop) break;

    console.log(`Sleeping for ${config.interval} seconds...`);
    await sleep(config.interval * 1000);
  } while (config.loop);
}

main().catch((error) => {
  console.error("❌ autoClaimer failed:", error);
  process.exitCode = 1;
});
