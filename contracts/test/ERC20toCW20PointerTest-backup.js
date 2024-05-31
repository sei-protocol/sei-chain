const {setupSigners, deployErc20PointerForCw20, getAdmin, deployWasm, WASM, ABI, registerPointerForCw20,
    testAPIEnabled,
    incrementPointerVersion
} = require("./lib");
const {expect} = require("chai");

describe("ERC20 to CW20 Pointer", function () {
    let accounts;
    let pointer;
    let cw20Address;
    let admin;

    before(async function () {
        accounts = await setupSigners(await hre.ethers.getSigners())
        admin = await getAdmin()

        cw20Address = await deployWasm(WASM.CW20, accounts[0].seiAddress, "cw20", {
            name: "Test",
            symbol: "TEST",
            decimals: 6,
            initial_balances: [
                { address: admin.seiAddress, amount: "1000000" },
                { address: accounts[0].seiAddress, amount: "2000000"},
                { address: accounts[1].seiAddress, amount: "3000000"}
            ],
            mint: {
                "minter": admin.seiAddress, "cap": "99900000000"
            }
        })

        // deploy TestToken
        const pointerAddr = await deployErc20PointerForCw20(hre.ethers.provider, cw20Address)
        const contract = new hre.ethers.Contract(pointerAddr, ABI.ERC20, hre.ethers.provider);
        pointer = contract.connect(accounts[0].signer)
    })

    describe("validation", function(){
        it("should not allow a pointer to the pointer", async function(){
            try {
                await registerPointerForCw20(await pointer.getAddress())
                expect.fail(`Expected to be prevented from creating a pointer`);
            } catch(e){
                expect(e.message).to.include("contract deployment failed");
            }
        })
    })

    describe("read", function(){
        it("get name", async function () {
            const name = await pointer.name();
            expect(name).to.equal("Test");
        });

        it("get symbol", async function () {
            const symbol = await pointer.symbol();
            expect(symbol).to.equal("TEST");
        });

        it("get decimals", async function () {
            const decimals = await pointer.decimals();
            expect(Number(decimals)).to.equal(6);
        });

        it("get balanceOf", async function () {
            expect(await pointer.balanceOf(admin.evmAddress)).to.equal(1000000)
            expect(await pointer.balanceOf(accounts[0].evmAddress)).to.equal(2000000);
            expect(await pointer.balanceOf(accounts[1].evmAddress)).to.equal(3000000);
        });

        it("get totalSupply", async function () {
            expect(await pointer.totalSupply()).to.equal(6000000);
        });

        it("get allowance", async function () {
            expect(await pointer.allowance(accounts[0].evmAddress, accounts[1].evmAddress)).to.equal(0);
        });
    })

    describe("transfer()", function () {
        it("should transfer", async function () {
            let sender = accounts[0];
            let recipient = accounts[1];

            expect(await pointer.balanceOf(sender.evmAddress)).to.equal(2000000);
            expect(await pointer.balanceOf(recipient.evmAddress)).to.equal(3000000);

            const tx = await pointer.transfer(recipient.evmAddress, 1);
            await tx.wait();

            expect(await pointer.balanceOf(sender.evmAddress)).to.equal(1999999);
            expect(await pointer.balanceOf(recipient.evmAddress)).to.equal(3000001);

            const cleanupTx = await pointer.connect(recipient.signer).transfer(sender.evmAddress, 1)
            await cleanupTx.wait();
        });

        it("should fail transfer() if sender has insufficient balance", async function () {
            const recipient = accounts[1];
            await expect(pointer.transfer(recipient.evmAddress, 20000000)).to.be.revertedWith("CosmWasm execute failed");
        });

        it("transfer to unassociated address should succeed", async function() {
            const unassociatedRecipient = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266";
            const tx = await pointer.transfer(unassociatedRecipient, 1);
            await tx.wait();
        });

        it("transfer to contract address should succeed", async function() {
            const contract = await pointer.getAddress();
            const tx = await pointer.transfer(contract, 1);
            await tx.wait();
        });
    });

    describe("approve()", function () {
        it("should approve", async function () {
            const owner = accounts[0].evmAddress;
            const spender = accounts[1].evmAddress;
            const tx = await pointer.approve(spender, 1000000);
            await tx.wait();
            const allowance = await pointer.allowance(owner, spender);
            expect(Number(allowance)).to.equal(1000000);
        });

        it("should lower approval", async function () {
            const owner = accounts[0].evmAddress;
            const spender = accounts[1].evmAddress;
            const tx = await pointer.approve(spender, 0);
            await tx.wait();
            const allowance = await pointer.allowance(owner, spender);
            expect(Number(allowance)).to.equal(0);
        });
    });


    describe("transferFrom()", function () {
        it("should transferFrom", async function () {
            const recipient = admin;
            const owner = accounts[0];
            const spender = accounts[1];
            const amountToTransfer = 10;

            // capture balances before
            const recipientBalanceBefore = await pointer.balanceOf(recipient.evmAddress);
            const ownerBalanceBefore = await pointer.balanceOf(owner.evmAddress);
            expect(Number(ownerBalanceBefore)).to.be.greaterThanOrEqual(amountToTransfer);

            // approve the amount
            const tx = await pointer.approve(spender.evmAddress, amountToTransfer);
            await tx.wait();
            const allowanceBefore = await pointer.allowance(owner.evmAddress, spender.evmAddress);
            expect(Number(allowanceBefore)).to.be.greaterThanOrEqual(amountToTransfer);

            // transfer
            const tfTx = await pointer.connect(spender.signer).transferFrom(owner.evmAddress, recipient.evmAddress, amountToTransfer);
            const receipt = await tfTx.wait();

            // capture balances after
            const recipientBalanceAfter = await pointer.balanceOf(recipient.evmAddress);
            const ownerBalanceAfter = await pointer.balanceOf(owner.evmAddress);

            // check balance diff to ensure transfer went through
            const diff = recipientBalanceAfter - recipientBalanceBefore;
            expect(diff).to.equal(amountToTransfer);

            // check balanceOf sender (deployerAddr) to ensure it went down
            const diff2 = ownerBalanceBefore - ownerBalanceAfter;
            expect(diff2).to.equal(amountToTransfer);

            // check that allowance has gone down by amountToTransfer
            const allowanceAfter = await pointer.allowance(owner.evmAddress, spender.evmAddress);
            expect(Number(allowanceBefore) - Number(allowanceAfter)).to.equal(amountToTransfer);
        });

        it("should fail transferFrom() if sender has insufficient balance", async function () {
            const recipient = admin;
            const owner = accounts[0];
            const spender = accounts[1];

            const tx = await pointer.approve(spender.evmAddress, 999999999);
            await tx.wait();

            await expect(pointer.connect(spender.signer).transferFrom(owner.evmAddress, recipient.evmAddress, 999999999)).to.be.revertedWith("CosmWasm execute failed");
        });

        it("should fail transferFrom() if allowance is too low", async function () {
            const recipient = admin;
            const owner = accounts[0];
            const spender = accounts[1];

            const tx = await pointer.approve(spender.evmAddress, 10);
            await tx.wait();

            await expect(pointer.connect(spender.signer).transferFrom(owner.evmAddress, recipient.evmAddress, 20)).to.be.revertedWith("CosmWasm execute failed");

            await (await pointer.approve(spender.evmAddress, 0)).wait()
        });
    });

    describe("upgrade behavior", function () {

        before(async function(){
            const enabled = await testAPIEnabled(ethers.provider)
            if(!enabled) {
                this.skip()
            }
        })

        it("upgrade without version update not allowed", async function () {
            try {
                await deployErc20PointerForCw20(hre.ethers.provider, cw20Address)
                expect.fail("Expected to be prevented from creating a pointer");
            } catch (e) {
                expect(e.message).to.equal("contract deployment failed");
            }
        })

        describe("with upgrade", function(){
            let newPointer;

            before(async function(){
                await incrementPointerVersion(ethers.provider, "cw20", 1)

                // deploy a new pointer, now that the version has been incremented
                const pointerAddr = await deployErc20PointerForCw20(ethers.provider, cw20Address)
                const contract = new hre.ethers.Contract(pointerAddr, ABI.ERC20, hre.ethers.provider);
                newPointer = contract.connect(accounts[0].signer)
            })

            it ("should have the correct balance", async function(){
                expect(await pointer.balanceOf(admin.evmAddress)).to.equal(1000010)
                expect(await pointer.balanceOf(accounts[0].evmAddress)).to.equal(1999989);
                expect(await pointer.balanceOf(accounts[1].evmAddress)).to.equal(3000000);
                expect(await newPointer.balanceOf(admin.evmAddress)).to.equal(1000010)
                expect(await newPointer.balanceOf(accounts[0].evmAddress)).to.equal(1999989);
                expect(await newPointer.balanceOf(accounts[1].evmAddress)).to.equal(3000000);
            })

        })

    })
})