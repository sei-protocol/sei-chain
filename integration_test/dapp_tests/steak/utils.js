const { execute, getSeiAddress } = require("../../../contracts/test/lib");
const {v4: uuidv4} = require("uuid");

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

async function setupAccountWithMnemonic(baseName, mnemonic, path, deployer) {
  const uniqueName = `${baseName}-${uuidv4()}`;
  const address = await getSeiAddress(deployer.address)

  return await addDeployerAccount(uniqueName, address, mnemonic, path)
}

async function addDeployerAccount(keyName, address, mnemonic, path) {
  // First try to retrieve by address
  try {
    const output = await execute(`seid keys show ${address} --output json`);
    return JSON.parse(output);
  } catch (e) {
    console.log(e)
  }

  // Since the address doesn't exist, create the key with random name
  try {
    const output = await execute(`printf "${mnemonic}" | seid keys add ${keyName}-${Date.now()} --recover --hd-path "${path}" --keyring-backend test`)
    if (output.code !== 0) {
      throw new Error(output.raw_log);
    }
  }
  catch (e) {
    console.log("Key doesn't exist", e);
  }

  const output = await execute(`seid keys show ${keyName} --output json`);
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
};
