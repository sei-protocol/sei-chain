const { expect } = require("chai");
const { ethers } = require("hardhat");

describe("SeiSecurityProxy", function () {
  it("executes call through security modules", async function () {
    const RoleGate = await ethers.getContractFactory("MockRoleGate");
    const ProofDecoder = await ethers.getContractFactory("MockProofDecoder");
    const MemoInterpreter = await ethers.getContractFactory("MockMemoInterpreter");
    const RecoveryGuard = await ethers.getContractFactory("MockRecoveryGuard");
    const Proxy = await ethers.getContractFactory("SeiSecurityProxy");
    const Box = await ethers.getContractFactory("Box");

    const [roleGate, proofDecoder, memoInterpreter, recoveryGuard, proxy, box] = await Promise.all([
      RoleGate.deploy(),
      ProofDecoder.deploy(),
      MemoInterpreter.deploy(),
      RecoveryGuard.deploy(),
      Proxy.deploy(),
      Box.deploy()
    ]);

    await Promise.all([
      roleGate.waitForDeployment(),
      proofDecoder.waitForDeployment(),
      memoInterpreter.waitForDeployment(),
      recoveryGuard.waitForDeployment(),
      proxy.waitForDeployment(),
      box.waitForDeployment()
    ]);

    await proxy.setRoleGate(roleGate.target);
    await proxy.setProofDecoder(proofDecoder.target);
    await proxy.setMemoInterpreter(memoInterpreter.target);
    await proxy.setRecoveryGuard(recoveryGuard.target);

    const role = await roleGate.DEFAULT_ROLE();
    const calldata = box.interface.encodeFunctionData("store", [123]);
    await expect(proxy.execute(role, "0x", "0x", box.target, calldata)).to.not.be.reverted;
    expect(await box.retrieve()).to.equal(123n);
  });
});
