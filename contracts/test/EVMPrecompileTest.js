const { execSync } = require('child_process');
const { expect } = require("chai");
const fs = require('fs');
const path = require('path');

const { expectRevert } = require('@openzeppelin/test-helpers');
const { setupSigners, getAdmin, deployWasm, storeWasm, execute, isDocker, ABI, createTokenFactoryTokenAndMint, getSeiBalance} = require("./lib");


describe("EVM Precompile Tester", function () {

    let accounts;
    let admin;

    before(async function () {
        accounts = await setupSigners(await hre.ethers.getSigners());
        admin = await getAdmin();
    })

    describe("EVM Addr Precompile Tester", function () {
        const AddrPrecompileContract = '0x0000000000000000000000000000000000001004';
        let addr;

        before(async function () {
            const signer = accounts[0].signer
            const contractABIPath = '../../precompiles/addr/abi.json';
            const contractABI = require(contractABIPath);
            // Get a contract instance
            addr = new ethers.Contract(AddrPrecompileContract, contractABI, signer);
        });

        it("Associates successfully", async function () {
            const unassociatedWallet = hre.ethers.Wallet.createRandom();
            try {
                await addr.getSeiAddr(unassociatedWallet.address);
                expect.fail("Expected an error here since we look up an unassociated address");
            } catch (error) {
                expect(error).to.have.property('message').that.includes('execution reverted');
            }
            
            const message = `Please sign this message to link your EVM and Sei addresses. No SEI will be spent as a result of this signature.\n\n`;
            const messageLength = Buffer.from(message, 'utf8').length;
            const signatureHex = await unassociatedWallet.signMessage(message);

            const sig = hre.ethers.Signature.from(signatureHex);
            
            const appendedMessage = `\x19Ethereum Signed Message:\n${messageLength}${message}`;
            const associatedAddrs = await addr.associate(`0x${sig.v-27}`, sig.r, sig.s, appendedMessage)
            const addrs = await associatedAddrs.wait();
            expect(addrs).to.not.be.null;

            // Verify that addresses are now associated.
            const seiAddr = await addr.getSeiAddr(unassociatedWallet.address);
            expect(seiAddr).to.not.be.null;
        });

        it("Associates with Public Key successfully", async function () {
            const unassociatedWallet = hre.ethers.Wallet.createRandom();
            try {
                await addr.getSeiAddr(unassociatedWallet.address);
                expect.fail("Expected an error here since we look up an unassociated address");
            } catch (error) {
                expect(error).to.have.property('message').that.includes('execution reverted');
            }

            // Use the PublicKey without the '0x' prefix.
            const associatedAddrs = await addr.associatePubKey(unassociatedWallet.publicKey.slice(2))
            const addrs = await associatedAddrs.wait();
            expect(addrs).to.not.be.null;

            // Verify that addresses are now associated.
            const seiAddr = await addr.getSeiAddr(unassociatedWallet.address);
            expect(seiAddr).to.not.be.null;
        });
    });

    describe("EVM Gov Precompile Tester", function () {
        const GovPrecompileContract = '0x0000000000000000000000000000000000001006';
        let gov;
        let govProposal;

        before(async function () {
            const govProposalResponse = JSON.parse(await execute(`seid tx gov submit-proposal param-change ../contracts/test/param_change_proposal.json --from admin --fees 20000usei -b block -y -o json`))
            govProposal = govProposalResponse.logs[0].events[3].attributes[1].value;

            const signer = accounts[0].signer
            const contractABIPath = '../../precompiles/gov/abi.json';
            const contractABI = require(contractABIPath);
            // Get a contract instance
            gov = new ethers.Contract(GovPrecompileContract, contractABI, signer);
        });

        it("Gov deposit", async function () {
            const depositAmount = ethers.parseEther('0.01');
            const deposit = await gov.deposit(govProposal, {
                value: depositAmount,
            })
            const receipt = await deposit.wait();
            expect(receipt.status).to.equal(1);
        });
    });

    // TODO: Update when we add distribution query precompiles
    describe("EVM Distribution Precompile Tester", function () {
        const DistributionPrecompileContract = '0x0000000000000000000000000000000000001007';
        let distribution;
        before(async function () {
            const signer = accounts[0].signer;
            const contractABIPath = '../../precompiles/distribution/abi.json';
            const contractABI = require(contractABIPath);
            // Get a contract instance
            distribution = new ethers.Contract(DistributionPrecompileContract, contractABI, signer);
        });

        it("Distribution set withdraw address", async function () {
            const setWithdraw = await distribution.setWithdrawAddress(accounts[0].evmAddress)
            const receipt = await setWithdraw.wait();
            expect(receipt.status).to.equal(1);
        });
        it("Should query rewards and get non null response", async function () {
            const rewards = await distribution.rewards(accounts[0].evmAddress)
            expect(rewards).to.not.be.null;
        });
    });

    // TODO: Update when we add staking query precompiles
    describe("EVM Staking Precompile Tester", function () {
        const StakingPrecompileContract = '0x0000000000000000000000000000000000001005';
        let validatorAddr;
        let signer;
        let staking;

        before(async function () {
            validatorAddr = JSON.parse(await execute("seid q staking validators -o json")).validators[0].operator_address
            signer = accounts[0].signer;

            const contractABIPath = '../../precompiles/staking/abi.json';
            const contractABI = require(contractABIPath);

            staking = new ethers.Contract(StakingPrecompileContract, contractABI, signer);
        });

        it("Staking delegate", async function () {
            const delegateAmount = ethers.parseEther('0.01');
            const delegate = await staking.delegate(validatorAddr, {
                value: delegateAmount,
            });
            const receipt = await delegate.wait();
            expect(receipt.status).to.equal(1);

            const delegation = await staking.delegation(accounts[0].evmAddress, validatorAddr);
            expect(delegation).to.not.be.null;
            expect(delegation[0][0]).to.equal(10000n);

            const undelegate = await staking.undelegate(validatorAddr, delegation[0][0]);
            const undelegateReceipt = await undelegate.wait();
            expect(undelegateReceipt.status).to.equal(1);

            try {
                await staking.delegation(accounts[0].evmAddress, validatorAddr);
                expect.fail("Expected an error here since we undelegated the amount and delegation should not exist anymore.");
            } catch (error) {
                expect(error).to.have.property('message').that.includes('execution reverted');
            }
        });
    });

    describe("EVM Oracle Precompile Tester", function () {
        const OraclePrecompileContract = '0x0000000000000000000000000000000000001008';
        let oracle;
        let twapsJSON;
        let exchangeRatesJSON;

        before(async function() {
            // this requires an oracle to run which does not happen outside of an integration test
            if(!await isDocker()) {
                this.skip()
                return;
            }
            const exchangeRatesContent = await execute("seid q oracle exchange-rates -o json")
            const twapsContent = await execute("seid q oracle twaps 3600 -o json")

            exchangeRatesJSON = JSON.parse(exchangeRatesContent).denom_oracle_exchange_rate_pairs;
            twapsJSON = JSON.parse(twapsContent).oracle_twaps;

            const contractABIPath = '../../precompiles/oracle/abi.json';
            const contractABI = require(contractABIPath);
            // Get a contract instance
            oracle = new ethers.Contract(OraclePrecompileContract, contractABI, accounts[0].signer);
        });

        it("Oracle Exchange Rates", async function () {
            const exchangeRates = await oracle.getExchangeRates();
            const exchangeRatesLen = exchangeRatesJSON.length;
            expect(exchangeRates.length).to.equal(exchangeRatesLen);

            for (let i = 0; i < exchangeRatesLen; i++) {
                expect(exchangeRates[i].denom).to.equal(exchangeRatesJSON[i].denom);
                expect(exchangeRates[i].oracleExchangeRateVal.exchangeRate).to.be.a('string').and.to.not.be.empty;
                expect(exchangeRates[i].oracleExchangeRateVal.exchangeRate).to.be.a('string').and.to.not.be.empty;
                expect(exchangeRates[i].oracleExchangeRateVal.lastUpdateTimestamp).to.exist.and.to.be.gt(0);
            }
        });

        it("Oracle Twaps", async function () {
            const twaps = await oracle.getOracleTwaps(3600);
            const twapsLen = twapsJSON.length
            expect(twaps.length).to.equal(twapsLen);

            for (let i = 0; i < twapsLen; i++) {
                expect(twaps[i].denom).to.equal(twapsJSON[i].denom);
                expect(twaps[i].twap).to.be.a('string').and.to.not.be.empty;
                expect(twaps[i].lookbackSeconds).to.exist.and.to.be.gt(0);
            }
        });
    });

    describe("EVM Wasm Precompile Tester", function () {
        const WasmPrecompileContract = '0x0000000000000000000000000000000000001002';
        let wasmCodeID;
        let wasmContractAddress;
        let wasmd;
        let owner;
        let denom;
        let admin;

        before(async function () {
            const counterWasm = '../integration_test/contracts/counter_parallel.wasm';
            wasmCodeID = await storeWasm(counterWasm);

            const counterParallelWasm = '../integration_test/contracts/counter_parallel.wasm'
            wasmContractAddress = await deployWasm(counterParallelWasm, accounts[0].seiAddress, "counter", {count: 0});
            owner = accounts[0].signer;

            const contractABIPath = '../../precompiles/wasmd/abi.json';
            const contractABI = require(contractABIPath);
            // Get a contract instance
            wasmd = new ethers.Contract(WasmPrecompileContract, contractABI, owner);

            accounts = await setupSigners(await hre.ethers.getSigners())
            admin = await getAdmin()
            const random_num = Math.floor(Math.random() * 10000)
            denom = await createTokenFactoryTokenAndMint(`native-pointer-test-${random_num}`, 1000, accounts[0].seiAddress);
        });

        it("Wasm Precompile Instantiate", async function () {
            const encoder = new TextEncoder();

            const instantiateMsg = {count: 2};
            const instantiateStr = JSON.stringify(instantiateMsg);
            const instantiateBz = encoder.encode(instantiateStr);

            const coins = [];
            const coinsStr = JSON.stringify(coins);
            const coinsBz = encoder.encode(coinsStr);

            const instantiate = await wasmd.instantiate(wasmCodeID, "", instantiateBz, "counter-contract", coinsBz);
            const receipt = await instantiate.wait();
            expect(receipt.status).to.equal(1);
        });

        it("Wasm Precompile Execute", async function () {
            const encoder = new TextEncoder();

            const queryCountMsg = {get_count: {}};
            const queryStr = JSON.stringify(queryCountMsg);
            const queryBz = encoder.encode(queryStr);
            const initialCountBz = await wasmd.query(wasmContractAddress, queryBz);
            const initialCount = parseHexToJSON(initialCountBz)

            const incrementMsg = {increment: {}};
            const incrementStr = JSON.stringify(incrementMsg);
            const incrementBz = encoder.encode(incrementStr);

            const coins = [];
            const coinsStr = JSON.stringify(coins);
            const coinsBz = encoder.encode(coinsStr);

            const response = await wasmd.execute(wasmContractAddress, incrementBz, coinsBz);
            const receipt = await response.wait();
            expect(receipt.status).to.equal(1);

            const finalCountBz = await wasmd.query(wasmContractAddress, queryBz);
            const finalCount = parseHexToJSON(finalCountBz)
            expect(finalCount.count).to.equal(initialCount.count + 1);
        });

        it("Wasm Precompile Batch Execute", async function () {
            const encoder = new TextEncoder();

            const queryCountMsg = {get_count: {}};
            const queryStr = JSON.stringify(queryCountMsg);
            const queryBz = encoder.encode(queryStr);
            const initialCountBz = await wasmd.query(wasmContractAddress, queryBz);
            const initialCount = parseHexToJSON(initialCountBz)

            const incrementMsg = {increment: {}};
            const incrementStr = JSON.stringify(incrementMsg);
            const incrementBz = encoder.encode(incrementStr);

            const coins = [];
            const coinsStr = JSON.stringify(coins);
            const coinsBz = encoder.encode(coinsStr);

            const executeBatch = [
                {
                    contractAddress: wasmContractAddress,
                    msg: incrementBz,
                    coins: coinsBz,
                },
                {
                    contractAddress: wasmContractAddress,
                    msg: incrementBz,
                    coins: coinsBz,
                },
                {
                    contractAddress: wasmContractAddress,
                    msg: incrementBz,
                    coins: coinsBz,
                },
                {
                    contractAddress: wasmContractAddress,
                    msg: incrementBz,
                    coins: coinsBz,
                },
            ];

            const response = await wasmd.execute_batch(executeBatch);
            const receipt = await response.wait();
            expect(receipt.status).to.equal(1);

            const finalCountBz = await wasmd.query(wasmContractAddress, queryBz);
            const finalCount = parseHexToJSON(finalCountBz)
            expect(finalCount.count).to.equal(initialCount.count + 4);
        });

        it("Wasm Precompile Send Coins", async function () {
            const encoder = new TextEncoder();

            const incrementMsg = {increment: {}};
            const incrementStr = JSON.stringify(incrementMsg);
            const incrementBz = encoder.encode(incrementStr);

            const coins = [
                {
                    denom: denom,
                    amount: "10",
                },
                {
                    denom: "usei",
                    amount: "1000000",
                },
            ];
            const coinsStr = JSON.stringify(coins);
            const coinsBz = encoder.encode(coinsStr);

            const oldBalance = await getSeiBalance(wasmContractAddress);
            const oldTokenBalance = await getSeiBalance(wasmContractAddress, denom);

            const oldUserTokenBalance = await getSeiBalance(accounts[0].seiAddress, denom);

            const response = await wasmd.execute(wasmContractAddress, incrementBz, coinsBz, {value: ethers.parseUnits('1.0', 18)});
            const receipt = await response.wait();
            expect(receipt.status).to.equal(1);

            // usei assertions
            const useiBalance = await getSeiBalance(wasmContractAddress);
            expect(useiBalance).to.equal(oldBalance + 1000000);

            // token assertions
            const contractTokenBalance = await getSeiBalance(wasmContractAddress, denom);
            expect(contractTokenBalance).to.equal(oldTokenBalance + 10);
            const userTokenBalance = await getSeiBalance(accounts[0].seiAddress, denom);
            expect(userTokenBalance).to.equal(oldUserTokenBalance - 10);

        });

        it("Wasm Precompile Batch Execute Send Coins", async function () {
            const encoder = new TextEncoder();

            const incrementMsg = {increment: {}};
            const incrementStr = JSON.stringify(incrementMsg);
            const incrementBz = encoder.encode(incrementStr);

            const coins = [
                {
                    denom: denom,
                    amount: "10",
                },
                {
                    denom: "usei",
                    amount: "1000000",
                },
            ];
            const coinsStr = JSON.stringify(coins);
            const coinsBz = encoder.encode(coinsStr);

            const oldBalance = await getSeiBalance(wasmContractAddress);
            const oldTokenBalance = await getSeiBalance(wasmContractAddress, denom);

            const oldUserTokenBalance = await getSeiBalance(accounts[0].seiAddress, denom);

            const executeBatch = [
                {
                    contractAddress: wasmContractAddress,
                    msg: incrementBz,
                    coins: coinsBz,
                },
                {
                    contractAddress: wasmContractAddress,
                    msg: incrementBz,
                    coins: coinsBz,
                },
                {
                    contractAddress: wasmContractAddress,
                    msg: incrementBz,
                    coins: coinsBz,
                },
                {
                    contractAddress: wasmContractAddress,
                    msg: incrementBz,
                    coins: coinsBz,
                },
            ];

            const response = await wasmd.execute_batch(executeBatch, {value: ethers.parseUnits('4.0', 18)});
            const receipt = await response.wait();
            expect(receipt.status).to.equal(1);

            // usei assertions
            const useiBalance = await getSeiBalance(wasmContractAddress);
            expect(useiBalance).to.equal(oldBalance + 4000000);

            // token assertions
            const contractTokenBalance = await getSeiBalance(wasmContractAddress, denom);
            expect(contractTokenBalance).to.equal(oldTokenBalance + 40);
            const userTokenBalance = await getSeiBalance(accounts[0].seiAddress, denom);
            expect(userTokenBalance).to.equal(oldUserTokenBalance - 40);

        });

    });

});

function parseHexToJSON(hexStr) {
    // Remove the 0x prefix
    hexStr = hexStr.slice(2);
    // Convert to bytes
    const bytes = Buffer.from(hexStr, 'hex');
    // Convert to JSON
    return JSON.parse(bytes.toString());
}