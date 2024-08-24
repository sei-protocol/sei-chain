const {v4: uuidv4} = require("uuid");
const hre = require("hardhat");
const { ABI, deployErc20PointerForCw20, getSeiAddress, deployWasm, execute, delay, isDocker } = require("../../contracts/test/lib.js");
const path = require('path')

async function deployTokenPool(managerContract, firstTokenAddr, secondTokenAddr, swapRatio=1, fee=3000) {
  const sqrtPriceX96 = BigInt(Math.sqrt(swapRatio) * (2 ** 96)); // Initial price (1:1)

  const gasPrice = await hre.ethers.provider.getGasPrice();
  const [token0, token1] = tokenOrder(firstTokenAddr, secondTokenAddr);

  let gasLimit = await managerContract.estimateGas.createAndInitializePoolIfNecessary(
      token0.address,
      token1.address,
      fee,
      sqrtPriceX96,
  );

  gasLimit = gasLimit.mul(12).div(10)
  // token0 addr must be < token1 addr
  const poolTx = await managerContract.createAndInitializePoolIfNecessary(
      token0.address,
      token1.address,
      fee,
      sqrtPriceX96,
      {gasLimit, gasPrice}
  );
  await poolTx.wait();
  console.log("Pool created and initialized");
}

// Supplies liquidity to then given pools. The signer connected to the contracts must have the prerequisite tokens or this will fail.
async function supplyLiquidity(managerContract, recipientAddr, firstTokenContract, secondTokenContract, firstTokenAmt=100, secondTokenAmt=100) {
  // Define the amount of tokens to be approved and added as liquidity
  console.log("Supplying liquidity to pool")
  const [token0, token1] = tokenOrder(firstTokenContract.address, secondTokenContract.address, firstTokenAmt, secondTokenAmt);
  const gasPrice = await hre.ethers.provider.getGasPrice();

  let gasLimit = await firstTokenContract.estimateGas.approve(managerContract.address, firstTokenAmt);
  gasLimit = gasLimit.mul(12).div(10)

  // Approve the NonfungiblePositionManager to spend the specified amount of firstToken
  const approveFirstTokenTx = await firstTokenContract.approve(managerContract.address, firstTokenAmt, {gasLimit, gasPrice});
  await approveFirstTokenTx.wait();
  let allowance = await firstTokenContract.allowance(recipientAddr, managerContract.address);
  let balance = await firstTokenContract.balanceOf(recipientAddr);

  gasLimit = await secondTokenContract.estimateGas.approve(managerContract.address, secondTokenAmt);
  gasLimit = gasLimit.mul(12).div(10)

  // Approve the NonfungiblePositionManager to spend the specified amount of secondToken
  const approveSecondTokenTx = await secondTokenContract.approve(managerContract.address, secondTokenAmt, {gasLimit, gasPrice});
  await approveSecondTokenTx.wait();

  allowance = await secondTokenContract.allowance(recipientAddr, managerContract.address);
  balance = await secondTokenContract.balanceOf(recipientAddr);

  gasLimit = await managerContract.estimateGas.mint({
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
  })

  gasLimit = gasLimit.mul(12).div(10)

  // Add liquidity to the pool
  const liquidityTx = await managerContract.mint({
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
  }, {gasLimit, gasPrice});

  await liquidityTx.wait();
  console.log("Liquidity added");
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
  const command = `seid tx wasm execute ${contractAddress} "${jsonString}" --amount=${amount}usei --from=${address} --gas=500000 --gas-prices=0.1usei --broadcast-mode=block -y --output=json`;
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

async function setupAccountWithMnemonic(baseName, mnemonic, deployer) {
  const uniqueName = `${baseName}-${uuidv4()}`;
  const address = await getSeiAddress(deployer.address)

  return await addDeployerAccount(uniqueName, address, mnemonic)
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

module.exports = {
  getValidators,
  instantiateHubContract,
  bond,
  unbond,
  harvest,
  queryTokenBalance,
  addAccount,
  addDeployerAccount,
  setupAccountWithMnemonic,
  transferTokens,
  deployTokenPool,
  supplyLiquidity,
  deployCw20WithPointer,
  deployEthersContract,
  doesTokenFactoryDenomExist,
  pollBalance,
  sendFunds
};