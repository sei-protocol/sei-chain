const {setupSigners, deployErc1155PointerForCw1155, getAdmin, deployWasm,  executeWasm, ABI, WASM} = require("./lib");
const {expect} = require("chai");

describe("ERC1155 to CW1155 Pointer", function () {
    let accounts;
    let pointerAcc0;
    let pointerAcc1;
    let cw1155Address;
    let admin;

    before(async function () {
        accounts = await setupSigners(await hre.ethers.getSigners())
        admin = await getAdmin()

        cw1155Address = await deployWasm(WASM.CW1155, admin.seiAddress, "cw1155", {
            name: "Test",
            symbol: "TEST",
            minter: admin.seiAddress
        })

        await executeWasm(cw1155Address, {mint: {recipient: admin.seiAddress, msg: {token_id: "1", amount: "10", token_uri: "uri1"}}});
        await executeWasm(cw1155Address, {mint: {recipient: accounts[0].seiAddress, msg: {token_id: "1", amount: "11"}}});
        await executeWasm(cw1155Address, {mint: {recipient: accounts[1].seiAddress, msg: {token_id: "2", amount: "12", token_uri: "uri2"}}});
        await executeWasm(cw1155Address, {mint: {recipient: admin.seiAddress, msg: {token_id: "2", amount: "13"}}});
        await executeWasm(cw1155Address, {mint: {recipient: accounts[1].seiAddress, msg: {token_id: "3", amount: "14", token_uri: "uri3"}}});

        const pointerAddr = await deployErc1155PointerForCw1155(hre.ethers.provider, cw1155Address)
        const contract = new hre.ethers.Contract(pointerAddr, ABI.ERC1155, hre.ethers.provider);
        pointerAcc0 = contract.connect(accounts[0].signer)
        pointerAcc1 = contract.connect(accounts[1].signer)
    })

    describe("read", function(){
        it("owner of collection", async function () {
            const owner = await pointerAcc0.owner();
            expect(owner).to.equal(admin.evmAddress);
        });

        it("get name", async function () {
            const name = await pointerAcc0.name();
            expect(name).to.equal("Test");
        });

        it("get symbol", async function () {
            const symbol = await pointerAcc0.symbol();
            expect(symbol).to.equal("TEST");
        });

        it("token uri", async function () {
            const uri = await pointerAcc0.uri(1);
            expect(uri).to.equal("uri1");
        });

        it("balance of", async function () {
            const balance = await pointerAcc0.balanceOf(admin.evmAddress, 1);
            expect(balance).to.equal(10);
        });

        it("balance of batch", async function () {
            const froms = [
                admin.evmAddress,
                admin.evmAddress,
                accounts[0].evmAddress,
                accounts[0].evmAddress,
            ];
            const tids = [1, 2, 1, 2];
            const balances = await pointerAcc0.balanceOfBatch(froms, tids);
            expect(balances.length).to.equal(froms.length);
            expect(balances[0]).to.equal(10);
            expect(balances[1]).to.equal(13);
            expect(balances[2]).to.equal(11);
            expect(balances[3]).to.equal(0);
        });

        it("is approved for all", async function () {
            const approved = await pointerAcc0.isApprovedForAll(admin.evmAddress, admin.evmAddress);
            expect(approved).to.equal(false);
        });
    })

    describe("write", function(){
        it("transfer from", async function () {
            // accounts[0] should transfer token id 1 to accounts[1]
            let balance0 = await pointerAcc0.balanceOf(accounts[0].evmAddress, 1);
            expect(balance0).to.equal(11);
            let balance1 = await pointerAcc0.balanceOf(accounts[1].evmAddress, 1);
            expect(balance1).to.equal(0);
            const transferTxResp = await pointerAcc0.safeTransferFrom(accounts[0].evmAddress, accounts[1].evmAddress, 1, 5, '0x');
            await transferTxResp.wait();
            balance0 = await pointerAcc0.balanceOf(accounts[0].evmAddress, 1);
            expect(balance0).to.equal(6);
            balance1 = await pointerAcc0.balanceOf(accounts[1].evmAddress, 1);
            expect(balance1).to.equal(5);
        });

        it("cannot transfer token you don't own", async function () {
            await expect(pointerAcc0.safeTransferFrom(accounts[0].evmAddress, accounts[1].evmAddress, 3, 1, '0x')).to.be.reverted;
        });

        it("cannot transfer token with insufficient balance", async function () {
            await expect(pointerAcc0.safeTransferFrom(accounts[0].evmAddress, accounts[1].evmAddress, 1, 100, '0x')).to.be.reverted;
        });

        it("batch transfer from", async function () {
            const tids = [3, 2];
            const tamounts = [5, 4];
            let balances = await pointerAcc1.balanceOfBatch(
                [accounts[1].evmAddress, accounts[1].evmAddress, accounts[0].evmAddress, accounts[0].evmAddress],
                [...tids, ...tids]
            );
            expect(balances[0]).to.equal(14);
            expect(balances[1]).to.equal(12);
            expect(balances[2]).to.equal(0);
            expect(balances[3]).to.equal(0);
            const transferTxResp = await pointerAcc1.safeBatchTransferFrom(
                accounts[1].evmAddress,
                accounts[0].evmAddress,
                tids,
                tamounts,
                '0x'
            );
            await transferTxResp.wait();
            balances = await pointerAcc1.balanceOfBatch(
                [accounts[1].evmAddress, accounts[1].evmAddress, accounts[0].evmAddress, accounts[0].evmAddress],
                [...tids, ...tids]
            );
            expect(balances[0]).to.equal(9);
            expect(balances[1]).to.equal(8);
            expect(balances[2]).to.equal(5);
            expect(balances[3]).to.equal(4);
        });

        it("set approval for all", async function () {
            const setApprovalForAllTxResp = await pointerAcc0.setApprovalForAll(accounts[1].evmAddress, true);
            await setApprovalForAllTxResp.wait();
            const approved = await pointerAcc0.isApprovedForAll(accounts[0].evmAddress, accounts[1].evmAddress);
            expect(approved).to.equal(true);

            // test revoking approval
            await mine(pointerAcc0.setApprovalForAll(accounts[1].evmAddress, false));
            const approvedAfter = await pointerAcc0.isApprovedForAll(accounts[0].evmAddress, accounts[1].evmAddress);
            expect(approvedAfter).to.equal(false);
        });
    })
})

async function mine(action) {
    await (await action).wait()
}
