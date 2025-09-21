import { ethers } from "ethers";
import distributorAbi from "../abi/IMultiRewardDistributor.json" assert { type: "json" };

/**
 * Creates a typed MultiRewardDistributor contract instance.
 * @param {ethers.Signer | ethers.Provider} signerOrProvider
 * @param {string} distributorAddress
 */
export function createDistributorContract(signerOrProvider, distributorAddress) {
  if (!ethers.isAddress(distributorAddress)) {
    throw new Error(`Invalid distributor address: ${distributorAddress}`);
  }

  return new ethers.Contract(distributorAddress, distributorAbi, signerOrProvider);
}

/**
 * Resolve a signer from the provided configuration.
 * @param {{ rpcUrl?: string, privateKey?: string }} config
 */
export function buildSigner(config = {}) {
  const provider = buildProvider(config);
  const privateKey = config.privateKey ?? process.env.PRIVATE_KEY;

  if (!privateKey) {
    throw new Error("Missing private key. Provide config.privateKey or set PRIVATE_KEY env var.");
  }

  return new ethers.Wallet(privateKey, provider);
}

export function buildProvider(config = {}) {
  const rpcUrl = config.rpcUrl ?? process.env.RPC_URL;

  if (!rpcUrl) {
    throw new Error("Missing RPC URL. Provide config.rpcUrl or set RPC_URL env var.");
  }

  return new ethers.JsonRpcProvider(rpcUrl);
}

/**
 * Normalise outstanding reward tuples into friendlier objects.
 * @param {Array<{ tToken: string, rewardToken: string, amount: bigint, isBorrowReward: boolean }>} rewards
 */
export function mapOutstandingRewards(rewards) {
  return rewards.map((reward) => ({
    ...reward,
    amount: BigInt(reward.amount),
  }));
}

/**
 * Simple delay helper.
 * @param {number} ms
 */
export function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
