import { ethers } from "hardhat";

async function main() {
  const [deployer] = await ethers.getSigners();

  const Royalty = await ethers.getContractFactory("KinRoyaltyEnforcer");
  const royalty = await Royalty.deploy(deployer.address);
  await royalty.waitForDeployment();

  const Vault = await ethers.getContractFactory("VaultScannerV2WithGasProof");
  const vault = await Vault.deploy(await royalty.getAddress());
  await vault.waitForDeployment();

  console.log("Royalty:", await royalty.getAddress());
  console.log("Vault:", await vault.getAddress());
}

main().catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
