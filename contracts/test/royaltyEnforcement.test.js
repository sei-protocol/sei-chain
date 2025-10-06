const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("KinVault Royalty", function () {
  let enforcer;
  let vault;
  let owner;
  let sink;

  beforeEach(async function () {
    [owner, sink] = await ethers.getSigners();

    const Enforcer = await ethers.getContractFactory("KinRoyaltyEnforcer");
    enforcer = await Enforcer.deploy(sink.address);
    await enforcer.waitForDeployment();

    const Vault = await ethers.getContractFactory("VaultScannerV2WithGasProof");
    vault = await Vault.deploy(await enforcer.getAddress());
    await vault.waitForDeployment();
  });

  it("pays royalty on vault write", async function () {
    const gasPrice = ethers.parseUnits("1", "gwei");
    const sstoreGasCost = await enforcer.SSTORE_GAS_COST();
    const expected = (sstoreGasCost * gasPrice) / 10n;

    const key = ethers.encodeBytes32String("kinvault-key");
    const value = ethers.encodeBytes32String("kinvault-value");

    const initialSinkBalance = await ethers.provider.getBalance(sink.address);

    const tx = await vault.write(key, value, {
      value: expected,
      gasPrice,
    });
    await tx.wait();

    await expect(tx).to.emit(enforcer, "RoyaltyPaid").withArgs(owner.address, expected);

    const stored = await vault.vault(key);
    expect(stored).to.equal(value);

    const finalSinkBalance = await ethers.provider.getBalance(sink.address);
    const delta = finalSinkBalance - initialSinkBalance;
    expect(delta).to.equal(expected);
  });
});
