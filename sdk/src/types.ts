import type { ContractRunner, Provider, Signer } from "ethers";

export type RewardMode = "supply" | "borrow";

export interface RewardStream {
  market: string;
  rewardToken: string;
  amount: bigint;
  mode: RewardMode;
}

export interface RewardAgent {
  id: string;
  address: string;
  type: RewardMode | "compounder";
  autoClaim?: boolean;
  tags?: string[];
}

export interface ClaimEstimateDetail {
  market: string;
  rewardToken: string;
  amount: bigint;
  mode: RewardMode;
  gasLimit?: bigint;
  error?: string;
}

export interface ClaimEstimate {
  user: string;
  totalGas?: bigint;
  details: ClaimEstimateDetail[];
}

export interface SweepTransferResult {
  token: string;
  amount: bigint;
  transactionHash: string;
}

export interface SweepResult {
  from: string;
  target: string;
  transfers: SweepTransferResult[];
}

export interface SweepConfig {
  tokens: string[];
  from?: string;
  unwrapWsei?: boolean;
}

export interface RewardClientConfig {
  provider: Provider;
  distributorAddress: string;
  signer?: Signer;
  sweep?: SweepConfig;
  runner?: ContractRunner;
}

export interface RewardConfigInput {
  rpcUrl?: string;
  provider?: Provider;
  signer?: Signer;
  distributorAddress: string;
  sweep?: Partial<SweepConfig> | null;
}

export interface EstimateClaimGasOptions {
  mode?: RewardMode;
}

export interface SweepOptions {
  tokens?: string[];
  waitForReceipt?: boolean;
  from?: string;
}
