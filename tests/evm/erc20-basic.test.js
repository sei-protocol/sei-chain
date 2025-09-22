const { ethers } = require("hardhat");
const { expect } = require("chai");

describe("ERC20 Basic Interop", function () {
  let token, deployer;

  before(async function () {
    [deployer] = await ethers.getSigners();
    const ERC20Mock = await ethers.getContractFactory("ERC20Mock");
    token = await ERC20Mock.deploy("TestToken", "TT", deployer.address, 1000);
    await token.waitForDeployment(); // Hardhat v6+ replacement for .deployed()
  });

  it("should assign initial supply to the deployer", async function () {
    const balance = await token.balanceOf(deployer.address);
    expect(balance).to.equal(1000);
  });
});

