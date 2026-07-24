/**
 * Runs ONCE, sequentially, before any other spec in this module:
 *   1. Verifies the Sei EVM RPC is reachable before deploying.
 *   2. Captures the chain id and block numbers at well-defined points.
 *   3. Funds + associates the admin, then deploys the PrecompileCaller fixture
 *      (real CALL / STATICCALL / DELEGATECALL dispatch from contract bytecode).
 *   4. Pre-funds a pool of fresh EVM accounts; claimPool hands each spec a disjoint,
 *      non-overlapping slice (suite runs serially) so no two specs ever share a key.
 *   5. Writes the above to runtime/runtime.json, read via utils/testUtils.ts:readRuntimeState().
 *
 * The bootstrap is the ONLY place that writes runtime.json; specs MUST treat it as read-only
 * (writing back from a spec would clobber the shared state every later spec depends on).
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { AdminMnemonic, Endpoints } from '../config/endpoints';
import { isReachable, seiRpc, waitUntil } from '../utils/chainUtils';
import { EvmAccount, deployContract, fundManyEvm } from '../utils/evmUtils';
import { fundAdminOnSei, generateMnemonic, seiAddressFromMnemonic } from '../utils/cosmosUtils';
import { writeRuntimeState, RuntimeState } from '../utils/testUtils';
import { ADDRESS } from '../utils/format';

const POOL_SIZE = 48;
const POOL_FUND_WEI = ethers.parseEther('5');

describe('precompile_tests bootstrap', function () {
    this.timeout(10 * 60 * 1000);

    let admin: EvmAccount;
    let adminMnemonic: string;
    let state: Partial<RuntimeState> = {};

    before(async () => {
        // Use SEI_ADMIN_MNEMONIC when provided (e.g. a pre-funded key for a non-docker
        // chain); otherwise mint a random admin and let the docker step fund it below.
        adminMnemonic = AdminMnemonic || (await generateMnemonic());
        admin = EvmAccount.fromMnemonic(adminMnemonic);
    });

    it('Sei EVM RPC is reachable', async () => {
        const ok = await isReachable(Endpoints.sei.evmRpc);
        expect(ok, `Sei EVM RPC at ${Endpoints.sei.evmRpc} is not reachable`).to.equal(true);
    });

    it('captures the chain id', async () => {
        const chainId = await seiRpc().send('eth_chainId', []);
        // Coerce via BigInt, not Number(): eth_chainId returns a 0x hex quantity, and
        // BigInt parses it unambiguously and throws on a malformed value.
        state.chainId = Number(BigInt(chainId));
    });

    it('captures the block height before any deploys', async () => {
        const height = await seiRpc().getBlockNumber();
        state.blocks = { beforeDeploy: height, callerDeploy: -1, afterDeploy: -1 };
    });

    it('funds and associates the admin on Sei', async () => {
        await fundAdminOnSei(admin.address, adminMnemonic, seiRpc());
        expect(
            (await admin.balance()) > 0n,
            'admin should hold a spendable EVM balance after funding + association',
        ).to.equal(true);
    });

    it('deploys the PrecompileCaller fixture', async () => {
        const { address, receipt } = await deployContract(
            admin,
            'PrecompileCaller.sol',
            [],
            'PrecompileCaller',
        );
        state.contracts = { precompileCaller: address };
        state.blocks!.callerDeploy = receipt.blockNumber;
        expect(address).to.match(ADDRESS);
    });

    it('pre-funds a pool of fresh EVM accounts', async () => {
        const pool = Array.from({ length: POOL_SIZE }, () => EvmAccount.random(seiRpc()));
        await fundManyEvm(admin, pool.map(p => p.address), POOL_FUND_WEI);

        const balances = await Promise.all(pool.map(p => p.balance()));
        balances.forEach((bal, i) => {
            expect(bal, `pool[${i}] (${pool[i].address}) funded`).to.equal(POOL_FUND_WEI);
        });

        state.funded = {
            admin: admin.address,
            adminMnemonic,
            adminSeiAddress: await seiAddressFromMnemonic(adminMnemonic),
            pool: pool.map(p => ({
                address: p.address,
                privateKey: (p.wallet as ethers.Wallet | ethers.HDNodeWallet).privateKey,
            })),
        };
    });

    it('records the post-deploy block height and writes runtime/runtime.json', async () => {
        // Poll until the chain mints a block past the pre-deploy height instead of a
        // fixed sleep, which is flaky on loaded CI runners where blocks come slowly.
        const before = state.blocks!.beforeDeploy;
        const after = await waitUntil(
            async () => {
                const h = await seiRpc().getBlockNumber();
                return h > before ? h : null;
            },
            { timeoutMs: 30_000, label: 'Sei block height to advance past bootstrap' },
        );
        state.blocks!.afterDeploy = after;
        state.bootstrappedAt = new Date().toISOString();

        const finalised = state as RuntimeState;
        writeRuntimeState(finalised);

        expect(finalised.blocks.afterDeploy).to.be.greaterThan(
            finalised.blocks.beforeDeploy,
            'expected Sei to advance at least one block during bootstrap',
        );
        expect(finalised.contracts.precompileCaller, 'PrecompileCaller address missing').to.match(
            ADDRESS,
        );
        expect(finalised.funded.adminSeiAddress, 'admin sei address missing').to.match(/^sei1/);
    });
});
