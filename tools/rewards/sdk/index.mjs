import { createDistributorContract } from "../lib/distributorClient.mjs";

/**
 * Build a high-level client that exposes helper methods for interacting with the distributor.
 * @param {import("ethers").Signer | import("ethers").Provider} signerOrProvider
 * @param {string} distributorAddress
 */
export function createRewardClient(signerOrProvider, distributorAddress) {
  const distributor = createDistributorContract(signerOrProvider, distributorAddress);

  return {
    distributor,
    async claimBorrowerRewards(user, options = {}) {
      const rewards = await distributor.getOutstandingRewardsForUser(user);
      const calls = rewards
        .filter((reward) => reward.isBorrowReward)
        .map((reward) => callWithOverrides(distributor, "disburseBorrowerRewards", reward, user, options));

      return Promise.all(calls);
    },
    async claimSupplierRewards(user, options = {}) {
      const rewards = await distributor.getOutstandingRewardsForUser(user);
      const calls = rewards
        .filter((reward) => !reward.isBorrowReward)
        .map((reward) => callWithOverrides(distributor, "disburseSupplierRewards", reward, user, options));

      return Promise.all(calls);
    },
    async claimAllRewards(user, options = {}) {
      const rewards = await distributor.getOutstandingRewardsForUser(user);
      const calls = rewards.map((reward) => {
        const method = reward.isBorrowReward ? "disburseBorrowerRewards" : "disburseSupplierRewards";
        return callWithOverrides(distributor, method, reward, user, options);
      });

      return Promise.all(calls);
    },
  };
}

export { createDistributorContract } from "../lib/distributorClient.mjs";

function callWithOverrides(distributor, method, reward, user, options) {
  const args = [reward.tToken, user, options.autoClaim ?? true];
  const overrides = options.overrides ?? {};
  if (overrides && Object.keys(overrides).length > 0) {
    args.push(overrides);
  }

  return distributor[method](...args);
}
