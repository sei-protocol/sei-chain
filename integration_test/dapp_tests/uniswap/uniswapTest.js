const hre = require("hardhat"); // Require Hardhat Runtime Environment

const { abi: WETH9_ABI, bytecode: WETH9_BYTECODE } = require("@uniswap/v2-periphery/build/WETH9.json");
const { abi: FACTORY_ABI, bytecode: FACTORY_BYTECODE } = require("@uniswap/v3-core/artifacts/contracts/UniswapV3Factory.sol/UniswapV3Factory.json");
const { abi: DESCRIPTOR_ABI, bytecode: DESCRIPTOR_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/libraries/NFTDescriptor.sol/NFTDescriptor.json");
const { abi: MANAGER_ABI, bytecode: MANAGER_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/NonfungiblePositionManager.sol/NonfungiblePositionManager.json");
const { abi: SWAP_ROUTER_ABI, bytecode: SWAP_ROUTER_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/SwapRouter.sol/SwapRouter.json");
const {exec} = require("child_process");
const { fundAddress, createTokenFactoryTokenAndMint, getPointerForNative, deployErc20PointerNative, execute, getSeiAddress, queryWasm, getSeiBalance, ABI } = require("../../../contracts/test/lib.js");
const { deployTokenPool, supplyLiquidity, deployCw20WithPointer, deployEthersContract, sendFunds } = require("./uniswapHelpers.js")
const {cw20Addresses, tokenFactoryDenoms, rpcUrls, chainIds} = require("./constants")
const { expect } = require("chai");

const testChain = process.env.DAPP_TEST_ENV;

describe("EVM Test", function () {
    let weth9;
    let token;
    let erc20TokenFactory;
    let tokenFactoryDenom;
    let erc20cw20;
    let cw20Address;
    let router;
    let manager;
    let deployer;
    let user;
    before(async function () {
        [deployer] = await hre.ethers.getSigners();

        // const account = hre.config.networks.hardhat.accounts;
        // const deployerKeyName = "deployer"
        // const deployerAddr = await addDeployerKey(deployerKeyName, account.mnemonic, account.path);
        // console.log(deployerAddr);
        if (testChain === 'seilocal') {
            await fundAddress(deployer.address, amount="2000000000000000000000");
        } else {
            // Set default seid config to the specified rpc url.
            await execute(`seid config chain-id ${chainIds[testChain]}`)
            await execute(`seid config node ${rpcUrls[testChain]}`)
        }

        // Fund user account
        const userWallet = hre.ethers.Wallet.createRandom();
        user = userWallet.connect(hre.ethers.provider);

        await sendFunds("20", user.address, deployer)

        const deployerSeiAddr = await getSeiAddress(deployer.address);

        // Deploy Required Tokens
        // If local chain, deployer should have received all the tokens on first mint.
        // Otherwise, deployer needs to own all the tokens before this test is run.
        const time = Date.now().toString();

        if (testChain === 'seilocal') {
            // Deploy TokenFactory token with ERC20 pointer
            const tokenName = `dappTests${time}`
            tokenFactoryDenom = await createTokenFactoryTokenAndMint(tokenName, 10000000000, deployerSeiAddr)
            console.log("DENOM", tokenFactoryDenom)
            const pointerAddr = await deployErc20PointerNative(hre.ethers.provider, tokenFactoryDenom)
            console.log("Pointer Addr", pointerAddr);
            erc20TokenFactory = new hre.ethers.Contract(pointerAddr, ABI.ERC20, deployer);
        } else {
            tokenFactoryDenom = tokenFactoryDenoms[testChain]
            const pointer= await getPointerForNative(tokenFactoryDenom);
            erc20TokenFactory = new hre.ethers.Contract(pointer.pointer, ABI.ERC20, deployer);
        }

        if (testChain === 'seilocal') {
            // Deploy CW20 token with ERC20 pointer
            const cw20Details = await deployCw20WithPointer(deployerSeiAddr, deployer, time)
            erc20cw20 = cw20Details.pointerContract;
            cw20Address = cw20Details.cw20Address;
        } else {
            const cw20addrs = cw20Addresses[testChain]
            cw20Address = cw20addrs.sei
            erc20cw20 = new hre.ethers.Contract(cw20addrs.evm, ABI.ERC20, deployer);
        }

        // Deploy WETH9 Token (ETH representation on Uniswap)
        weth9 = await deployEthersContract("WETH9", WETH9_ABI, WETH9_BYTECODE, deployer);

        // Deploy MockToken
        console.log("Deploying MockToken with the account:", deployer.address);
        const MockERC20 = await hre.ethers.getContractFactory("MockERC20");
        token = await MockERC20.deploy("MockToken", "MKT", hre.ethers.utils.parseEther("1000000"));
        await token.deployed();
        console.log("MockToken deployed to:", token.address);

        // Deploy NFT Descriptor. These NFTs are used by the NonFungiblePositionManager to represent liquidity positions.
        const descriptor = await deployEthersContract("NFT Descriptor", DESCRIPTOR_ABI, DESCRIPTOR_BYTECODE, deployer);

        // Deploy Uniswap Contracts
        // Create UniswapV3 Factory
        const factory = await deployEthersContract("Uniswap V3 Factory", FACTORY_ABI, FACTORY_BYTECODE, deployer);

        // Deploy NonFungiblePositionManager
        manager = await deployEthersContract("NonfungiblePositionManager", MANAGER_ABI, MANAGER_BYTECODE, deployer, deployParams=[factory.address, weth9.address, descriptor.address]);

        // Deploy SwapRouter
        router = await deployEthersContract("SwapRouter", SWAP_ROUTER_ABI, SWAP_ROUTER_BYTECODE, deployer, deployParams=[factory.address, weth9.address]);

        const amountETH = hre.ethers.utils.parseEther("30")

        // Gets the amount of WETH9 required to instantiate pools by depositing Sei to the contract
        const txWrap = await weth9.deposit({ value: amountETH });
        await txWrap.wait();
        console.log(`Deposited ${amountETH.toString()} to WETH9`);

        // Create liquidity pools
        await deployTokenPool(manager, weth9.address, token.address)
        await deployTokenPool(manager, weth9.address, erc20TokenFactory.address, swapRatio=10**-13)
        await deployTokenPool(manager, weth9.address, erc20cw20.address, swapRatio=10**-13)

        // Add Liquidity to pools
        await supplyLiquidity(manager, deployer.address, weth9, token, hre.ethers.utils.parseEther("10"), hre.ethers.utils.parseEther("10"))
        await supplyLiquidity(manager, deployer.address, weth9, erc20TokenFactory, hre.ethers.utils.parseEther("10"), 100000000)
        await supplyLiquidity(manager, deployer.address, weth9, erc20cw20, hre.ethers.utils.parseEther("10"), 100000000)
    })

    describe("Swaps", function () {
        // Swaps token1 for token2.
        async function basicSwapTestAssociated(token1, token2, expectSwapFail=false) {
            const fee = 3000; // Fee tier (0.3%)

            // Perform a Swap
            const amountIn = hre.ethers.utils.parseEther("1");
            const amountOutMin = hre.ethers.utils.parseEther("0"); // Minimum amount of MockToken expected

            const gasLimit = hre.ethers.utils.hexlify(1000000); // Example gas limit
            const gasPrice = await hre.ethers.provider.getGasPrice();

            const deposit = await token1.connect(user).deposit({ value: amountIn, gasLimit, gasPrice });
            await deposit.wait();

            const token1balance = await token1.connect(user).balanceOf(user.address);
            expect(token1balance).to.equal(amountIn.toString(), "token1 balance should be equal to value passed in")

            const approval = await token1.connect(user).approve(router.address, amountIn, {gasLimit, gasPrice});
            await approval.wait();

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
                const tx = await router.connect(user).exactInputSingle({
                    tokenIn: token1.address,
                    tokenOut: token2.address,
                    fee,
                    recipient: user.address,
                    deadline: Math.floor(Date.now() / 1000) + 60 * 10, // 10 minutes from now
                    amountIn,
                    amountOutMinimum: amountOutMin,
                    sqrtPriceLimitX96: 0
                }, {gasLimit, gasPrice});

                await tx.wait();

                // Check User's MockToken Balance
                const balance = BigInt(await token2.balanceOf(user.address));
                // Check that it's more than 0 (no specified amount since there might be slippage)
                expect(Number(balance)).to.greaterThan(0, "Token2 should have been swapped successfully.")
            }
        }

        async function basicSwapTestUnassociated(token1, token2, expectSwapFail=false) {
            const unassocUserWallet = ethers.Wallet.createRandom();
            const unassocUser = unassocUserWallet.connect(ethers.provider);

            // Fund the user account
            await sendFunds("2", unassocUser.address, deployer)

            const fee = 3000; // Fee tier (0.3%)

            // Perform a Swap
            const amountIn = hre.ethers.utils.parseEther("1");
            const amountOutMin = hre.ethers.utils.parseEther("0"); // Minimum amount of MockToken expected

            const deposit = await token1.deposit({ value: amountIn });
            await deposit.wait();

            const token1balance = await token1.balanceOf(deployer.address);

            // Check that deployer has amountIn amount of token1
            expect(Number(token1balance)).to.greaterThanOrEqual(Number(amountIn), "token1 balance should be received by user")

            const approval = await token1.approve(router.address, amountIn);
            await approval.wait();

            const allowance = await token1.allowance(deployer.address, router.address);

            // Check that deployer has approved amountIn amount of token1 to be used by router
            expect(allowance).to.equal(amountIn, "token1 allowance to router should be set correctly by user")

            if (expectSwapFail) {
                expect(router.exactInputSingle({
                    tokenIn: token1.address,
                    tokenOut: token2.address,
                    fee,
                    recipient: unassocUser.address,
                    deadline: Math.floor(Date.now() / 1000) + 60 * 10, // 10 minutes from now
                    amountIn,
                    amountOutMinimum: amountOutMin,
                    sqrtPriceLimitX96: 0
                })).to.be.reverted;
            } else {
                // Perform the swap, with recipient being the unassociated account.
                const tx = await router.exactInputSingle({
                    tokenIn: token1.address,
                    tokenOut: token2.address,
                    fee,
                    recipient: unassocUser.address,
                    deadline: Math.floor(Date.now() / 1000) + 60 * 10, // 10 minutes from now
                    amountIn,
                    amountOutMinimum: amountOutMin,
                    sqrtPriceLimitX96: 0
                });

                await tx.wait();

                // Check User's MockToken Balance
                const balance = await token2.balanceOf(unassocUser.address);
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
            await basicSwapTestUnassociated(weth9, erc20cw20, expectSwapFail=true)
        });
    })

    // We've already tested that an associated account (deployer) can deploy pools and supply liquidity in the Before() step.
    describe("Pools", function () {
        it("Unssosciated account should be able to deploy pools successfully", async function () {
          const unassocUserWallet = hre.ethers.Wallet.createRandom();
          const unassocUser = unassocUserWallet.connect(hre.ethers.provider);

          // Fund the user account. Creating pools is a expensive operation so we supply more funds here for gas.
          await sendFunds("5", unassocUser.address, deployer)

          await deployTokenPool(manager.connect(unassocUser), erc20TokenFactory.address, token.address)
        })

        it("Unssosciated account should be able to supply liquidity pools successfully", async function () {
            const unassocUserWallet = hre.ethers.Wallet.createRandom();
            const unassocUser = unassocUserWallet.connect(hre.ethers.provider);

            // Fund the user account
            await sendFunds("2", unassocUser.address, deployer)

            const erc20TokenFactoryAmount = "100000"
            const tx = await erc20TokenFactory.transfer(unassocUser.address, erc20TokenFactoryAmount);
            await tx.wait();
            const mockTokenAmount = "100000"
            const tx2 = await token.transfer(unassocUser.address, mockTokenAmount);
            await tx2.wait();
            const managerConnected = manager.connect(unassocUser);
            const erc20TokenFactoryConnected = erc20TokenFactory.connect(unassocUser);
            const mockTokenConnected = token.connect(unassocUser);
            await supplyLiquidity(managerConnected, unassocUser.address, erc20TokenFactoryConnected, mockTokenConnected, Number(erc20TokenFactoryAmount)/2, Number(mockTokenAmount)/2)
        })
    })

    after(async function () {
        // Set the chain back to regular state
        console.log("Resetting")
        if (testChain !== 'seilocal') {
            await execute(`seid config chain-id sei-chain`)
            await execute(`seid config node tcp://localhost:26657`)
        }
    })
})