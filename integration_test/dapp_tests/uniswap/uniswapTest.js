const hre = require("hardhat"); // Require Hardhat Runtime Environment

const { abi: WETH9_ABI, bytecode: WETH9_BYTECODE } = require("@uniswap/v2-periphery/build/WETH9.json");
const { abi: FACTORY_ABI, bytecode: FACTORY_BYTECODE } = require("@uniswap/v3-core/artifacts/contracts/UniswapV3Factory.sol/UniswapV3Factory.json");
const { abi: DESCRIPTOR_ABI, bytecode: DESCRIPTOR_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/libraries/NFTDescriptor.sol/NFTDescriptor.json");
const { abi: MANAGER_ABI, bytecode: MANAGER_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/NonfungiblePositionManager.sol/NonfungiblePositionManager.json");
const { abi: SWAP_ROUTER_ABI, bytecode: SWAP_ROUTER_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/SwapRouter.sol/SwapRouter.json");
const {exec} = require("child_process");
const { fundAddress, setupSigners, createTokenFactoryTokenAndMint, deployErc20PointerNative } = require("../../../contracts/test/lib.js");

const { expect } = require("chai");

describe("EVM Test", async function () {
    let weth9;
    let token;
    let router;
    let manager;
    let deployer;
    before(async function () {
        [deployerObj] = await setupSigners(await hre.ethers.getSigners());
        deployer = deployerObj.signer

        await fundAddress(deployer.address, amount="2000000000000000000000")

        // Deploy Required Tokens

        // Deploy TokenFactory token with ERC20 pointer
        const tokenName = "tokenfactorytest"
        const denom = await createTokenFactoryTokenAndMint(tokenName, 10000000, deployerObj.seiAddress)
        console.log("DENOM", denom)
        const pointerAddr = await deployErc20PointerNative(hre.ethers.provider, denom)
        console.log("Pointer Addr", pointerAddr);

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
    
        // Create WETH9 x MockToken liquidity pool
        console.log("Deploying SwapRouter with the account:", deployer.address);
        const fee = 3000; // Fee tier (0.3%)
        const sqrtPriceX96 = BigInt(Math.sqrt(1)) * BigInt(2) ** BigInt(96); // Initial price (1:1)
    
        // token0 addr must be < token1 addr
        let token0addr;
        let token1addr;
        if (parseInt(weth9.address, 16) < parseInt(token.address, 16)) {
            token0addr = weth9.address;
            token1addr = token.address;
        } else {
            token0addr = token.address;
            token1addr = weth9.address;
        }
        const poolTx = await manager.createAndInitializePoolIfNecessary(
            token0addr,
            token1addr,
            fee,
            sqrtPriceX96
        );
        await poolTx.wait();
        console.log("Pool created and initialized");
    
        // Add Liquidity to pool
        // Define the amount of tokens to be approved and added as liquidity
        console.log("Supplying liquidity to pool")
        const amountETH = hre.ethers.utils.parseEther("10000");
        const amountToken = hre.ethers.utils.parseEther("10000");
    
        let token0amt;
        let token1amt;
        if (token0addr === weth9.address) {
            token0amt = amountETH;
            token1amt = amountToken
        } else {
            token0amt = amountToken;
            token1amt = amountETH;
        }

        // Approve the NonfungiblePositionManager to spend the specified amount of the mock token
        await token.approve(manager.address, amountToken);
    
        // Wrap ETH to WETH by depositing ETH into the WETH9 contract
        const txWrap = await weth9.deposit({ value: amountETH });
        await txWrap.wait();
        console.log(`Deposited ${amountETH.toString()} ETH to WETH9`);
    
        // Approve the NonfungiblePositionManager to spend the specified amount of WETH
        const approveWETHTx = await weth9.approve(manager.address, amountETH);
        await approveWETHTx.wait();
        console.log(`Approved ${amountETH.toString()} WETH to the NonfungiblePositionManager`);

        
        // Add liquidity to the pool
        const liquidityTx = await manager.mint({
            token0: token0addr,
            token1: token1addr,
            fee: 3000, // Fee tier (0.3%)
            tickLower: -887220,
            tickUpper: 887220,
            amount0Desired: token0amt,
            amount1Desired: token1amt,
            amount0Min: 0,
            amount1Min: 0,
            recipient: deployer.address,
            deadline: Math.floor(Date.now() / 1000) + 60 * 10 // 10 minutes from now
        });

        await liquidityTx.wait();
        console.log("Liquidity added");

    })

    describe("Swaps", async function () {
        it("Unassociated account should swap successfully", async function () {

            const userWallet = ethers.Wallet.createRandom();
            const user = userWallet.connect(ethers.provider);

            // Fund the user account from the deployer account
            const fundAmount = hre.ethers.utils.parseEther("10"); // Amount of ETH to fund the user with
            const txFund = await deployer.sendTransaction({
                to: user.address,
                value: fundAmount
            });
            await txFund.wait();

            const currSeiBal = await user.getBalance()
            console.log(`Funded user account ${user.addrses} with ${hre.ethers.utils.formatEther(currSeiBal)} sei`);

            const fee = 3000; // Fee tier (0.3%)

            // Perform a Swap
            const amountIn = hre.ethers.utils.parseEther("1");
            const amountOutMin = hre.ethers.utils.parseEther("0"); // Minimum amount of MockToken expected

            const deposit = await weth9.connect(user).deposit({ value: amountIn });
            await deposit.wait();

            const weth9balance = await weth9.connect(user).balanceOf(user.address);
            // Change to expect
            expect(weth9balance).to.equal(amountIn)

            const approval = await weth9.connect(user).approve(router.address, amountIn);
            await approval.wait();

            const allowance = await weth9.allowance(user.address, router.address);
            // Change to expect
            expect(allowance).to.equal(amountIn)
            
            const tx = await router.connect(user).exactInputSingle({
                tokenIn: weth9.address,
                tokenOut: token.address,
                fee,
                recipient: user.address,
                deadline: Math.floor(Date.now() / 1000) + 60 * 10, // 10 minutes from now
                amountIn,
                amountOutMinimum: amountOutMin,
                sqrtPriceLimitX96: 0
            });

            await tx.wait();

            // Check User's MockToken Balance
            const balance = await token.balanceOf(user.address);
            // Check that it's more than 0 (no specified amount since there might be slippage)
            expect(balance).to.greaterThan(0)
        })

    })
})