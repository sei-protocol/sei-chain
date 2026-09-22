const hre = require("hardhat"); // Require Hardhat Runtime Environment

const { abi: WETH9_ABI, bytecode: WETH9_BYTECODE } = require("@uniswap/v2-periphery/build/WETH9.json");
const { abi: FACTORY_ABI, bytecode: FACTORY_BYTECODE } = require("@uniswap/v3-core/artifacts/contracts/UniswapV3Factory.sol/UniswapV3Factory.json");
const { abi: DESCRIPTOR_ABI, bytecode: DESCRIPTOR_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/libraries/NFTDescriptor.sol/NFTDescriptor.json");
const { abi: MANAGER_ABI, bytecode: MANAGER_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/NonfungiblePositionManager.sol/NonfungiblePositionManager.json");
const { abi: SWAP_ROUTER_ABI, bytecode: SWAP_ROUTER_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/SwapRouter.sol/SwapRouter.json");
const {exec} = require("child_process");
const { fundAddress, execute } = require("../../../contracts/test/lib.js");
const { deployTokenPool, supplyLiquidity, deployEthersContract, sendFunds, pollBalance, setupAccountWithMnemonic, estimateAndCall } = require("../utils")
const { rpcUrls, chainIds } = require("../constants")
const { expect } = require("chai");

const testChain = process.env.DAPP_TEST_ENV;

describe("Uniswap Test", function () {
    let weth9;
    let token;
    let altToken;
    let factory;
    let router;
    let manager;
    let deployer;
    let user;
    let originalSeidConfig;
    before(async function () {
        const accounts = hre.config.networks[testChain].accounts
        const deployerWallet = hre.ethers.Wallet.fromMnemonic(accounts.mnemonic, accounts.path);
        deployer = deployerWallet.connect(hre.ethers.provider);

        const seidConfig = await execute('seid config');
        originalSeidConfig = JSON.parse(seidConfig);

        if (testChain === 'seilocal') {
            await fundAddress(deployer.address, amount="2000000000000000000000");
        } else {
            // Set default seid config to the specified rpc url.
            await execute(`seid config chain-id ${chainIds[testChain]}`)
            await execute(`seid config node ${rpcUrls[testChain]}`)
        }

        // Set the config keyring to 'test' since we're using the key added to test from here.
        await execute(`seid config keyring-backend test`)

        await sendFunds('0.01', deployer.address, deployer)
        await setupAccountWithMnemonic("dapptest", accounts.mnemonic, deployer);


        // Fund user account
        const userWallet = hre.ethers.Wallet.createRandom();
        user = userWallet.connect(hre.ethers.provider);

        await sendFunds("1", user.address, deployer)

        // Deploy WETH9 Token (ETH representation on Uniswap)
        weth9 = await deployEthersContract("WETH9", WETH9_ABI, WETH9_BYTECODE, deployer);

        // Deploy MockToken
        console.log("Deploying MockToken with the account:", deployer.address);
        const contractArtifact = await hre.artifacts.readArtifact("MockERC20");
        token = await deployEthersContract("MockToken", contractArtifact.abi, contractArtifact.bytecode, deployer, ["MockToken", "MKT", hre.ethers.utils.parseEther("1000000")])

        // The pool tests must create a pool that does not exist yet, so this second token
        // is deliberately left unpaired by the setup below.
        altToken = await deployEthersContract("AltMockToken", contractArtifact.abi, contractArtifact.bytecode, deployer, ["AltMockToken", "ALT", hre.ethers.utils.parseEther("1000000")])

        // Deploy NFT Descriptor. These NFTs are used by the NonFungiblePositionManager to represent liquidity positions.
        const descriptor = await deployEthersContract("NFT Descriptor", DESCRIPTOR_ABI, DESCRIPTOR_BYTECODE, deployer);

        // Deploy Uniswap Contracts
        // Create UniswapV3 Factory
        factory = await deployEthersContract("Uniswap V3 Factory", FACTORY_ABI, FACTORY_BYTECODE, deployer);

        // Deploy NonFungiblePositionManager
        manager = await deployEthersContract("NonfungiblePositionManager", MANAGER_ABI, MANAGER_BYTECODE, deployer, deployParams=[factory.address, weth9.address, descriptor.address]);

        // Deploy SwapRouter
        router = await deployEthersContract("SwapRouter", SWAP_ROUTER_ABI, SWAP_ROUTER_BYTECODE, deployer, deployParams=[factory.address, weth9.address]);

        const amountETH = hre.ethers.utils.parseEther("3")

        // Gets the amount of WETH9 required to instantiate pools by depositing Sei to the contract
        let gasEstimate = await weth9.estimateGas.deposit({ value: amountETH })
        let gasPrice = await deployer.getGasPrice();
        const txWrap = await weth9.deposit({ value: amountETH, gasPrice, gasLimit: gasEstimate });
        await txWrap.wait();
        console.log(`Deposited ${amountETH.toString()} to WETH9`);

        // Create liquidity pools
        await deployTokenPool(manager, weth9.address, token.address)

        // Add Liquidity to pools
        await supplyLiquidity(manager, deployer.address, weth9, token, hre.ethers.utils.parseEther("1"), hre.ethers.utils.parseEther("1"))
    })

    describe("Swaps", function () {
        // Swaps token1 for token2.
        async function basicSwapTestAssociated(token1, token2, expectSwapFail=false) {
            const fee = 3000; // Fee tier (0.3%)

            // Perform a Swap
            const amountIn = hre.ethers.utils.parseEther("0.1");
            const amountOutMin = hre.ethers.utils.parseEther("0"); // Minimum amount of MockToken expected

            // const gasLimit = hre.ethers.utils.hexlify(1000000); // Example gas limit
            // const gasPrice = await hre.ethers.provider.getGasPrice();

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

        async function basicSwapTestUnassociated(token1, token2) {
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

            // Perform the swap, with recipient being the unassociated account.
            await estimateAndCall(router, "exactInputSingle", [txParams])

            // Check User's MockToken Balance
            const balance = await pollBalance(token2, unassocUser.address, function(bal) {return bal === 0});

            // Check that it's more than 0 (no specified amount since there might be slippage)
            expect(Number(balance)).to.greaterThan(0, "User should have received some token2")

            // Return the user in case we want to run any more tests.
            return unassocUser;
        }

        it("Associated account should swap erc20 successfully", async function () {
            await basicSwapTestAssociated(weth9, token);
        });

        it("Unassociated account should receive erc20 tokens successfully", async function () {
            await basicSwapTestUnassociated(weth9, token)
        });
    })

    // We've already tested that an associated account (deployer) can deploy pools and supply liquidity in the Before() step.
    describe("Pools", function () {
        it("Unssosciated account should be able to deploy pools successfully", async function () {
          const unassocUserWallet = hre.ethers.Wallet.createRandom();
          const unassocUser = unassocUserWallet.connect(hre.ethers.provider);

          // Fund the user account. Creating pools is a expensive operation so we supply more funds here for gas.
          await sendFunds("0.5", unassocUser.address, deployer)

          await deployTokenPool(manager.connect(unassocUser), altToken.address, token.address)
        })

        it("Unssosciated account should be able to supply liquidity pools successfully", async function () {
            const unassocUserWallet = hre.ethers.Wallet.createRandom();
            const unassocUser = unassocUserWallet.connect(hre.ethers.provider);

            // Fund the user account
            await sendFunds("0.5", unassocUser.address, deployer)

            const altTokenAmount = "100000"

            await estimateAndCall(altToken, "transfer", [unassocUser.address, altTokenAmount])
            const mockTokenAmount = "100000"

            await estimateAndCall(token, "transfer", [unassocUser.address, mockTokenAmount])

            const managerConnected = manager.connect(unassocUser);
            const altTokenConnected = altToken.connect(unassocUser);
            const mockTokenConnected = token.connect(unassocUser);
            await supplyLiquidity(managerConnected, unassocUser.address, altTokenConnected, mockTokenConnected, Number(altTokenAmount)/2, Number(mockTokenAmount)/2)
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