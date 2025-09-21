import { Contract, getAddress, isAddress } from "ethers";
import { erc20Abi, rewardDistributorAbi } from "./abiRegistry.js";
import type {
  ClaimEstimate,
  ClaimEstimateDetail,
  EstimateClaimGasOptions,
  RewardClientConfig,
  RewardMode,
  RewardStream,
  SweepOptions,
  SweepResult,
} from "./types.js";

const CLAIM_METHODS: Record<RewardMode, "disburseSupplierRewards" | "disburseBorrowerRewards"> = {
  supply: "disburseSupplierRewards",
  borrow: "disburseBorrowerRewards",
};

function normalizeMode(value: unknown): RewardMode {
  if (typeof value === "string") {
    const normalized = value.toLowerCase();
    if (normalized === "borrow") {
      return "borrow";
    }
    if (normalized === "supply") {
      return "supply";
    }
    const numeric = Number.parseInt(normalized, 10);
    if (!Number.isNaN(numeric)) {
      return numeric === 1 ? "borrow" : "supply";
    }
  }
  if (typeof value === "number") {
    return value === 1 ? "borrow" : "supply";
  }
  if (typeof value === "bigint") {
    return value === 1n ? "borrow" : "supply";
  }
  return "supply";
}

function toBigInt(value: unknown): bigint {
  if (typeof value === "bigint") {
    return value;
  }
  if (typeof value === "number") {
    return BigInt(Math.trunc(value));
  }
  if (typeof value === "string") {
    return BigInt(value);
  }
  return 0n;
}

function normalizeRewardRecord(record: any): RewardStream {
  const market = getAddress(record?.tToken ?? record?.market ?? record?.[0]);
  const rewardToken = getAddress(record?.rewardToken ?? record?.token ?? record?.[1]);
  const amount = toBigInt(record?.amount ?? record?.totalAccrued ?? record?.[2]);
  const mode = normalizeMode(record?.rewardType ?? record?.mode ?? record?.[3]);
  return { market, rewardToken, amount, mode };
}

function ensureAddress(address: string, field: string): string {
  if (!isAddress(address)) {
    throw new Error(`Invalid address for ${field}: ${address}`);
  }
  return getAddress(address);
}

export class RewardClient {
  private readonly config: RewardClientConfig;
  private readonly distributor: Contract;

  constructor(config: RewardClientConfig) {
    this.config = config;
    const runner = config.signer ?? config.runner ?? config.provider;
    this.distributor = new Contract(config.distributorAddress, rewardDistributorAbi, runner);
  }

  async getOutstandingRewards(user: string): Promise<RewardStream[]> {
    const normalizedUser = ensureAddress(user, "user");
    const response = await this.distributor.getOutstandingRewardsForUser(normalizedUser);
    if (!Array.isArray(response)) {
      return [];
    }
    return response.map((entry: any) => normalizeRewardRecord(entry));
  }

  async getMarketsWithRewards(user: string): Promise<string[]> {
    const rewards = await this.getOutstandingRewards(user);
    const markets = new Set<string>();
    for (const reward of rewards) {
      if (reward.amount > 0n) {
        markets.add(reward.market);
      }
    }
    return Array.from(markets);
  }

  async estimateClaimGas(user: string, options: EstimateClaimGasOptions = {}): Promise<ClaimEstimate> {
    const normalizedUser = ensureAddress(user, "user");
    const rewards = await this.getOutstandingRewards(normalizedUser);
    const filtered = options.mode ? rewards.filter((reward) => reward.mode === options.mode) : rewards;

    let totalGas: bigint | undefined;
    const details: ClaimEstimateDetail[] = [];
    for (const reward of filtered) {
      const method = CLAIM_METHODS[reward.mode];
      const estimator = (this.distributor.estimateGas as Record<string, Function | undefined>)[method];
      if (typeof estimator !== "function") {
        details.push({
          ...reward,
          gasLimit: undefined,
          error: `Gas estimator not available for method ${method}`,
        });
        continue;
      }
      try {
        const gasLimit = await estimator.call(this.distributor.estimateGas, normalizedUser, reward.market);
        const gasBigInt = toBigInt(gasLimit);
        totalGas = totalGas === undefined ? gasBigInt : totalGas + gasBigInt;
        details.push({ ...reward, gasLimit: gasBigInt });
      } catch (error) {
        const message = error instanceof Error ? error.message : String(error);
        details.push({ ...reward, error: message });
      }
    }

    return {
      user: normalizedUser,
      totalGas,
      details,
    };
  }

  async sweepTokensTo(target: string, options: SweepOptions = {}): Promise<SweepResult> {
    if (!this.config.signer) {
      throw new Error("A signer is required to sweep tokens");
    }
    const signer = this.config.signer;
    const normalizedTarget = ensureAddress(target, "target");
    const from = ensureAddress(
      options.from ?? this.config.sweep?.from ?? (await signer.getAddress()),
      "sweep.from"
    );
    const tokens = (options.tokens ?? this.config.sweep?.tokens ?? []).map((token, index) =>
      ensureAddress(token, `sweep.tokens[${index}]`)
    );
    const waitForReceipt = options.waitForReceipt ?? true;

    const transfers = [] as SweepResult["transfers"];
    for (const token of tokens) {
      const contract = new Contract(token, erc20Abi, signer);
      const balance: bigint = await contract.balanceOf(from);
      if (balance === 0n) {
        continue;
      }
      const tx = await contract.transfer(normalizedTarget, balance);
      if (waitForReceipt) {
        await tx.wait();
      }
      transfers.push({ token, amount: balance, transactionHash: tx.hash });
    }

    return {
      from,
      target: normalizedTarget,
      transfers,
    };
  }
}
