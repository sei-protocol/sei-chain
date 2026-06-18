/**
 * Runs ONCE, sequentially, before any other spec in this module:
 *   1. Verifies both endpoints (local Sei EVM RPC + local geth --dev reference) are
 *      reachable before deploying, since most specs compare geth against Sei.
 *   2. Captures chain ids and block numbers at well-defined points so specs can make
 *      precise historical-state assertions (eth_call before deploy, eth_getStorageAt at
 *      the deploy block, etc.) without coordinating with each other.
 *   3. Deploys the common contracts (currently just TestERC20), records addresses, mints to admin.
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
import { gethRpc, isReachable, seiRpc } from '../utils/chainUtils';
import { EvmAccount } from '../utils/evmUtils';
import { deployContract, deployTestErc20 } from '../utils/evmUtils';
import { fundEvm, fundFromUnlocked, fundManyEvm, gethUnlockedAccount } from '../utils/evmUtils';
import { fundAdminOnSei, generateMnemonic, seiAddressFromMnemonic } from '../utils/cosmosUtils';
import {
    isWasmEnabled,
    deployCw20,
    registerCw20Pointer,
    Cw20InitMsg,
} from '../utils/wasmUtils';
import { writeRuntimeState, RuntimeState } from '../utils/testUtils';
import { waitUntil } from '../utils/chainUtils';

const POOL_SIZE = 96;
const POOL_FUND_WEI = ethers.parseEther('5');
const ADMIN_MINT = ethers.parseEther('1000000');
// CW20 base units minted to every pool/admin/actor address (decimals 6). Big enough
// that the rich-block pointer transfer and the admin cw20 transfer never run dry.
const CW20_DECIMALS = 6;
const CW20_MINT = '1000000000000';
// Geth --dev pre-funds its dev account with 10^49 ETH, so we can seed the mirror
// deployer generously; the deploy + mint costs a tiny fraction of this.
const GETH_ADMIN_FUND_WEI = ethers.parseEther('100');

describe('new_rpc_tests bootstrap', function () {
    this.timeout(10 * 60 * 1000);

    let admin: EvmAccount;
    let adminMnemonic: string;
    let gethAdmin: EvmAccount | undefined;
    let pool: EvmAccount[] = [];
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

    it('geth reference node is reachable', async () => {
        const ok = await isReachable(Endpoints.eth.geth);
        expect(
            ok,
            `geth --dev at ${Endpoints.eth.geth} is not reachable. ` +
                'Run `yarn rpc:geth` in another terminal before running this suite.',
        ).to.equal(true);
    });

    it('captures chain ids from both endpoints', async () => {
        const [seiChainId, gethChainId] = await Promise.all([
            seiRpc().send('eth_chainId', []),
            gethRpc().send('eth_chainId', []),
        ]);
        // Coerce via BigInt, not Number(): eth_chainId returns a 0x hex quantity, and
        // BigInt parses it unambiguously and throws on a malformed value, rather than
        // letting a bad response slip through as NaN that downstream specs compare against.
        state.chainIds = {
            sei: Number(BigInt(seiChainId)),
            eth: Number(BigInt(gethChainId)),
        };
    });

    it('captures block heights before any deploys', async () => {
        const [seiBlock, gethBlock] = await Promise.all([
            seiRpc().getBlockNumber(),
            gethRpc().getBlockNumber(),
        ]);
        state.blocks = {
            seiBeforeDeploy: seiBlock,
            seiErc20Deploy: -1,
            seiAfterDeploy: -1,
            ethAtBootstrap: gethBlock,
            ethErc20Deploy: -1,
        };
    });

    it('funds and associates the admin on Sei through docker node', async () => {
        await fundAdminOnSei(admin.address, adminMnemonic, seiRpc());
        expect(
            (await admin.balance()) > 0n,
            'admin should hold a spendable EVM balance after funding + association',
        ).to.equal(true);
    });

    it('deploys the canonical TestERC20 and mints to admin', async () => {
        const { address, receipt } = await deployTestErc20(admin);
        // erc20Geth is filled in by the geth mirror step that runs next; simpleAccount7702
        // by the delegation-target step.
        state.contracts = { erc20: address, erc20Geth: '', simpleAccount7702: '', gasBurner: '' };
        state.blocks!.seiErc20Deploy = receipt.blockNumber;

        const erc20 = new ethers.Contract(
            address,
            ['function mint(address,uint256)', 'function balanceOf(address) view returns (uint256)'],
            admin.wallet,
        );
        const mintTx = await erc20.mint(admin.address, ADMIN_MINT);
        await mintTx.wait();

        const balance: bigint = await erc20.balanceOf(admin.address);
        expect(balance).to.equal(ADMIN_MINT);
    });

    it('deploys the SimpleAccount7702 delegation target on Sei', async () => {
        // Shared EIP-7702 delegation implementation so specs never redeploy it.
        const { address } = await deployContract(admin, 'SimpleAccount7702.sol', [], 'SimpleAccount7702');
        state.contracts!.simpleAccount7702 = address;
        expect(address).to.match(/^0x[0-9a-fA-F]{40}$/);
    });

    it('deploys the RealGasBurner on Sei', async () => {
        // Lets eth_estimateGas (and fee-market specs) burn arbitrary gas to push the
        // base fee up without depending on other suites' traffic.
        const { address } = await deployContract(admin, 'GasBurner.sol', [], 'RealGasBurner');
        state.contracts!.gasBurner = address;
        expect(address).to.match(/^0x[0-9a-fA-F]{40}$/);
    });

    it('mirrors the TestERC20 deploy on the geth reference and mints to the geth admin', async () => {
        const geth = gethRpc();

        // geth --dev exposes exactly one pre-funded, auto-unlocked dev account. We
        // fund a fresh key from it (node-signed) so we control the deployer locally.
        const devAccount = await gethUnlockedAccount(geth);

        gethAdmin = EvmAccount.random(geth);
        await fundFromUnlocked(geth, devAccount, gethAdmin.address, GETH_ADMIN_FUND_WEI);
        const funded = await gethAdmin.balance();
        expect(funded).to.equal(GETH_ADMIN_FUND_WEI);

        // Same contract, same constructor (initialOwner = deployer), same mint as Sei
        // so contract-touching parity specs see an identical layout on both chains.
        const { address, receipt } = await deployTestErc20(gethAdmin);
        state.contracts!.erc20Geth = address;
        state.blocks!.ethErc20Deploy = receipt.blockNumber;

        const erc20 = new ethers.Contract(
            address,
            ['function mint(address,uint256)', 'function balanceOf(address) view returns (uint256)'],
            gethAdmin.wallet,
        );
        // geth --dev instamines, so the deploy's `pending` nonce can briefly still
        // read 0 right after the receipt; pin the mint to the mined (`latest`) nonce
        // to avoid a "nonce too low" race.
        const mintNonce = await geth.getTransactionCount(gethAdmin.address, 'latest');
        const mintTx = await erc20.mint(gethAdmin.address, ADMIN_MINT, { nonce: mintNonce });
        await mintTx.wait();

        const balance: bigint = await erc20.balanceOf(gethAdmin.address);
        expect(balance).to.equal(ADMIN_MINT);
    });

    it('pre-funds a pool of fresh EVM accounts', async () => {
        pool = Array.from({ length: POOL_SIZE }, () => EvmAccount.random(seiRpc()));
        await fundManyEvm(admin, pool.map(p => p.address), POOL_FUND_WEI);

        const balances = await Promise.all(pool.map(p => p.balance()));
        balances.forEach((bal, i) => {
            expect(bal, `pool[${i}] (${pool[i].address}) funded`).to.equal(POOL_FUND_WEI);
        });

        if (!gethAdmin) throw new Error('geth admin was not initialised by the mirror deploy step');
        state.funded = {
            admin: admin.address,
            adminMnemonic,
            gethAdmin: {
                address: gethAdmin.address,
                privateKey: (gethAdmin.wallet as ethers.Wallet | ethers.HDNodeWallet).privateKey,
            },
            pool: pool.map(p => ({
                address: p.address,
                privateKey: (p.wallet as ethers.Wallet | ethers.HDNodeWallet).privateKey,
            })),
        };
    });

    it('deploys a CW20 + ERC20 pointer when wasm is enabled', async function () {
        // Pure-cosmos chains (no wasm) simply skip the dual-VM fixtures; runtime.wasm
        // stays undefined and buildRichSeiBlock omits the pointer / cw20 transfer.
        if (!(await isWasmEnabled())) {
            this.skip();
            return;
        }

        // A dedicated actor signs the rich-block pointer transfer. Keeping it off the
        // shared pool means adding this fixture never shifts any spec's claimPool slice.
        const actor = EvmAccount.random(seiRpc());
        await fundEvm(admin, actor.address, POOL_FUND_WEI);
        // Associate the actor up front (a 0-value self-send reveals its pubkey) so the
        // CW20 pointer resolves it to the same sei address we mint to below.
        await fundEvm(actor, actor.address, 0n);

        // Mint to the admin (cosmos cw20 transfer sender), the actor (pointer transfer
        // sender) and every pool account so any pool key the suite associates already
        // holds a CW20 balance under its pubkey-derived sei address.
        const adminSei = await seiAddressFromMnemonic(adminMnemonic);
        const holders = [adminSei, actor.seiAddress(), ...pool.map(p => p.seiAddress())];
        const initMsg: Cw20InitMsg = {
            name: 'RpcTests CW20',
            symbol: 'RPCW',
            decimals: CW20_DECIMALS,
            initial_balances: holders.map(address => ({ address, amount: CW20_MINT })),
            mint: { minter: adminSei },
        };

        const { address: cw20 } = await deployCw20(initMsg, adminMnemonic);
        const cw20Pointer = await registerCw20Pointer(cw20);

        state.wasm = {
            cw20,
            cw20Pointer,
            actor: {
                address: actor.address,
                privateKey: (actor.wallet as ethers.Wallet | ethers.HDNodeWallet).privateKey,
            },
        };

        expect(cw20, 'CW20 contract address').to.match(/^sei1[0-9a-z]+$/);
        expect(cw20Pointer, 'CW20 ERC20 pointer address').to.match(/^0x[0-9a-fA-F]{40}$/);
    });

    it('records the post-deploy block height and writes runtime/runtime.json', async () => {
        // Poll until the chain mints a block past the pre-deploy height instead of a
        // fixed sleep, which is flaky on loaded CI runners where blocks come slowly.
        const seiBefore = state.blocks!.seiBeforeDeploy;
        const seiAfter = await waitUntil(
            async () => {
                const h = await seiRpc().getBlockNumber();
                return h > seiBefore ? h : null;
            },
            { timeoutMs: 30_000, label: 'Sei block height to advance past bootstrap' },
        );
        state.blocks!.seiAfterDeploy = seiAfter;
        state.bootstrappedAt = new Date().toISOString();

        const finalised = state as RuntimeState;
        writeRuntimeState(finalised);

        expect(finalised.blocks.seiAfterDeploy).to.be.greaterThan(
            finalised.blocks.seiBeforeDeploy,
            'expected Sei to advance at least one block during bootstrap',
        );
        expect(finalised.contracts.erc20Geth, 'geth mirror contract address missing').to.match(
            /^0x[0-9a-fA-F]{40}$/,
        );
        expect(finalised.blocks.ethErc20Deploy, 'geth mirror deploy block missing').to.be.greaterThan(0);
        expect(finalised.contracts.simpleAccount7702, 'SimpleAccount7702 address missing').to.match(
            /^0x[0-9a-fA-F]{40}$/,
        );
        expect(finalised.contracts.gasBurner, 'RealGasBurner address missing').to.match(
            /^0x[0-9a-fA-F]{40}$/,
        );
    });
});
