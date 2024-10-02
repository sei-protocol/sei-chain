const hre = require("hardhat"); // Require Hardhat Runtime Environment

const {
    execute,
    getSeiAddress,
    queryWasm,
    getSeiBalance,
} = require("../../../contracts/test/lib.js");
const {
    deployTokenPool,
    supplyLiquidity,
    sendFunds,
    pollBalance,
    estimateAndCall,
    deployAndReturnUniswapContracts, setDaemonConfig
} = require("../utils")
const {expect} = require("chai");

const testChain = process.env.DAPP_TEST_ENV;
const isFastTrackEnabled = process.env.IS_FAST_TRACK;

/**
 * Deploy uniswap contracts =>
 */

describe("Uniswap Test", function () {
    let weth9;
    let token;
    let erc20TokenFactory;
    let tokenFactoryDenom;
    let erc20cw20;
    let cw20Address;
    let factory;
    let router;
    let manager;
    let deployer;
    let user;
    let originalSeidConfig;

    before(async function () {
        originalSeidConfig = setDaemonConfig(testChain);
        const accounts = hre.config.networks[testChain].accounts;
        const deployerWallet = hre.ethers.Wallet.fromMnemonic(accounts.mnemonic, accounts.path);
        deployer = deployerWallet.connect(hre.ethers.provider);

        ({
            router,
            manager,
            erc20cw20,
            erc20TokenFactory,
            weth9,
            token,
            tokenFactoryDenom,
            cw20Address
        } = await deployAndReturnUniswapContracts(deployer, testChain, accounts, isFastTrackEnabled));

        const userWallet = hre.ethers.Wallet.createRandom();
        user = userWallet.connect(hre.ethers.provider);

        await sendFunds("1", user.address, deployer)

    })

    describe("Swaps", function () {
        // Swaps token1 for token2.
        async function basicSwapTestAssociated(token1, token2, expectSwapFail = false) {
            const fee = 3000; // Fee tier (0.3%)

            // Perform a Swap
            const amountIn = hre.ethers.utils.parseEther("0.1");
            const amountOutMin = hre.ethers.utils.parseEther("0"); // Minimum amount of MockToken expected

            await estimateAndCall(token1.connect(user), "deposit", [], amountIn)

            const token1balance = await token1.connect(user).balanceOf(user.address);
            expect(token1balance).to.equal(amountIn.toString(), "token1 balance should be equal to value passed in")

            await estimateAndCall(token1.connect(user), "approve", [router.address, amountIn])

            const allowance = await token1.allowance(user.address, router.address);
            // Change to expect
            expect(allowance).to.equal(amountIn.toString(), "token1 allowance for router should be equal to value passed in")

            if (expectSwapFail) {
                expect(router.connect(user).exactInputSingle({
                    tokenIn: token1.address,
                    tokenOut: token2.address,
                    fee,
                    recipient: user.address,
                    deadline: Math.floor(Date.now() / 1000) + 60 * 10, // 10 minutes from now
                    amountIn,
                    amountOutMinimum: amountOutMin,
                    sqrtPriceLimitX96: 0
                }, {gasLimit, gasPrice})).to.be.reverted;
            } else {
                await estimateAndCall(router.connect(user), "exactInputSingle", [{
                    tokenIn: token1.address,
                    tokenOut: token2.address,
                    fee,
                    recipient: user.address,
                    deadline: Math.floor(Date.now() / 1000) + 60 * 10, // 10 minutes from now
                    amountIn,
                    amountOutMinimum: amountOutMin,
                    sqrtPriceLimitX96: 0
                }])

                // Check User's MockToken Balance
                const balance = await token2.balanceOf(user.address);
                // Check that it's more than 0 (no specified amount since there might be slippage)
                expect(Number(balance)).to.greaterThan(0, "Token2 should have been swapped successfully.")
            }
        }

        async function basicSwapTestUnassociated(token1, token2, expectSwapFail = false) {
            const unassocUserWallet = ethers.Wallet.createRandom();
            const unassocUser = unassocUserWallet.connect(ethers.provider);

            // Fund the user account
            await sendFunds("0.1", unassocUser.address, deployer)

            const fee = 3000; // Fee tier (0.3%)

            // Perform a Swap
            const amountIn = hre.ethers.utils.parseEther("0.1");
            const amountOutMin = hre.ethers.utils.parseEther("0"); // Minimum amount of MockToken expected

            await estimateAndCall(token1, "deposit", [], amountIn)

            const token1balance = await token1.balanceOf(deployer.address);

            // Check that deployer has amountIn amount of token1
            expect(Number(token1balance)).to.greaterThanOrEqual(Number(amountIn), "token1 balance should be received by user")

            await estimateAndCall(token1, "approve", [router.address, amountIn])
            const allowance = await token1.allowance(deployer.address, router.address);

            // Check that deployer has approved amountIn amount of token1 to be used by router
            expect(allowance).to.equal(amountIn, "token1 allowance to router should be set correctly by user")

            const txParams = {
                tokenIn: token1.address,
                tokenOut: token2.address,
                fee,
                recipient: unassocUser.address,
                deadline: Math.floor(Date.now() / 1000) + 60 * 10, // 10 minutes from now
                amountIn,
                amountOutMinimum: amountOutMin,
                sqrtPriceLimitX96: 0
            }

            if (expectSwapFail) {
                expect(router.exactInputSingle(txParams)).to.be.reverted;
            } else {
                // Perform the swap, with recipient being the unassociated account.
                await estimateAndCall(router, "exactInputSingle", [txParams])

                // Check User's MockToken Balance
                const balance = await pollBalance(token2, unassocUser.address, function (bal) {
                    return bal === 0
                });

                // Check that it's more than 0 (no specified amount since there might be slippage)
                expect(Number(balance)).to.greaterThan(0, "User should have received some token2")
            }

            // Return the user in case we want to run any more tests.
            return unassocUser;
        }

        it("Associated account should swap erc20 successfully", async function () {
            await basicSwapTestAssociated(weth9, token);
        });

        it("Associated account should swap erc20-tokenfactory successfully", async function () {
            await basicSwapTestAssociated(weth9, erc20TokenFactory);
            const userSeiAddr = await getSeiAddress(user.address);

            const userBal = await getSeiBalance(userSeiAddr, tokenFactoryDenom)
            expect(Number(userBal)).to.be.greaterThan(0);
        });

        it("Associated account should swap erc20-cw20 successfully", async function () {
            await basicSwapTestAssociated(weth9, erc20cw20);

            // Also check on the cw20 side that the token balance has been updated.
            const userSeiAddr = await getSeiAddress(user.address);
            const result = await queryWasm(cw20Address, "balance", {address: userSeiAddr});
            expect(Number(result.data.balance)).to.be.greaterThan(0);
        });

        it("Unassociated account should receive erc20 tokens successfully", async function () {
            await basicSwapTestUnassociated(weth9, token)
        });

        it("Unassociated account should receive erc20-tokenfactory tokens successfully", async function () {
            const unassocUser = await basicSwapTestUnassociated(weth9, erc20TokenFactory)

            // Send funds to associate accounts.
            await sendFunds("0.001", deployer.address, unassocUser)
            const userSeiAddr = await getSeiAddress(unassocUser.address);

            const userBal = await getSeiBalance(userSeiAddr, tokenFactoryDenom)
            expect(Number(userBal)).to.be.greaterThan(0);
        })

        it("Unassociated account should not be able to receive erc20cw20 tokens successfully", async function () {
            await basicSwapTestUnassociated(weth9, erc20cw20, expectSwapFail = true)
        });
    })

    // We've already tested that an associated account (deployer) can deploy pools and supply liquidity in the Before() step.
    describe("Pools", function () {
        it("Unssosciated account should be able to deploy pools successfully", async function () {
            const unassocUserWallet = hre.ethers.Wallet.createRandom();
            const unassocUser = unassocUserWallet.connect(hre.ethers.provider);

            // Fund the user account. Creating pools is a expensive operation so we supply more funds here for gas.
            await sendFunds("0.5", unassocUser.address, deployer)

            await deployTokenPool(manager.connect(unassocUser), erc20TokenFactory.address, token.address)
        })

        it("Unssosciated account should be able to supply liquidity pools successfully", async function () {
            const unassocUserWallet = hre.ethers.Wallet.createRandom();
            const unassocUser = unassocUserWallet.connect(hre.ethers.provider);

            // Fund the user account
            await sendFunds("0.5", unassocUser.address, deployer)

            const erc20TokenFactoryAmount = "100000"

            await estimateAndCall(erc20TokenFactory, "transfer", [unassocUser.address, erc20TokenFactoryAmount])
            const mockTokenAmount = "100000"

            await estimateAndCall(token, "transfer", [unassocUser.address, mockTokenAmount])

            const managerConnected = manager.connect(unassocUser);
            const erc20TokenFactoryConnected = erc20TokenFactory.connect(unassocUser);
            const mockTokenConnected = token.connect(unassocUser);
            await supplyLiquidity(managerConnected, unassocUser.address, erc20TokenFactoryConnected, mockTokenConnected, Number(erc20TokenFactoryAmount) / 2, Number(mockTokenAmount) / 2)
        })
    })

    after(async function () {
        // Set the chain back to regular state
        console.log("Resetting")
        await execute(`seid config chain-id ${originalSeidConfig["chain-id"]}`)
        await execute(`seid config node ${originalSeidConfig["node"]}`)
        await execute(`seid config keyring-backend ${originalSeidConfig["keyring-backend"]}`)
    })
})