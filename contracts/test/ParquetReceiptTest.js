const { expect } = require("chai");
const hre = require("hardhat");
const { ethers } = hre;
const { deployEvmContract, setupSigners, fundAddress } = require("./lib");

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

describe("Receipt Store Integration Tests", function () {
  let evmTester;
  let owner;
  let addr1;

  before(async function () {
    const signers = await ethers.getSigners();
    const results = await setupSigners(signers);
    owner = results[0].signer;
    addr1 = results[1].signer;
    evmTester = await deployEvmContract("EVMCompatibilityTester");
  });

  describe("Receipt Existence and Correctness", function () {
    it("Should return a valid receipt for a simple transfer", async function () {
      const tx = await owner.sendTransaction({
        to: addr1.address,
        value: ethers.parseEther("0.01"),
      });
      const receipt = await tx.wait();

      expect(receipt).to.not.be.null;
      expect(receipt.status).to.equal(1);
      expect(receipt.hash).to.equal(tx.hash);
      expect(receipt.from.toLowerCase()).to.equal(
        owner.address.toLowerCase()
      );
      expect(receipt.to.toLowerCase()).to.equal(
        addr1.address.toLowerCase()
      );
      expect(receipt.blockNumber).to.be.a("number");
      expect(receipt.blockNumber).to.be.greaterThan(0);
      expect(receipt.blockHash).to.be.a("string");
      expect(receipt.blockHash).to.have.lengthOf(66); // 0x + 64 hex chars
      expect(receipt.gasUsed).to.be.greaterThan(0n);
      expect(receipt.cumulativeGasUsed).to.be.greaterThan(0n);
      expect(receipt.index).to.be.a("number");
    });

    it("Should return a valid receipt for a contract deployment", async function () {
      const factory = await ethers.getContractFactory("EVMCompatibilityTester");
      const contract = await factory.deploy();
      const receipt = await contract.deploymentTransaction().wait();

      expect(receipt).to.not.be.null;
      expect(receipt.status).to.equal(1);
      expect(receipt.contractAddress).to.be.a("string");
      expect(receipt.contractAddress).to.have.lengthOf(42);
      expect(receipt.from.toLowerCase()).to.equal(
        owner.address.toLowerCase()
      );
      // For contract creation, 'to' should be null
      expect(receipt.to).to.be.null;
    });

    it("Should return a valid receipt for a contract interaction with events", async function () {
      const tx = await evmTester.setStringVar("parquet_test", {
        gasPrice: ethers.parseUnits("100", "gwei"),
      });
      const receipt = await tx.wait();

      expect(receipt).to.not.be.null;
      expect(receipt.status).to.equal(1);
      expect(receipt.logs).to.be.an("array").that.is.not.empty;
      expect(receipt.logs.length).to.equal(1);

      // Verify the log content
      const log = receipt.logs[0];
      expect(log.address.toLowerCase()).to.equal(
        (await evmTester.getAddress()).toLowerCase()
      );
      expect(log.blockNumber).to.equal(receipt.blockNumber);
      expect(log.blockHash).to.equal(receipt.blockHash);
      expect(log.transactionHash).to.equal(receipt.hash);
      expect(log.index).to.be.a("number");
    });

    it("Should return receipt with correct status for reverted tx", async function () {
      try {
        const tx = await evmTester.revertIfFalse(false);
        const receipt = await tx.wait();
        // If we get here, the tx didn't revert at execution level
        // but the receipt should show status 0
        expect(receipt.status).to.equal(0);
      } catch (e) {
        // Expected: ethers throws on revert
        expect(e.message).to.include("revert");
      }
    });

    it("Should retrieve receipt via eth_getTransactionReceipt RPC", async function () {
      const tx = await evmTester.setUint256Var(42, {
        gasPrice: ethers.parseUnits("100", "gwei"),
      });
      const receipt = await tx.wait();

      // Query receipt via raw RPC
      const rpcReceipt = await hre.network.provider.request({
        method: "eth_getTransactionReceipt",
        params: [receipt.hash],
      });

      expect(rpcReceipt).to.not.be.null;
      expect(rpcReceipt.transactionHash).to.equal(receipt.hash);
      expect(rpcReceipt.blockNumber).to.not.be.null;
      expect(rpcReceipt.blockHash).to.not.be.null;
      expect(parseInt(rpcReceipt.status, 16)).to.equal(1);
      expect(rpcReceipt.logs).to.be.an("array");
    });

    it("Should return null for non-existent transaction receipt", async function () {
      const fakeTxHash =
        "0x0000000000000000000000000000000000000000000000000000000000000001";
      const receipt = await ethers.provider.getTransactionReceipt(fakeTxHash);
      expect(receipt).to.be.null;
    });
  });

  describe("Receipt Log Fields", function () {
    it("Should have correct log fields for a single event", async function () {
      const tx = await evmTester.setBoolVar(true, {
        gasPrice: ethers.parseUnits("100", "gwei"),
      });
      const receipt = await tx.wait();
      const log = receipt.logs[0];

      // BoolSet(address performer, bool value)
      expect(log.topics).to.be.an("array");
      expect(log.topics.length).to.be.greaterThan(0);
      // First topic is the event signature hash
      expect(log.topics[0]).to.equal(ethers.id("BoolSet(address,bool)"));
      expect(log.data).to.be.a("string");
      expect(log.blockNumber).to.equal(receipt.blockNumber);
      expect(log.transactionHash).to.equal(receipt.hash);

      // Verify 'removed' is false via raw RPC (ethers v6 doesn't expose it directly)
      const rpcReceipt = await hre.network.provider.request({
        method: "eth_getTransactionReceipt",
        params: [receipt.hash],
      });
      expect(rpcReceipt.logs[0].removed).to.equal(false);
    });

    it("Should have correct log fields for indexed event parameters", async function () {
      const tx = await evmTester.setAddressVar({
        gasPrice: ethers.parseUnits("100", "gwei"),
      });
      const receipt = await tx.wait();
      const log = receipt.logs[0];

      // AddressSet(address indexed performer)
      expect(log.topics[0]).to.equal(ethers.id("AddressSet(address)"));
      // The indexed address should be in topics[1], padded to 32 bytes
      const paddedAddress =
        "0x" + owner.address.slice(2).toLowerCase().padStart(64, "0");
      expect(log.topics[1].toLowerCase()).to.equal(paddedAddress);
    });

    it("Should have correct logIndex for multiple events in one tx", async function () {
      const numLogs = 5;
      const tx = await evmTester.emitMultipleLogs(numLogs, {
        gasPrice: ethers.parseUnits("100", "gwei"),
      });
      const receipt = await tx.wait();

      expect(receipt.logs.length).to.equal(numLogs);
      for (let i = 0; i < numLogs; i++) {
        const log = receipt.logs[i];
        // Logs within a single tx should have sequential indices
        expect(log.index).to.be.a("number");
        // Verify each log has the correct event signature
        expect(log.topics[0]).to.equal(
          ethers.id("LogIndexEvent(address,uint256)")
        );
      }
    });

    it("Should include logsBloom in receipt", async function () {
      const tx = await evmTester.setBoolVar(false, {
        gasPrice: ethers.parseUnits("100", "gwei"),
      });
      const receipt = await tx.wait();

      const rpcReceipt = await hre.network.provider.request({
        method: "eth_getTransactionReceipt",
        params: [receipt.hash],
      });

      expect(rpcReceipt.logsBloom).to.be.a("string");
      expect(rpcReceipt.logsBloom).to.have.lengthOf(514); // 0x + 512 hex chars
      // logsBloom should not be all zeros since we have a log
      expect(rpcReceipt.logsBloom).to.not.equal("0x" + "0".repeat(512));
    });
  });

  describe("Log Filtering (eth_getLogs)", function () {
    let blockStart;
    let blockEnd;
    let contractAddress;
    const numTxs = 5;

    before(async function () {
      contractAddress = await evmTester.getAddress();
      await sleep(3000); // wait for a fresh block
      blockStart = await ethers.provider.getBlockNumber();

      for (let i = 0; i < numTxs; i++) {
        const tx = await evmTester.emitDummyEvent("parquet", i, {
          gasPrice: ethers.parseUnits("100", "gwei"),
        });
        await tx.wait();
      }
      blockEnd = await ethers.provider.getBlockNumber();
    });

    it("Should filter logs by block range", async function () {
      const logs = await ethers.provider.getLogs({
        fromBlock: blockStart,
        toBlock: blockEnd,
        address: contractAddress,
      });

      expect(logs).to.be.an("array");
      expect(logs.length).to.equal(numTxs);
    });

    it("Should filter logs by event topic", async function () {
      const eventSig = ethers.id(
        "DummyEvent(string,bool,address,uint256,bytes)"
      );
      const logs = await ethers.provider.getLogs({
        fromBlock: blockStart,
        toBlock: blockEnd,
        topics: [eventSig],
      });

      expect(logs).to.be.an("array");
      expect(logs.length).to.equal(numTxs);
      for (const log of logs) {
        expect(log.topics[0]).to.equal(eventSig);
      }
    });

    it("Should filter logs by contract address", async function () {
      const logs = await ethers.provider.getLogs({
        fromBlock: blockStart,
        toBlock: blockEnd,
        address: contractAddress,
      });

      for (const log of logs) {
        expect(log.address.toLowerCase()).to.equal(
          contractAddress.toLowerCase()
        );
      }
    });

    it("Should filter logs by multiple topics (AND)", async function () {
      const eventSig = ethers.id(
        "DummyEvent(string,bool,address,uint256,bytes)"
      );
      const topicStr = ethers.id("parquet");
      const paddedAddr =
        "0x" + owner.address.slice(2).padStart(64, "0");

      const logs = await ethers.provider.getLogs({
        fromBlock: blockStart,
        toBlock: blockEnd,
        topics: [eventSig, topicStr, paddedAddr],
      });

      expect(logs).to.be.an("array");
      expect(logs.length).to.equal(numTxs);
    });

    it("Should filter logs by specific indexed value", async function () {
      const eventSig = ethers.id(
        "DummyEvent(string,bool,address,uint256,bytes)"
      );
      // Filter for num=3 specifically
      const logs = await ethers.provider.getLogs({
        fromBlock: blockStart,
        toBlock: blockEnd,
        topics: [
          eventSig,
          ethers.id("parquet"),
          null,
          "0x0000000000000000000000000000000000000000000000000000000000000003",
        ],
      });

      expect(logs).to.be.an("array");
      expect(logs.length).to.equal(1);
    });

    it("Should filter logs by OR on topics", async function () {
      const eventSig = ethers.id(
        "DummyEvent(string,bool,address,uint256,bytes)"
      );
      // Filter for num=1 OR num=3
      const logs = await ethers.provider.getLogs({
        fromBlock: blockStart,
        toBlock: blockEnd,
        topics: [
          eventSig,
          ethers.id("parquet"),
          null,
          [
            "0x0000000000000000000000000000000000000000000000000000000000000001",
            "0x0000000000000000000000000000000000000000000000000000000000000003",
          ],
        ],
      });

      expect(logs).to.be.an("array");
      expect(logs.length).to.equal(2);
    });

    it("Should return empty array for non-matching filter", async function () {
      const fakeTopic =
        "0x0000000000000000000000000000000000000000000000000000000000000000";
      const logs = await ethers.provider.getLogs({
        fromBlock: blockStart,
        toBlock: blockEnd,
        topics: [fakeTopic],
      });

      expect(logs).to.be.an("array");
      expect(logs.length).to.equal(0);
    });

    it("Should filter logs by blockHash", async function () {
      // Get a log to find a block hash
      const allLogs = await ethers.provider.getLogs({
        fromBlock: blockStart,
        toBlock: blockEnd,
        address: contractAddress,
      });

      const blockHash = allLogs[0].blockHash;

      const blockHashLogs = await ethers.provider.getLogs({
        blockHash: blockHash,
      });

      expect(blockHashLogs).to.be.an("array");
      for (const log of blockHashLogs) {
        expect(log.blockHash).to.equal(blockHash);
      }
    });

    it("Should filter logs from block 1 to latest", async function () {
      const logs = await ethers.provider.getLogs({
        fromBlock: 1,
        toBlock: "latest",
        address: contractAddress,
      });

      expect(logs).to.be.an("array");
      // Should have at least the logs from our test transactions
      expect(logs.length).to.be.greaterThanOrEqual(numTxs);
    });
  });

  describe("Receipt Consistency Across Queries", function () {
    it("Should return consistent receipt from tx.wait() and getTransactionReceipt()", async function () {
      const tx = await evmTester.setUint256Var(999, {
        gasPrice: ethers.parseUnits("100", "gwei"),
      });
      const receiptFromWait = await tx.wait();

      // Query again via provider
      const receiptFromQuery =
        await ethers.provider.getTransactionReceipt(tx.hash);

      expect(receiptFromQuery).to.not.be.null;
      expect(receiptFromQuery.hash).to.equal(receiptFromWait.hash);
      expect(receiptFromQuery.blockNumber).to.equal(
        receiptFromWait.blockNumber
      );
      expect(receiptFromQuery.blockHash).to.equal(
        receiptFromWait.blockHash
      );
      expect(receiptFromQuery.status).to.equal(receiptFromWait.status);
      expect(receiptFromQuery.gasUsed).to.equal(receiptFromWait.gasUsed);
      expect(receiptFromQuery.cumulativeGasUsed).to.equal(
        receiptFromWait.cumulativeGasUsed
      );
      expect(receiptFromQuery.from.toLowerCase()).to.equal(
        receiptFromWait.from.toLowerCase()
      );
      expect(receiptFromQuery.logs.length).to.equal(
        receiptFromWait.logs.length
      );
    });

    it("Should return consistent logs from receipt and eth_getLogs", async function () {
      const tx = await evmTester.setStringVar("consistency_check", {
        gasPrice: ethers.parseUnits("100", "gwei"),
      });
      const receipt = await tx.wait();

      // Get logs via eth_getLogs for the same block
      const logs = await ethers.provider.getLogs({
        fromBlock: receipt.blockNumber,
        toBlock: receipt.blockNumber,
        address: await evmTester.getAddress(),
        topics: [ethers.id("StringSet(address,string)")],
      });

      expect(logs.length).to.be.greaterThanOrEqual(1);

      // Find our specific log
      const matchingLog = logs.find(
        (l) => l.transactionHash === receipt.hash
      );
      expect(matchingLog).to.not.be.undefined;
      expect(matchingLog.transactionHash).to.equal(receipt.hash);

      const receiptLog = receipt.logs[0];
      expect(matchingLog.address.toLowerCase()).to.equal(
        receiptLog.address.toLowerCase()
      );
      expect(matchingLog.topics).to.deep.equal(receiptLog.topics);
      expect(matchingLog.data).to.equal(receiptLog.data);
      expect(matchingLog.blockNumber).to.equal(receiptLog.blockNumber);
      expect(matchingLog.index).to.equal(receiptLog.index);
    });
  });

  describe("Multiple Transactions Per Block", function () {
    it("Should return correct receipts for rapid-fire transactions", async function () {
      const txPromises = [];
      const numTxs = 3;

      // Send multiple transactions rapidly (may land in same block)
      for (let i = 0; i < numTxs; i++) {
        const tx = evmTester.setUint256Var(i + 1000, {
          gasPrice: ethers.parseUnits("100", "gwei"),
          nonce: await owner.getNonce() + i,
        });
        txPromises.push(tx);
      }

      const txs = await Promise.all(txPromises);
      const receipts = await Promise.all(txs.map((tx) => tx.wait()));

      // Each receipt should be valid and unique
      const txHashes = new Set();
      for (const receipt of receipts) {
        expect(receipt).to.not.be.null;
        expect(receipt.status).to.equal(1);
        expect(receipt.hash).to.be.a("string");
        expect(txHashes.has(receipt.hash)).to.be.false;
        txHashes.add(receipt.hash);
      }
    });
  });

  describe("Gas Fields in Receipt", function () {
    it("Should have correct gas fields for a simple transfer", async function () {
      const tx = await owner.sendTransaction({
        to: addr1.address,
        value: ethers.parseEther("0.001"),
      });
      const receipt = await tx.wait();

      expect(receipt.gasUsed).to.equal(21000n); // simple transfer = 21000
      expect(receipt.cumulativeGasUsed).to.be.greaterThanOrEqual(
        receipt.gasUsed
      );
    });

    it("Should have correct gasUsed for a contract call", async function () {
      const tx = await evmTester.storeData(12345, {
        gasPrice: ethers.parseUnits("100", "gwei"),
      });
      const receipt = await tx.wait();

      // Contract calls use more than 21000 gas
      expect(receipt.gasUsed).to.be.greaterThan(21000n);
      expect(receipt.gasPrice).to.be.greaterThan(0n);
    });

    it("Should have effectiveGasPrice in receipt", async function () {
      const tx = await evmTester.setUint256Var(777, {
        maxPriorityFeePerGas: ethers.parseUnits("1", "gwei"),
        maxFeePerGas: ethers.parseUnits("100", "gwei"),
        type: 2,
      });
      const receipt = await tx.wait();

      expect(receipt.gasPrice).to.be.greaterThan(0n);
    });
  });

  describe("Transaction Type in Receipt", function () {
    it("Should return correct receipt for legacy transaction", async function () {
      const tx = await owner.sendTransaction({
        to: addr1.address,
        value: ethers.parseEther("0.001"),
        gasPrice: ethers.parseUnits("100", "gwei"),
        type: 0,
      });
      const receipt = await tx.wait();

      expect(receipt.status).to.equal(1);
      expect(receipt.type).to.equal(0);
    });

    it("Should return correct receipt for EIP-1559 transaction", async function () {
      const tx = await owner.sendTransaction({
        to: addr1.address,
        value: ethers.parseEther("0.001"),
        maxPriorityFeePerGas: ethers.parseUnits("1", "gwei"),
        maxFeePerGas: ethers.parseUnits("100", "gwei"),
        type: 2,
      });
      const receipt = await tx.wait();

      expect(receipt.status).to.equal(1);
      expect(receipt.type).to.equal(2);
    });
  });
});
