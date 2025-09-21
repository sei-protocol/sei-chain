export const rewardDistributorAbi = [
  "function getOutstandingRewardsForUser(address user) view returns ((address tToken, address rewardToken, uint256 amount, uint8 rewardType)[])",
  "function disburseSupplierRewards(address user, address market)",
  "function disburseBorrowerRewards(address user, address market)",
];

export const erc20Abi = [
  "function balanceOf(address owner) view returns (uint256)",
  "function transfer(address to, uint256 amount) returns (bool)",
];

export type AbiRegistryKey = "rewardDistributor" | "erc20";

const abiRegistry: Record<AbiRegistryKey, readonly string[]> = {
  rewardDistributor: rewardDistributorAbi,
  erc20: erc20Abi,
};

export function getAbi(key: AbiRegistryKey): readonly string[] {
  return abiRegistry[key];
}
