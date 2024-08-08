const hre = require("hardhat"); // Require Hardhat Runtime Environment

const { abi: WETH9_ABI, bytecode: WETH9_BYTECODE } = require("@uniswap/v2-periphery/build/WETH9.json");
const { abi: FACTORY_ABI, bytecode: FACTORY_BYTECODE } = require("@uniswap/v3-core/artifacts/contracts/UniswapV3Factory.sol/UniswapV3Factory.json");
const { abi: DESCRIPTOR_ABI, bytecode: DESCRIPTOR_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/libraries/NFTDescriptor.sol/NFTDescriptor.json");
const { abi: MANAGER_ABI, bytecode: MANAGER_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/NonfungiblePositionManager.sol/NonfungiblePositionManager.json");
const { abi: SWAP_ROUTER_ABI, bytecode: SWAP_ROUTER_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/SwapRouter.sol/SwapRouter.json");
const {exec} = require("child_process");
const { fundAddress, setupSigners, createTokenFactoryTokenAndMint, deployErc20PointerNative, ABI } = require("../../../contracts/test/lib.js");
const { deployTokenPool, supplyLiquidity, deployCw20WithPointer } = require("./uniswapHelpers.js")
const { expect } = require("chai");
const {deployCw20Pointer} = require("./uniswapHelpers");
require("it-each")({ testPerIteration: true });

describe("EVM Test", function () {
    let weth9;
    let token;
    let erc20TokenFactory;
    let erc20cw20;
    let router;
    let manager;
    let deployer;
    let user;
    before(async function () {
        [deployerObj] = await setupSigners(await hre.ethers.getSigners());
        deployer = deployerObj.signer
        await fundAddress(deployer.address, amount="2000000000000000000000")

        // Fund user account
        const userWallet = ethers.Wallet.createRandom();
        user = userWallet.connect(ethers.provider);

        await fundAddress(user.address)
        // Deploy Required Tokens

        // Deploy TokenFactory token with ERC20 pointer
        const time = Date.now().toString();
        const tokenName = `test${time}`
        const denom = await createTokenFactoryTokenAndMint(tokenName, 1000000000, deployerObj.seiAddress)
        console.log("DENOM", denom)
        const pointerAddr = await deployErc20PointerNative(hre.ethers.provider, denom)
        console.log("Pointer Addr", pointerAddr);
        erc20TokenFactory = new hre.ethers.Contract(pointerAddr, ABI.ERC20, deployer);

        // Deploy CW20 token with ERC20 pointer
        erc20cw20= await deployCw20WithPointer(deployerObj, time)
        const cwBal = await erc20cw20.balanceOf(deployer.address);
        console.log("cwBAL", cwBal)

        // Deploy WETH9 Token (ETH representation on Uniswap)
        console.log("Deploying WETH9 with the account:", deployer.address);
        const WETH9 = new hre.ethers.ContractFactory(WETH9_ABI, WETH9_BYTECODE, deployer);
        weth9 = await WETH9.deploy();
        await weth9.deployed();
        console.log("WETH9 deployed to:", weth9.address);

        // Deploy MockToken
        console.log("Deploying MockToken with the account:", deployer.address);
        const MockERC20 = await hre.ethers.getContractFactory("MockERC20");
        token = await MockERC20.deploy("MockToken", "MKT", hre.ethers.utils.parseEther("1000000"));
        await token.deployed();
        console.log("MockToken deployed to:", token.address);

        // Deploy NFT Descriptor. These NFTs are used by the NonFungiblePositionManager to represent liquidity positions.
        console.log("Deploying NFT Descriptor with the account:", deployer.address);
        const NFTDescriptor = new hre.ethers.ContractFactory(DESCRIPTOR_ABI, DESCRIPTOR_BYTECODE, deployer);
        descriptor = await NFTDescriptor.deploy();
        await descriptor.deployed();
        console.log("NFTDescriptor deployed to:", descriptor.address);

        // Deploy Uniswap Contracts
        // Create UniswapV3 Factory
        console.log("Deploying Factory Contract with the account:", deployer.address);
        const FactoryContract = new hre.ethers.ContractFactory(FACTORY_ABI, FACTORY_BYTECODE, deployer);
        const factory = await FactoryContract.deploy();
        await factory.deployed();
        console.log("Uniswap V3 Factory deployed to:", factory.address);

        // Deploy NonFungiblePositionManager
        const NonfungiblePositionManager = new hre.ethers.ContractFactory(MANAGER_ABI, MANAGER_BYTECODE, deployer);
        manager = await NonfungiblePositionManager.deploy(factory.address, weth9.address, descriptor.address);
        await manager.deployed();
        console.log("NonfungiblePositionManager deployed to:", manager.address);

        // Deploy SwapRouter
        console.log("Deploying SwapRouter with the account:", deployer.address);
        const SwapRouter = new hre.ethers.ContractFactory(SWAP_ROUTER_ABI, SWAP_ROUTER_BYTECODE, deployer);
        router = await SwapRouter.deploy(factory.address, weth9.address);
        await router.deployed();
        console.log("SwapRouter deployed to:", router.address);

        const amountETH = hre.ethers.utils.parseEther("300")

        // Gets the amount of WETH9 required to instantiate pools by depositing Sei to the contract
        const txWrap = await weth9.deposit({ value: amountETH });
        await txWrap.wait();
        console.log(`Deposited ${amountETH.toString()} ETH to WETH9`);

        // Create liquidity pools
        await deployTokenPool(manager, weth9.address, token.address)
        await deployTokenPool(manager, weth9.address, erc20TokenFactory.address, swapRatio=10**-13)
        await deployTokenPool(manager, weth9.address, erc20cw20.address, swapRatio=10**-13)

        // Add Liquidity to pools
        await supplyLiquidity(manager, deployer.address, weth9, token, hre.ethers.utils.parseEther("100"), hre.ethers.utils.parseEther("100"))
        await supplyLiquidity(manager, deployer.address, weth9, erc20TokenFactory, hre.ethers.utils.parseEther("100"), 100000000)
        await supplyLiquidity(manager, deployer.address, weth9, erc20cw20, hre.ethers.utils.parseEther("100"), 1000000000, 100000000)
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
                }, {gasLimit, gasPrice})).to.be.revertedWithoutReason();
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
            await fundAddress(unassocUser.address)

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
                })).to.be.revertedWithoutReason();
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
        }

        it("Associated account should swap erc20 successfully", async function () {
            await basicSwapTestAssociated(weth9, token);
        });

        it("Associated account should swap erc20-tokenfactory successfully", async function () {
            await basicSwapTestAssociated(weth9, erc20TokenFactory);
        });

        it("Associated account should swap erc20-cw20 successfully", async function () {
            await basicSwapTestAssociated(weth9, erc20cw20);
        });

        it("Unassociated account should receive erc20-tokenfactory tokens successfully", async function () {
            await basicSwapTestUnassociated(weth9, erc20TokenFactory)
        })

        it("Unassociated account should receive erc20 tokens successfully", async function () {
            await basicSwapTestUnassociated(weth9, token)
        });

        it("Unassociated account should not be able to receive erc20cw20 tokens successfully", async function () {
            await basicSwapTestUnassociated(weth9, erc20cw20, expectSwapFail=true)
        });
    })
})