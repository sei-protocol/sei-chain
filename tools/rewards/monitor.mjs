#!/usr/bin/env node
import fs from "node:fs";
import { ethers } from "ethers";
import { buildProvider, createDistributorContract, mapOutstandingRewards, sleep } from "./lib/distributorClient.mjs";

const USAGE = `
Usage: node monitor.mjs [options]

Environment variables:
  RPC_URL                 JSON-RPC endpoint (HTTP)

Options:
  --distributor <address>   Reward distributor contract address (required)
  --user <address>          User account to inspect (default: distributor signer)
  --interval <seconds>      Poll interval (default: 300)
  --webhook <url>           POST reward snapshots to webhook URL
  --metrics-file <path>     Append JSON metrics into a file (rotatable via logrotate)
  --once                    Run a single iteration and exit
  --help                    Show this message
`;

function parseArgs() {
  const args = process.argv.slice(2);
  const config = {
    distributor: process.env.REWARD_DISTRIBUTOR,
    user: process.env.REWARD_USER,
    interval: Number(process.env.MONITOR_INTERVAL ?? 300),
    webhook: process.env.MONITOR_WEBHOOK,
    metricsFile: process.env.MONITOR_METRICS_FILE,
    once: false,
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
        if (!args[i + 1]) throw new Error("--distributor requires an address value");
        config.distributor = args[i + 1];
        i += 2;
        break;
      case "--user":
        if (!args[i + 1]) throw new Error("--user requires an address value");
        config.user = args[i + 1];
        i += 2;
        break;
      case "--interval":
        if (!args[i + 1]) throw new Error("--interval requires a numeric value");
        config.interval = Number(args[i + 1]);
        i += 2;
        break;
      case "--webhook":
        if (!args[i + 1]) throw new Error("--webhook requires a URL value");
        config.webhook = args[i + 1];
        i += 2;
        break;
      case "--metrics-file":
        if (!args[i + 1]) throw new Error("--metrics-file requires a path");
        config.metricsFile = args[i + 1];
        i += 2;
        break;
      case "--once":
        config.once = true;
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
    throw new Error("Reward distributor address missing. Provide --distributor or set REWARD_DISTRIBUTOR env var.");
  }

  if (!ethers.isAddress(config.distributor)) {
    throw new Error(`Invalid distributor address: ${config.distributor}`);
  }

  if (config.user && !ethers.isAddress(config.user)) {
    throw new Error(`Invalid user address: ${config.user}`);
  }

  if (Number.isNaN(config.interval) || config.interval <= 0) {
    throw new Error("--interval must be a positive number");
  }

  return config;
}

async function emitMetrics(config, snapshot) {
  if (config.metricsFile) {
    try {
      const payload = JSON.stringify(snapshot);
      await fs.promises.appendFile(config.metricsFile, `${payload}\n`);
    } catch (error) {
      console.error("⚠️ Failed to append metrics file:", error);
    }
  }

  if (config.webhook) {
    try {
      const response = await fetch(config.webhook, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(snapshot),
      });
      if (!response.ok) {
        console.error(`⚠️ Webhook responded with status ${response.status}`);
      }
    } catch (error) {
      console.error("⚠️ Failed to send webhook payload:", error);
    }
  }
}

async function runMonitor(config) {
  const provider = buildProvider({});
  const distributor = createDistributorContract(provider, config.distributor);
  const account = config.user ?? config.distributor;

  do {
    const startedAt = Date.now();
    const rewards = mapOutstandingRewards(await distributor.getOutstandingRewardsForUser(account));
    const summary = rewards.reduce(
      (acc, reward) => {
        const key = reward.isBorrowReward ? "borrow" : "supply";
        acc[key].push(reward);
        acc.totalValue = acc.totalValue + reward.amount;
        return acc;
      },
      { borrow: [], supply: [], totalValue: 0n }
    );

    const snapshot = {
      timestamp: new Date().toISOString(),
      distributor: config.distributor,
      user: account,
      outstandingBorrow: summary.borrow.map((reward) => ({
        market: reward.tToken,
        token: reward.rewardToken,
        amount: reward.amount.toString(),
      })),
      outstandingSupply: summary.supply.map((reward) => ({
        market: reward.tToken,
        token: reward.rewardToken,
        amount: reward.amount.toString(),
      })),
      totalOutstanding: summary.totalValue.toString(),
    };

    console.log(
      `[${snapshot.timestamp}] Borrow rewards: ${summary.borrow.length}, Supply rewards: ${summary.supply.length}, ` +
        `Total raw amount: ${summary.totalValue.toString()}`
    );
    await emitMetrics(config, snapshot);

    if (config.once) break;

    const elapsed = Date.now() - startedAt;
    const sleepMs = Math.max(0, config.interval * 1000 - elapsed);
    if (sleepMs > 0) {
      await sleep(sleepMs);
    }
  } while (!config.once);
}

async function main() {
  try {
    const config = parseArgs();
    await runMonitor(config);
  } catch (error) {
    console.error("❌ monitor failed:", error);
    process.exitCode = 1;
  }
}

main();
