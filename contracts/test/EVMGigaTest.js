const { expect } = require("chai");
const hre = require('hardhat');
const { ethers } = hre;
const { setupSigners, fundAddress, delay } = require("./lib");

/**
 * EVMGigaTest - Integration tests for GIGA executor mode
 * 
 * These tests verify core EVM functionality when running with the GIGA executor
 * (evmone-based) and OCC (Optimistic Concurrency Control) enabled.
 * 
 * Tests cover:
 * - Native SEI transfers between accounts
 * - ERC20 contract deployment
 * - ERC20 token transfers
 * - Balance verification before/after operations
 */

describe("GIGA EVM Tests", function () {
  let owner;
  let accounts;

  before(async function () {
    // Set up signers and fund them
    accounts = await setupSigners(await ethers.getSigners());
    owner = accounts[0].signer;
  });

  describe("Native Transfers", function () {
    it("should transfer native SEI between accounts", async function () {
      const recipient = ethers.Wallet.createRandom().connect(ethers.provider);
      const recipientAddress = await recipient.getAddress();
      
      // Get initial balances
      const senderBalanceBefore = await ethers.provider.getBalance(owner.address);
      const recipientBalanceBefore = await ethers.provider.getBalance(recipientAddress);
      
      // Recipient should start with 0 balance
      expect(recipientBalanceBefore).to.equal(0n);
      
      // Send 1 SEI (1e18 wei)
      const transferAmount = ethers.parseEther("1");
      const tx = await owner.sendTransaction({
        to: recipientAddress,
        value: transferAmount,
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      const receipt = await tx.wait();
      
      // Verify transaction succeeded
      expect(receipt.status).to.equal(1);
      
      // Get final balances
      const senderBalanceAfter = await ethers.provider.getBalance(owner.address);
      const recipientBalanceAfter = await ethers.provider.getBalance(recipientAddress);
      
      // Recipient should have received exactly the transfer amount
      expect(recipientBalanceAfter).to.equal(transferAmount);
      
      // Sender should have decreased by transfer amount + gas
      const gasUsed = receipt.gasUsed;
      const gasPrice = receipt.gasPrice;
      const gasCost = gasUsed * gasPrice;
      const expectedSenderBalance = senderBalanceBefore - transferAmount - gasCost;
      expect(senderBalanceAfter).to.equal(expectedSenderBalance);
    });

    it("should handle multiple sequential transfers", async function () {
      const recipient1 = ethers.Wallet.createRandom().connect(ethers.provider);
      const recipient2 = ethers.Wallet.createRandom().connect(ethers.provider);
      
      const amount1 = ethers.parseEther("0.5");
      const amount2 = ethers.parseEther("0.3");
      
      // First transfer
      const tx1 = await owner.sendTransaction({
        to: await recipient1.getAddress(),
        value: amount1,
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      await tx1.wait();
      
      // Second transfer
      const tx2 = await owner.sendTransaction({
        to: await recipient2.getAddress(),
        value: amount2,
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      await tx2.wait();
      
      // Verify both recipients received correct amounts
      const balance1 = await ethers.provider.getBalance(await recipient1.getAddress());
      const balance2 = await ethers.provider.getBalance(await recipient2.getAddress());
      
      expect(balance1).to.equal(amount1);
      expect(balance2).to.equal(amount2);
    });
  });

  describe("ERC20 Deployment", function () {
    let testToken;
    
    it("should deploy TestToken contract", async function () {
      const TestToken = await ethers.getContractFactory("TestToken");
      testToken = await TestToken.deploy("GigaTestToken", "GTT", {
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      await testToken.waitForDeployment();
      
      const tokenAddress = await testToken.getAddress();
      expect(tokenAddress).to.be.properAddress;
      
      // Verify contract code was deployed
      const code = await ethers.provider.getCode(tokenAddress);
      expect(code.length).to.be.greaterThan(2); // More than just "0x"
    });

    it("should have correct initial token supply", async function () {
      const TestToken = await ethers.getContractFactory("TestToken");
      testToken = await TestToken.deploy("GigaTestToken", "GTT", {
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      await testToken.waitForDeployment();
      
      // TestToken.sol mints 1000 * (10 ** decimals()) to msg.sender
      // decimals() is 18 by default in OpenZeppelin ERC20
      const expectedInitialSupply = ethers.parseUnits("1000", 18);
      
      const ownerBalance = await testToken.balanceOf(owner.address);
      const totalSupply = await testToken.totalSupply();
      
      // Verify the Solidity behavior: deployer receives 1000e18 tokens
      expect(ownerBalance).to.equal(expectedInitialSupply);
      expect(totalSupply).to.equal(expectedInitialSupply);
    });

    it("should have correct token metadata", async function () {
      const TestToken = await ethers.getContractFactory("TestToken");
      testToken = await TestToken.deploy("MetadataToken", "MTK", {
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      await testToken.waitForDeployment();
      
      const name = await testToken.name();
      const symbol = await testToken.symbol();
      const decimals = await testToken.decimals();
      
      expect(name).to.equal("MetadataToken");
      expect(symbol).to.equal("MTK");
      expect(decimals).to.equal(18n);
    });
  });

  describe("ERC20 Transfers", function () {
    let testToken;
    const initialSupply = ethers.parseUnits("1000", 18);
    
    beforeEach(async function () {
      const TestToken = await ethers.getContractFactory("TestToken");
      testToken = await TestToken.deploy("TransferToken", "TFR", {
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      await testToken.waitForDeployment();
    });

    it("should transfer ERC20 tokens between accounts", async function () {
      const recipient = accounts[1] ? accounts[1].signer : ethers.Wallet.createRandom().connect(ethers.provider);
      const recipientAddress = await recipient.getAddress();
      
      // Fund recipient if it's a new wallet
      if (!accounts[1]) {
        await fundAddress(recipientAddress);
        await delay();
      }
      
      const transferAmount = ethers.parseUnits("100", 18);
      
      // Get balances before transfer
      const ownerBalanceBefore = await testToken.balanceOf(owner.address);
      const recipientBalanceBefore = await testToken.balanceOf(recipientAddress);
      
      // Verify initial state
      expect(ownerBalanceBefore).to.equal(initialSupply);
      expect(recipientBalanceBefore).to.equal(0n);
      
      // Execute transfer
      const tx = await testToken.transfer(recipientAddress, transferAmount, {
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      const receipt = await tx.wait();
      
      // Verify transaction succeeded
      expect(receipt.status).to.equal(1);
      
      // Get balances after transfer
      const ownerBalanceAfter = await testToken.balanceOf(owner.address);
      const recipientBalanceAfter = await testToken.balanceOf(recipientAddress);
      
      // Verify balances changed correctly
      expect(ownerBalanceAfter).to.equal(ownerBalanceBefore - transferAmount);
      expect(recipientBalanceAfter).to.equal(transferAmount);
      
      // Verify total supply unchanged
      const totalSupply = await testToken.totalSupply();
      expect(totalSupply).to.equal(initialSupply);
    });

    it("should emit Transfer event on ERC20 transfer", async function () {
      const recipient = ethers.Wallet.createRandom().connect(ethers.provider);
      const recipientAddress = await recipient.getAddress();
      const transferAmount = ethers.parseUnits("50", 18);
      
      // Execute transfer and wait for receipt explicitly
      const tx = await testToken.transfer(recipientAddress, transferAmount, {
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      const receipt = await tx.wait();
      
      // Verify transaction succeeded
      expect(receipt.status).to.equal(1);
      
      // Check for Transfer event in logs
      const transferEvent = receipt.logs.find(log => {
        try {
          const parsed = testToken.interface.parseLog(log);
          return parsed && parsed.name === 'Transfer';
        } catch {
          return false;
        }
      });
      
      expect(transferEvent).to.not.be.undefined;
      
      // Parse and verify event args
      const parsedEvent = testToken.interface.parseLog(transferEvent);
      expect(parsedEvent.args.from).to.equal(owner.address);
      expect(parsedEvent.args.to).to.equal(recipientAddress);
      expect(parsedEvent.args.value).to.equal(transferAmount);
    });

    it("should revert when transferring more than balance", async function () {
      const recipient = ethers.Wallet.createRandom().connect(ethers.provider);
      const recipientAddress = await recipient.getAddress();
      
      // Try to transfer more than the owner has
      const excessAmount = ethers.parseUnits("2000", 18); // More than 1000 initial supply
      
      await expect(
        testToken.transfer(recipientAddress, excessAmount, {
          gasPrice: ethers.parseUnits('100', 'gwei')
        })
      ).to.be.reverted;
    });

    it("should handle multiple ERC20 transfers in sequence", async function () {
      const recipient1 = ethers.Wallet.createRandom().connect(ethers.provider);
      const recipient2 = ethers.Wallet.createRandom().connect(ethers.provider);
      const recipient3 = ethers.Wallet.createRandom().connect(ethers.provider);
      
      const amount1 = ethers.parseUnits("100", 18);
      const amount2 = ethers.parseUnits("200", 18);
      const amount3 = ethers.parseUnits("150", 18);
      
      // Execute three sequential transfers
      await (await testToken.transfer(await recipient1.getAddress(), amount1, {
        gasPrice: ethers.parseUnits('100', 'gwei')
      })).wait();
      
      await (await testToken.transfer(await recipient2.getAddress(), amount2, {
        gasPrice: ethers.parseUnits('100', 'gwei')
      })).wait();
      
      await (await testToken.transfer(await recipient3.getAddress(), amount3, {
        gasPrice: ethers.parseUnits('100', 'gwei')
      })).wait();
      
      // Verify all balances
      const balance1 = await testToken.balanceOf(await recipient1.getAddress());
      const balance2 = await testToken.balanceOf(await recipient2.getAddress());
      const balance3 = await testToken.balanceOf(await recipient3.getAddress());
      const ownerBalance = await testToken.balanceOf(owner.address);
      
      expect(balance1).to.equal(amount1);
      expect(balance2).to.equal(amount2);
      expect(balance3).to.equal(amount3);
      expect(ownerBalance).to.equal(initialSupply - amount1 - amount2 - amount3);
      
      // Total supply should remain unchanged
      const totalSupply = await testToken.totalSupply();
      expect(totalSupply).to.equal(initialSupply);
    });
  });

  describe("ERC20 Approvals and TransferFrom", function () {
    let testToken;
    const initialSupply = ethers.parseUnits("1000", 18);
    
    beforeEach(async function () {
      const TestToken = await ethers.getContractFactory("TestToken");
      testToken = await TestToken.deploy("ApprovalToken", "APR", {
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      await testToken.waitForDeployment();
    });

    it("should approve and transferFrom ERC20 tokens", async function () {
      // Use second account as spender, or create a new one
      let spender;
      if (accounts[1]) {
        spender = accounts[1].signer;
      } else {
        spender = ethers.Wallet.createRandom().connect(ethers.provider);
        await fundAddress(await spender.getAddress());
        await delay();
      }
      const spenderAddress = await spender.getAddress();
      
      const recipient = ethers.Wallet.createRandom().connect(ethers.provider);
      const recipientAddress = await recipient.getAddress();
      
      const approveAmount = ethers.parseUnits("200", 18);
      const transferAmount = ethers.parseUnits("150", 18);
      
      // Approve spender
      const approveTx = await testToken.approve(spenderAddress, approveAmount, {
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      await approveTx.wait();
      
      // Verify allowance
      const allowance = await testToken.allowance(owner.address, spenderAddress);
      expect(allowance).to.equal(approveAmount);
      
      // Connect as spender and execute transferFrom
      const tokenAsSpender = testToken.connect(spender);
      const transferTx = await tokenAsSpender.transferFrom(
        owner.address,
        recipientAddress,
        transferAmount,
        { gasPrice: ethers.parseUnits('100', 'gwei') }
      );
      await transferTx.wait();
      
      // Verify balances
      const recipientBalance = await testToken.balanceOf(recipientAddress);
      const ownerBalance = await testToken.balanceOf(owner.address);
      
      expect(recipientBalance).to.equal(transferAmount);
      expect(ownerBalance).to.equal(initialSupply - transferAmount);
      
      // Verify allowance decreased
      const remainingAllowance = await testToken.allowance(owner.address, spenderAddress);
      expect(remainingAllowance).to.equal(approveAmount - transferAmount);
    });
  });

  describe("Gas Usage Verification", function () {
    it("should correctly account for gas in native transfers", async function () {
      const recipient = ethers.Wallet.createRandom().connect(ethers.provider);
      const recipientAddress = await recipient.getAddress();
      
      const balanceBefore = await ethers.provider.getBalance(owner.address);
      
      const tx = await owner.sendTransaction({
        to: recipientAddress,
        value: 0,
        gasPrice: ethers.parseUnits('100', 'gwei'),
        type: 1 // Legacy transaction for predictable gas
      });
      const receipt = await tx.wait();
      
      const balanceAfter = await ethers.provider.getBalance(owner.address);
      
      // For a simple transfer, gas used should be 21000
      expect(receipt.gasUsed).to.equal(21000n);
      
      // Balance decrease should equal gas cost
      const gasCost = receipt.gasUsed * receipt.gasPrice;
      expect(balanceBefore - balanceAfter).to.equal(gasCost);
    });

    it("should correctly account for gas in ERC20 transfers", async function () {
      const TestToken = await ethers.getContractFactory("TestToken");
      const testToken = await TestToken.deploy("GasToken", "GAS", {
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      await testToken.waitForDeployment();
      
      const recipient = ethers.Wallet.createRandom().connect(ethers.provider);
      const recipientAddress = await recipient.getAddress();
      const transferAmount = ethers.parseUnits("10", 18);
      
      const nativeBalanceBefore = await ethers.provider.getBalance(owner.address);
      
      const tx = await testToken.transfer(recipientAddress, transferAmount, {
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      const receipt = await tx.wait();
      
      const nativeBalanceAfter = await ethers.provider.getBalance(owner.address);
      
      // Verify gas was charged
      expect(receipt.gasUsed).to.be.greaterThan(21000n);
      
      // Native balance decrease should equal gas cost
      const gasCost = receipt.gasUsed * receipt.gasPrice;
      expect(nativeBalanceBefore - nativeBalanceAfter).to.equal(gasCost);
    });
  });
});
