#!/usr/bin/env node
import { readFile, writeFile } from "fs/promises";
import path from "path";
import { fileURLToPath } from "url";
import {
  normaliseConfig,
  mergeOutstandingRewards,
  buildGovernancePlan,
  formatPlanAsText,
  formatPlanAsMarkdown,
} from "./lib/aiPlanner.mjs";
import { isAddress, normalizeAddress } from "./lib/tokenMath.mjs";

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const DEFAULT_CONFIG_PATH = path.resolve(__dirname, "templates/governance-config.example.json");

const USAGE = `
Usage: node aiScribe.mjs [options]

Options:
  --config <path>             Path to governance config JSON (default: templates/governance-config.example.json)
  --distributor <address>     Reward distributor contract address
  --accounts <addr1,addr2>    Comma-separated list of accounts to analyse
  --forecast-horizon <days>   Forecast window in days (default: 30)
  --format <text|markdown|json>
                             Output format (default: text)
  --export <path>             Write the plan to a file
  --rpc <url>                 Override RPC URL (defaults to RPC_URL env)
  --offline                   Skip on-chain queries and rely on config seed data
  --help                      Show this message
`;

function parseArgs() {
  const args = process.argv.slice(2);
  const config = {
    configPath: process.env.GOVERNANCE_CONFIG ?? DEFAULT_CONFIG_PATH,
    distributor: process.env.REWARD_DISTRIBUTOR ?? null,
    accounts: process.env.GOVERNANCE_ACCOUNTS ?? null,
    forecastHorizon: process.env.GOVERNANCE_FORECAST ? Number(process.env.GOVERNANCE_FORECAST) : 30,
    format: process.env.GOVERNANCE_FORMAT ?? "text",
    exportPath: process.env.GOVERNANCE_EXPORT ?? null,
    rpcUrl: process.env.GOVERNANCE_RPC ?? null,
    offline: false,
  };

  for (let i = 0; i < args.length; ) {
    const arg = args[i];
    switch (arg) {
      case "--help":
      case "-h":
        console.log(USAGE);
        process.exit(0);
        break;
      case "--config":
        config.configPath = args[i + 1];
        if (!config.configPath) {
          throw new Error("--config requires a path value");
        }
        i += 2;
        break;
      case "--distributor":
        config.distributor = args[i + 1];
        if (!config.distributor) {
          throw new Error("--distributor requires an address value");
        }
        i += 2;
        break;
      case "--accounts":
        config.accounts = args[i + 1];
        if (!config.accounts) {
          throw new Error("--accounts requires a value");
        }
        i += 2;
        break;
      case "--forecast-horizon":
        config.forecastHorizon = Number(args[i + 1]);
        if (!Number.isFinite(config.forecastHorizon) || config.forecastHorizon <= 0) {
          throw new Error("--forecast-horizon must be a positive number");
        }
        i += 2;
        break;
      case "--format":
        config.format = args[i + 1];
        if (!config.format) {
          throw new Error("--format requires a value");
        }
        i += 2;
        break;
      case "--export":
        config.exportPath = args[i + 1];
        if (!config.exportPath) {
          throw new Error("--export requires a path value");
        }
        i += 2;
        break;
      case "--rpc":
        config.rpcUrl = args[i + 1];
        if (!config.rpcUrl) {
          throw new Error("--rpc requires a URL");
        }
        i += 2;
        break;
      case "--offline":
        config.offline = true;
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

  if (!config.format || !["text", "markdown", "json"].includes(config.format)) {
    throw new Error("--format must be one of: text, markdown, json");
  }

  return config;
}

async function loadJson(pathname) {
  const contents = await readFile(pathname, "utf8");
  return JSON.parse(contents);
}

function parseAccounts(input, configAccounts = []) {
  const values = [];
  if (typeof input === "string" && input.length > 0) {
    values.push(...input.split(","));
  }
  if (Array.isArray(input)) {
    values.push(...input);
  }
  if (Array.isArray(configAccounts)) {
    values.push(...configAccounts);
  }
  const seen = new Set();
  const accounts = [];
  for (const raw of values) {
    const candidate = raw?.trim();
    if (!candidate) continue;
    if (!isAddress(candidate)) {
      throw new Error(`Invalid account address provided: ${candidate}`);
    }
    const normalised = normalizeAddress(candidate);
    if (seen.has(normalised)) continue;
    seen.add(normalised);
    accounts.push(normalised);
  }
  return accounts;
}

async function main() {
  const args = parseArgs();
  const configPath = path.resolve(args.configPath);
  let rawConfig = {};
  try {
    rawConfig = await loadJson(configPath);
  } catch (error) {
    if (error.code === "ENOENT") {
      console.warn(`⚠️  Config file not found at ${configPath}. Proceeding with minimal defaults.`);
    } else {
      throw error;
    }
  }

  const normalisedConfig = normaliseConfig(rawConfig);
  const distributorAddress = args.distributor ?? normalisedConfig.distributor;
  if (!args.offline) {
    if (!distributorAddress) {
      throw new Error("Distributor address is required. Provide via --distributor, config file, or REWARD_DISTRIBUTOR env var.");
    }
    if (!isAddress(distributorAddress)) {
      throw new Error(`Invalid distributor address: ${distributorAddress}`);
    }
  }

  const accounts = parseAccounts(args.accounts, normalisedConfig.accounts);
  let outstanding = new Map();

  if (!args.offline) {
    const { buildProvider, createDistributorContract, mapOutstandingRewards } = await import("../rewards/lib/distributorClient.mjs");
    const provider = buildProvider({ rpcUrl: args.rpcUrl ?? undefined });
    const distributor = createDistributorContract(provider, distributorAddress);
    if (accounts.length === 0) {
      console.warn("⚠️  No accounts provided. Planner will only use seedOutstanding values from config.");
    }
    for (const account of accounts) {
      const rewards = await distributor.getOutstandingRewardsForUser(account);
      const mapped = mapOutstandingRewards(rewards);
      outstanding = mergeOutstandingRewards(outstanding, mapped);
    }
  }

  const plan = buildGovernancePlan({
    config: { ...normalisedConfig, distributor: distributorAddress },
    outstandingByStream: outstanding,
    accounts,
    horizonDays: args.forecastHorizon,
  });

  let output;
  if (args.format === "json") {
    output = JSON.stringify(plan, null, 2);
  } else if (args.format === "markdown") {
    output = formatPlanAsMarkdown(plan);
  } else {
    output = formatPlanAsText(plan);
  }

  console.log(output);

  if (args.exportPath) {
    const exportTarget = path.resolve(args.exportPath);
    await writeFile(exportTarget, output + (args.format === "json" ? "" : "\n"), "utf8");
    console.log(`✅ Plan written to ${exportTarget}`);
  }
}

main().catch((error) => {
  console.error("❌ aiScribe failed:", error);
  process.exitCode = 1;
});
