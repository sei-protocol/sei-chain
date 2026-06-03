import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, sleep } from '../utils/chainUtils';
import { rawSei, rawGeth, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState } from '../utils/testUtils';
import { claimPool, expectSameError } from '../utils/testUtils';
import { EvmAccount } from '../utils/evmUtils';
import { fundFromUnlocked } from '../utils/evmUtils';
import { AdminMnemonic } from '../config/endpoints';
import { cosmosBankSend, generateSeiAddress, CosmosBankSend } from '../utils/cosmosUtils';
import { buildRichSeiBlock, sendSingleTx, RichBlock, SentTx } from '../utils/txUtils';
import { blockReceipts } from '../utils/txUtils';
import { txCountByNumber, assertTxCount, findEmptyBlock } from '../utils/txUtils';

// eth_getBlockTransactionCountByNumber: the count must agree with every other view of the
// same block (eth_getBlockByNumber's tx list and eth_getBlockReceipts), resolve all tags,
// count only EVM txs in a dual-VM block, and match geth's encoding + error behaviour.
describe('eth_getBlockTransactionCountByNumber', function () {
    this.timeout(300 * 1000);

    const { sei, geth } = bothProviders();

    let runtime: RuntimeState;
    let richSei: RichBlock;
    let seiOne: { number: number; hash: string; tx: SentTx };
    let gethOne: { number: number; hash: string; tx: SentTx };
    let gethSigner: EvmAccount;
    let emptyBlock: { number: number; hash: string } | undefined;

    before(async function () {
        this.timeout(300 * 1000);
        const t0 = Date.now();
        const step = (msg: string) => console.log(`[before +${((Date.now() - t0) / 1000).toFixed(1)}s] ${msg}`);

        // Brief settle window so this spec doesn't start broadcasting on top of the
        // previous spec's still-pending txs (which can leave the chain congested).
        step('waiting 5s before starting');
        await sleep(5000);

        runtime = readRuntimeState();
        const signers = claimPool(runtime, sei, 9, 'eth_getBlockTransactionCountByNumber');
        step('signers created');

        const gethDev: string = (await geth.send('eth_accounts', []))[0];
        step(`geth dev account = ${gethDev}`);
        gethSigner = EvmAccount.fromPrivateKey(ethers.Wallet.createRandom().privateKey, geth);
        await fundFromUnlocked(geth, gethDev, gethSigner.address, ethers.parseEther('10'));
        step('geth signer funded');

        richSei = await buildRichSeiBlock(sei, runtime, signers.slice(0, 7));
        step(`richSei block built (#${richSei.number}, ${richSei.txs.length} txs)`);

        seiOne = await sendSingleTx(sei, signers[7]);
        step(`sei single tx sent (#${seiOne.number})`);
        gethOne = await sendSingleTx(geth, gethSigner);
        step(`geth single tx sent (#${gethOne.number})`);
        emptyBlock = await findEmptyBlock(sei);
        step(`empty block ${emptyBlock ? '#' + emptyBlock.number : 'not found'}`);
    });

    describe('counts agree with every other view of the block', () => {
        it('counts every transaction in the rich block', async () => {
            const count = await txCountByNumber(sei, richSei.number);
            assertTxCount(count, richSei.txs.length, 'rich block tx count');
        });

        it('equals the eth_getBlockByNumber tx list length and the receipt count', async () => {
            const [count, block, receipts] = await Promise.all([
                txCountByNumber(sei, richSei.number),
                sei.send('eth_getBlockByNumber', [ethers.toQuantity(richSei.number), false]),
                blockReceipts(sei, richSei.number),
            ]);
            assertTxCount(count, block.transactions.length, 'count == block.transactions');
            expect(receipts.length, 'count == receipts length').to.equal(Number(BigInt(count)));
        });

        it('a known empty block reports 0x0', async function () {
            if (!emptyBlock) this.skip();
            const count = await txCountByNumber(sei, emptyBlock!.number);
            expect(count, 'empty block count is exactly 0x0').to.equal('0x0');
            const block = await sei.send('eth_getBlockByNumber', [
                ethers.toQuantity(emptyBlock!.number),
                false,
            ]);
            expect(block.transactions.length, 'block really is empty').to.equal(0);
        });
    });

    describe('block tags resolve', () => {
        it('earliest (genesis) has no transactions', async () => {
            expect(await txCountByNumber(sei, 'earliest')).to.equal('0x0');
        });

        it('latest matches the head block tx list', async () => {
            const [count, head] = await Promise.all([
                txCountByNumber(sei, 'latest'),
                sei.send('eth_getBlockByNumber', ['latest', false]),
            ]);
            assertTxCount(count, head.transactions.length, 'latest count');
        });

        it('pending returns a canonical quantity', async () => {
            const count = await txCountByNumber(sei, 'pending');
            assertTxCount(count, Number(BigInt(count)), 'pending count');
        });
    });

    describe('geth parity', () => {
        it('a single-transaction block counts 0x1 on both chains', async () => {
            const [s, g] = await Promise.all([
                txCountByNumber(sei, seiOne.number),
                txCountByNumber(geth, gethOne.number),
            ]);
            expect(s, 'Sei single-tx count').to.equal('0x1');
            expect(g, 'geth single-tx count').to.equal('0x1');
        });
    });

    describe('dual-VM: a Cosmos bank send is not counted', () => {
        let height: number | undefined;
        let cosmos: CosmosBankSend;
        let evm: { number: number; hash: string; tx: SentTx };
        const AMOUNT_USEI = 222_111n;

        before(async function () {
            this.timeout(180 * 1000);
            const evmSigner = claimPool(runtime, sei, 1, 'eth_getBlockTransactionCountByNumber:cosmos')[0];
            for (let attempt = 0; attempt < 4 && height === undefined; attempt++) {
                const recipient = await generateSeiAddress();
                const [cos, ev] = await Promise.all([
                    cosmosBankSend(AdminMnemonic, recipient, AMOUNT_USEI),
                    sendSingleTx(sei, evmSigner),
                ]);
                if (cos.code === 0 && cos.height === ev.number) {
                    height = cos.height;
                    cosmos = cos;
                    evm = ev;
                }
            }
            if (height === undefined) this.skip();
        });

        it('the count equals the EVM tx count, excluding the Cosmos tx', async () => {
            const [count, block] = await Promise.all([
                txCountByNumber(sei, height!),
                sei.send('eth_getBlockByNumber', [ethers.toQuantity(height!), false]),
            ]);
            // Both the Cosmos send and the EVM tx are in this block, but only the EVM tx counts.
            const cosmosAsEvmHash = '0x' + cosmos.hash.toLowerCase();
            expect(
                block.transactions.map((h: string) => h.toLowerCase()),
                'EVM tx present',
            ).to.include(evm.tx.hash.toLowerCase());
            expect(
                block.transactions.map((h: string) => h.toLowerCase()),
                'Cosmos tx not in the EVM block',
            ).to.not.include(cosmosAsEvmHash);
            assertTxCount(count, block.transactions.length, 'count == EVM tx count');
        });
    });

    describe('wrong params / error handling (parity with geth)', () => {
        it('empty params fail identically (-32602, missing argument 0)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockTransactionCountByNumber', []),
                rawGeth('eth_getBlockTransactionCountByNumber', []),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 0/);
            expectSameError(s, g);
        });

        it('too many positional args fail identically (-32602, want at most 1)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockTransactionCountByNumber', ['latest', 1]),
                rawGeth('eth_getBlockTransactionCountByNumber', ['latest', 1]),
            ]);
            expectJsonRpcError(s, -32602, /too many arguments, want at most 1/);
            expectSameError(s, g);
        });

        it('a far-future block returns null on both chains', async () => {
            const future = ethers.toQuantity((await sei.getBlockNumber()) + 10_000_000);
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockTransactionCountByNumber', [future]),
                rawGeth('eth_getBlockTransactionCountByNumber', [future]),
            ]);
            // A not-yet-mined height has no count: both return a null result, not an error.
            expect(g.error, 'geth does not error on a future block').to.equal(undefined);
            expect(g.result, 'geth returns null for a future block').to.equal(null);
            expect(s.error, 'Sei does not error on a future block').to.equal(undefined);
            expect(s.result, 'Sei returns null for a future block').to.equal(null);
        });
    });
});
