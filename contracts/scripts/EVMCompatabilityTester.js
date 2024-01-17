const { expect } = require("chai");
const {isBigNumber} = require("hardhat/common");


function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

async function delay() {
  // await sleep(3000)
}

function debug(msg) {
  console.log(msg)
}

describe("EVM Test", function () {

  describe("EVMCompatibilityTester", function () {
    let evmTester;
    let testToken;
    let owner;

    // This function deploys a new instance of the contract before each test
    beforeEach(async function () {
      if(evmTester && testToken) {
        return
      }
      let signers = await ethers.getSigners();
      owner = signers[0]
      debug(`OWNER = ${owner.address}`)

      const TestToken = await ethers.getContractFactory("TestToken")
      testToken = await TestToken.deploy("TestToken", "TTK");

      const EVMCompatibilityTester = await ethers.getContractFactory("EVMCompatibilityTester");
      evmTester = await EVMCompatibilityTester.deploy();

      await Promise.all([evmTester.waitForDeployment(), testToken.waitForDeployment()])

      let tokenAddr = await testToken.getAddress()
      let evmAddr = await evmTester.getAddress()

      debug(`Token: ${tokenAddr}, EvmAddr: ${evmAddr}`);
    });

    describe("Deployment", function () {
      it("Should deploy successfully", async function () {
        expect(await evmTester.getAddress()).to.be.properAddress;
        expect(await testToken.getAddress()).to.be.properAddress;
        expect(await evmTester.getAddress()).to.not.equal(await testToken.getAddress());
      });
    });

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
        await delay()
        const txResponse = await evmTester.revertIfFalse(true)
        await delay()
        await txResponse.wait();  // Wait for the transaction to be mined

        await expect(txResponse).to.not.be.reverted;
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
        const txResponse = await evmTester.setBoolVar(false);
        await txResponse.wait();
        const receipt = await ethers.provider.getTransactionReceipt(txResponse.hash);
        expect(receipt).to.not.be.null;
        expect(receipt.hash).to.equal(txResponse.hash);
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

      it("Should fetch logs for a specific event", async function () {
        // Emit an event by making a transaction
        const txResponse = await evmTester.setBoolVar(true);
        await txResponse.wait();

        // Create a filter to get logs
        const filter = {
          fromBlock: 0,
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



  });

});
