const {
    getAdmin,
    queryWasm,
    executeWasm,
    deployEvmContract,
    setupSigners,
    storeWasm,
    instantiateWasm,
    registerPointerForERC20,
    disableWasm,
    enableWasm,
    isWasmEnabled,
    isWasmDisabled,
    ensureWasmEnabled,
    ensureWasmDisabled,
    proposeDisableWasm,
    proposeEnableWasm,
    passProposal,
    WASM,
} = require("./lib");
const { expect } = require("chai");

/**
 * Disable WASM Test
 * 
 * Tests the chain parameter controls for CosmWasm operations:
 * 
 * 1. Pre-flight Check - Verifies WASM is enabled and store/instantiate work
 * 2. Disable WASM - Disables via governance, verifies new deployments fail,
 *                   but existing CW20 pointers still work (query + execute)
 * 3. Re-enable WASM - Re-enables via governance, verifies deployments work again
 * 4. Two-step Proposal - Tests creating and passing proposals as separate steps
 */
describe("Disable WASM Test", function () {
    let accounts;
    let admin;
    let testToken;
    let cw20Pointer;

    async function setBalance(addr, balance) {
        const resp = await testToken.setBalance(addr, balance);
        await resp.wait();
    }

    before(async function () {
        accounts = await setupSigners(await hre.ethers.getSigners());
        admin = await getAdmin();

        // Ensure WASM is enabled before running tests
        await ensureWasmEnabled();
        expect(await isWasmEnabled()).to.be.true;

        // Deploy TestToken (ERC20)
        testToken = await deployEvmContract("TestToken", ["TEST", "TEST"]);
        const tokenAddr = await testToken.getAddress();

        // Give admin balance
        await setBalance(admin.evmAddress, 1000000000000);

        // Register CW20 pointer for the ERC20 token
        cw20Pointer = await registerPointerForERC20(tokenAddr);
    });

    // Global cleanup - always ensure WASM is enabled when tests finish
    after(async function () {
        await ensureWasmEnabled();
    });

    describe("Pre-flight Check", function () {
        it("should have WASM enabled (Everybody) at the start", async function () {
            expect(await isWasmEnabled()).to.be.true;
        });

        it("should be able to store and instantiate wasm when enabled", async function () {
            const codeId = await storeWasm(WASM.CW20);
            expect(parseInt(codeId)).to.be.greaterThan(0);

            const contractAddr = await instantiateWasm(
                codeId, 
                admin.seiAddress, 
                "test-cw20-preflight",
                {
                    name: "Test",
                    symbol: "TST",
                    decimals: 6,
                    initial_balances: [],
                    mint: { minter: admin.seiAddress }
                }
            );
            expect(contractAddr).to.be.a("string");
        });
    });

    describe("Disable WASM Scenario", function () {
        after(async function () {
            await ensureWasmEnabled();
        });

        it("should disable WASM via governance", async function () {
            await disableWasm();
            expect(await isWasmDisabled()).to.be.true;
        });

        it("should have wasm params set to Nobody", async function () {
            expect(await isWasmDisabled()).to.be.true;
        });

        it("should fail to store new wasm code when disabled", async function () {
            try {
                await storeWasm(WASM.CW20);
                expect.fail("Expected storeWasm to fail when disabled");
            } catch (error) {
                // Error should indicate unauthorized/permission denied
                expect(error.message.toLowerCase()).to.include("storewasm failed");
            }
        });

        it("should fail to instantiate new contracts when disabled", async function () {
            try {
                await instantiateWasm(
                    1,
                    admin.seiAddress,
                    "test-cw20-should-fail",
                    {
                        name: "Test",
                        symbol: "TST",
                        decimals: 6,
                        initial_balances: [],
                        mint: { minter: admin.seiAddress }
                    }
                );
                expect.fail("Expected instantiateWasm to fail when disabled");
            } catch (error) {
                // Error should indicate unauthorized/permission denied
                expect(error.message.toLowerCase()).to.include("instantiatewasm failed");
            }
        });

        it("should still allow querying existing CW20 pointer when wasm is disabled", async function () {
            const result = await queryWasm(cw20Pointer, "token_info", {});
            expect(result.data).to.have.property("name");
            expect(result.data).to.have.property("symbol");
        });

        it("should still allow executing on existing CW20 pointer when wasm is disabled", async function () {
            const balanceBefore = await queryWasm(cw20Pointer, "balance", { address: admin.seiAddress });

            const transferResult = await executeWasm(cw20Pointer, {
                transfer: { recipient: accounts[0].seiAddress, amount: "100" }
            });
            expect(transferResult.txhash).to.be.a("string");

            const balanceAfter = await queryWasm(cw20Pointer, "balance", { address: admin.seiAddress });
            expect(parseInt(balanceAfter.data.balance)).to.be.lessThan(parseInt(balanceBefore.data.balance));
        });
    });

    describe("Re-enable WASM Scenario", function () {
        before(async function () {
            await ensureWasmDisabled();
        });

        after(async function () {
            await ensureWasmEnabled();
        });

        it("should re-enable WASM via governance", async function () {
            await enableWasm();
            expect(await isWasmEnabled()).to.be.true;
        });

        it("should have wasm params set back to Everybody", async function () {
            expect(await isWasmEnabled()).to.be.true;
        });

        it("should be able to store wasm after re-enabling", async function () {
            const codeId = await storeWasm(WASM.CW20);
            expect(parseInt(codeId)).to.be.greaterThan(0);
        });

        it("should be able to instantiate contracts after re-enabling", async function () {
            const codeId = await storeWasm(WASM.CW20);
            const contractAddr = await instantiateWasm(
                codeId,
                admin.seiAddress,
                "test-cw20-reenabled",
                {
                    name: "Test",
                    symbol: "TST",
                    decimals: 6,
                    initial_balances: [],
                    mint: { minter: admin.seiAddress }
                }
            );
            expect(contractAddr).to.be.a("string");
        });

        it("should still allow existing pointer to work after re-enabling", async function () {
            const result = await queryWasm(cw20Pointer, "token_info", {});
            expect(result.data).to.have.property("name");
        });
    });

    describe("Two-step Proposal Process", function () {
        after(async function () {
            await ensureWasmEnabled();
        });

        it("should be able to create and pass disable proposal in separate steps", async function () {
            await ensureWasmEnabled();

            // Step 1: Create proposal
            const proposalId = await proposeDisableWasm();
            expect(parseInt(proposalId)).to.be.greaterThan(0);

            // Step 2: Pass proposal
            await passProposal(proposalId);

            expect(await isWasmDisabled()).to.be.true;

            // Cleanup
            await enableWasm();
        });

        it("should be able to create and pass enable proposal in separate steps", async function () {
            await ensureWasmDisabled();

            // Step 1: Create proposal
            const proposalId = await proposeEnableWasm();
            expect(parseInt(proposalId)).to.be.greaterThan(0);

            // Step 2: Pass proposal
            await passProposal(proposalId);

            expect(await isWasmEnabled()).to.be.true;
        });
    });

    describe("Final State Verification", function () {
        it("should end with WASM enabled (Everybody)", async function () {
            expect(await isWasmEnabled()).to.be.true;
        });
    });
});
