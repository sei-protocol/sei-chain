const {
    setupSigners, deployErc20PointerForCw20, getAdmin, deployWasm, WASM, ABI, registerPointerForERC20, testAPIEnabled,
    incrementPointerVersion
} = require("./lib");
const { expect } = require("chai");
const BigNumber = require('bignumber.js');

describe("ERC20 to CW20 Pointer", function () {
    let accounts;
    let admin;
    let cw20Address;

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
    });

    async function setupPointer() {
        const pointerAddr = await deployErc20PointerForCw20(hre.ethers.provider, cw20Address);
        const contract = new hre.ethers.Contract(pointerAddr, ABI.ERC20, hre.ethers.provider);
        return contract.connect(accounts[0].signer);
    }

    function testPointer(getPointer, balances) {
        describe("pointer functions", function () {
            let pointer;

            beforeEach(async function () {
                pointer = await getPointer();
            });

            describe("validation", function () {
                it("should not allow a pointer to the pointer", async function () {
                    try {
                        await registerPointerForERC20(await pointer.getAddress());
                        expect.fail(`Expected to be prevented from creating a pointer`);
                    } catch (e) {
                        expect(e.message).to.include("contract deployment failed");
                    }
                });
            });

            describe("read", function () {
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
                    expect(await pointer.balanceOf(admin.evmAddress)).to.equal(balances.admin);
                    expect(await pointer.balanceOf(accounts[0].evmAddress)).to.equal(balances.account0);
                    expect(await pointer.balanceOf(accounts[1].evmAddress)).to.equal(balances.account1);
                });

                it("get totalSupply", async function () {
                    expect(await pointer.totalSupply()).to.equal(6000000);
                });

                it("get allowance", async function () {
                    expect(await pointer.allowance(accounts[0].evmAddress, accounts[1].evmAddress)).to.equal(0);
                });
            });

            describe("transfer()", function () {
                it("should transfer", async function () {
                    let sender = accounts[0];
                    let recipient = accounts[1];

                    expect(await pointer.balanceOf(sender.evmAddress)).to.equal(balances.account0);
                    expect(await pointer.balanceOf(recipient.evmAddress)).to.equal(balances.account1);

                    const tx = await pointer.transfer(recipient.evmAddress, 1);
                    await tx.wait();

                    expect(await pointer.balanceOf(sender.evmAddress)).to.equal(balances.account0-1);
                    expect(await pointer.balanceOf(recipient.evmAddress)).to.equal(balances.account1+1);

                    const cleanupTx = await pointer.connect(recipient.signer).transfer(sender.evmAddress, 1);
                    await cleanupTx.wait();
                });

                it("should fail transfer() if sender has insufficient balance", async function () {
                    const recipient = accounts[1];
                    await expect(pointer.transfer(recipient.evmAddress, balances.account0*10)).to.be.revertedWith("CosmWasm execute failed");
                });

                it("transfer to unassociated address should fail", async function () {
                    const unassociatedRecipient = "0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266";
                    await expect(pointer.transfer(unassociatedRecipient, 1)).to.be.revertedWithoutReason;
                });

                it("transfer to contract address should succeed", async function () {
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

                it("approvals above uint128 max int should work", async function() {
                    const owner = accounts[0].evmAddress;
                    const spender = accounts[1].evmAddress;
                    const maxUint128 = new BigNumber("0xffffffffffffffffffffffffffffffff", 16);
                    const tx = await pointer.approve(spender, maxUint128.toFixed());
                    await tx.wait();
                    const allowance = await pointer.allowance(owner, spender);
                    expect(allowance).to.equal(maxUint128.toFixed());

                    // approving uint128 max int + 1 should work but only approve uint128
                    const maxUint128Plus1 = maxUint128.plus(1);
                    const tx128plus1 = await pointer.approve(spender, maxUint128Plus1.toFixed());
                    await tx128plus1.wait();
                    const allowance128plus1 = await pointer.allowance(owner, spender);
                    expect(allowance128plus1).to.equal(maxUint128.toFixed());

                    // approving uint256 should also work but only approve uint128
                    const maxUint256 = new BigNumber("0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 16);
                    const tx256 = await pointer.approve(spender, maxUint256.toFixed());
                    await tx256.wait();
                    const allowance256 = await pointer.allowance(owner, spender);
                    expect(allowance256).to.equal(maxUint128.toFixed());
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
                    // put it back
                    await (await pointer.approve(spender.evmAddress, 0)).wait()
                });
            });
        });
    }

    describe("Pointer Functionality", function () {
        let pointer;

        before(async function () {
            pointer = await setupPointer();
        });

        // verify pointer
        testPointer(() => pointer, {
            admin: 1000000,
            account0: 2000000,
            account1: 3000000
        });

        // Pointer version is going to be coupled with seid version going forward (as in,
        // given a seid version, it's impossible to have multiple versions of pointer).
        // We need to recreate the equivalent of the following test once we have a framework
        // for simulating chain-level upgrade.
        describe.skip("Pointer Upgrade", function () {
            let newPointer;

            before(async function () {
                const enabled = await testAPIEnabled(ethers.provider);
                if (!enabled) {
                    this.skip();
                }

                await incrementPointerVersion(ethers.provider, "cw20", 1);

                const pointerAddr = await deployErc20PointerForCw20(hre.ethers.provider, cw20Address);
                const contract = new hre.ethers.Contract(pointerAddr, ABI.ERC20, hre.ethers.provider);
                newPointer = contract.connect(accounts[0].signer);
            });

            // verify new pointer
            testPointer(() => newPointer, {
                admin: 1000010,
                account0: 1999989,
                account1: 3000000
            });

        });

        // this does not yet pass, so skip
        describe.skip("Original Pointer after Upgrade", function(){

            before(async function () {
                const enabled = await testAPIEnabled(ethers.provider);
                if (!enabled) {
                    this.skip();
                }
            });

                // original pointer
            testPointer(() => pointer, {
                admin: 1000020,
                account0: 1999978,
                account1: 3000000
            });
        })
    });

    });

