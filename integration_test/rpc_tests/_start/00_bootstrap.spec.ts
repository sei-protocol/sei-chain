/**
 * Bootstrap for the new_rpc_tests module.
 *
 * Runs ONCE, sequentially, before any other spec file in this module. It is
 * responsible for:
 *
 *   1. Verifying both endpoints (local Sei EVM RPC + local Hardhat mainnet fork)
 *      are reachable. We refuse to deploy anything until the reference fork is up
 *      because most parallel specs will compare its responses against Sei's.
 *   2. Capturing chain ids and block numbers at well-defined points so spec files
 *      can make precise historical-state assertions (`eth_call` at the block
 *      before deploy, `eth_getStorageAt` at the deploy block, etc.) without
 *      coordinating with each other.
 *   3. Deploying the common contracts (currently just TestERC20) every spec might
 *      need, recording their addresses, and minting an initial supply to the
 *      admin.
 *   4. Pre-funding a pool of fresh EVM accounts so individual specs do not have to
 *      fund their own throw-away signers and serialize against the admin nonce. The
 *      suite runs serially and claimPool hands each spec a disjoint, non-overlapping
 *      slice, so no two specs ever share a key.
 *   5. Writing all of the above to runtime/runtime.json, which every other spec
 *      reads via utils/testUtils.ts:readRuntimeState().
 *
 * The bootstrap is the ONLY place that writes runtime.json. Spec files MUST treat
 * the state as read-only — writing back to it from a parallel worker would race.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { AdminMnemonic, Endpoints } from '../config/endpoints';
import { gethRpc, isReachable, seiRpc } from '../utils/chainUtils';
import { EvmAccount } from '../utils/evmUtils';
import { deployContract, deployTestErc20 } from '../utils/evmUtils';
import { fundFromUnlocked, fundManyEvm } from '../utils/evmUtils';
import { fundAdminOnSei } from '../utils/cosmosUtils';
import { writeRuntimeState, RuntimeState } from '../utils/testUtils';
import { sleep } from '../utils/chainUtils';

const POOL_SIZE = 96;
const POOL_FUND_WEI = ethers.parseEther('5');
const ADMIN_MINT = ethers.parseEther('1000000');
// Geth --dev pre-funds its dev account with 10^49 ETH, so we can seed the mirror
// deployer generously; the deploy + mint costs a tiny fraction of this.
const GETH_ADMIN_FUND_WEI = ethers.parseEther('100');

describe('new_rpc_tests bootstrap', function () {
    this.timeout(10 * 60 * 1000);

    let admin: EvmAccount;
    let gethAdmin: EvmAccount | undefined;
    let state: Partial<RuntimeState> = {};

    before(async () => {
        admin = EvmAccount.fromMnemonic(AdminMnemonic);
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
        state.chainIds = {
            sei: Number(seiChainId),
            eth: Number(gethChainId),
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

    it('funds and associates the admin on Sei (mirror of UserFactory.fundAdminOnSei)', async () => {
        await fundAdminOnSei(admin.address, AdminMnemonic, seiRpc());
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
        const devAccounts: string[] = await geth.send('eth_accounts', []);
        expect(devAccounts.length, 'geth --dev should expose a pre-funded dev account').to.be.greaterThan(0);
        const devAccount = devAccounts[0];

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
        const pool = Array.from({ length: POOL_SIZE }, () => EvmAccount.random(seiRpc()));
        await fundManyEvm(admin, pool.map(p => p.address), POOL_FUND_WEI);

        // fundManyEvm already asserts every funding tx succeeded (status 1), but verify
        // every balance too: runtime.json must never advertise a pool account as ready
        // unless it actually holds the funds a spec will claim it for.
        const balances = await Promise.all(pool.map(p => p.balance()));
        balances.forEach((bal, i) => {
            expect(bal, `pool[${i}] (${pool[i].address}) funded`).to.equal(POOL_FUND_WEI);
        });

        if (!gethAdmin) throw new Error('geth admin was not initialised by the mirror deploy step');
        state.funded = {
            admin: admin.address,
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

    it('records the post-deploy block height and writes runtime/runtime.json', async () => {
        // Give the chain a moment to finalize the funding txs.
        await sleep(500);
        const seiAfter = await seiRpc().getBlockNumber();
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
