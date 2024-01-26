const { expect } = require("chai");
const {isBigNumber} = require("hardhat/common");
const { ethers } = require("hardhat");
const {uniq, shuffle} = require("lodash");

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

describe("EVM Test", function () {

  describe("EVMCompatibilityTester", function () {
    let evmTester;
    let testToken;
    let owner;
    let evmAddr;

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
      it("Should get and print the block properties", async function () {
        await delay()
        const blockProperties = await evmTester.getBlockProperties();
        debug(blockProperties)
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


      it("Should set the string correctly and emit an event", async function () {
        // Call setBoolVar
        await delay()
        const txResponse = await evmTester.setStringVar("test");
        await txResponse.wait();  // Wait for the transaction to be mined
        await expect(txResponse)
            .to.emit(evmTester, 'StringSet')
            .withArgs(owner.address, "test");

        // Verify that addr is set correctly
        expect(await evmTester.stringVar()).to.equal("test");
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
              console.log("receipt = ", receipt)

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
