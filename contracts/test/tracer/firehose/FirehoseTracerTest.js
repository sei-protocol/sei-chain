const {
    setupSigners, deployErc20PointerForCw20, getAdmin, execCommand, deployEvmContract, executeWasm, deployWasm, WASM, ABI, registerPointerForERC20, testAPIEnabled,
    incrementPointerVersion,
    isDocker
} = require("../../lib");
const { expect } = require("chai");
const BigNumber = require('bignumber.js');

/**
 * @typedef SeiDuplexAccount
 * @property {string} evmAddress
 * @property {string} seiAddress
 */

/**
 * @typedef ResuableContractDeployment
 * @property {SeiDuplexAccount} [admin]
 * @property {string} [cw20Address]
 * @property {string} [cw20EvmPointerAddress]
 * @property {string} [wrappedCw20EvmPointerAddress]
 */
describe("Firehose tracer", function () {
    /** @type {SeiDuplexAccount[]} */
    let accounts;
    /** @type {SeiDuplexAccount} */
    let admin;
    let cw20Address;
    let cw20EvmPointerAddress;
    let cw20EvmPointerContract;

    // We have a link ERC20 -> CW20, this contract wraps the ERC20 to add an extra log when doing
    // either a transfer or a transferFrom. An approval is required before transferFrom can be done
    // as ERC20 rules.
    //
    // This extra log tests the case where a DeliverTxHook adds logs to a transaction that already contains
    // log.
    let wrappedCw20EvmPointerAddress;
    let wrappedCw20EvmPointerContract;

    /** @type {ResuableContractDeployment} */
    let deployment

    const balances = {
        admin: 1000000,
        account0: 2000000,
        account1: 3000000
    }

    before(async function () {
        deployment = JSON.parse(process.env['SEI_TEST_REUSABLE_CONTRACTS'] ?? '{}')

        accounts = await setupSigners(await hre.ethers.getSigners());

        if (deployment.admin) {
            admin = deployment.admin;
        } else {
            admin = deployment.admin = await getAdmin();
        }

        if (deployment.cw20Address) {
            cw20Address = deployment.cw20Address;
        } else {
            cw20Address = deployment.cw20Address = await deployWasm(WASM.CW20, accounts[0].seiAddress, "cw20", {
                name: "Test",
                symbol: "TEST",
                decimals: 6,
                initial_balances: [
                    { address: admin.seiAddress, amount: "1000000" },
                    { address: accounts[0].seiAddress, amount: "2000000" },
                    { address: accounts[1].seiAddress, amount: "3000000" }
                ],
                mint: {
                    "minter": admin.seiAddress, "cap": "99900000000"
                }
            });
        }

        if (deployment.cw20EvmPointerAddress) {
            cw20EvmPointerAddress = deployment.cw20EvmPointerAddress;
        } else {
            cw20EvmPointerAddress = deployment.cw20EvmPointerAddress = await deployErc20PointerForCw20(hre.ethers.provider, cw20Address, 5);
        }

        contract = new hre.ethers.Contract(cw20EvmPointerAddress, ABI.ERC20, hre.ethers.provider);
        cw20EvmPointerContract = await contract.connect(accounts[0].signer);

        if (deployment.wrappedCw20EvmPointerAddress) {
            wrappedCw20EvmPointerAddress = deployment.wrappedCw20EvmPointerAddress;
            contract = new hre.ethers.Contract(wrappedCw20EvmPointerAddress, ABI.ERC20, hre.ethers.provider);
            wrappedCw20EvmPointerContract = await contract.connect(accounts[0].signer);
        } else {
            contract = await ethers.getContractFactory("ERC20PreTransferFromWrapper")
            wrappedCw20EvmPointerContract = await contract.deploy(await cw20EvmPointerAddress, { gasPrice: ethers.parseUnits('100', 'gwei') });
            await wrappedCw20EvmPointerContract.waitForDeployment();
            wrappedCw20EvmPointerAddress = deployment.wrappedCw20EvmPointerAddress = await wrappedCw20EvmPointerContract.getAddress();
        }

        if ((process.env.SEI_TEST_REUSABLE_CONTRACTS || '').match(/^\?$/)) {
            const asJson = JSON.stringify(deployment)
            console.log(`export SEI_TEST_REUSABLE_CONTRACTS='${asJson}'`);
        }
    });

    it("EVM calls WasmPrecompile which performs ERC20 transfer", async function () {
        const testToken = await deployEvmContract("TestToken", ["TEST", "TEST"]);
        const tokenAddr = await testToken.getAddress();

        const resp = await testToken.setBalance(admin.evmAddress, 1000000000000);
        await resp.wait();

        const cw20Pointer = await registerPointerForERC20(tokenAddr);

        const WasmPrecompileContract = '0x0000000000000000000000000000000000001002';
        const contractABIPath = '../../../../precompiles/wasmd/abi.json';
        const contractABI = require(contractABIPath);
        const wasmd = new ethers.Contract(WasmPrecompileContract, contractABI, accounts[0].signer);

        const encoder = new TextEncoder();

        const transferMsg = { transfer: { recipient: accounts[1].seiAddress, amount: "100" } };
        const transferStr = JSON.stringify(transferMsg);
        const transferBz = encoder.encode(transferStr);

        const coins = [];
        const coinsStr = JSON.stringify(coins);
        const coinsBz = encoder.encode(coinsStr);

        const response = await wasmd.execute(cw20Pointer, transferBz, coinsBz);
        const receipt = await response.wait();
        expect(receipt.status).to.equal(1);

        const block = await getFirehoseBlock(receipt.blockNumber);
        const txHashLookedFor = receipt.hash.replace("0x", "");
        const trace = block.transaction_traces.find((trace) => trace.hash === txHashLookedFor);

        const logs = collectTrxCallsLogs(trace)
        const receiptLogs = trace.receipt.logs
        expect(logs.length).to.equal(1)
        expect(logs.length, "Transaction calls logs and receipt logs count mismatch").to.equal(receiptLogs.length)

        assertTrxOrdinals(trace, receipt.blockNumber)
    });

    it("CW20 transfer performed through ERC20 pointer contract", async function () {
        let sender = accounts[0];
        let recipient = accounts[1];

        expect(await cw20EvmPointerContract.balanceOf(sender.evmAddress)).to.equal(
            balances.account0
        );
        expect(await cw20EvmPointerContract.balanceOf(recipient.evmAddress)).to.equal(
            balances.account1
        );

        let tx = await cw20EvmPointerContract.approve(wrappedCw20EvmPointerAddress, 1);
        let receipt = await tx.wait();

        const startBlockNumber = await ethers.provider.getBlockNumber();

        tx = await wrappedCw20EvmPointerContract.transferFrom(sender.evmAddress, recipient.evmAddress, 1, { gasPrice: ethers.parseUnits('100', 'gwei') });
        receipt = await tx.wait();

        // We can cleanup right now, we just check the logs
        const cleanupTx = await cw20EvmPointerContract
            .connect(recipient.signer)
            .transfer(sender.evmAddress, 1);
        await cleanupTx.wait();

        const block = await getFirehoseBlock(receipt.blockNumber);

        const txHashLookedFor = receipt.hash.replace("0x", "");
        const trace = block.transaction_traces.find((trace) => trace.hash === txHashLookedFor);

        const logs = collectTrxCallsLogs(trace)
        const receiptLogs = trace.receipt.logs
        expect(logs.length).to.equal(2)
        expect(logs.length, "Transaction calls logs and receipt logs count mismatch").to.equal(receiptLogs.length)

        assertTrxOrdinals(trace, receipt.blockNumber)
    });

    it("CW20 transfer on CW contract when ERC20 pointer exists leads to added logs on non-EVM transaction", async function () {
        // *Important* The "amount" must be in string otherwise the contract execution fails!
        const output = await executeWasm(cw20Address, { transfer: { recipient: cw20Address, amount: "100" } })
        if (output.code !== 0) {
            throw new Error(`Failed transaction\n${JSON.stringify(output, null, 2)}`)
        }

        // TODO: Perform cleanup by returning the fund!

        const blockNumber = output.height
        const txHashLookedFor = evmHash(output.txhash)

        const firehoseBlock = await getFirehoseBlock(blockNumber)
        const trace = firehoseBlock.transaction_traces.find((trace) => trace.hash === txHashLookedFor);
        expect(trace.from).to.equal(evmAddr(admin.evmAddress))
        expect(trace.to).to.equal(evmAddr(cw20EvmPointerAddress))

        expect(trace.calls).to.be.lengthOf(1)

        const call = trace.calls[0]
        expect(call.caller).to.equal(evmAddr(admin.evmAddress))
        expect(call.address).to.equal(evmAddr(cw20EvmPointerAddress))

        const logs = collectTrxCallsLogs(trace)
        const receiptLogs = trace.receipt.logs
        expect(logs.length).to.equal(1)
        expect(logs.length, "Transaction calls logs and receipt logs count mismatch").to.equal(receiptLogs.length)

        expect(logs[0].address).to.equal(evmAddr(cw20EvmPointerAddress))

        assertTrxOrdinals(trace, blockNumber)
    });

    it("CW20 transfer performed through ERC20 pointer contract, multiple trx bridged per block", async function () {
        let sender = accounts[0];
        let recipient = accounts[1];

        expect(await cw20EvmPointerContract.balanceOf(sender.evmAddress)).to.equal(
            balances.account0
        );
        expect(await cw20EvmPointerContract.balanceOf(recipient.evmAddress)).to.equal(
            balances.account1
        );

        const txCount = 15;

        const tx = await cw20EvmPointerContract.approve(wrappedCw20EvmPointerAddress, txCount);
        const receipt = await tx.wait();

        const nonce = await ethers.provider.getTransactionCount(
            sender.evmAddress
        );

        const transfer = async (index) => {
            let tx
            try {
                tx = await wrappedCw20EvmPointerContract.transferFrom(sender.evmAddress, recipient.evmAddress, 1, {
                    nonce: nonce + (index - 1),
                    gasPrice: ethers.parseUnits('100', 'gwei')
                });
            } catch (error) {
                console.log(`Transfer ${index} send transaction failed`, error);
                throw error;
            }

            let receipt
            try {
                receipt = await tx.wait();
            } catch (error) {
                console.log(`Transfer ${index} send transaction failed`, error);
                throw error;
            }
        };

        let promises = [];
        for (let i = 1; i <= txCount; i++) {
            promises.push(transfer(i));
        }

        const blockNumber = await ethers.provider.getBlockNumber();

        await Promise.all(promises);

        expect(await cw20EvmPointerContract.balanceOf(sender.evmAddress)).to.equal(
            balances.account0 - txCount
        );
        expect(await cw20EvmPointerContract.balanceOf(recipient.evmAddress)).to.equal(
            balances.account1 + txCount
        );

        // check logs
        const filter = {
            fromBlock: blockNumber,
            toBlock: "latest",
            address: await cw20EvmPointerContract.getAddress(),
            topics: [ethers.id("Transfer(address,address,uint256)")],
        };

        const logs = await ethers.provider.getLogs(filter);
        expect(logs.length).to.equal(txCount);

        /** @type Record<number, Record<string, []unknown>> */
        const byBlockThenTx = {};
        logs.forEach((log) => {
            if (!byBlockThenTx[log.blockNumber]) {
                byBlockThenTx[log.blockNumber] = {};
            }

            if (!byBlockThenTx[log.blockNumber][log.transactionHash]) {
                byBlockThenTx[log.blockNumber][log.transactionHash] = [];
            }

            byBlockThenTx[log.blockNumber][log.transactionHash].push(log);
        });

        // Sanity check to ensure we were able to generate a block with multiple logs
        expect(
            Object.entries(byBlockThenTx).some(
                ([blockNumber, byTx]) => {
                    const logCountInBlock = Object.values(byTx).reduce((logCount, logsInTx) => logCount + logsInTx.length, 0)

                    return logCountInBlock > 1
                }
            )
        ).to.be.true;

        Object.entries(byBlockThenTx).forEach(
            ([blockNumber, byTx]) => {
                Object.entries(byTx).forEach(
                    ([txHash, logs]) => {
                        const logIndexes = {}
                        logs.forEach((log, index) => {
                            expect(logIndexes[log.index], `all log indexes in block tx ${txHash} (at block #${blockNumber}) should be unique but log's Index value ${log.index} for log at position ${index} has already been seen`).to.be.undefined;
                            logIndexes[log.index] = index
                        })
                    }
                )
            }
        )

        const cleanupTx = await cw20EvmPointerContract
            .connect(recipient.signer)
            .transfer(sender.evmAddress, txCount);
        await cleanupTx.wait();
    });
});

function evmAddr(input) {
    return input.toLowerCase().replace("0x", "")
}

function evmHash(input) {
    return input.toLowerCase().replace("0x", "")
}

function assertTrxOrdinals(trace, blockNumber) {
    const ordinals = Object.entries(collectTrxOrdinals(trace))
    ordinals.sort(([a], [b]) => parseInt(a) - parseInt(b))

    expect(ordinals.length).to.be.greaterThan(0);

    let previous = undefined;
    ordinals.forEach(([ordinal, count]) => {
        expect(count, `Ordinal ${ordinal} has been seen ${count} times throughout transaction ${trace.hash} at block ${blockNumber}, that is invalid`).to.equal(1);

        if (previous) {
            expect(previous + 1, `Ordinal ${ordinal} should have strictly follow ${previous}, so that ${previous} + 1 == ${ordinal} which was not the case here`).to.be.equal(parseInt(ordinal));
        }

        previous = parseInt(ordinal);
    });
}

function collectTrxCallsLogs(firehoseTrx) {
    const logs = [];
    firehoseTrx.calls.forEach((call) => {
        (call.logs || []).forEach((log) => {
            logs.push(log)
        })
    })

    return logs
}

function collectTrxOrdinals(firehoseTrx) {
    /** @type { Record<number, number> } */
    const ordinals = {};

    ordinals[firehoseTrx.begin_ordinal || 0] = (ordinals[firehoseTrx.begin_ordinal || 0] || 0) + 1
    ordinals[firehoseTrx.end_ordinal || 0] = (ordinals[firehoseTrx.end_ordinal || 0] || 0) + 1

    firehoseTrx.calls.forEach((call) => {
        collectCallOrdinals(ordinals, call)
    })

    return ordinals
}

/** @type { (ordinals: Record<number, number>, call: unknown) } */
function collectCallOrdinals(ordinals, call) {
    ordinals[call.begin_ordinal || 0] = (ordinals[call.begin_ordinal || 0] || 0) + 1
    ordinals[call.end_ordinal || 0] = (ordinals[call.end_ordinal || 0] || 0) + 1

    collectChangesOrdinals(ordinals, call.logs || [])
    collectChangesOrdinals(ordinals, call.account_creations || [])
    collectChangesOrdinals(ordinals, call.balance_changes || [])
    collectChangesOrdinals(ordinals, call.gas_changes || [])
    collectChangesOrdinals(ordinals, call.nonce_changes || [])
    collectChangesOrdinals(ordinals, call.storage_changes || [])
    collectChangesOrdinals(ordinals, call.code_changes || [])
}

function collectChangesOrdinals(ordinals, changes) {
    changes.forEach((change) => {
        ordinals[change.ordinal || 0] = (ordinals[change.ordinal || 0] || 0) + 1
    })
}

async function getFirehoseBlock(blockNumber) {
    if (await isDocker()) {
        throw new Error("FirehoseTracerTest can only be run outside of docker for now");
    }

    const command = `fireeth tools firehose-client -p localhost:8089 ${blockNumber}:+0`
    const output = await execCommand(command, { errorOnAnyStderrContent: false });
    return JSON.parse(output).block;
}
