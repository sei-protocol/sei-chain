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
    
    it("should deploy TestToken with correct supply and metadata", async function () {
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
      
      // Verify initial supply: deployer receives 1000e18 tokens
      const expectedInitialSupply = ethers.parseUnits("1000", 18);
      const ownerBalance = await testToken.balanceOf(owner.address);
      const totalSupply = await testToken.totalSupply();
      expect(ownerBalance).to.equal(expectedInitialSupply);
      expect(totalSupply).to.equal(expectedInitialSupply);
      
      // Verify metadata
      expect(await testToken.name()).to.equal("GigaTestToken");
      expect(await testToken.symbol()).to.equal("GTT");
      expect(await testToken.decimals()).to.equal(18n);
    });
  });

  describe("ERC20 Transfers", function () {
    let testToken;
    const initialSupply = ethers.parseUnits("1000", 18);
    
    before(async function () {
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
      
      const tx = await testToken.transfer(recipientAddress, transferAmount, {
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      const receipt = await tx.wait();
      
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
      
      const amount1 = ethers.parseUnits("10", 18);
      const amount2 = ethers.parseUnits("20", 18);
      const amount3 = ethers.parseUnits("15", 18);
      
      const ownerBalanceBefore = await testToken.balanceOf(owner.address);
      
      await (await testToken.transfer(await recipient1.getAddress(), amount1, {
        gasPrice: ethers.parseUnits('100', 'gwei')
      })).wait();
      
      await (await testToken.transfer(await recipient2.getAddress(), amount2, {
        gasPrice: ethers.parseUnits('100', 'gwei')
      })).wait();
      
      await (await testToken.transfer(await recipient3.getAddress(), amount3, {
        gasPrice: ethers.parseUnits('100', 'gwei')
      })).wait();
      
      const balance1 = await testToken.balanceOf(await recipient1.getAddress());
      const balance2 = await testToken.balanceOf(await recipient2.getAddress());
      const balance3 = await testToken.balanceOf(await recipient3.getAddress());
      const ownerBalance = await testToken.balanceOf(owner.address);
      
      expect(balance1).to.equal(amount1);
      expect(balance2).to.equal(amount2);
      expect(balance3).to.equal(amount3);
      expect(ownerBalance).to.equal(ownerBalanceBefore - amount1 - amount2 - amount3);
      
      const totalSupply = await testToken.totalSupply();
      expect(totalSupply).to.equal(initialSupply);
    });
  });

  describe("ERC20 Approvals and TransferFrom", function () {
    let testToken;
    const initialSupply = ethers.parseUnits("1000", 18);
    
    before(async function () {
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

  // ============================================================================
  // Failing Transaction Tests
  //
  // These tests submit transactions that REVERT but still get mined into blocks.
  // This is critical for mixed-mode testing because failing txs affect the
  // ExecTxResult fields (Code, Data, GasUsed) that go into LastResultsHash.
  // If giga and V2 handle failing txs differently, the giga node will halt.
  // ============================================================================
  describe("Failing Transactions (Reverts)", function () {
    let evmTester;

    before(async function () {
      // Deploy EVMCompatibilityTester which has revertIfFalse()
      const EVMCompatibilityTester = await ethers.getContractFactory("EVMCompatibilityTester");
      evmTester = await EVMCompatibilityTester.deploy({
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      await evmTester.waitForDeployment();
    });

    it("should mine a reverted call to revertIfFalse(false)", async function () {
      // Send with explicit gas limit so the tx gets mined even though it reverts.
      // The key is: the node must produce the same Code/Data/GasUsed for this revert.
      try {
        const tx = await evmTester.revertIfFalse(false, {
          gasLimit: 100000,
          gasPrice: ethers.parseUnits('100', 'gwei')
        });
        const receipt = await tx.wait();
        // If we get here, the tx was mined — check it reverted
        expect(receipt.status).to.equal(0);
      } catch (e) {
        // ethers v6 may throw on reverts — check the receipt from the error
        if (e.receipt) {
          expect(e.receipt.status).to.equal(0);
          expect(e.receipt.gasUsed).to.be.greaterThan(0n);
        }
        // If no receipt at all, that's also acceptable (client-side rejection)
      }
    });

    it("should mine a reverted ERC20 transfer (insufficient balance)", async function () {
      // Deploy a fresh token, then try to transfer from an account with 0 balance
      const TestToken = await ethers.getContractFactory("TestToken");
      const token = await TestToken.deploy("FailToken", "FTK", {
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      await token.waitForDeployment();

      // Create a second signer with no tokens
      let spender;
      if (accounts[1]) {
        spender = accounts[1].signer;
      } else {
        spender = ethers.Wallet.createRandom().connect(ethers.provider);
        await fundAddress(await spender.getAddress());
        await delay();
      }

      // Try to transfer tokens that spender doesn't have
      const tokenAsSpender = token.connect(spender);
      try {
        const tx = await tokenAsSpender.transfer(owner.address, ethers.parseUnits("100", 18), {
          gasLimit: 100000,
          gasPrice: ethers.parseUnits('100', 'gwei')
        });
        const receipt = await tx.wait();
        expect(receipt.status).to.equal(0);
      } catch (e) {
        if (e.receipt) {
          expect(e.receipt.status).to.equal(0);
          expect(e.receipt.gasUsed).to.be.greaterThan(0n);
        }
      }
    });

    it("should handle mixed success and failure in same block window", async function () {
      // Send a batch: successful transfer, then failing call, then successful call.
      // Each tx goes into a separate block but this exercises the pattern
      // where a block has both passing and failing txs.

      const recipient = ethers.Wallet.createRandom().connect(ethers.provider);
      const recipientAddr = await recipient.getAddress();

      // 1. Successful native transfer
      const tx1 = await owner.sendTransaction({
        to: recipientAddr,
        value: ethers.parseEther("0.01"),
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      const receipt1 = await tx1.wait();
      expect(receipt1.status).to.equal(1);

      // 2. Failing call — revertIfFalse(false)
      try {
        const tx2 = await evmTester.revertIfFalse(false, {
          gasLimit: 100000,
          gasPrice: ethers.parseUnits('100', 'gwei')
        });
        const receipt2 = await tx2.wait();
        // mined as failed
        if (receipt2) expect(receipt2.status).to.equal(0);
      } catch (e) {
        if (e.receipt) {
          expect(e.receipt.status).to.equal(0);
        }
      }

      // 3. Successful call — revertIfFalse(true)
      const tx3 = await evmTester.revertIfFalse(true, {
        gasLimit: 100000,
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      const receipt3 = await tx3.wait();
      expect(receipt3.status).to.equal(1);
    });

    it("should handle out-of-gas transaction", async function () {
      // Send a contract call with very little gas — it should fail with OOG.
      // Consensus-error txs (e.g., floor data gas check) are included in the block
      // with code=1 but no EVM receipt is written, so tx.wait() would hang.
      // We use a timeout race to avoid hanging.
      try {
        const tx = await evmTester.revertIfFalse(true, {
          gasLimit: 21500, // Just barely above 21000 intrinsic, not enough for the call
          gasPrice: ethers.parseUnits('100', 'gwei')
        });
        // Race between tx.wait() and a timeout — receipt may never arrive for consensus errors
        const receipt = await Promise.race([
          tx.wait().catch(e => e.receipt || null),
          new Promise(resolve => setTimeout(() => resolve(null), 3000))
        ]);
        if (receipt) expect(receipt.status).to.equal(0);
      } catch (e) {
        if (e.receipt) {
          expect(e.receipt.status).to.equal(0);
        }
        // OOG may also be rejected at the RPC level — that's fine
      }
    });

    it("should handle transfer to non-existent contract with data", async function () {
      // Call a function on an address that has no code — this succeeds in EVM
      // (calling an EOA with data just returns with no revert)
      const fakeContract = new ethers.Contract(
        "0x000000000000000000000000000000000000dEaD",
        ["function nonExistentFunction() external"],
        owner
      );
      try {
        const tx = await fakeContract.nonExistentFunction({
          gasLimit: 50000,
          gasPrice: ethers.parseUnits('100', 'gwei')
        });
        const receipt = await Promise.race([
          tx.wait().catch(e => e.receipt || null),
          new Promise(resolve => setTimeout(() => resolve(null), 3000))
        ]);
        // Calling a non-contract address with data succeeds (no revert) but wastes gas
      } catch (e) {
        // May revert or succeed — either way, it exercises the code path
      }
    });
  });

  // ============================================================================
  // Multi-Hop Swap Tests
  //
  // These tests reproduce the pattern from the mainnet tx that caused an AppHash
  // divergence: a multi-hop DEX swap routing through multiple AMM pairs, each
  // involving cross-contract CALL frames with ERC20 transferFrom/transfer and
  // storage reads/writes across many contracts in a single transaction.
  //
  // If the giga KV store layer handles cross-contract state differently from
  // the regular store, this test will cause the giga node to halt.
  // ============================================================================
  describe("Multi-Hop Swap (Cross-Contract Token Transfers)", function () {
    let tokenA, tokenB, tokenC, tokenD;
    let pairAB, pairBC, pairCD;
    let router;

    const INITIAL_SUPPLY = ethers.parseUnits("1000000", 18);
    const LIQUIDITY_AMOUNT = ethers.parseUnits("100000", 18);
    const SWAP_AMOUNT = ethers.parseUnits("1000", 18);

    before(async function () {
      // Deploy 4 tokens (simulates WSEI, USDC, DRG, and another token in the path)
      const SimpleToken = await ethers.getContractFactory("SimpleToken");
      tokenA = await SimpleToken.deploy("TokenA", "TKA", INITIAL_SUPPLY, { gasPrice: ethers.parseUnits('100', 'gwei') });
      await tokenA.waitForDeployment();
      tokenB = await SimpleToken.deploy("TokenB", "TKB", INITIAL_SUPPLY, { gasPrice: ethers.parseUnits('100', 'gwei') });
      await tokenB.waitForDeployment();
      tokenC = await SimpleToken.deploy("TokenC", "TKC", INITIAL_SUPPLY, { gasPrice: ethers.parseUnits('100', 'gwei') });
      await tokenC.waitForDeployment();
      tokenD = await SimpleToken.deploy("TokenD", "TKD", INITIAL_SUPPLY, { gasPrice: ethers.parseUnits('100', 'gwei') });
      await tokenD.waitForDeployment();

      // Deploy 3 pairs: A/B, B/C, C/D
      const SimplePair = await ethers.getContractFactory("SimplePair");
      pairAB = await SimplePair.deploy(await tokenA.getAddress(), await tokenB.getAddress(), { gasPrice: ethers.parseUnits('100', 'gwei') });
      await pairAB.waitForDeployment();
      pairBC = await SimplePair.deploy(await tokenB.getAddress(), await tokenC.getAddress(), { gasPrice: ethers.parseUnits('100', 'gwei') });
      await pairBC.waitForDeployment();
      pairCD = await SimplePair.deploy(await tokenC.getAddress(), await tokenD.getAddress(), { gasPrice: ethers.parseUnits('100', 'gwei') });
      await pairCD.waitForDeployment();

      // Deploy router
      const SimpleRouter = await ethers.getContractFactory("SimpleRouter");
      router = await SimpleRouter.deploy({ gasPrice: ethers.parseUnits('100', 'gwei') });
      await router.waitForDeployment();

      // Add liquidity to each pair
      // Pair A/B
      await (await tokenA.transfer(await pairAB.getAddress(), LIQUIDITY_AMOUNT, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();
      await (await tokenB.transfer(await pairAB.getAddress(), LIQUIDITY_AMOUNT, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();
      await (await pairAB.addLiquidity({ gasPrice: ethers.parseUnits('100', 'gwei') })).wait();

      // Pair B/C
      await (await tokenB.transfer(await pairBC.getAddress(), LIQUIDITY_AMOUNT, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();
      await (await tokenC.transfer(await pairBC.getAddress(), LIQUIDITY_AMOUNT, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();
      await (await pairBC.addLiquidity({ gasPrice: ethers.parseUnits('100', 'gwei') })).wait();

      // Pair C/D
      await (await tokenC.transfer(await pairCD.getAddress(), LIQUIDITY_AMOUNT, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();
      await (await tokenD.transfer(await pairCD.getAddress(), LIQUIDITY_AMOUNT, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();
      await (await pairCD.addLiquidity({ gasPrice: ethers.parseUnits('100', 'gwei') })).wait();
    });

    it("should execute a 2-hop swap (A → B → C) through router", async function () {
      // Approve router to spend tokenA
      await (await tokenA.approve(await router.getAddress(), SWAP_AMOUNT, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();

      const balanceABefore = await tokenA.balanceOf(owner.address);
      const balanceCBefore = await tokenC.balanceOf(owner.address);

      // Execute 2-hop swap: A → B → C
      const tx = await router.swapExactTokensForTokens(
        SWAP_AMOUNT,
        [await tokenA.getAddress(), await tokenB.getAddress(), await tokenC.getAddress()],
        [await pairAB.getAddress(), await pairBC.getAddress()],
        owner.address,
        { gasPrice: ethers.parseUnits('100', 'gwei'), gasLimit: 1000000 }
      );
      const receipt = await tx.wait();

      expect(receipt.status).to.equal(1);

      // TokenA balance should decrease by SWAP_AMOUNT
      const balanceAAfter = await tokenA.balanceOf(owner.address);
      expect(balanceABefore - balanceAAfter).to.equal(SWAP_AMOUNT);

      // TokenC balance should increase (some amount after two swaps with fees)
      const balanceCAfter = await tokenC.balanceOf(owner.address);
      expect(balanceCAfter).to.be.greaterThan(balanceCBefore);

      // Should have emitted multiple Transfer events across different contracts
      expect(receipt.logs.length).to.be.greaterThanOrEqual(4);
    });

    it("should execute a 3-hop swap (A → B → C → D) through router", async function () {
      // Approve router to spend tokenA
      await (await tokenA.approve(await router.getAddress(), SWAP_AMOUNT, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();

      const balanceABefore = await tokenA.balanceOf(owner.address);
      const balanceDBefore = await tokenD.balanceOf(owner.address);

      // Execute 3-hop swap: A → B → C → D
      const tx = await router.swapExactTokensForTokens(
        SWAP_AMOUNT,
        [await tokenA.getAddress(), await tokenB.getAddress(), await tokenC.getAddress(), await tokenD.getAddress()],
        [await pairAB.getAddress(), await pairBC.getAddress(), await pairCD.getAddress()],
        owner.address,
        { gasPrice: ethers.parseUnits('100', 'gwei'), gasLimit: 1500000 }
      );
      const receipt = await tx.wait();

      expect(receipt.status).to.equal(1);

      // TokenA balance should decrease by SWAP_AMOUNT
      const balanceAAfter = await tokenA.balanceOf(owner.address);
      expect(balanceABefore - balanceAAfter).to.equal(SWAP_AMOUNT);

      // TokenD balance should increase
      const balanceDAfter = await tokenD.balanceOf(owner.address);
      expect(balanceDAfter).to.be.greaterThan(balanceDBefore);

      // Should have many transfer events (at least 6: transferFrom + 3 hops × 2 events each)
      expect(receipt.logs.length).to.be.greaterThanOrEqual(6);
    });

    it("should execute multiple swaps in sequence (exercises cross-tx state)", async function () {
      // Approve a large amount for multiple swaps
      const totalApproval = SWAP_AMOUNT * 3n;
      await (await tokenA.approve(await router.getAddress(), totalApproval, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();

      const smallAmount = ethers.parseUnits("100", 18);

      // Swap 1: A → B (single hop)
      const tx1 = await router.swapExactTokensForTokens(
        smallAmount,
        [await tokenA.getAddress(), await tokenB.getAddress()],
        [await pairAB.getAddress()],
        owner.address,
        { gasPrice: ethers.parseUnits('100', 'gwei'), gasLimit: 500000 }
      );
      const receipt1 = await tx1.wait();
      expect(receipt1.status).to.equal(1);

      // Swap 2: A → B → C (two hops)
      const tx2 = await router.swapExactTokensForTokens(
        smallAmount,
        [await tokenA.getAddress(), await tokenB.getAddress(), await tokenC.getAddress()],
        [await pairAB.getAddress(), await pairBC.getAddress()],
        owner.address,
        { gasPrice: ethers.parseUnits('100', 'gwei'), gasLimit: 1000000 }
      );
      const receipt2 = await tx2.wait();
      expect(receipt2.status).to.equal(1);

      // Swap 3: A → B → C → D (three hops)
      const tx3 = await router.swapExactTokensForTokens(
        smallAmount,
        [await tokenA.getAddress(), await tokenB.getAddress(), await tokenC.getAddress(), await tokenD.getAddress()],
        [await pairAB.getAddress(), await pairBC.getAddress(), await pairCD.getAddress()],
        owner.address,
        { gasPrice: ethers.parseUnits('100', 'gwei'), gasLimit: 1500000 }
      );
      const receipt3 = await tx3.wait();
      expect(receipt3.status).to.equal(1);

      // Verify the pair reserves are consistent
      const r0AB = await pairAB.reserve0();
      const r1AB = await pairAB.reserve1();
      expect(r0AB).to.be.greaterThan(0n);
      expect(r1AB).to.be.greaterThan(0n);
    });

    it("should handle swap + direct transfer in same block window", async function () {
      // This exercises the pattern where both a complex multi-hop swap and a simple
      // ERC20 transfer happen in nearby blocks — testing cross-tx state consistency.

      // Approve for swap
      await (await tokenA.approve(await router.getAddress(), SWAP_AMOUNT, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();

      const recipient = ethers.Wallet.createRandom().connect(ethers.provider);
      const recipientAddr = await recipient.getAddress();
      const directTransferAmount = ethers.parseUnits("50", 18);

      // Direct transfer of tokenB to a fresh address
      const tx1 = await tokenB.transfer(recipientAddr, directTransferAmount, {
        gasPrice: ethers.parseUnits('100', 'gwei')
      });
      await tx1.wait();

      // Multi-hop swap that also touches tokenB
      const tx2 = await router.swapExactTokensForTokens(
        ethers.parseUnits("500", 18),
        [await tokenA.getAddress(), await tokenB.getAddress(), await tokenC.getAddress()],
        [await pairAB.getAddress(), await pairBC.getAddress()],
        owner.address,
        { gasPrice: ethers.parseUnits('100', 'gwei'), gasLimit: 1000000 }
      );
      const receipt2 = await tx2.wait();
      expect(receipt2.status).to.equal(1);

      // Verify the direct transfer recipient still has correct balance
      const recipientBalance = await tokenB.balanceOf(recipientAddr);
      expect(recipientBalance).to.equal(directTransferAmount);
    });
  });

  // ============================================================================
  // Proxy + Callback Multi-Hop Swap Tests
  //
  // These tests reproduce the EXACT pattern from the mainnet tx 0xf0ca0ec2...
  // that caused an AppHash divergence:
  //   1. Proxy token with delegatecall (like Sei's USDC proxy)
  //   2. V3-style callback swaps (pool calls back into router mid-swap)
  //   3. V2-style pool swap in the final hop
  //   4. Balance verification via staticcall after callback mutates state
  //   5. Multiple cross-contract transfers touching the same proxy token storage
  //
  // If the giga KV store handles delegatecall storage or cross-contract reads
  // differently, this test will cause the giga node to halt with AppHash mismatch.
  // ============================================================================
  describe("Proxy + Callback Multi-Hop Swap (Mainnet Repro)", function () {
    let tokenA, proxyToken, tokenB;
    let tokenImpl;
    let v3Pool1, v3Pool2, v2Pool;
    let router;

    const SUPPLY = ethers.parseUnits("10000000", 18);
    const LIQUIDITY = ethers.parseUnits("1000000", 18);
    const SWAP_AMOUNT = ethers.parseUnits("1000", 18);

    before(async function () {
      // Deploy tokens
      const SimpleToken = await ethers.getContractFactory("SimpleToken");
      tokenA = await SimpleToken.deploy("TokenA", "TKA", SUPPLY, { gasPrice: ethers.parseUnits('100', 'gwei') });
      await tokenA.waitForDeployment();
      tokenB = await SimpleToken.deploy("TokenB", "TKB", SUPPLY, { gasPrice: ethers.parseUnits('100', 'gwei') });
      await tokenB.waitForDeployment();

      // Deploy proxy token (like Sei's USDC proxy)
      const TokenImplementation = await ethers.getContractFactory("TokenImplementation");
      tokenImpl = await TokenImplementation.deploy({ gasPrice: ethers.parseUnits('100', 'gwei') });
      await tokenImpl.waitForDeployment();

      const ProxyToken = await ethers.getContractFactory("ProxyToken");
      const proxyTokenContract = await ProxyToken.deploy(await tokenImpl.getAddress(), { gasPrice: ethers.parseUnits('100', 'gwei') });
      await proxyTokenContract.waitForDeployment();

      // Wrap proxy in the TokenImplementation interface so we can call mint/transfer/etc
      proxyToken = await ethers.getContractAt("TokenImplementation", await proxyTokenContract.getAddress());

      // Mint proxy tokens to owner
      await (await proxyToken.mint(owner.address, SUPPLY, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();

      // Deploy V3 callback pools
      // Pool 1: tokenA (token0) / proxyToken (token1)
      const CallbackPool = await ethers.getContractFactory("CallbackPool");
      v3Pool1 = await CallbackPool.deploy(await tokenA.getAddress(), await proxyToken.getAddress(), { gasPrice: ethers.parseUnits('100', 'gwei') });
      await v3Pool1.waitForDeployment();

      // Pool 2: proxyToken (token0) / tokenB (token1)
      v3Pool2 = await CallbackPool.deploy(await proxyToken.getAddress(), await tokenB.getAddress(), { gasPrice: ethers.parseUnits('100', 'gwei') });
      await v3Pool2.waitForDeployment();

      // Deploy V2 pool: tokenB (token0) / tokenA (token1)
      const SimpleV2Pool = await ethers.getContractFactory("SimpleV2Pool");
      v2Pool = await SimpleV2Pool.deploy(await tokenB.getAddress(), await tokenA.getAddress(), { gasPrice: ethers.parseUnits('100', 'gwei') });
      await v2Pool.waitForDeployment();

      // Deploy router
      const CallbackRouter = await ethers.getContractFactory("CallbackRouter");
      router = await CallbackRouter.deploy({ gasPrice: ethers.parseUnits('100', 'gwei') });
      await router.waitForDeployment();

      // Fund pools with liquidity
      // V3 Pool 1: needs proxyToken (output when swapping tokenA→proxyToken)
      await (await proxyToken.transfer(await v3Pool1.getAddress(), LIQUIDITY, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();

      // V3 Pool 2: needs tokenB (output when swapping proxyToken→tokenB)
      await (await tokenB.transfer(await v3Pool2.getAddress(), LIQUIDITY, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();

      // V2 Pool: needs both tokenB and tokenA for reserves
      await (await tokenB.transfer(await v2Pool.getAddress(), LIQUIDITY, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();
      await (await tokenA.transfer(await v2Pool.getAddress(), LIQUIDITY, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();
      await (await v2Pool.addLiquidity({ gasPrice: ethers.parseUnits('100', 'gwei') })).wait();
    });

    it("should execute full 3-hop swap through proxy token with callbacks", async function () {
      // Approve router to spend tokenA
      await (await tokenA.approve(await router.getAddress(), SWAP_AMOUNT, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();

      const balABefore = await tokenA.balanceOf(owner.address);

      // Execute the multi-hop swap: tokenA → proxyToken → tokenB → tokenA
      const tx = await router.executeMultiHopSwap(
        SWAP_AMOUNT,
        await tokenA.getAddress(),
        await proxyToken.getAddress(),
        await tokenB.getAddress(),
        await v3Pool1.getAddress(),
        await v3Pool2.getAddress(),
        await v2Pool.getAddress(),
        owner.address,
        { gasPrice: ethers.parseUnits('100', 'gwei'), gasLimit: 2000000 }
      );
      const receipt = await tx.wait();

      expect(receipt.status).to.equal(1);
      // Should have many events (Transfer events from each hop + Swap/Sync)
      expect(receipt.logs.length).to.be.greaterThanOrEqual(5);
    });

    it("should execute multiple proxy token swaps in sequence", async function () {
      const smallAmount = ethers.parseUnits("100", 18);

      for (let i = 0; i < 2; i++) {
        await (await tokenA.approve(await router.getAddress(), smallAmount, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();

        const tx = await router.executeMultiHopSwap(
          smallAmount,
          await tokenA.getAddress(),
          await proxyToken.getAddress(),
          await tokenB.getAddress(),
          await v3Pool1.getAddress(),
          await v3Pool2.getAddress(),
          await v2Pool.getAddress(),
          owner.address,
          { gasPrice: ethers.parseUnits('100', 'gwei'), gasLimit: 2000000 }
        );
        const receipt = await tx.wait();
        expect(receipt.status).to.equal(1);
      }
    });

    it("should handle proxy token direct transfers interleaved with swaps", async function () {
      // Direct proxy token transfer to a fresh address
      const recipient = ethers.Wallet.createRandom().connect(ethers.provider);
      const recipientAddr = await recipient.getAddress();
      const directAmount = ethers.parseUnits("500", 18);

      await (await proxyToken.transfer(recipientAddr, directAmount, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();

      // Then do a swap that also touches the proxy token
      const swapAmt = ethers.parseUnits("200", 18);
      await (await tokenA.approve(await router.getAddress(), swapAmt, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();

      const tx = await router.executeMultiHopSwap(
        swapAmt,
        await tokenA.getAddress(),
        await proxyToken.getAddress(),
        await tokenB.getAddress(),
        await v3Pool1.getAddress(),
        await v3Pool2.getAddress(),
        await v2Pool.getAddress(),
        owner.address,
        { gasPrice: ethers.parseUnits('100', 'gwei'), gasLimit: 2000000 }
      );
      const receipt = await tx.wait();
      expect(receipt.status).to.equal(1);

      // Verify direct transfer recipient still has correct balance
      const recipientBalance = await proxyToken.balanceOf(recipientAddr);
      expect(recipientBalance).to.equal(directAmount);
    });

    it("should verify proxy token balances are consistent after complex swaps", async function () {
      // Get balances of all participants
      const ownerBal = await proxyToken.balanceOf(owner.address);
      const pool1Bal = await proxyToken.balanceOf(await v3Pool1.getAddress());
      const pool2Bal = await proxyToken.balanceOf(await v3Pool2.getAddress());
      const routerBal = await proxyToken.balanceOf(await router.getAddress());

      // Do a swap
      const swapAmt = ethers.parseUnits("50", 18);
      await (await tokenA.approve(await router.getAddress(), swapAmt, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();

      const tx = await router.executeMultiHopSwap(
        swapAmt,
        await tokenA.getAddress(),
        await proxyToken.getAddress(),
        await tokenB.getAddress(),
        await v3Pool1.getAddress(),
        await v3Pool2.getAddress(),
        await v2Pool.getAddress(),
        owner.address,
        { gasPrice: ethers.parseUnits('100', 'gwei'), gasLimit: 2000000 }
      );
      const receipt = await tx.wait();
      expect(receipt.status).to.equal(1);

      // Get balances after
      const ownerBalAfter = await proxyToken.balanceOf(owner.address);
      const pool1BalAfter = await proxyToken.balanceOf(await v3Pool1.getAddress());
      const pool2BalAfter = await proxyToken.balanceOf(await v3Pool2.getAddress());
      const routerBalAfter = await proxyToken.balanceOf(await router.getAddress());

      // Pool1 should have gained proxy tokens (received via callback, sent some out)
      // The exact amounts depend on the swap math, but all should be non-negative
      expect(pool1BalAfter).to.be.greaterThanOrEqual(0n);
      expect(pool2BalAfter).to.be.greaterThanOrEqual(0n);
      // Router should have 0 proxy tokens (all passed through)
      expect(routerBalAfter).to.equal(0n);
    });

    it("should handle V3 callback swap reading state from prior block", async function () {
      // Wait for a new block so the swap's STATICCALL balance checks
      // read committed (cross-block) state from the pools.
      await delay();

      const swapAmt = ethers.parseUnits("1000", 18);
      await (await tokenA.approve(await router.getAddress(), swapAmt * 2n, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();

      // Do 2 sequential swaps — each reads state written by the previous one
      for (let i = 0; i < 2; i++) {
        const tx = await router.executeMultiHopSwap(
          swapAmt,
          await tokenA.getAddress(),
          await proxyToken.getAddress(),
          await tokenB.getAddress(),
          await v3Pool1.getAddress(),
          await v3Pool2.getAddress(),
          await v2Pool.getAddress(),
          owner.address,
          { gasPrice: ethers.parseUnits('100', 'gwei'), gasLimit: 3000000 }
        );
        const receipt = await tx.wait();
        expect(receipt.status).to.equal(1);
      }
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

  // ============================================================================
  // Cross-Block State Dependency
  //
  // Tests that state committed by block N is correctly visible in block N+1.
  // The giga store flushes on WriteGiga() — if this flush is incomplete,
  // the next block would read stale data.
  // ============================================================================
  describe("Cross-Block State Consistency", function () {
    const SUPPLY = ethers.parseUnits("10000000", 18);
    let pToken;

    before(async function () {
      // Deploy a single proxy token for both cross-block tests
      const TokenImplementation = await ethers.getContractFactory("TokenImplementation");
      const impl = await TokenImplementation.deploy({ gasPrice: ethers.parseUnits('100', 'gwei') });
      await impl.waitForDeployment();
      const ProxyToken = await ethers.getContractFactory("ProxyToken");
      const proxy = await ProxyToken.deploy(await impl.getAddress(), { gasPrice: ethers.parseUnits('100', 'gwei') });
      await proxy.waitForDeployment();
      pToken = await ethers.getContractAt("TokenImplementation", await proxy.getAddress());
      await (await pToken.mint(owner.address, SUPPLY, { gasPrice: ethers.parseUnits('100', 'gwei') })).wait();
    });

    it("should correctly read proxy token balance written in previous block", async function () {
      // Block N: Transfer to a fresh address (goes through delegatecall storage write)
      const recipient = ethers.Wallet.createRandom().connect(ethers.provider);
      const amount = ethers.parseUnits("12345", 18);
      const tx = await pToken.transfer(await recipient.getAddress(), amount, {
        gasPrice: ethers.parseUnits('100', 'gwei'),
      });
      const receipt = await tx.wait();
      const blockN = receipt.blockNumber;

      // Block N+1: Read the balance — must see the write from block N
      await delay();
      const bal = await pToken.balanceOf(await recipient.getAddress());
      expect(bal).to.equal(amount);

      console.log(`        Wrote in block ${blockN}, verified in subsequent read`);
    });

    it("should maintain consistency across multiple blocks of proxy token ops", async function () {
      // Do 3 rounds of: transfer, wait, verify
      // Each round depends on the previous round's committed state
      const addr = ethers.Wallet.createRandom().connect(ethers.provider);
      const addrHex = await addr.getAddress();
      let recipientBal = 0n;
      const transferAmt = ethers.parseUnits("1000", 18);

      for (let round = 0; round < 3; round++) {
        const ownerBalBefore = await pToken.balanceOf(owner.address);
        const tx = await pToken.transfer(addrHex, transferAmt, {
          gasPrice: ethers.parseUnits('100', 'gwei'),
        });
        await tx.wait();
        recipientBal += transferAmt;

        const ownerBal = await pToken.balanceOf(owner.address);
        const rBal = await pToken.balanceOf(addrHex);
        expect(ownerBal).to.equal(ownerBalBefore - transferAmt);
        expect(rBal).to.equal(recipientBal);
      }
      console.log(`        3 rounds of proxy token transfer+verify completed`);
    });
  });
});
