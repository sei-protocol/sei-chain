const {setupSigners, deployErc20PointerNative, getAdmin, createTokenFactoryTokenAndMint, ABI, generateWallet,
    fundAddress, getSeiAddress,
    delay,
    adminKeyName
} = require("./lib");
const {expect} = require("chai");

const { expectRevert } = require('@openzeppelin/test-helpers');
require("@nomicfoundation/hardhat-chai-matchers");

describe("ERC20 to Native Pointer", function () {
    let accounts;
    let pointer;
    let admin;
    let denom;

    before(async function () {
        accounts = await setupSigners(await hre.ethers.getSigners())
        admin = await getAdmin()
        const random_num = Math.floor(Math.random() * 10000)
        denom = await createTokenFactoryTokenAndMint(`native-pointer-test-${random_num}`, 1000, accounts[0].seiAddress)

        // deploy TestToken
        const pointerAddr = await deployErc20PointerNative(hre.ethers.provider, denom, from=adminKeyName, evmRpc="", gasLimit=1370000)
        const contract = new hre.ethers.Contract(pointerAddr, ABI.ERC20, hre.ethers.provider);
        pointer = contract.connect(accounts[0].signer)
    })

    describe("read", function(){
        it("get name", async function () {
            const name = await pointer.name();
            expect(name).to.equal(denom);
        });

        it("get symbol", async function () {
            const symbol = await pointer.symbol();
            expect(symbol).to.equal(denom);
        });

        it("get decimals", async function () {
            const decimals = await pointer.decimals();
            expect(Number(decimals)).to.equal(0);
        });

        it("get balanceOf", async function () {
            expect(await pointer.balanceOf(admin.evmAddress)).to.equal(0)
            expect(await pointer.balanceOf(accounts[0].evmAddress)).to.equal(1000);
            expect(await pointer.balanceOf(accounts[1].evmAddress)).to.equal(0);
        });

        it("get totalSupply", async function () {
            expect(await pointer.totalSupply()).to.equal(1000);
        });

        it("get allowance", async function () {
            expect(await pointer.allowance(accounts[0].evmAddress, accounts[1].evmAddress)).to.equal(0);
        });
    })

    describe("transfer()", function () {
        it("should transfer to linked address", async function () {
            let sender = accounts[0];
            let recipient = accounts[1];

            expect(await pointer.balanceOf(sender.evmAddress)).to.equal(1000);
            expect(await pointer.balanceOf(recipient.evmAddress)).to.equal(0);

            const tx = await pointer.transfer(recipient.evmAddress, 5);
            await tx.wait();

            expect(await pointer.balanceOf(sender.evmAddress)).to.equal(995);
            expect(await pointer.balanceOf(recipient.evmAddress)).to.equal(5);

            const cleanupTx = await pointer.connect(recipient.signer).transfer(sender.evmAddress, 5)
            await cleanupTx.wait();
        });

        it("should transfer to unlinked address", async function () {
            let sender = accounts[0];
            let recipientWallet = generateWallet()
            let recipient = await recipientWallet.getAddress()
            const amount = BigInt(5);
            const startBal = await pointer.balanceOf(sender.evmAddress);

            // send token to unlinked wallet
            const tx = await pointer.transfer(recipient, amount);
            await tx.wait();

            // should have sent balance (sender spent, receiver received)
            expect(await pointer.balanceOf(sender.evmAddress)).to.equal(startBal-amount);
            expect(await pointer.balanceOf(recipient)).to.equal(amount);

            // fund address so it can transact
            await fundAddress(recipient, "1000000000000000000000")
            await delay()

            // unlinked wallet can send balance back to sender (becomes linked at this moment)
            await (await pointer.connect(recipientWallet).transfer(sender.evmAddress, amount, {
                gasPrice: ethers.parseUnits('100', 'gwei')
            })).wait()
            expect(await pointer.balanceOf(recipient)).to.equal(BigInt(0));
            expect(await pointer.balanceOf(sender.evmAddress)).to.equal(startBal);

            // confirm association actually happened
            const seiAddress = await getSeiAddress(recipient)
            expect(seiAddress.indexOf("sei")).to.equal(0)
        });

        it("should fail transfer() if sender has insufficient balance", async function () {
            let recipient = accounts[1];
            // TODO: determine why we aren't able to extract the error message
            await expectRevert.unspecified(pointer.transfer(recipient.evmAddress, 1001));
        });
    });

    describe("approve()", function () {
        it("should approve", async function () {
            const owner = accounts[0].evmAddress;
            const spender = accounts[1].evmAddress;
            const tx = await pointer.approve(spender, 50);
            await tx.wait();
            const allowance = await pointer.allowance(owner, spender);
            expect(Number(allowance)).to.equal(50);
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
            // TODO: determine why we aren't able to extract the error message
            await expectRevert.unspecified(pointer.connect(spender.signer).transferFrom(owner.evmAddress, recipient.evmAddress, 999999999));
        });

        it("should fail transferFrom() if allowance is too low", async function () {
            const recipient = admin;
            const owner = accounts[0];
            const spender = accounts[1];

            const tx = await pointer.approve(spender.evmAddress, 10);
            await tx.wait();

            await expect(pointer.connect(spender.signer).transferFrom(owner.evmAddress, recipient.evmAddress, 20)).to.be.revertedWithCustomError(pointer, "ERC20InsufficientAllowance");
        });
    });
})