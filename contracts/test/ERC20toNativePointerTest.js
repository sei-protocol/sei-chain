const {setupSigners, deployErc20PointerNative, getAdmin, createTokenFactoryTokenAndMint, ABI} = require("./lib");
const {expect} = require("chai");
const {expectRevert} = require('@openzeppelin/test-helpers');
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
        const pointerAddr = await deployErc20PointerNative(hre.ethers.provider, denom)
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
            await expect(pointer.allowance(accounts[0].evmAddress, accounts[1].evmAddress))
                .to.be.revertedWith("NativeSeiTokensERC20: allowance is not implemented for native pointers");
        });
    });

    describe("transfer()", function () {
        it("should transfer", async function () {
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

        it("should fail transfer() if sender has insufficient balance", async function () {
            let recipient = accounts[1];
            // TODO: determine why we aren't able to extract the error message
            await expectRevert.unspecified(pointer.transfer(recipient.evmAddress, 1001));
        });
    });

    describe("approve()", function () {
        it("should fail approve", async function () {
            const owner = accounts[0].evmAddress;
            const spender = accounts[1].evmAddress;
            await expect(pointer.approve(spender, 50)).to.be.revertedWith("NativeSeiTokensERC20: approve is not implemented for native pointers");
        });
    });

    describe("transferFrom()", function () {
        it("should fail transferFrom", async function () {
            const recipient = admin;
            const owner = accounts[0];
            const spender = accounts[1];
            await expect(pointer.connect(spender.signer).transferFrom(owner.evmAddress, recipient.evmAddress, 10))
                .to.be.revertedWith("NativeSeiTokensERC20: transferFrom is not implemented for native pointers");
        });
    });
});
