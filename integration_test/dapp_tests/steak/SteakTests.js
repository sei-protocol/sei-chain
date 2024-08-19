const {
  storeWasm,
  deployErc20PointerForCw20,
  ABI,
  getEvmAddress,
  fundSeiAddress,
  associateKey,
  execute,
  isDocker,
} = require("../../../contracts/test/lib.js");
const {
  getValidators,
  instantiateHubContract,
  bond,
  addAccount,
  queryTokenBalance,
  unbond,
  transferTokens,
  setupAccountWithMnemonic,
  sendFunds
} = require("../utils.js");

const { expect } = require("chai");
const { v4: uuidv4 } = require("uuid");
const hre = require("hardhat");
const {chainIds, rpcUrls, evmRpcUrls} = require("../constants");
const path = require("path");

const testChain = process.env.DAPP_TEST_ENV;

describe("Steak", async function () {
  let owner;
  let hubAddress;
  let tokenAddress;
  let tokenPointer;
  let originalSeidConfig;

  async function setupAccount(baseName, associate = true, amount="100000000000", denom="usei", funder='admin') {
    const uniqueName = `${baseName}-${uuidv4()}`;

    const account = await addAccount(uniqueName);
    await fundSeiAddress(account.address, amount, denom, funder);
    if (associate) {
      await associateKey(account.address);
    }

    return account;
  }

  async function deployContracts(ownerAddress) {
    // Store CW20 token wasm
    const STEAK_TOKEN_WASM = (await isDocker()) ? '../integration_test/dapp_tests/steak/contracts/steak_token.wasm' : path.resolve(__dirname, '../steak/contracts/steak_token.wasm')
    const tokenCodeId = await storeWasm(STEAK_TOKEN_WASM, ownerAddress);

    // Store Hub contract
    const STEAK_HUB_WASM = (await isDocker()) ? '../integration_test/dapp_tests/steak/contracts/steak_hub.wasm' : path.resolve(__dirname, '../steak/contracts/steak_hub.wasm')
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

  async function testBonding(address, amount) {
    const initialBalance = await queryTokenBalance(tokenAddress, address);
    expect(initialBalance).to.equal("0");

    await bond(hubAddress, address, amount);
    const tokenBalance = await queryTokenBalance(tokenAddress, address);
    expect(tokenBalance).to.equal(`${amount}`);
  }

  async function testUnbonding(address, amount) {
    const initialBalance = await queryTokenBalance(tokenAddress, address);
    const response = await unbond(hubAddress, tokenAddress, address, amount);
    expect(response.code).to.equal(0);

    // Balance should be updated
    const tokenBalance = await queryTokenBalance(tokenAddress, address);
    expect(tokenBalance).to.equal(`${Number(initialBalance) - amount}`);
  }

  before(async function () {

    const seidConfig = await execute('seid config');
    originalSeidConfig = JSON.parse(seidConfig);

    // Set up the owner account
    if (testChain === 'seilocal') {
      owner = await setupAccount("steak-owner");
    } else {
      // Set default seid config to the specified rpc url.
      await execute(`seid config chain-id ${chainIds[testChain]}`)
      await execute(`seid config node ${rpcUrls[testChain]}`)

      const accounts = hre.config.networks[testChain].accounts
      const deployerWallet = hre.ethers.Wallet.fromMnemonic(accounts.mnemonic, accounts.path);
      const deployer = deployerWallet.connect(hre.ethers.provider)

      await sendFunds('0.01', deployer.address, deployer)
      // Set the config keyring to 'test' since we're using the key added to test from here.
      owner = await setupAccountWithMnemonic("steak-owner", accounts.mnemonic, deployer)
    }

    await execute(`seid config keyring-backend test`);

    // Store and deploy contracts
    ({ hubAddress, tokenAddress, tokenPointer } = await deployContracts(
      owner.address
    ));
  });

  describe("Bonding and unbonding", async function () {
    it("Associated account should be able to bond and unbond", async function () {
      const amount = 1000000;
      await testBonding(owner.address, amount);

      // Verify that address is associated
      const evmAddress = await getEvmAddress(owner.address);
      expect(evmAddress).to.not.be.empty;

      // Check pointer balance
      const pointerBalance = await tokenPointer.balanceOf(evmAddress);
      expect(pointerBalance).to.equal(`${amount}`);

      await testUnbonding(owner.address, 500000);
    });

    it("Unassociated account should be able to bond", async function () {
      const unassociatedAccount = await setupAccount("unassociated", false, '2000000', 'usei', owner.address);
      // Verify that account is not associated yet
      const initialEvmAddress = await getEvmAddress(
        unassociatedAccount.address
      );
      expect(initialEvmAddress).to.be.empty;

      await testBonding(unassociatedAccount.address, 1000000);

      // Account should now be associated
      const evmAddress = await getEvmAddress(unassociatedAccount.address);
      expect(evmAddress).to.not.be.empty;

      // Send tokens to a new unassociated account
      const newUnassociatedAccount = await setupAccount("unassociated", false, '2000000', 'usei', owner.address);
      const transferAmount = 500000;
      await transferTokens(
        tokenAddress,
        unassociatedAccount.address,
        newUnassociatedAccount.address,
        transferAmount
      );
      const tokenBalance = await queryTokenBalance(
        tokenAddress,
        newUnassociatedAccount.address
      );
      expect(tokenBalance).to.equal(`${transferAmount}`);

      // Try unbonding on unassociated account
      await testUnbonding(newUnassociatedAccount.address, transferAmount / 2);
    });
  });

  after(async function () {
    // Set the chain back to regular state
    console.log(`Resetting to ${originalSeidConfig}`)
    await execute(`seid config chain-id ${originalSeidConfig["chain-id"]}`)
    await execute(`seid config node ${originalSeidConfig["node"]}`)
    await execute(`seid config keyring-backend ${originalSeidConfig["keyring-backend"]}`)
  })
});
