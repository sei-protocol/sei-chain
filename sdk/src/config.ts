import { JsonRpcProvider, getAddress, isAddress } from "ethers";
import type { Provider } from "ethers";
import type { RewardClientConfig, RewardConfigInput, SweepConfig } from "./types.js";

function buildProvider(input: RewardConfigInput): Provider {
  if (input.provider) {
    return input.provider;
  }
  if (!input.rpcUrl) {
    throw new Error("Reward configuration requires either a provider or rpcUrl");
  }
  return new JsonRpcProvider(input.rpcUrl);
}

function normalizeAddress(address: string, field: string): string {
  if (!isAddress(address)) {
    throw new Error(`Invalid address for ${field}: ${address}`);
  }
  return getAddress(address);
}

function normalizeSweepConfig(sweep?: Partial<SweepConfig> | null): SweepConfig | undefined {
  if (!sweep) {
    return undefined;
  }
  const tokens = (sweep.tokens ?? []).map((token, index) =>
    normalizeAddress(token, `sweep.tokens[${index}]`)
  );
  return {
    tokens,
    from: sweep.from ? normalizeAddress(sweep.from, "sweep.from") : undefined,
    unwrapWsei: sweep.unwrapWsei ?? false,
  };
}

export function parseRewardConfig(input: RewardConfigInput): RewardClientConfig {
  if (!input.distributorAddress) {
    throw new Error("Reward configuration requires distributorAddress");
  }
  const provider = buildProvider(input);
  const distributorAddress = normalizeAddress(input.distributorAddress, "distributorAddress");
  return {
    provider,
    distributorAddress,
    signer: input.signer,
    sweep: normalizeSweepConfig(input.sweep ?? undefined),
  };
}

export interface EnvConfigShape {
  REWARD_RPC_URL?: string;
  RPC_URL?: string;
  SEI_RPC_URL?: string;
  REWARD_DISTRIBUTOR_ADDRESS?: string;
  DISTRIBUTOR_ADDRESS?: string;
  REWARD_SWEEP_TOKENS?: string;
  SWEEP_TOKENS?: string;
  REWARD_SWEEP_FROM?: string;
  SWEEP_FROM?: string;
  REWARD_SWEEP_UNWRAP_WSEI?: string;
  SWEEP_UNWRAP_WSEI?: string;
}

function parseTokens(value?: string): string[] {
  if (!value) {
    return [];
  }
  return value
    .split(",")
    .map((entry) => entry.trim())
    .filter((entry) => entry.length > 0);
}

function parseBoolean(value?: string): boolean | undefined {
  if (value === undefined) {
    return undefined;
  }
  return value === "1" || value.toLowerCase() === "true";
}

export function parseRewardConfigFromEnv(env: EnvConfigShape = {}): RewardClientConfig {
  const rpcUrl = env.REWARD_RPC_URL ?? env.RPC_URL ?? env.SEI_RPC_URL;
  const distributorAddress = env.REWARD_DISTRIBUTOR_ADDRESS ?? env.DISTRIBUTOR_ADDRESS;
  if (!rpcUrl) {
    throw new Error("Environment config missing RPC URL (REWARD_RPC_URL or RPC_URL)");
  }
  if (!distributorAddress) {
    throw new Error(
      "Environment config missing distributor address (REWARD_DISTRIBUTOR_ADDRESS or DISTRIBUTOR_ADDRESS)"
    );
  }
  const sweepTokens = parseTokens(env.REWARD_SWEEP_TOKENS ?? env.SWEEP_TOKENS);
  const sweepFrom = env.REWARD_SWEEP_FROM ?? env.SWEEP_FROM;
  const unwrapWsei = parseBoolean(env.REWARD_SWEEP_UNWRAP_WSEI ?? env.SWEEP_UNWRAP_WSEI);

  return parseRewardConfig({
    rpcUrl,
    distributorAddress,
    sweep: {
      tokens: sweepTokens,
      from: sweepFrom,
      unwrapWsei: unwrapWsei ?? false,
    },
  });
}
