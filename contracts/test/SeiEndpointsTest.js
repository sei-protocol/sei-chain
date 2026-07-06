const { expect } = require("chai");
const { ethers } = require('hardhat');
const { deployWasm, ABI, WASM, executeWasm, deployErc20PointerForCw20, getAdmin, setupSigners } = require("./lib")

describe("Sei Endpoints Tester", function () {
    let accounts;
    let admin;
    let cw20Address;
    let pointer;

    before(async function () {
        accounts = await setupSigners(await hre.ethers.getSigners());
        admin = await getAdmin();

        cw20Address = await deployWasm(WASM.CW20, accounts[0].seiAddress, "cw20", {
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
        // deploy a pointer
        const pointerAddr = await deployErc20PointerForCw20(hre.ethers.provider, cw20Address);
        const contract = new hre.ethers.Contract(pointerAddr, ABI.ERC20, hre.ethers.provider);
        pointer = contract.connect(accounts[0].signer);
    });

    it("Should emit a synthetic event upon transfer", async function () {
        const res = await executeWasm(cw20Address,  { transfer: { recipient: accounts[1].seiAddress, amount: "1" } });
        const blockNumber = parseInt(res["height"], 10);
        // look for synthetic event on evm sei_ endpoints
        const filter = {
            fromBlock: '0x' + blockNumber.toString(16),
            toBlock: '0x' + blockNumber.toString(16),
            address: pointer.address,
            topics: [ethers.id("Transfer(address,address,uint256)")]
        };
        const seilogs = await ethers.provider.send('sei_getLogs', [filter]);
        expect(seilogs.length).to.equal(1);
    });

    it("sei_getBlockByNumberExcludeTraceFail should not have the synthetic tx", async function () {
        // create a synthetic tx
        const res = await executeWasm(cw20Address,  { transfer: { recipient: accounts[1].seiAddress, amount: "1" } });
        const blockNumber = parseInt(res["height"], 10);

        // Query sei_getBlockByNumber - should have synthetic tx
        const seiBlock = await ethers.provider.send('sei_getBlockByNumber', ['0x' + blockNumber.toString(16), false]);
        expect(seiBlock.transactions.length).to.equal(1);

        // Query sei_getBlockByNumberExcludeTraceFail - should not have synthetic tx
        const seiBlockExcludeTrace = await ethers.provider.send('sei_getBlockByNumberExcludeTraceFail', ['0x' + blockNumber.toString(16), false]);
        expect(seiBlockExcludeTrace.transactions.length).to.equal(0);
    });

})
