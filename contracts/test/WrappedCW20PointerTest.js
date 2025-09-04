const {
    setupSigners, deployErc20PointerForCw20, getAdmin, deployWasm, WASM, ABI
} = require("./lib");
const { expect } = require("chai");

describe("Wrapped CW20 Pointer", function () {
    let accounts;
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
    let deployment;

    const balances = {
        admin: 1000000,
        account0: 2000000,
        account1: 3000000
    }

    before(async function () {
        deployment = {}

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
        await tx.wait();

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
