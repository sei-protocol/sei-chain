const { execSync } = require('child_process');
const { expect } = require("chai");
const fs = require('fs');
const path = require('path');

const { expectRevert } = require('@openzeppelin/test-helpers');
const {deployWasm, storeWasm, setupSigners, getAdmin, execute, isDocker, ABI} = require("./lib");


    describe("EVM Precompile Tester", function () {

        let accounts;
        let admin;

        before(async function () {
            accounts = await setupSigners(await hre.ethers.getSigners());
            admin = await getAdmin();
        })

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
                // TODO: Add staking query precompile here
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

        });

    });


function sleep(ms) {
  return new Promise(resolve => setTimeout(resolve, ms));
}

function readDeploymentOutput(fileName) {
    let fileContent;
    try {
        if (fs.existsSync(fileName)) {
            fileContent = fs.readFileSync(fileName, 'utf8').trim();
        } else {
            console.error("File not found:", fileName);
        }
    } catch (error) {
        console.error(`Error reading file: ${error}`);
    }
    return fileContent;
}

function parseHexToJSON(hexStr) {
    // Remove the 0x prefix
    hexStr = hexStr.slice(2);
    // Convert to bytes
    const bytes = Buffer.from(hexStr, 'hex');
    // Convert to JSON
    return JSON.parse(bytes.toString());
}