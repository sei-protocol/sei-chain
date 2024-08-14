const {
  storeWasm,
  deployErc20PointerForCw20,
  ABI,
  getEvmAddress,
  fundSeiAddress,
  associateKey,
} = require("../../../contracts/test/lib.js");
const {
  getValidators,
  instantiateHubContract,
  bond,
  addAccount,
  queryTokenBalance,
  unbond,
  transferTokens,
} = require("./utils.js");

const { expect } = require("chai");
const { v4: uuidv4 } = require("uuid");

const STEAK_HUB_WASM =
  "../integration_test/dapp_tests/steak/contracts/steak_hub.wasm";
const STEAK_TOKEN_WASM =
  "../integration_test/dapp_tests/steak/contracts/steak_token.wasm";

describe("Steak", async function () {
  let owner;
  let hubAddress;
  let tokenAddress;
  let tokenPointer;

  async function setupAccount(baseName, associate = true) {
    const uniqueName = `${baseName}-${uuidv4()}`;
    const account = await addAccount(uniqueName);
    await fundSeiAddress(account.address);
    if (associate) {
      await associateKey(account.address);
    }
    return account;
  }

  async function deployContracts(ownerAddress) {
    // Store CW20 token wasm
    const tokenCodeId = await storeWasm(STEAK_TOKEN_WASM);

    // Store Hub contract
    const hubCodeId = await storeWasm(STEAK_HUB_WASM);

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
      contractAddresses.tokenContract
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
    // Set up the owner account
    owner = await setupAccount("steak-owner");

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
      const unassociatedAccount = await setupAccount("unassociated", false);
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
      const newUnassociatedAccount = await setupAccount("unassociated", false);
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
});
