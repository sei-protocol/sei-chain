const {getAdmin, queryWasm, executeWasm, associateWasm, deployEvmContract, setupSigners, deployErc20PointerForCw20, deployWasm, WASM,
    registerPointerForCw20
} = require("./lib")
const { expect } = require("chai");

describe("CW20 to ERC20 Pointer", function () {
    let accounts;
    let testToken;
    let cw20Pointer;
    let admin;

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
        await setBalance(admin.evmAddress, 1000000000000)

        cw20Pointer = await registerPointerForCw20(tokenAddr)
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

    describe("validation", function(){
        it("should not allow a pointer to the pointer", async function(){
            try {
                await deployErc20PointerForCw20(hre.ethers.provider, cw20Pointer, 5)
                expect.fail(`Expected to be prevented from creating a pointer`);
            } catch(e){
                expect(e.message).to.include("contract deployment failed");
            }
        })
    })

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
        it("should transfer token", async function() {
            const respBefore = await queryWasm(cw20Pointer, "balance", {address: accounts[1].seiAddress})
            const balanceBefore = respBefore.data.balance;

            await executeWasm(cw20Pointer,  { transfer: { recipient: accounts[1].seiAddress, amount: "100" } });
            const respAfter = await queryWasm(cw20Pointer, "balance", {address: accounts[1].seiAddress})
            const balanceAfter = respAfter.data.balance;

            expect(balanceAfter).to.equal((parseInt(balanceBefore) + 100).toString())
        });

        it("transfer to unassociated address should fail", async function() {
            const unassociatedSeiAddr = "sei1z7qugn2xy4ww0c9nsccftxw592n4xhxccmcf4q";
            const respBefore = await queryWasm(cw20Pointer, "balance", {address: accounts[1].seiAddress})
            const balanceBefore = respBefore.data.balance;

            await executeWasm(cw20Pointer,  { transfer: { recipient: unassociatedSeiAddr, amount: "100" } });
            const respAfter = await queryWasm(cw20Pointer, "balance", {address: accounts[1].seiAddress})
            const balanceAfter = respAfter.data.balance;

            expect(balanceAfter).to.equal((parseInt(balanceBefore)).toString())
        });

        it("transfer to contract address should succeed", async function() {
            await associateWasm(cw20Pointer);
            const respBefore = await queryWasm(cw20Pointer, "balance", {address: admin.seiAddress})
            const balanceBefore = respBefore.data.balance;

            await executeWasm(cw20Pointer,  { transfer: { recipient: cw20Pointer, amount: "100" } });
            const respAfter = await queryWasm(cw20Pointer, "balance", {address: admin.seiAddress})
            const balanceAfter = respAfter.data.balance;

            expect(balanceAfter).to.equal((parseInt(balanceBefore) - 100).toString())
        })

        it("should increase and decrease allowance for a spender", async function() {
            const spender = accounts[0].seiAddress
            await executeWasm(cw20Pointer, { increase_allowance: { spender: spender, amount: "300" } });

            let allowance = await queryWasm(cw20Pointer, "allowance", { owner: admin.seiAddress, spender: spender });
            expect(allowance.data.allowance).to.equal("300");
        
            await executeWasm(cw20Pointer, { decrease_allowance: { spender: spender, amount: "300" } });

            allowance = await queryWasm(cw20Pointer, "allowance", { owner: admin.seiAddress, spender: spender });
            expect(allowance.data.allowance).to.equal("0");
        });

        it("should transfer token using transferFrom", async function() {
            const resp = await testToken.approve(admin.evmAddress, 100);
            await resp.wait();
            const respBefore = await queryWasm(cw20Pointer, "balance", {address: accounts[0].seiAddress});
            const balanceBefore = respBefore.data.balance;
            await executeWasm(cw20Pointer,  { transfer_from: { owner: accounts[0].seiAddress, recipient: accounts[1].seiAddress, amount: "100" } });
            const respAfter = await queryWasm(cw20Pointer, "balance", {address: accounts[0].seiAddress});
            const balanceAfter = respAfter.data.balance;
            expect(balanceAfter).to.equal((parseInt(balanceBefore) - 100).toString())
        });
    })
})
