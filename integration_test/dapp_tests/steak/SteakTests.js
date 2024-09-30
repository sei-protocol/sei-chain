const {
    getEvmAddress,
    execute,
} = require("../../../contracts/test/lib.js");
const {
    bond,
    queryTokenBalance,
    unbond,
    transferTokens,
} = require("../utils.js");

const {expect} = require("chai");
const {v4: uuidv4} = require("uuid");
const hre = require("hardhat");
const {
    setupAccount,
    deployAndReturnContractsForSteakTests, setDaemonConfig
} = require("../utils");

const testChain = process.env.DAPP_TEST_ENV;

describe("Steak", async function () {
    let owner;
    let hubAddress;
    let tokenAddress;
    let tokenPointer;
    let originalSeidConfig;

    async function testBonding(address, amount) {
        const initialBalance = await queryTokenBalance(tokenAddress, address);
        await bond(hubAddress, address, amount);
        const tokenBalance = await queryTokenBalance(tokenAddress, address);
        const expectedBalance = Number(initialBalance) + amount;
        expect(tokenBalance).to.equal(`${expectedBalance}`);
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
        originalSeidConfig = await setDaemonConfig(testChain);
        const accounts = hre.config.networks[testChain].accounts
        const deployerWallet = hre.ethers.Wallet.fromMnemonic(accounts.mnemonic, accounts.path);
        const deployer = deployerWallet.connect(hre.ethers.provider);
        ({
            hubAddress,
            tokenAddress,
            tokenPointer,
            owner
        } = await deployAndReturnContractsForSteakTests(deployer, testChain, accounts))
    });

    describe("Bonding and unbonding", async function () {
        it("Associated account should be able to bond and unbond", async function () {
            const amount = 1000000;
            const evmAddress = await getEvmAddress(owner.address);
            const pointerInitialBalance = await tokenPointer.balanceOf(evmAddress);
            await testBonding(owner.address, amount);

            // Verify that address is associated
            expect(evmAddress).to.not.be.empty;

            // Check pointer balance
            const pointerBalance = await tokenPointer.balanceOf(evmAddress);
            const expectedAfterBalance = Number(pointerInitialBalance) + amount;
            expect(pointerBalance).to.equal(`${expectedAfterBalance}`);

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
