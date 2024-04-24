const { expect } = require("chai");
const {isBigNumber} = require("hardhat/common");
const {uniq, shuffle} = require("lodash");
const { ethers, upgrades } = require('hardhat');
const { getImplementationAddress } = require('@openzeppelin/upgrades-core');

function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

async function delay() {
  // await sleep(3000)
}

function debug(msg) {
  // leaving commented out to make output readable (unless debugging)
  // console.log(msg)
}

async function sendTransactionAndCheckGas(sender, recipient, amount) {
  // Get the balance of the sender before the transaction
  const balanceBefore = await ethers.provider.getBalance(sender.address);

  // Send the transaction
  const tx = await sender.sendTransaction({
    to: recipient.address,
    value: amount
  });

  // Wait for the transaction to be mined and get the receipt
  const receipt = await tx.wait();

  // Get the balance of the sender after the transaction
  const balanceAfter = await ethers.provider.getBalance(sender.address);

  // Calculate the total cost of the transaction (amount + gas fees)
  const gasPrice = receipt.gasPrice;
  const gasUsed = receipt.gasUsed;
  const totalCost = gasPrice * gasUsed + BigInt(amount);

  // Check that the sender's balance decreased by the total cost
  return balanceBefore - balanceAfter === totalCost
}

function generateWallet() {
  const wallet = ethers.Wallet.createRandom();
  return wallet.connect(ethers.provider);
}

async function sendTx(sender, txn, responses) {
  const txResponse = await sender.sendTransaction(txn);
  responses.push({nonce: txn.nonce, response: txResponse})
}


describe("EVM Test", function () {

  describe("EVMCompatibilityTester", function () {
    let evmTester;
    let testToken;
    let owner;
    let evmAddr;

    // The first contract address deployed from 0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52
    // should always be 0xbD5d765B226CaEA8507EE030565618dAFFD806e2 when sent with nonce=0
    const firstContractAddress = "0xbD5d765B226CaEA8507EE030565618dAFFD806e2";

    // This function deploys a new instance of the contract before each test
    beforeEach(async function () {
      if(evmTester && testToken) {
        return
      }
      let signers = await ethers.getSigners();
      owner = signers[0];
      debug(`OWNER = ${owner.address}`)

      const TestToken = await ethers.getContractFactory("TestToken")
      testToken = await TestToken.deploy("TestToken", "TTK");

      const EVMCompatibilityTester = await ethers.getContractFactory("EVMCompatibilityTester");
      evmTester = await EVMCompatibilityTester.deploy();

      await Promise.all([evmTester.waitForDeployment(), testToken.waitForDeployment()])

      let tokenAddr = await testToken.getAddress()
      evmAddr = await evmTester.getAddress()

      debug(`Token: ${tokenAddr}, EvmAddr: ${evmAddr}`);
    });

    describe("Deployment", function () {
      it("Should deploy successfully", async function () {
        expect(await evmTester.getAddress()).to.be.properAddress;
        expect(await testToken.getAddress()).to.be.properAddress;
        expect(await evmTester.getAddress()).to.not.equal(await testToken.getAddress());

        // The first contract address deployed from 0xF87A299e6bC7bEba58dbBe5a5Aa21d49bCD16D52
        // should always be 0xbD5d765B226CaEA8507EE030565618dAFFD806e2 when sent with nonce=0
        expect(await testToken.getAddress()).to.equal(firstContractAddress);
      });

      it("Should estimate gas for a contract deployment", async function () {
        const callData = evmTester.interface.encodeFunctionData("createToken", ["TestToken", "TTK"]);
        const estimatedGas = await ethers.provider.estimateGas({
          to: await evmTester.getAddress(),
          data: callData
        });
        expect(estimatedGas).to.greaterThan(0);
      });
    });

    describe("Contract Factory", function() {
      it("should deploy a second contract from createToken", async function () {
        const txResponse = await evmTester.createToken("TestToken", "TTK");
        const testerAddress = await evmTester.getAddress();
        const receipt = await txResponse.wait();
        const newTokenAddress = receipt.logs[0].address;
        expect(newTokenAddress).to.not.equal(testerAddress);
        const TestToken = await ethers.getContractFactory("TestToken")
        const tokenInstance = await TestToken.attach(newTokenAddress);
        const bal = await tokenInstance.balanceOf(await owner.getAddress());
        expect(bal).to.equal(100);
      });
    })

      describe("Call Another Contract", function(){
        it("should set balance and then retrieve it via callAnotherContract", async function () {
          const setAmount = ethers.parseUnits("1000", 18);

          await delay()
          // Set balance
          await testToken.setBalance(owner.address, setAmount);

          // Prepare call data for balanceOf function of MyToken
          const balanceOfData = testToken.interface.encodeFunctionData("balanceOf", [owner.address]);

          const tokenAddress = await testToken.getAddress()

          await delay()
          // Call balanceOf using callAnotherContract from EVMCompatibilityTester
          await evmTester.callAnotherContract(tokenAddress, balanceOfData);

          await delay()
          // Verify the balance using MyToken contract directly
          const balance = await testToken.balanceOf(owner.address);
          expect(balance).to.equal(setAmount);
        });
    })

    describe("Msg Properties", function() {
      it("Should store and retrieve msg properties correctly", async function() {
        // Store msg properties
        const txResponse = await evmTester.storeMsgProperties({ value: 1 });
        await txResponse.wait();

        // Retrieve stored msg properties
        const msgDetails = await evmTester.lastMsgDetails();

        debug(msgDetails)

        // Assertions
        expect(msgDetails.sender).to.equal(owner.address);
        expect(msgDetails.value).to.equal(1);
        // `data` is the encoded function call, which is difficult to predict and assert
        // `gas` is the remaining gas after the transaction, which is also difficult to predict and assert
      });
    });


    describe("Block Properties", function () {
      it("Should have consistent block properties for a block", async function () {
        const currentBlockNumber = await ethers.provider.getBlockNumber();
        const iface = new ethers.Interface(["function getBlockProperties() view returns (bytes32 blockHash, address coinbase, uint256 prevrandao, uint256 gaslimit, uint256 number, uint256 timestamp)"]);
        const addr = await evmTester.getAddress()
        const tx = {
          to: addr,
          data: iface.encodeFunctionData("getBlockProperties", []),
          blockTag: currentBlockNumber-2
        };
        const result = await ethers.provider.call(tx);

        // wait for block to change
        while(true){
          const bn = await ethers.provider.getBlockNumber();
          if(bn !== currentBlockNumber){
                break
          }
          await sleep(100)
        }
        const result2 = await ethers.provider.call(tx);
        expect(result).to.equal(result2)
      });
    });

    describe("Variable Types", function () {
      it("Should set the address correctly and emit an event", async function () {
        // Call setAddress
        await delay()
        const txResponse = await evmTester.setAddressVar();
        await txResponse.wait();  // Wait for the transaction to be mined
        await expect(txResponse)
            .to.emit(evmTester, 'AddressSet')
            .withArgs(owner.address);
      });

      it("Should set the bool correctly and emit an event", async function () {
        // Call setBoolVar
        await delay()
        const txResponse = await evmTester.setBoolVar(true);
        await txResponse.wait();  // Wait for the transaction to be mined

        debug(JSON.stringify(txResponse))

        await expect(txResponse)
            .to.emit(evmTester, 'BoolSet')
            .withArgs(owner.address, true);

        // Verify that addr is set correctly
        expect(await evmTester.boolVar()).to.equal(true);
      });

      it("Should set the uint256 correctly and emit an event", async function () {
        // Call setBoolVar
        await delay()
        const txResponse = await evmTester.setUint256Var(12345);
        await txResponse.wait();  // Wait for the transaction to be mined

        debug(JSON.stringify(txResponse))

        await expect(txResponse)
            .to.emit(evmTester, 'Uint256Set')
            .withArgs(owner.address, 12345);

        // Verify that addr is set correctly
        expect(await evmTester.uint256Var()).to.equal(12345);
      });

      // this uses a newer version of ethers to attempt a blob transaction (different signer wallet)
      it("should return an error for blobs", async function(){
        const key = "0x57acb95d82739866a5c29e40b0aa2590742ae50425b7dd5b5d279a986370189e"
        const signer = new ethers.Wallet(key, ethers.provider);
        const blobData = "BLOB";
        const blobDataBytes = ethers.toUtf8Bytes(blobData);
        const blobHash = ethers.keccak256(blobDataBytes);

        const tx = {
          type: 3,
          to: owner.address,
          value: ethers.parseEther("0.1"),
          data: '0x',
          maxFeePerGas: ethers.parseUnits('100', 'gwei'),
          maxPriorityFeePerGas: ethers.parseUnits('1', 'gwei'),
          gasLimit: 100000,
          maxFeePerBlobGas: ethers.parseUnits('10', 'gwei'),
          blobVersionedHashes: [blobHash],
        }

        await expect(signer.sendTransaction(tx)).to.be.rejectedWith("unsupported transaction type");
      })

      it("Should trace a call with timestamp", async function () {
        await delay()
        const txResponse = await evmTester.setTimestamp();
        const receipt = await txResponse.wait();  // Wait for the transaction to be mined

        // get the timestamp that was saved off during setTimestamp()
        const lastTimestamp = await evmTester.lastTimestamp();

        // perform two trace calls with a small delay in between
        const trace1 = await hre.network.provider.request({
          method: "debug_traceTransaction",
          params: [receipt.hash],
        });
        await sleep(500)
        const trace2 = await hre.network.provider.request({
          method: "debug_traceTransaction",
          params: [receipt.hash],
        });

        // expect consistency in the trace calls (timestamp should be fixed to block)
        expect(JSON.stringify(trace1)).to.equal(JSON.stringify(trace2))

        // expect timestamp in the actual trace to match the timestamp seen at the time of invocation
        let found = false
        for(let log of trace1.structLogs) {
          if(log.op === "SSTORE" && log.stack.length >= 3) {
            const ts = log.stack[2]
            expect(ts).to.equal(lastTimestamp)
            found = true
            break;
          }
        }
        expect(found).to.be.true;
      });


      it("Should set the string correctly and emit an event", async function () {
        await delay()
        const txResponse = await evmTester.setStringVar("test");
        await txResponse.wait();  // Wait for the transaction to be mined

        await expect(txResponse)
            .to.emit(evmTester, 'StringSet')
            .withArgs(owner.address, "test");
        expect(await evmTester.stringVar()).to.equal("test");
      });

      it("Should set the bytes correctly and emit an event", async function () {
        await delay()
        const txResponse = await evmTester.setBytesVar(ethers.toUtf8Bytes("test"));
        await txResponse.wait();

        await expect(txResponse)
            .to.emit(evmTester, 'BytesSet')
            .withArgs(owner.address, ethers.toUtf8Bytes("test"));
        const bytesVar = await evmTester.bytesVar()
        expect(ethers.toUtf8String(bytesVar)).to.equal("test");
      });

      it("Should correctly set and retrieve balances in the mapping", async function () {
        const testAmount = 1000;

        await delay()
        // Send the transaction and wait for it to be confirmed
        const txResponse = await evmTester.setBalance(owner.address, testAmount);
        await txResponse.wait();
        await delay()
        // Now check the balance
        const balance = await evmTester.balances(owner.address);
        expect(balance).to.equal(testAmount);
      });

      it("Should store and retrieve a private var correctly", async function () {
        const testAmount = 12345;
        await delay()
        const txResponse = await evmTester.storeData(testAmount);
        await txResponse.wait();  // Wait for the transaction to be mined
        await delay()
        const retrievedAmount = await evmTester.retrieveData();
        expect(retrievedAmount).to.equal(BigInt(testAmount));
      });
    });

    describe("Require Logic", function(){
      it("Should revert when false is passed to revertIfFalse", async function () {
        await expect(evmTester.revertIfFalse(false)).to.be.reverted;
      });

      it("Should not revert when true is passed to revertIfFalse", async function () {
        await evmTester.revertIfFalse(true)
      });
    })


    describe("Assembly", function(){
      it("Should add numbers correctly", async function () {
        expect(await evmTester.addNumbers(10, 20)).to.equal(30);
      });

      it("Should return the current balance of the contract", async function () {
        const balance = await evmTester.getContractBalance();
        const address = await evmTester.getAddress()
        await delay()
        expect(balance).to.equal(await ethers.provider.getBalance(address));
      });

      it("Should return correct value from readFromStorage(index)", async function () {
        const testAmount = 12345;
        await delay()
        const txResponse = await evmTester.storeData(testAmount);
        await delay()
        await txResponse.wait();  // Wait for the transaction to be mined

        const retrievedAmount = await evmTester.readFromStorage(0);
        expect(retrievedAmount).to.equal(BigInt(testAmount));
      });
    })

    describe("Historical query test", function() {
      it("Should be able to get historical block data", async function() {
        const feeData = await ethers.provider.getFeeData();
        const gasPrice = Number(feeData.gasPrice);
        const zero = ethers.parseUnits('0', 'ether')
        const txResponse = await owner.sendTransaction({
          to: owner.address,
          gasPrice: gasPrice,
          value: zero,
          type: 1,
        });
        const receipt = await txResponse.wait();
        const bn = receipt.blockNumber;

        // Check historical balance
        const balance1 = await ethers.provider.getBalance(owner, bn-1);
        const balance2 = await ethers.provider.getBalance(owner, bn);
        expect(balance1 - balance2).to.equal(21000 * Number(gasPrice))

        // Check historical nonce
        const nonce1 = await ethers.provider.getTransactionCount(owner, bn-1);
        const nonce2 = await ethers.provider.getTransactionCount(owner, bn);
        expect(nonce1 + 1).to.equal(nonce2)
      });
    });

    describe("Gas tests", function() {
      it("Should deduct correct amount of gas on transfer", async function () {
        const balanceBefore = await ethers.provider.getBalance(owner);

        const feeData = await ethers.provider.getFeeData();
        const gasPrice = Number(feeData.gasPrice);

        const zero = ethers.parseUnits('0', 'ether')
        const txResponse = await owner.sendTransaction({
          to: owner.address,
          gasPrice: gasPrice,
          value: zero,
          type: 1,
        });
        await txResponse.wait();

        const balanceAfter = await ethers.provider.getBalance(owner);

        const diff = balanceBefore - balanceAfter;
        expect(diff).to.equal(21000 * gasPrice);

        const success = await sendTransactionAndCheckGas(owner, owner, 0)
        expect(success).to.be.true
      });

      it("Should fail if insufficient gas is provided", async function () {
        const feeData = await ethers.provider.getFeeData();
        const gasPrice = Number(feeData.gasPrice);
        const zero = ethers.parseUnits('0', 'ether')
        expect(owner.sendTransaction({
          to: owner.address,
          gasPrice: gasPrice - 1,
          value: zero,
          type: 1,
        })).to.be.reverted;
      });

      it("Should deduct correct amount even if higher gas price is used", async function () {
        const balanceBefore = await ethers.provider.getBalance(owner);

        const feeData = await ethers.provider.getFeeData();
        const gasPrice = Number(feeData.gasPrice);
        const higherGasPrice = Number(gasPrice + 9)
        console.log(`gasPrice = ${gasPrice}`)

        const zero = ethers.parseUnits('0', 'ether')
        const txResponse = await owner.sendTransaction({
          to: owner.address,
          value: zero,
          gasPrice: higherGasPrice,
          type: 1,
        });
        const receipt = await txResponse.wait();

        const balanceAfter = await ethers.provider.getBalance(owner);

        const diff = balanceBefore - balanceAfter;
        expect(diff).to.equal(21000 * higherGasPrice);

        const success = await sendTransactionAndCheckGas(owner, owner, 0)
        expect(success).to.be.true
      });

      describe("EIP-1559", async function() {
        const zero = ethers.parseUnits('0', 'ether')
        const twoGwei = ethers.parseUnits("2", "gwei");
        const oneGwei = ethers.parseUnits("1", "gwei");

        const testCases = [
          ["No truncation from max priority fee", oneGwei, oneGwei],
          ["With truncation from max priority fee", oneGwei, twoGwei],
          ["With complete truncation from max priority fee", zero, twoGwei]
        ];

        it("Should be able to send many EIP-1559 txs", async function () {
          const oneGwei = ethers.parseUnits("1", "gwei");
          const zero = ethers.parseUnits('0', 'ether')
          for (let i = 0; i < 10; i++) {
            const txResponse = await owner.sendTransaction({
              to: owner.address,
              value: zero,
              maxPriorityFeePerGas: oneGwei,
              maxFeePerGas: oneGwei,
              type: 2
            });
            await txResponse.wait();
          }
        });

        describe("Differing maxPriorityFeePerGas and maxFeePerGas", async function() {
          for (const [name, maxPriorityFeePerGas, maxFeePerGas] of testCases) {
            it(`EIP-1559 test: ${name}`, async function() {
              console.log(`maxPriorityFeePerGas = ${maxPriorityFeePerGas}`)
              console.log(`maxFeePerGas = ${maxFeePerGas}`)
              const balanceBefore = await ethers.provider.getBalance(owner);
              const zero = ethers.parseUnits('0', 'ether')
              const txResponse = await owner.sendTransaction({
                to: owner.address,
                value: zero,
                maxPriorityFeePerGas: maxPriorityFeePerGas,
                maxFeePerGas: maxFeePerGas,
                type: 2
              });
              const receipt = await txResponse.wait();
              expect(receipt).to.not.be.null;
              expect(receipt.status).to.equal(1);
              const gasPrice = Number(receipt.gasPrice);
              console.log(`gasPrice = ${gasPrice}`)

              const balanceAfter = await ethers.provider.getBalance(owner);

              const tip = Math.min(
                Number(maxFeePerGas) - gasPrice,
                Number(maxPriorityFeePerGas)
              );
              console.log(`tip = ${tip}`)
              const effectiveGasPrice = tip + gasPrice;
              console.log(`effectiveGasPrice = ${effectiveGasPrice}`)

              const diff = balanceBefore - balanceAfter;
              console.log(`diff = ${diff}`)
              expect(diff).to.equal(21000 * effectiveGasPrice);
            });
          }
        });
      });
    });

    describe("JSON-RPC", function() {
      it("Should retrieve a transaction by its hash", async function () {
        // Send a transaction to get its hash
        const txResponse = await evmTester.setBoolVar(true);
        await txResponse.wait();

        // Retrieve the transaction by its hash
        const tx = await ethers.provider.getTransaction(txResponse.hash);
        expect(tx).to.not.be.null;
        expect(tx.hash).to.equal(txResponse.hash);
      });

      it("Should retrieve a block by its number", async function () {
        // Get the current block number
        const currentBlockNumber = await ethers.provider.getBlockNumber();

        // Retrieve the block by its number
        const block = await ethers.provider.getBlock(currentBlockNumber);
        expect(block).to.not.be.null;
        expect(block.number).to.equal(currentBlockNumber);
      });

      it("Should retrieve the latest block", async function () {
        // Retrieve the latest block
        const block = await ethers.provider.getBlock("latest");
        expect(block).to.not.be.null;
      });

      it("Should get the balance of an account", async function () {
        // Get the balance of an account (e.g., the owner)
        const balance = await ethers.provider.getBalance(owner.address);

        // The balance should be a BigNumber; we can't predict its exact value
        expect(isBigNumber(balance)).to.be.true;
      });

      it("Should get the code at a specific address", async function () {
        // Get the code at the address of a deployed contract (e.g., evmTester)
        const code = await ethers.provider.getCode(await evmTester.getAddress());

        // The code should start with '0x' and be longer than just '0x' for a deployed contract
        expect(code.startsWith("0x")).to.be.true;
        expect(code.length).to.be.greaterThan(2);
        debug(code)
      });


      it("Should retrieve a block by its hash", async function () {
        const blockNumber = await ethers.provider.getBlockNumber();
        const block = await ethers.provider.getBlock(blockNumber);
        const fetchedBlock = await ethers.provider.getBlock(block.hash);
        expect(fetchedBlock).to.not.be.null;
        expect(fetchedBlock.hash).to.equal(block.hash);
      });

      it("Should fetch the number of transactions in a block", async function () {
        const block = await ethers.provider.getBlock("latest");
        expect(block.transactions).to.be.an('array');
      });

      it("Should retrieve a transaction receipt", async function () {
        const txResponse = await evmTester.setBoolVar(false, {
          type: 2, // force it to be EIP-1559
          maxPriorityFeePerGas: ethers.parseUnits('100', 'gwei'), // set gas high just to get it included
          maxFeePerGas: ethers.parseUnits('100', 'gwei')
        });
        await txResponse.wait();
        const receipt = await ethers.provider.getTransactionReceipt(txResponse.hash);
        expect(receipt).to.not.be.undefined;
        expect(receipt.hash).to.equal(txResponse.hash);
        expect(receipt.blockHash).to.not.be.undefined;
        expect(receipt.blockNumber).to.not.be.undefined;
        expect(receipt.logsBloom).to.not.be.undefined;
        expect(receipt.gasUsed).to.be.greaterThan(0);
        expect(receipt.gasPrice).to.be.greaterThan(0);
        expect(receipt.type).to.equal(2); // sei is failing this
        expect(receipt.status).to.equal(1);
        expect(receipt.to).to.equal(await evmTester.getAddress());
        expect(receipt.from).to.equal(owner.address);
        expect(receipt.cumulativeGasUsed).to.be.greaterThanOrEqual(0); // on seilocal, this is 0

        // undefined / null on anvil and goerli
        // expect(receipt.contractAddress).to.be.equal(null); // seeing this be null (sei devnet) and not null (anvil, goerli)
        expect(receipt.effectiveGasPrice).to.be.undefined;
        expect(receipt.transactionHash).to.be.undefined;
        expect(receipt.transactionIndex).to.be.undefined;
        const logs = receipt.logs
        for (let i = 0; i < logs.length; i++) {
          const log = logs[i];
          expect(log).to.not.be.undefined;
          expect(log.address).to.equal(receipt.to);
          expect(log.topics).to.be.an('array');
          expect(log.data).to.be.a('string');
          expect(log.data.startsWith('0x')).to.be.true;
          expect(log.data.length).to.be.greaterThan(3);
          expect(log.blockNumber).to.equal(receipt.blockNumber);
          expect(log.transactionHash).to.not.be.undefined; // somehow log.transactionHash exists but receipt.transactionHash does not
          expect(log.transactionHash).to.not.be.undefined;
          expect(log.transactionIndex).to.be.greaterThanOrEqual(0);
          expect(log.blockHash).to.equal(receipt.blockHash);

          // undefined / null on anvil and goerli
          expect(log.logIndex).to.be.undefined;
          expect(log.removed).to.be.undefined;
        }
      });

      it("Should fetch the current gas price", async function () {
        const feeData = await ethers.provider.getFeeData()
        expect(isBigNumber(feeData.gasPrice)).to.be.true;
      });

      it("Should estimate gas for a transaction", async function () {
        const estimatedGas = await ethers.provider.estimateGas({
          to: await evmTester.getAddress(),
          data: evmTester.interface.encodeFunctionData("setBoolVar", [true])
        });
        expect(isBigNumber(estimatedGas)).to.be.true;
      });

      it("Should check the network status", async function () {
        const network = await ethers.provider.getNetwork();
        expect(network).to.have.property('name');
        expect(network).to.have.property('chainId');
      });

      it("Should fetch the nonce for an account", async function () {
        const nonce = await ethers.provider.getTransactionCount(owner.address);
        expect(nonce).to.be.a('number');
      });

      it("Should set log index correctly", async function () {
        const blockNumber = await ethers.provider.getBlockNumber();
        const numberOfEvents = 5;

        // check receipt
        const txResponse = await evmTester.emitMultipleLogs(numberOfEvents);
        const receipt = await txResponse.wait();
        expect(receipt.logs.length).to.equal(numberOfEvents)
        for(let i=0; i<receipt.logs.length; i++) {
          expect(receipt.logs[i].index).to.equal(i);
        }

        // check logs
        const filter = {
          fromBlock: blockNumber,
          toBlock: 'latest',
          address: await evmTester.getAddress(),
          topics: [ethers.id("LogIndexEvent(address,uint256)")]
        };
        const logs = await ethers.provider.getLogs(filter);
        expect(logs.length).to.equal(numberOfEvents)
        for(let i=0; i<logs.length; i++) {
          expect(logs[i].index).to.equal(i);
        }
      })

      it("Should fetch logs for a specific event", async function () {
        // Emit an event by making a transaction
        const blockNumber = await ethers.provider.getBlockNumber();
        const txResponse = await evmTester.setBoolVar(true);
        await txResponse.wait();

        // Create a filter to get logs
        const filter = {
          fromBlock: blockNumber,
          toBlock: 'latest',
          address: await evmTester.getAddress(),
          topics: [ethers.id("BoolSet(address,bool)")]
        };
        // Get the logs
        const logs = await ethers.provider.getLogs(filter);
        expect(logs).to.be.an('array').that.is.not.empty;
      });

      it("Should subscribe to an event", async function () {
        this.timeout(10000); // Increase timeout for this test

        // Create a filter to subscribe to
        const filter = {
          address: await evmTester.getAddress(),
          topics: [ethers.id("BoolSet(address,bool)")]
        };

        // Subscribe to the filter
        const listener = (log) => {
          expect(log).to.not.be.null;
          ethers.provider.removeListener(filter, listener);
        };
        ethers.provider.on(filter, listener);

        // Trigger the event
        const txResponse = await evmTester.setBoolVar(false);
        await txResponse.wait();
      });

      it("Should get the current block number", async function () {
        const blockNumber = await ethers.provider.getBlockNumber();
        expect(blockNumber).to.be.a('number');
      });

      it("Should fetch a block with full transactions", async function () {
        const blockNumber = await ethers.provider.getBlockNumber();
        const blockWithTransactions = await ethers.provider.getBlock(blockNumber, true);
        expect(blockWithTransactions).to.not.be.null;
        expect(blockWithTransactions.transactions).to.be.an('array');
      });

      it("Should get the chain ID", async function () {
        const { chainId } = await ethers.provider.getNetwork()
        expect(chainId).to.be.greaterThan(0)
      });

      it("Should fetch past logs", async function () {
        const contractAddress = await evmTester.getAddress()
        const filter = {
          fromBlock: 0,
          toBlock: 'latest',
          address: contractAddress
        };
        const logs = await ethers.provider.getLogs(filter);
        expect(logs).to.be.an('array');
        expect(logs).length.to.be.greaterThan(0)
      });

      it("Should check account's transaction count", async function () {
        const nonce = await ethers.provider.getTransactionCount(owner.address, "latest");
        expect(nonce).to.be.a('number');
      });

      it("Should check if an address is a contract", async function () {
        const code = await ethers.provider.getCode(await evmTester.getAddress());
        const isContract = code !== '0x';
        expect(isContract).to.be.true;
      });

      it("advanced log topic filtering", async function() {
        describe("log topic filtering", async function() {
          let blockStart;
          let blockEnd;
          let numTxs = 5;
          before(async function() {
            await sleep(5000); // wait for a block to pass so we get a fresh block number
            blockStart = await ethers.provider.getBlockNumber();

            // Emit an event by making a transaction
            for (let i = 0; i < numTxs; i++) {
              const txResponse = await evmTester.emitDummyEvent("test", i);
              await txResponse.wait();
            }
            blockEnd = await ethers.provider.getBlockNumber();
            console.log("blockStart = ", blockStart)
            console.log("blockEnd = ", blockEnd)
          });

          it("Block range filter", async function () {
            const filter = {
              fromBlock: blockStart,
              toBlock: blockEnd,
            };
          
            const logs = await ethers.provider.getLogs(filter);
            expect(logs).to.be.an('array');
            expect(logs.length).to.equal(numTxs);
          });

          it("Single topic filter", async function() {
            const filter = {
              fromBlock: blockStart,
              toBlock: blockEnd,
              topics: [ethers.id("DummyEvent(string,bool,address,uint256,bytes)")]
            };
          
            const logs = await ethers.provider.getLogs(filter);
            expect(logs).to.be.an('array');
            expect(logs.length).to.equal(numTxs);
          });

          it("Blockhash filter", async function() {
            // first get a log
            const filter = {
              fromBlock: blockStart,
              toBlock: blockEnd,
              topics: [ethers.id("DummyEvent(string,bool,address,uint256,bytes)")]
            };
          
            const logs = await ethers.provider.getLogs(filter);
            const blockHash = logs[0].blockHash;

            // now get logs by blockhash
            const blockHashFilter = {
              blockHash: blockHash,
            };

            const blockHashLogs = await ethers.provider.getLogs(blockHashFilter);
            expect(blockHashLogs).to.be.an('array');
            for (let i = 0; i < blockHashLogs.length; i++) {
              expect(blockHashLogs[i].blockHash).to.equal(blockHash);
            }
          });

          it("Multiple topic filter", async function() {
            // Topic A and B represented as [A, B]
            const paddedOwnerAddr = "0x" + owner.address.slice(2).padStart(64, '0');
            const filter1 = {
              fromBlock: blockStart,
              toBlock: blockEnd,
              topics: [
                ethers.id("DummyEvent(string,bool,address,uint256,bytes)"),
                ethers.id("test"),
                paddedOwnerAddr,
              ]
            };
          
            const logs = await ethers.provider.getLogs(filter1);
          
            expect(logs).to.be.an('array');
            expect(logs.length).to.equal(numTxs);

            // Topic A and B represented as [A, [B]]
            const filter2 = {
              fromBlock: blockStart,
              toBlock: blockEnd,
              topics: [
                ethers.id("DummyEvent(string,bool,address,uint256,bytes)"),
                [ethers.id("test")],
                [paddedOwnerAddr],
              ]
            };

            const logs2 = await ethers.provider.getLogs(filter1);
          
            expect(logs2).to.be.an('array');
            expect(logs2.length).to.equal(numTxs);
          });

          it("Wildcard topic filter", async function() {
            const filter1 = {
              fromBlock: blockStart,
              toBlock: blockEnd,
              topics: [
                ethers.id("DummyEvent(string,bool,address,uint256,bytes)"),
                ethers.id("test"),
                null,
                "0x0000000000000000000000000000000000000000000000000000000000000003",
              ]
            };
          
            const logs1 = await ethers.provider.getLogs(filter1);
            expect(logs1).to.be.an('array');
            expect(logs1.length).to.equal(1);

            // filter for topic A and (B or C) = [A, [B, C]]
            const filter2 = {
              fromBlock: blockStart,
              toBlock: blockEnd,
              topics: [
                ethers.id("DummyEvent(string,bool,address,uint256,bytes)"),
                ethers.id("test"),
                null,
                [
                    "0x0000000000000000000000000000000000000000000000000000000000000002",
                    "0x0000000000000000000000000000000000000000000000000000000000000003",
                ]
              ]
            }
            const logs2 = await ethers.provider.getLogs(filter2);
            expect(logs2).to.be.an('array');
            expect(logs2.length).to.equal(2);
          });

          it("Address and topics combination filter", async function() {
            const filter = {
              fromBlock: blockStart,
              toBlock: blockEnd,
              address: await evmTester.getAddress(),
              topics: [
                ethers.id("DummyEvent(string,bool,address,uint256,bytes)"),
              ]
            }
            const logs = await ethers.provider.getLogs(filter);
            expect(logs).to.be.an('array');
            expect(logs.length).to.equal(numTxs);
          });

          it("Empty result filter", async function() {
            const filter = {
              fromBlock: blockStart,
              toBlock: blockEnd,
              topics: [
                ethers.id("DummyEvent(string,bool,address,uint256,bytes)"),
                ethers.id("nonexistent event string"),
              ]
            };
          
            const logs = await ethers.provider.getLogs(filter);
            expect(logs).to.be.an('array');
            expect(logs.length).to.equal(0);
          });

          it("Overlapping criteria filter", async function() {
            // [ (topic[0] = A) OR (topic[0] = B) ] AND [ (topic[1] = C) OR (topic[1] = D) ]
            const filter = {
              fromBlock: blockStart,
              toBlock: blockEnd,
              topics: [
                ethers.id("DummyEvent(string,bool,address,uint256,bytes)"),
                [ethers.id("test"), ethers.id("nonexistent event string")],
                null,
                [
                    "0x0000000000000000000000000000000000000000000000000000000000000001",
                    "0x0000000000000000000000000000000000000000000000000000000000000002",
                    "0x0000000000000000000000000000000000000000000000000000000000000003",
                ]
              ]
            }

            const logs = await ethers.provider.getLogs(filter);
            expect(logs).to.be.an('array');
            expect(logs.length).to.equal(3);
          });
        });
      });
    });

    describe("Contract Upgradeability", function() {
      it("Should allow for contract upgrades", async function() {
        // deploy BoxV1
        const Box = await ethers.getContractFactory("Box");
        const val = 42;
        console.log('Deploying Box...');
        const box = await upgrades.deployProxy(Box, [val], { initializer: 'store' });
        const boxReceipt = await box.waitForDeployment()
        console.log("boxReceipt = ", JSON.stringify(boxReceipt))
        const boxAddr = await box.getAddress();
        const implementationAddress = await getImplementationAddress(ethers.provider, boxAddr);
        console.log('Box Implementation address:', implementationAddress);
        console.log('Box deployed to:', boxAddr)

        // make sure you can retrieve the value
        const retrievedValue = await box.retrieve();
        expect(retrievedValue).to.equal(val);

        // increment value
        console.log("Incrementing value...")
        const resp = await box.boxIncr();
        await resp.wait();

        // make sure value is incremented
        const retrievedValue1 = await box.retrieve();
        expect(retrievedValue1).to.equal(val+1);

        // upgrade to BoxV2
        const BoxV2 = await ethers.getContractFactory('BoxV2');
        console.log('Upgrading Box...');
        const box2 = await upgrades.upgradeProxy(boxAddr, BoxV2, [val+1], { initializer: 'store' });
        await box2.deployTransaction.wait();
        console.log('Box upgraded');
        const boxV2Addr = await box2.getAddress();
        expect(boxV2Addr).to.equal(boxAddr); // should be same address as it should be the proxy
        console.log('BoxV2 deployed to:', boxV2Addr);
        const boxV2 = await BoxV2.attach(boxV2Addr);

        // check that value is still the same
        console.log("Calling boxV2 retrieve()...")
        const retrievedValue2 = await boxV2.retrieve();
        console.log("retrievedValue2 = ", retrievedValue2)
        expect(retrievedValue2).to.equal(val+1);

        // use new function in boxV2 and increment value
        console.log("Calling boxV2 boxV2Incr()...")
        const txResponse = await boxV2.boxV2Incr();
        await txResponse.wait();

        // make sure value is incremented
        expect(await boxV2.retrieve()).to.equal(val+2);

        // store something in value2 and check it(check value2)
        const store2Resp = await boxV2.store2(10);
        await store2Resp.wait();
        expect(await boxV2.retrieve2()).to.equal(10);

        // ensure value is still the same in boxV2 (checking for any storage corruption)
        expect(await boxV2.retrieve()).to.equal(val+2);
      });
    });

    describe("Usei/Wei testing", function() {
      it("Send 1 usei to contract", async function() {
        const usei = ethers.parseUnits("1", 12);
        const wei = ethers.parseUnits("1", 0);
        const twoWei = ethers.parseUnits("2", 0);

        // Check that the contract has no ETH
        const initialBalance = await ethers.provider.getBalance(evmAddr);

        const txResponse = await evmTester.depositEther({
          value: usei,
        });
        await txResponse.wait();  // Wait for the transaction to be mined
      
        // Check that the contract received the ETH
        const contractBalance = await ethers.provider.getBalance(evmAddr);
        expect(contractBalance - initialBalance).to.equal(usei);

        // send 1 wei out of contract
        const txResponse2 = await evmTester.sendEther(owner.address, wei);
        await txResponse2.wait();  // Wait for the transaction to be mined

        const contractBalance2 = await ethers.provider.getBalance(evmAddr);
        expect(contractBalance2 - contractBalance).to.equal(-wei);

        // send 2 wei to contract
        const txResponse3 = await evmTester.depositEther({
          value: twoWei,
        });
        await txResponse3.wait();  // Wait for the transaction to be mined

        const contractBalance3 = await ethers.provider.getBalance(evmAddr);
        expect(contractBalance3 - contractBalance2).to.equal(twoWei);
      });
    });
  });
});

describe("EVM throughput", function(){

  it("send 100 transactions from one account", async function(){
    const wallet = generateWallet()
    const toAddress =await wallet.getAddress()
    const accounts = await ethers.getSigners();
    const sender = accounts[0]
    const address = await sender.getAddress();
    const txCount = 100;

    const nonce = await ethers.provider.getTransactionCount(address);
    const responses = []

    let txs = []
    let maxNonce = 0
    for(let i=0; i<txCount; i++){
      const nextNonce = nonce+i;
      txs.push({
        to: toAddress,
        value: 1,
        nonce: nextNonce,
      })
      maxNonce = nextNonce;
    }

    // send out of order because it's legal
    txs = shuffle(txs)
    const promises = txs.map((txn)=> {
      return sendTx(sender, txn, responses)
    });
    await Promise.all(promises)

    // wait for last nonce to mine (means all prior mined)
    for(let r of responses){
      if(r.nonce === maxNonce) {
        await r.response.wait()
        break;
      }
    }

    // get represented block numbers
    let blockNumbers = []
    for(let response of responses){
      const receipt = await response.response.wait()
      const blockNumber = receipt.blockNumber
      blockNumbers.push(blockNumber)
    }

    blockNumbers = uniq(blockNumbers).sort((a,b)=>{return a-b})
    const minedNonceOrder = []
    for(const blockNumber of blockNumbers){
      const block = await ethers.provider.getBlock(parseInt(blockNumber,10));
      // get receipt for transaction hash in block
      for(const txHash of block.transactions){
        const tx = await ethers.provider.getTransaction(txHash)
        minedNonceOrder.push(tx.nonce)
      }
    }

    expect(minedNonceOrder.length).to.equal(txCount);
    for (let i = 0; i < minedNonceOrder.length; i++) {
      expect(minedNonceOrder[i]).to.equal(i+nonce)
    }
  })
})
