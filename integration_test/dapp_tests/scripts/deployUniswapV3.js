const hre = require("hardhat"); // Require Hardhat Runtime Environment

const { abi: WETH9_ABI, bytecode: WETH9_BYTECODE } = require("@uniswap/v2-periphery/build/WETH9.json");
const { abi: FACTORY_ABI, bytecode: FACTORY_BYTECODE } = require("@uniswap/v3-core/artifacts/contracts/UniswapV3Factory.sol/UniswapV3Factory.json");
const { abi: DESCRIPTOR_ABI, bytecode: DESCRIPTOR_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/libraries/NFTDescriptor.sol/NFTDescriptor.json");
const { abi: MANAGER_ABI, bytecode: MANAGER_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/NonfungiblePositionManager.sol/NonfungiblePositionManager.json");
const { abi: SWAP_ROUTER_ABI, bytecode: SWAP_ROUTER_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/SwapRouter.sol/SwapRouter.json");

async function main() {
    const [deployer] = await hre.ethers.getSigners();
    // Deploy Required Tokens

    // Deploy MockToken
    console.log("Deploying MockToken with the account:", deployer.address);
    const MockERC20 = await hre.ethers.getContractFactory("MockERC20");
    const token = await MockERC20.deploy("MockToken", "MKT", hre.ethers.utils.parseEther("1000000"));
    await token.deployed();
    console.log("MockToken deployed to:", token.address);

    // Deploy WETH9 Token (ETH representation on Uniswap)
    console.log("Deploying WETH9 with the account:", deployer.address);
    const WETH9 = new hre.ethers.ContractFactory(WETH9_ABI, WETH9_BYTECODE, deployer);
    const weth9 = await WETH9.deploy();
    await weth9.deployed();
    console.log("WETH9 deployed to:", weth9.address);

    // Deploy NFT Descriptor. These NFTs are used by the NonFungiblePositionManager to represent liquidity positions.
    console.log("Deploying NFT Descriptor with the account:", deployer.address);
    const NFTDescriptor = new hre.ethers.ContractFactory(DESCRIPTOR_ABI, DESCRIPTOR_BYTECODE, deployer);
    const descriptor = await NFTDescriptor.deploy();
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
    const manager = await NonfungiblePositionManager.deploy(factory.address, weth9.address, descriptor.address);
    await manager.deployed();
    console.log("NonfungiblePositionManager deployed to:", manager.address);

    // Deploy SwapRouter
    console.log("Deploying SwapRouter with the account:", deployer.address);
    const SwapRouter = new hre.ethers.ContractFactory(SWAP_ROUTER_ABI, SWAP_ROUTER_BYTECODE, deployer);
    const router = await SwapRouter.deploy(factoryAddress, wethAddress);
    await router.deployed();
    console.log("SwapRouter deployed to:", router.address);
  }
  
  main()
    .then(() => process.exit(0))
    .catch((error) => {
      console.error(error);
      process.exit(1);
    });