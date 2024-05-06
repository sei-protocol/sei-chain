const {fundAddress, storeWasm, instantiateWasm, execute, getSeiAddress, getAdmin, queryWasm, executeWasm, executeWasmWithSigner, deployEvmContract, setupSigners,
    getEvmAddress, addKey
} = require("./lib")
const { expect } = require("chai");

const CW20_POINTER_WASM = "../example/cosmwasm/cw20/artifacts/cwerc20.wasm";
const CW20_RECEIVER_WASM = "../example/cosmwasm/cw20Escrow/artifacts/cw20_escrow.wasm";
describe("CW20 to ERC20 Pointer", function () {
    let accounts;
    let testToken;
    let cw20Pointer;
    let cw20Receiver;
    let admin;
    let admin2;

    async function setBalance(addr, balance) {
        const resp = await testToken.setBalance(addr, balance)
        await resp.wait()
    }

    before(async function () {
        accounts = await setupSigners(await hre.ethers.getSigners())

        // deploy TestToken
        testToken = await deployEvmContract("TestToken", ["TEST", "TEST"])
        const tokenAddr = await testToken.getAddress()
        await setBalance(accounts[0].evmAddress, 1000000000000)
        await setBalance(accounts[1].evmAddress, 1000000000000)

        // give admin balance
        admin = await getAdmin()
        admin2 = await addKey("admin2")

        await setBalance(admin.evmAddress, 1000000000000)

        const codeId = await storeWasm(CW20_POINTER_WASM)
        const codeIdReceiver = await storeWasm(CW20_RECEIVER_WASM)
        cw20Pointer = await instantiateWasm(codeId, admin.seiAddress, "cw20-erc20", {erc20_address: tokenAddr })
        cw20Receiver = await instantiateWasm(codeIdReceiver, admin.seiAddress, "cw20Escrow", { } )
    })

    async function assertUnsupported(addr, operation, args) {
        try {
            await queryWasm(addr, operation, args);
            // If the promise resolves, force the test to fail
            expect.fail(`Expected rejection: address=${addr} operation=${operation} args=${JSON.stringify(args)}`);
        } catch (error) {
            expect(error.message).to.include("ERC20 does not support");
        }
    }

    describe("query", function(){
        it("should return token_info", async function(){
            const result = await queryWasm(cw20Pointer, "token_info", {})
            expect(result).to.deep.equal({data:{name:"TEST",symbol:"TEST",decimals:18,total_supply:"3000000000000"}})
        })

        it("should return balance", async function(){
            const result = await queryWasm(cw20Pointer, "balance", {address: accounts[0].seiAddress})
            expect(result).to.deep.equal({ data: { balance: '1000000000000' } })
        })

        it("should return allowance", async function(){
            const result = await queryWasm(cw20Pointer, "allowance", {owner: accounts[0].seiAddress, spender: accounts[0].seiAddress})
            expect(result).to.deep.equal({ data: { allowance: '0', expires: { never: {} } } })
        })

        it("should throw exception on unsupported endpoints", async function() {
            await assertUnsupported(cw20Pointer, "minter", {})
            await assertUnsupported(cw20Pointer, "marketing_info", {})
            await assertUnsupported(cw20Pointer, "download_logo", {})
            await assertUnsupported(cw20Pointer, "all_allowances", { owner: accounts[0].seiAddress })
            await assertUnsupported(cw20Pointer, "all_accounts", {})
        });
    })


    describe("execute", function() {
        it("should transfer token using transfer", async function() {
            const respBefore = await queryWasm(cw20Pointer, "balance", {address: accounts[1].seiAddress})
            const balanceBefore = respBefore.data.balance;

            await executeWasm(cw20Pointer,  { transfer: { recipient: accounts[1].seiAddress, amount: "100" } });
            const respAfter = await queryWasm(cw20Pointer, "balance", {address: accounts[1].seiAddress})
            const balanceAfter = respAfter.data.balance;

            expect(balanceAfter).to.equal((parseInt(balanceBefore) + 100).toString())
        });

        // Having issues testing `send`
        it.only("should transfer token using send", async function() {
            const respBefore = await queryWasm(cw20Pointer, "balance", {address: accounts[1].seiAddress})
            const balanceBefore = respBefore.data.balance;

            const res = await executeWasm(cw20Pointer,  { send: { contract: cw20Receiver, amount: "100", msg: "msg" } });
            console.log("send res = ", res)

            const respAfter = await queryWasm(cw20Pointer, "balance", {address: accounts[1].seiAddress})
            const balanceAfter = respAfter.data.balance;

            console.log("balanceAfter", balanceAfter)

            expect(balanceAfter).to.equal((parseInt(balanceBefore) + 100).toString())
        });

        // TODO: other execute methods
        //  - transfer, send, transferFrom, sendFrom
        // TODO: unhappy paths
        //  - transfer more than balance should fail
        //  - transferFrom without allowance should fail
        //  - send unhappy paths

        it("should increase and decrease allowance for a spender", async function() {
            const spender = accounts[1].seiAddress
            await executeWasm(cw20Pointer, { increase_allowance: { spender: spender, amount: "300" } });

            let allowance = await queryWasm(cw20Pointer, "allowance", { owner: admin.seiAddress, spender: spender });
            expect(allowance.data.allowance).to.equal("300");
        
            await executeWasm(cw20Pointer, { decrease_allowance: { spender: spender, amount: "300" } });

            allowance = await queryWasm(cw20Pointer, "allowance", { owner: admin.seiAddress, spender: spender });
            expect(allowance.data.allowance).to.equal("0");
        });

        it("should transfer token using transferFrom", async function() {
            // TODO: Do simplified version where approvals are granted from accounts[0] to admin evm address and then admin can do a transferFrom.
            await executeWasm(cw20Pointer, { increase_allowance: { spender: admin2.seiAddress, amount: "300" } });
            let allowance = await queryWasm(cw20Pointer, "allowance", { owner: admin.seiAddress, spender: admin2.seiAddress });
            expect(allowance.data.allowance).to.equal("300");

            const respBefore = await queryWasm(cw20Pointer, "balance", {address: accounts[1].seiAddress});
            const balanceBefore = respBefore.data.balance;
            await executeWasmWithSigner(cw20Pointer,  { transfer_from: { owner: admin.seiAddress, recipient: accounts[1].seiAddress, amount: "100" } }, "admin2");
            const respAfter = await queryWasm(cw20Pointer, "balance", {address: accounts[1].seiAddress});
            const balanceAfter = respAfter.data.balance;
            expect(balanceAfter).to.equal((parseInt(balanceBefore) + 100).toString())
        });
    })


})