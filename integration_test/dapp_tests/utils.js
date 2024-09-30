const {v4: uuidv4} = require("uuid");
const hre = require("hardhat");
const { ABI, deployErc20PointerForCw20, associateKey, storeWasm, fundSeiAddress, deployErc721PointerForCw721, getSeiAddress, deployErc20PointerNative, deployWasm, execute, delay,createTokenFactoryTokenAndMint, isDocker, fundAddress } = require("../../contracts/test/lib.js");
const path = require('path');
const devnetUniswapConfig = require('./configs/uniswapConfig.json');
const devnetSteakConfig = require('./configs/steakConfig.json');
const devnetNftConfig = require('./configs/nftConfig.json');

const { abi: WETH9_ABI, bytecode: WETH9_BYTECODE } = require("@uniswap/v2-periphery/build/WETH9.json");
const { abi: FACTORY_ABI, bytecode: FACTORY_BYTECODE } = require("@uniswap/v3-core/artifacts/contracts/UniswapV3Factory.sol/UniswapV3Factory.json");
const { abi: DESCRIPTOR_ABI, bytecode: DESCRIPTOR_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/libraries/NFTDescriptor.sol/NFTDescriptor.json");
const { abi: MANAGER_ABI, bytecode: MANAGER_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/NonfungiblePositionManager.sol/NonfungiblePositionManager.json");
const { abi: SWAP_ROUTER_ABI, bytecode: SWAP_ROUTER_BYTECODE } = require("@uniswap/v3-periphery/artifacts/contracts/SwapRouter.sol/SwapRouter.json");
const {chainIds, rpcUrls, evmRpcUrls} = require("./constants");
const {expect} = require("chai");
const {existsSync, readFileSync, writeFileSync} = require("node:fs");

async function deployTokenPool(managerContract, firstTokenAddr, secondTokenAddr, swapRatio=1, fee=3000) {
  const sqrtPriceX96 = BigInt(Math.sqrt(swapRatio) * (2 ** 96)); // Initial price (1:1)

  const [token0, token1] = tokenOrder(firstTokenAddr, secondTokenAddr);

  await estimateAndCall(managerContract, "createAndInitializePoolIfNecessary", [token0.address, token1.address, fee, sqrtPriceX96])
  // token0 addr must be < token1 addr
  console.log("Pool created and initialized");
}

// Supplies liquidity to then given pools. The signer connected to the contracts must have the prerequisite tokens or this will fail.
async function supplyLiquidity(managerContract, recipientAddr, firstTokenContract, secondTokenContract, firstTokenAmt=100, secondTokenAmt=100) {
  // Define the amount of tokens to be approved and added as liquidity
  console.log("Supplying liquidity to pool")
  const [token0, token1] = tokenOrder(firstTokenContract.address, secondTokenContract.address, firstTokenAmt, secondTokenAmt);

  // Approve the NonfungiblePositionManager to spend the specified amount of firstToken
  await estimateAndCall(firstTokenContract, "approve", [managerContract.address, firstTokenAmt]);
  let allowance = await firstTokenContract.allowance(recipientAddr, managerContract.address);
  let balance = await firstTokenContract.balanceOf(recipientAddr);


  // Approve the NonfungiblePositionManager to spend the specified amount of secondToken
  await estimateAndCall(secondTokenContract, "approve", [managerContract.address, secondTokenAmt])
  // Add liquidity to the pool
  await estimateAndCall(managerContract, "mint", [{
    token0: token0.address,
    token1: token1.address,
    fee: 3000, // Fee tier (0.3%)
    tickLower: -887220,
    tickUpper: 887220,
    amount0Desired: token0.amount,
    amount1Desired: token1.amount,
    amount0Min: 0,
    amount1Min: 0,
    recipient: recipientAddr,
    deadline: Math.floor(Date.now() / 1000) + 60 * 10, // 10 minutes from now
  }]);
}

// Orders the 2 addresses sequentially, since this is required by uniswap.
function tokenOrder(firstTokenAddr, secondTokenAddr, firstTokenAmount=0, secondTokenAmount=0) {
  let token0;
  let token1;
  if (parseInt(firstTokenAddr, 16) < parseInt(secondTokenAddr, 16)) {
    token0= {address: firstTokenAddr, amount: firstTokenAmount};
    token1 = {address: secondTokenAddr, amount: secondTokenAmount};
  } else {
    token0 = {address: secondTokenAddr, amount: secondTokenAmount};
    token1 = {address: firstTokenAddr, amount: firstTokenAmount};
  }
  return [token0, token1]
}

async function returnContractsForFastTrackUniswap(deployer, config, testChain) {
  const contractArtifact = await hre.artifacts.readArtifact("MockERC20");
  return {
    manager: new hre.ethers.Contract(config.manager, MANAGER_ABI, deployer),
    router: new hre.ethers.Contract(config.router, SWAP_ROUTER_ABI, deployer),
    erc20TokenFactory: new hre.ethers.Contract(config.erc20TokenFactory, ABI.ERC20, deployer),
    erc20cw20: new hre.ethers.Contract(config.erc20cw20, ABI.ERC20, deployer),
    weth9: new hre.ethers.Contract(config.weth9, WETH9_ABI, deployer),
    token: new hre.ethers.Contract(config.token, contractArtifact.abi, deployer),
    tokenFactoryDenom: config.tokenFactoryDenom,
    cw20Address: config.cw20Address
  }
}

async function returnContractsForFastTrackSteak(deployer, config, testChain) {
  return {
    hubAddress: config.hubAddress,
    tokenAddress: config.tokenAddress,
    tokenPointer: new hre.ethers.Contract(
      config.pointerAddress,
      ABI.ERC20,
      hre.ethers.provider
    )
  }
}

async function deployAndReturnUniswapContracts(deployer, testChain, accounts) {
  if (testChain === 'devnetFastTrack') {
    console.log('Using already deployed contracts on arctic 1');
    return returnContractsForFastTrackUniswap(deployer, devnetUniswapConfig);
  } else if (testChain === 'seiClusterFastTrack') {
    if (clusterConfigExists('uniswapConfigCluster.json')) {
      const contractConfig =  path.join(__dirname, 'configs', 'uniswapConfigCluster.json');
      const clusterConfig = JSON.parse(readFileSync(contractConfig, 'utf8'));
      return returnContractsForFastTrackUniswap(deployer, clusterConfig, testChain);
    } else {
      return writeAddressesIntoUniswapConfig(deployer, testChain, accounts);
    }
  }
  return deployUniswapContracts(deployer, testChain, accounts);
}

async function writeAddressesIntoUniswapConfig(deployer, testChain, accounts){
  const contracts = await deployUniswapContracts(deployer, testChain, accounts);
  const contractAddresses = {
    manager: contracts.manager.address,
    router: contracts.router.address,
    erc20TokenFactory: contracts.erc20TokenFactory.address,
    erc20cw20: contracts.erc20cw20.address,
    weth9: contracts.weth9.address,
    token: contracts.token.address,
    tokenFactoryDenom: contracts.tokenFactoryDenom,
    cw20Address: contracts.cw20Address
  };
  writeFileSync('./configs/uniswapConfigCluster.json', JSON.stringify(contractAddresses, null, 2), 'utf8');
  console.log('contract addresses are saved');
  return contracts;
}

async function writeAddressesIntoSteakConfig(testChain){
  const contracts = await deployContractsForSteakTests(testChain);
  const contractAddresses = {
    hubAddress: contracts.hubAddress,
    tokenAddress: contracts.tokenAddress,
    pointerAddress: contracts.tokenPointer.address
  };
  writeFileSync('./configs/steakConfigCluster.json', JSON.stringify(contractAddresses, null, 2), 'utf8');
  console.log('contract addresses are saved');
  return contracts;
}

async function writeAddressesIntoNftConfig(deployer, testChain, accounts){
  const contracts = await deployContractsForNftTests(deployer, testChain, accounts);
  const contractAddresses = {
    marketplace: contracts.marketplace.address,
    erc721token: contracts.erc721token.address,
    cw721Address: contracts.cw721Address,
    erc721PointerToken: contracts.erc721PointerToken.address,
  };
  writeFileSync('./configs/nftConfigCluster.json', JSON.stringify(contractAddresses, null, 2), 'utf8');
  console.log('contract addresses are saved');
  return contracts;
}

async function deployUniswapContracts(deployer, testChain, accounts){

  if (testChain === 'seilocal') {
    const tx = await fundAddress(deployer.address, amount="2000000000000000000000");
    await waitFor(1);
  }

  // Set the config keyring to 'test' since we're using the key added to test from here.
  await execute(`seid config keyring-backend test`)

  await sendFunds('0.01', deployer.address, deployer)
  await setupAccountWithMnemonic("dapptest", accounts.mnemonic, deployer);

  const deployerSeiAddr = await getSeiAddress(deployer.address);
  // Deploy Required Tokens
  const time = Date.now().toString();

  // Deploy TokenFactory token with ERC20 pointer
  const tokenName = `dappTests${time}`
  const tokenFactoryDenom = await createTokenFactoryTokenAndMint(tokenName, hre.ethers.utils.parseEther("1000000").toString(), deployerSeiAddr, deployerSeiAddr)
  console.log("DENOM", tokenFactoryDenom)
  const pointerAddr = await deployErc20PointerNative(hre.ethers.provider, tokenFactoryDenom, deployerSeiAddr, evmRpcUrls[testChain])
  console.log("Pointer Addr", pointerAddr);
  const erc20TokenFactory = new hre.ethers.Contract(pointerAddr, ABI.ERC20, deployer);

  // Deploy CW20 token with ERC20 pointer
  const cw20Details = await deployCw20WithPointer(deployerSeiAddr, deployer, time, evmRpcUrls[testChain])
  const erc20cw20 = cw20Details.pointerContract;
  const cw20Address = cw20Details.cw20Address;

  // Deploy WETH9 Token (ETH representation on Uniswap)
  const weth9 = await deployEthersContract("WETH9", WETH9_ABI, WETH9_BYTECODE, deployer);

  // Deploy MockToken
  console.log("Deploying MockToken with the account:", deployer.address);
  const contractArtifact = await hre.artifacts.readArtifact("MockERC20");
  const token = await deployEthersContract("MockToken", contractArtifact.abi, contractArtifact.bytecode, deployer, ["MockToken", "MKT", hre.ethers.utils.parseEther("1000000")])

  // Deploy NFT Descriptor. These NFTs are used by the NonFungiblePositionManager to represent liquidity positions.
  const descriptor = await deployEthersContract("NFT Descriptor", DESCRIPTOR_ABI, DESCRIPTOR_BYTECODE, deployer);

  // Deploy Uniswap Contracts
  // Create UniswapV3 Factory
  const factory = await deployEthersContract("Uniswap V3 Factory", FACTORY_ABI, FACTORY_BYTECODE, deployer);

  // Deploy NonFungiblePositionManager
  const manager = await deployEthersContract("NonfungiblePositionManager", MANAGER_ABI, MANAGER_BYTECODE, deployer, deployParams=[factory.address, weth9.address, descriptor.address]);

  // Deploy SwapRouter
  const router = await deployEthersContract("SwapRouter", SWAP_ROUTER_ABI, SWAP_ROUTER_BYTECODE, deployer, deployParams=[factory.address, weth9.address]);

  const amountETH = hre.ethers.utils.parseEther("3")

  // Gets the amount of WETH9 required to instantiate pools by depositing Sei to the contract
  let gasEstimate = await weth9.estimateGas.deposit({ value: amountETH })
  let gasPrice = await deployer.getGasPrice();
  const txWrap = await weth9.deposit({ value: amountETH, gasPrice, gasLimit: gasEstimate });
  await txWrap.wait();
  console.log(`Deposited ${amountETH.toString()} to WETH9`);

  // Create liquidity pools
  await deployTokenPool(manager, weth9.address, token.address)
  await deployTokenPool(manager, weth9.address, erc20TokenFactory.address)
  await deployTokenPool(manager, weth9.address, erc20cw20.address)

  // Add Liquidity to pools
  await supplyLiquidity(manager, deployer.address, weth9, token, hre.ethers.utils.parseEther("1"), hre.ethers.utils.parseEther("1"))
  await supplyLiquidity(manager, deployer.address, weth9, erc20TokenFactory, hre.ethers.utils.parseEther("1"), hre.ethers.utils.parseEther("1"))
  await supplyLiquidity(manager, deployer.address, weth9, erc20cw20, hre.ethers.utils.parseEther("1"), hre.ethers.utils.parseEther("1"))

  return {
    router,
    manager,
    erc20cw20,
    erc20TokenFactory,
    weth9,
    token,
    tokenFactoryDenom,
    cw20Address
  }
}

function clusterConfigExists(fileName){
  const folderPath = path.join(__dirname, 'configs', fileName);
  return existsSync(folderPath)
}

async function deployCw20WithPointer(deployerSeiAddr, signer, time, evmRpc="") {
  const CW20_BASE_PATH = (await isDocker()) ? '../integration_test/dapp_tests/uniswap/cw20_base.wasm' : path.resolve(__dirname, '../dapp_tests/uniswap/cw20_base.wasm')
  const cw20Address = await deployWasm(CW20_BASE_PATH, deployerSeiAddr, "cw20", {
    name: `testCw20${time}`,
    symbol: "TEST",
    decimals: 6,
    initial_balances: [
      { address: deployerSeiAddr, amount: hre.ethers.utils.parseEther("1000000").toString() }
    ],
    mint: {
      "minter": deployerSeiAddr, "cap": hre.ethers.utils.parseEther("10000000").toString()
    }
  }, deployerSeiAddr);
  const pointerAddr = await deployErc20PointerForCw20(hre.ethers.provider, cw20Address, 10, deployerSeiAddr, evmRpc);
  const pointerContract = new hre.ethers.Contract(pointerAddr, ABI.ERC20, signer);
  return {"pointerContract": pointerContract, "cw20Address": cw20Address}
}

async function deployCw721WithPointer(deployerSeiAddr, signer, time, evmRpc="") {
  const CW721_BASE_PATH = (await isDocker()) ? '../integration_test/dapp_tests/nftMarketplace/cw721_base.wasm' : path.resolve(__dirname, '../dapp_tests/nftMarketplace/cw721_base.wasm')
  const cw721Address = await deployWasm(CW721_BASE_PATH, deployerSeiAddr, "cw721", {
    "name": `testCw721${time}`,
    "symbol": "TESTNFT",
    "minter": deployerSeiAddr,
    "withdraw_address": deployerSeiAddr,
  }, deployerSeiAddr);
  const pointerAddr = await deployErc721PointerForCw721(hre.ethers.provider, cw721Address, deployerSeiAddr, evmRpc);
  const pointerContract = new hre.ethers.Contract(pointerAddr, ABI.ERC721, signer);
  return {"pointerContract": pointerContract, "cw721Address": cw721Address}
}

async function deployEthersContract(name, abi, bytecode, deployer, deployParams=[]) {
  const contract = new hre.ethers.ContractFactory(abi, bytecode, deployer);
  const deployTx = contract.getDeployTransaction(...deployParams);
  const gasEstimate = await deployer.estimateGas(deployTx);
  const gasPrice = await deployer.getGasPrice();
  const deployed = await contract.deploy(...deployParams, {gasPrice, gasLimit: gasEstimate});
  await deployed.deployed();
  console.log(`${name} deployed to:`, deployed.address);
  return deployed;
}

async function doesTokenFactoryDenomExist(denom) {
  const output = await execute(`seid q tokenfactory denom-authority-metadata ${denom} --output json`);
  const parsed = JSON.parse(output);

  return parsed.authority_metadata.admin !== "";
}

async function sendFunds(amountSei, recipient, signer) {
  const bal = await signer.getBalance();
  if (bal.lt(hre.ethers.utils.parseEther(amountSei))) {
    throw new Error(`Signer has insufficient balance. Want ${hre.ethers.utils.parseEther(amountSei)}, has ${bal}`);
  }

  const gasLimit = await signer.estimateGas({
    to: recipient,
    value: hre.ethers.utils.parseEther(amountSei)
  })

  // Get current gas price from the network
  const gasPrice = await signer.getGasPrice();

  const fundUser = await signer.sendTransaction({
    to: recipient,
    value: hre.ethers.utils.parseEther(amountSei),
    gasLimit: gasLimit.mul(12).div(10),
    gasPrice: gasPrice,
  })

  await fundUser.wait();
}

async function estimateAndCall(contract, method, args=[], value=0) {
  let gasLimit;
  try {
    if (value) {
      gasLimit = await contract.estimateGas[method](...args, {value: value});
    } else {
      gasLimit = await contract.estimateGas[method](...args);
    }
  } catch (error) {
    if (error.data) {
      console.error("Transaction revert reason:", hre.ethers.utils.toUtf8String(error.data));
    } else {
      console.error("Error fulfilling order:", error);
    }
  }
  const gasPrice = await contract.signer.getGasPrice();
  let output;
  if (value) {
    output = await contract[method](...args, {gasPrice, gasLimit, value})
  } else {
    output = await contract[method](...args, {gasPrice, gasLimit})
  }
  await output.wait();
  return output;
}

const mintCw721 = async (contractAddress, address, id) => {
  const msg = {
    mint: {
      token_id: `${id}`,
      owner: `${address}`,
      token_uri:""
    },
  };
  const jsonString = JSON.stringify(msg).replace(/"/g, '\\"');
  const command = `seid tx wasm execute ${contractAddress} "${jsonString}" --from=${address} --gas=500000 --gas-prices=0.1usei --broadcast-mode=block -y --output=json`;
  const output = await execute(command);
  const response = JSON.parse(output);
  if (response.code !== 0) {
    throw new Error(response.raw_log);
  }
  return response;
};

async function pollBalance(erc20Contract, address, criteria, maxAttempts=3) {
  let bal = 0;
  let attempt = 1;
  while (attempt === 1 || attempt <= maxAttempts) {
    bal = await erc20Contract.balanceOf(address);
    attempt++;
    if (criteria(bal)) {
      return bal;
    }
    await delay();
  }

  return bal;
}

const encodeBase64 = (obj) => {
  return Buffer.from(JSON.stringify(obj)).toString("base64");
};

const getValidators = async () => {
  const command = `seid q staking validators --output json`;
  const output = await execute(command);
  const response = JSON.parse(output);
  return response.validators.map((v) => v.operator_address);
};

const getCodeIdFromContractAddress = async (contractAddress) => {
  const command = `seid q wasm contract ${contractAddress} --output json`;
  const output = await execute(command);
  const response = JSON.parse(output);
  return response.contract_info.code_id;
};

// Note: Not using the `deployWasm` function because we need to retrieve the
// hub and token contract addresses from the event logs
const instantiateHubContract = async (
  codeId,
  adminAddress,
  instantiateMsg,
  label
) => {
  const jsonString = JSON.stringify(instantiateMsg).replace(/"/g, '\\"');
  const command = `seid tx wasm instantiate ${codeId} "${jsonString}" --label ${label} --admin ${adminAddress} --from ${adminAddress} --gas=5000000 --fees=1000000usei -y --broadcast-mode block -o json`;
  const output = await execute(command);
  const response = JSON.parse(output);
  // Get all attributes with _contractAddress
  if (!response.logs || response.logs.length === 0) {
    throw new Error("logs not returned");
  }
  const addresses = [];
  for (let event of response.logs[0].events) {
    if (event.type === "instantiate") {
      for (let attribute of event.attributes) {
        if (attribute.key === "_contract_address") {
          addresses.push(attribute.value);
        }
      }
    }
  }

  // Return hub and token contracts
  const contracts = {};
  for (let address of addresses) {
    const contractCodeId = await getCodeIdFromContractAddress(address);
    if (contractCodeId === `${codeId}`) {
      contracts.hubContract = address;
    } else {
      contracts.tokenContract = address;
    }
  }
  return contracts;
};

const bond = async (contractAddress, address, amount) => {
  const msg = {
    bond: {},
  };
  const jsonString = JSON.stringify(msg).replace(/"/g, '\\"');
  const command = `seid tx wasm execute ${contractAddress} "${jsonString}" --amount=${amount}usei --from=${address} --gas=600000 --gas-prices=0.1usei --broadcast-mode=block -y --output=json`;
  const output = await execute(command);
  const response = JSON.parse(output);
  if (response.code !== 0) {
    throw new Error(response.raw_log);
  }
  return response;
};

const unbond = async (hubAddress, tokenAddress, address, amount) => {
  const msg = {
    send: {
      contract: hubAddress,
      amount: `${amount}`,
      msg: encodeBase64({
        queue_unbond: {},
      }),
    },
  };
  const jsonString = JSON.stringify(msg).replace(/"/g, '\\"');
  const command = `seid tx wasm execute ${tokenAddress} "${jsonString}" --from=${address} --gas=500000 --gas-prices=0.1usei --broadcast-mode=block -y --output=json`;
  const output = await execute(command);
  const response = JSON.parse(output);
  if (response.code !== 0) {
    throw new Error(response.raw_log);
  }
  return response;
};

const harvest = async (contractAddress, address) => {
  const msg = {
    harvest: {},
  };
  const jsonString = JSON.stringify(msg).replace(/"/g, '\\"');
  const command = `seid tx wasm execute ${contractAddress} "${jsonString}" --from=${address} --gas=500000 --gas-prices=0.1usei --broadcast-mode=block -y --output=json`;
  const output = await execute(command);
  const response = JSON.parse(output);
  if (response.code !== 0) {
    throw new Error(response.raw_log);
  }
  return response;
};

const queryTokenBalance = async (contractAddress, address) => {
  const msg = {
    balance: {
      address,
    },
  };
  const jsonString = JSON.stringify(msg).replace(/"/g, '\\"');
  const command = `seid q wasm contract-state smart ${contractAddress} "${jsonString}" --output=json`;
  const output = await execute(command);
  const response = JSON.parse(output);
  return response.data.balance;
};

const addAccount = async (accountName) => {
  const command = `seid keys add ${accountName}-${Date.now()} --output=json --keyring-backend test`;
  const output = await execute(command);
  return JSON.parse(output);
};

const transferTokens = async (tokenAddress, sender, destination, amount) => {
  const msg = {
    transfer: {
      recipient: destination,
      amount: `${amount}`,
    },
  };
  const jsonString = JSON.stringify(msg).replace(/"/g, '\\"');
  const command = `seid tx wasm execute ${tokenAddress} "${jsonString}" --from=${sender} --gas=200000 --gas-prices=0.1usei --broadcast-mode=block -y --output=json`;
  const output = await execute(command);
  const response = JSON.parse(output);
  if (response.code !== 0) {
    throw new Error(response.raw_log);
  }
  return response;
};

async function  setupAccountWithMnemonic(baseName, mnemonic, deployer) {
  const uniqueName = `${baseName}-${uuidv4()}`;
  const address = await getSeiAddress(deployer.address)

  return await addDeployerAccount(uniqueName, address, mnemonic)
}

async function waitFor(seconds){
  return new Promise((resolve) =>{
    setTimeout(() =>{
      resolve();
    }, seconds * 1000)
  })
}

async function addDeployerAccount(keyName, address, mnemonic) {
  // First try to retrieve by address
  try {
    const output = await execute(`seid keys show ${address} --output json --keyring-backend test`);
    return JSON.parse(output);
  } catch (e) {}

  // Since the address doesn't exist, create the key with random name
  try {
    let output;
    if (await isDocker()) {
      // NOTE: The path here is assumed to be "m/44'/118'/0'/0/0"
      output = await execute(`seid keys add ${keyName} --recover --keyring-backend test`,`printf "${mnemonic}"`)
    } else {
      output = await execute(`printf "${mnemonic}" | seid keys add ${keyName} --recover --keyring-backend test`)
    }
  }
  catch (e) {}

  // If both of the calls above fail, this one will fail.
  const output = await execute(`seid keys show ${keyName} --output json --keyring-backend test`);
  return JSON.parse(output);
}

async function setupAccount(baseName, associate = true, amount="100000000000", denom="usei", funder='admin') {
  const uniqueName = `${baseName}-${uuidv4()}`;

  const account = await addAccount(uniqueName);
  await fundSeiAddress(account.address, amount, denom, funder);
  if (associate) {
    await associateKey(account.address);
  }
  return account;
}

async function deployAndReturnContractsForSteakTests(deployer, testChain, accounts){
  const owner = await setupAccountWithMnemonic("steak-owner", accounts.mnemonic, deployer);
  let contracts;
  // Check the test chain type and retrieve or write the contract configuration
  if (testChain === 'devnetFastTrack') {
      console.log('Using already deployed contracts on arctic 1');
    contracts = await returnContractsForFastTrackSteak(deployer, devnetSteakConfig, testChain);
  } else if (testChain === 'seiClusterFastTrack') {
    if (clusterConfigExists('steakConfigCluster.json')) {
      const contractConfigPath = path.join(__dirname, 'configs', 'steakConfigCluster.json');
      const clusterConfig = JSON.parse(readFileSync(contractConfigPath, 'utf8'));
      contracts = await returnContractsForFastTrackSteak(deployer, clusterConfig, testChain);
    } else {
      contracts = await writeAddressesIntoSteakConfig(testChain);
    }
  } else {
    contracts = await deployContractsForSteakTests(testChain);
  }
  return { ...contracts, owner };
}

async function deployContractsForSteakTests(testChain){
  let owner;
  // Set up the owner account
  if (testChain === 'seilocal') {
    owner = await setupAccount("steak-owner");
  } else {
    const accounts = hre.config.networks[testChain].accounts
    const deployerWallet = hre.ethers.Wallet.fromMnemonic(accounts.mnemonic, accounts.path);
    const deployer = deployerWallet.connect(hre.ethers.provider)

    await sendFunds('0.01', deployer.address, deployer)
    // Set the config keyring to 'test' since we're using the key added to test from here.
    owner = await setupAccountWithMnemonic("steak-owner", accounts.mnemonic, deployer)
  }

  await execute(`seid config keyring-backend test`);

  // Store and deploy contracts
  const { hubAddress, tokenAddress, tokenPointer } = await deploySteakContracts(
    owner.address,
    testChain,
  );

  return {hubAddress, tokenAddress, tokenPointer, owner}
}

async function deploySteakContracts(ownerAddress, testChain) {
  // Store CW20 token wasm
  const STEAK_TOKEN_WASM = (await isDocker()) ? '../integration_test/dapp_tests/steak/contracts/steak_token.wasm' : path.resolve(__dirname, 'steak/contracts/steak_token.wasm')
  const tokenCodeId = await storeWasm(STEAK_TOKEN_WASM, ownerAddress);

  // Store Hub contract
  const STEAK_HUB_WASM = (await isDocker()) ? '../integration_test/dapp_tests/steak/contracts/steak_hub.wasm' : path.resolve(__dirname, 'steak/contracts/steak_hub.wasm')
  const hubCodeId = await storeWasm(STEAK_HUB_WASM, ownerAddress);

  // Instantiate hub and token contracts
  const validators = await getValidators();
  const instantiateMsg = {
    cw20_code_id: parseInt(tokenCodeId),
    owner: ownerAddress,
    name: "Steak",
    symbol: "STEAK",
    decimals: 6,
    epoch_period: 259200,
    unbond_period: 1814400,
    validators: validators.slice(0, 3),
  };
  const contractAddresses = await instantiateHubContract(
    hubCodeId,
    ownerAddress,
    instantiateMsg,
    "steakhub"
  );

  // Deploy pointer for token contract
  const pointerAddr = await deployErc20PointerForCw20(
    hre.ethers.provider,
    contractAddresses.tokenContract,
    10,
    ownerAddress,
    evmRpcUrls[testChain]
  );

  const tokenPointer = new hre.ethers.Contract(
    pointerAddr,
    ABI.ERC20,
    hre.ethers.provider
  );

  return {
    hubAddress: contractAddresses.hubContract,
    tokenAddress: contractAddresses.tokenContract,
    tokenPointer,
  };
}

async function deployAndReturnContractsForNftTests(deployer, testChain, accounts){
  let contracts;
  // Check the test chain type and retrieve or write the contract configuration
  if (testChain === 'devnetFastTrack') {
    console.log('Using already deployed contracts on arctic 1');
    return returnContractsForFastTrackNftTests(deployer, devnetNftConfig, testChain);
  } else if (testChain === 'seiClusterFastTrack') {
    // Set chain ID and node configuration for the cluster fast track
    if (clusterConfigExists('nftConfigCluster.json')) {
      const contractConfigPath = path.join(__dirname, 'configs', 'nftConfigCluster.json');
      const clusterConfig = JSON.parse(readFileSync(contractConfigPath, 'utf8'));
      contracts = await returnContractsForFastTrackNftTests(deployer, clusterConfig, testChain);
    } else {
      contracts = await writeAddressesIntoNftConfig(deployer, testChain, accounts);
    }
  } else {
    contracts = await deployContractsForNftTests(deployer, testChain, accounts);
  }
  return contracts;
}

async function deployContractsForNftTests(deployer, testChain, accounts){
  if (testChain === 'seilocal') {
    await fundAddress(deployer.address, amount="2000000000000000000000");
  }

  await execute(`seid config keyring-backend test`)

  await sendFunds('0.01', deployer.address, deployer)
  await setupAccountWithMnemonic("dapptest", accounts.mnemonic, deployer);

  // Deploy MockNFT
  const erc721ContractArtifact = await hre.artifacts.readArtifact("MockERC721");
  const erc721token = await deployEthersContract("MockERC721", erc721ContractArtifact.abi, erc721ContractArtifact.bytecode, deployer, ["MockERC721", "MKTNFT"])

  const numNftsToMint = 50
  await estimateAndCall(erc721token, "batchMint", [deployer.address, numNftsToMint]);

  // Deploy CW721 token with ERC721 pointer
  const time = Date.now().toString();
  const deployerSeiAddr = await getSeiAddress(deployer.address);
  const cw721Details = await deployCw721WithPointer(deployerSeiAddr, deployer, time, evmRpcUrls[testChain])
  const erc721PointerToken = cw721Details.pointerContract;
  const cw721Address = cw721Details.cw721Address;
  const numCwNftsToMint = 2;
  for (let i = 1; i <= numCwNftsToMint; i++) {
    await mintCw721(cw721Address, deployerSeiAddr, i)
  }
  const cwbal = await erc721PointerToken.balanceOf(deployer.address);
  expect(cwbal).to.equal(numCwNftsToMint)

  const nftMarketplaceArtifact = await hre.artifacts.readArtifact("NftMarketplace");
  const marketplace = await deployEthersContract("NftMarketplace", nftMarketplaceArtifact.abi, nftMarketplaceArtifact.bytecode, deployer)
  return {marketplace, erc721token, cw721Address, erc721PointerToken}
}

async function returnContractsForFastTrackNftTests(deployer, clusterConfig,  testChain) {
  await setupAccountWithMnemonic("dapptest", deployer.mnemonic.phrase, deployer);
  const nftMarketplaceArtifact = await hre.artifacts.readArtifact("NftMarketplace");
  const erc721ContractArtifact = await hre.artifacts.readArtifact("MockERC721");
  return {
    marketplace: new hre.ethers.Contract(clusterConfig.marketplace, nftMarketplaceArtifact.abi, deployer),
    erc721token: new hre.ethers.Contract(clusterConfig.erc721token, erc721ContractArtifact.abi, deployer),
    erc721PointerToken: new hre.ethers.Contract(clusterConfig.erc721PointerToken, ABI.ERC721, deployer),
    cw721Address: clusterConfig.cw721Address,
  }
}

async function queryLatestNftIds(contractAddress){
  return Number(
    await execute(`seid q wasm contract-state smart ${contractAddress} '{"num_tokens": {}}' -o json | jq ".data.count"`));
}

async function setDaemonConfig(testChain) {
  const seidConfig = await execute('seid config');
  const originalSeidConfig = JSON.parse(seidConfig);
  await execute(`seid config chain-id ${chainIds[testChain]}`)
  await execute(`seid config node ${rpcUrls[testChain]}`);
  return originalSeidConfig;
}

module.exports = {
  getValidators,
  returnContractsForFastTrackUniswap,
  instantiateHubContract,
  bond,
  unbond,
  harvest,
  queryTokenBalance,
  addAccount,
  estimateAndCall,
  deployAndReturnUniswapContracts,
  addDeployerAccount,
  setupAccountWithMnemonic,
  transferTokens,
  deployTokenPool,
  supplyLiquidity,
  deployCw20WithPointer,
  deployCw721WithPointer,
  deployEthersContract,
  doesTokenFactoryDenomExist,
  pollBalance,
  sendFunds,
  mintCw721,
  returnContractsForFastTrackSteak,
  deployContractsForSteakTests,
  setupAccount,
  returnContractsForFastTrackNftTests,
  deployContractsForNftTests,
  deployAndReturnContractsForSteakTests,
  deployAndReturnContractsForNftTests,
  waitFor,
  queryLatestNftIds,
  setDaemonConfig,
};