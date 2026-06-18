import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, expectSameError } from '../utils/testUtils';
import { RichBlock, richFailedTxs, assertFailedReceipt } from '../utils/txUtils';
import {
    sharedRichBlock,
    sharedGethTx,
    assertTxObject,
    assertTxMatchesSent,
    assertTxReceiptConsistency,
    assertTxKeysetParity,
    logsByBlockHash,
    filterLogsForBlock,
} from '../utils/txLookupUtils';

describe('eth_getTransactionByHash', function () {
    this.timeout(300 * 1000);

    const { sei, geth } = bothProviders();

    let runtime: RuntimeState;
    let rich: RichBlock;
    let blockByHash: any;
    let blockByNumber: any;
    let gethOne: { number: number; hash: string; tx: any };

    before(async function () {
        this.timeout(300 * 1000);
        runtime = readRuntimeState();
        rich = await sharedRichBlock(sei, runtime);
        [blockByHash, blockByNumber, gethOne] = await Promise.all([
            sei.send('eth_getBlockByHash', [rich.hash, true]),
            sei.send('eth_getBlockByNumber', [ethers.toQuantity(rich.number), true]),
            sharedGethTx(geth),
        ]);
    });

    describe('schema + field correctness', () => {
        it('returns a canonical, type-correct object for every transaction in the block', async () => {
            for (const sent of rich.txs) {
                const tx = await sei.send('eth_getTransactionByHash', [sent.hash]);
                expect(tx, `tx exists for ${sent.kind}`).to.not.equal(null);
                assertTxObject(tx, rich);
                assertTxMatchesSent(tx, sent);
            }
        });

        it('reports the same chainId as eth_chainId for every typed transaction', async () => {
            const chainId = await sei.send('eth_chainId', []);
            for (const sent of rich.txs) {
                const tx = await sei.send('eth_getTransactionByHash', [sent.hash]);
                if (tx.type === '0x0') continue; // legacy objects omit chainId
                expect(tx.chainId, `chainId for ${sent.kind}`).to.equal(chainId);
            }
        });
    });

    describe('cross-reference: block, logs, filter logs', () => {
        it('is byte-identical to the matching entry in eth_getBlockByHash(fullTx=true)', async () => {
            const byIndex = new Map<string, any>(
                blockByHash.transactions.map((t: any) => [t.hash, t]),
            );
            for (const sent of rich.txs) {
                const inBlock = byIndex.get(sent.hash);
                expect(inBlock, `${sent.kind} present in block-by-hash`).to.not.equal(undefined);
                const standalone = await sei.send('eth_getTransactionByHash', [sent.hash]);
                expect(standalone, `standalone == in-block for ${sent.kind}`).to.deep.equal(inBlock);
            }
        });

        it('and identical to the entry in eth_getBlockByNumber(fullTx=true)', () => {
            const byHashName = new Map<string, any>(
                blockByHash.transactions.map((t: any) => [t.hash, t]),
            );
            for (const t of blockByNumber.transactions) {
                expect(t, `${t.hash} matches across block-by-hash/number`).to.deep.equal(
                    byHashName.get(t.hash),
                );
            }
        });

        it('every eth_getLogs entry points back to the same tx hash + index', async () => {
            const logs = await logsByBlockHash(sei, rich.hash);
            expect(logs.length, 'the rich block emits logs').to.be.greaterThan(0);
            for (const log of logs) {
                const tx = await sei.send('eth_getTransactionByHash', [log.transactionHash]);
                expect(tx, `tx exists for log ${log.transactionHash}`).to.not.equal(null);
                expect(log.transactionIndex, 'log.transactionIndex == tx.transactionIndex').to.equal(
                    tx.transactionIndex,
                );
                expect(log.blockHash, 'log.blockHash == tx.blockHash').to.equal(tx.blockHash);
                expect(log.blockNumber, 'log.blockNumber == tx.blockNumber').to.equal(tx.blockNumber);
            }
        });

        it('eth_getFilterLogs returns the same log→tx mapping as eth_getLogs', async () => {
            const [byLogs, byFilter] = await Promise.all([
                logsByBlockHash(sei, rich.hash),
                filterLogsForBlock(sei, rich.number),
            ]);
            const key = (l: any) => `${l.transactionHash}:${l.logIndex}`;
            expect(byFilter.map(key).sort(), 'filter logs == getLogs').to.deep.equal(
                byLogs.map(key).sort(),
            );
            for (const log of byFilter) {
                const tx = await sei.send('eth_getTransactionByHash', [log.transactionHash]);
                expect(log.transactionIndex, 'filter log index matches tx').to.equal(
                    tx.transactionIndex,
                );
            }
        });
    });

    describe('consistency with eth_getTransactionReceipt', () => {
        it('shares the identity fields with its receipt and keeps the field partition disjoint', async () => {
            for (const sent of rich.txs) {
                const [tx, rc] = await Promise.all([
                    sei.send('eth_getTransactionByHash', [sent.hash]),
                    sei.send('eth_getTransactionReceipt', [sent.hash]),
                ]);
                expect(rc, `receipt exists for ${sent.kind}`).to.not.equal(null);
                assertTxReceiptConsistency(tx, rc);
            }
        });
    });

    describe('included-but-failed transactions', () => {
        it('returns canonical tx objects for the failed txs, whose receipts are status 0', async () => {
            const { outOfGas, revertErc20 } = richFailedTxs(rich);
            for (const sent of [outOfGas, revertErc20]) {
                const [tx, rc] = await Promise.all([
                    sei.send('eth_getTransactionByHash', [sent.hash]),
                    sei.send('eth_getTransactionReceipt', [sent.hash]),
                ]);
                expect(tx, `tx exists for ${sent.kind}`).to.not.equal(null);
                assertTxObject(tx, rich);
                assertTxMatchesSent(tx, sent);
                // The title's claim: a failed tx is still mined with a canonical object, and
                // its receipt records the failure (status 0x0, gas burned, no logs).
                assertFailedReceipt(rc, sent);
            }
        });
    });

    describe('geth schema parity', () => {
        it('exposes the same key set as geth for an EIP-1559 transaction', async () => {
            const sent1559 = rich.txs.find(t => t.type === 2)!;
            const [s, g] = await Promise.all([
                sei.send('eth_getTransactionByHash', [sent1559.hash]),
                geth.send('eth_getTransactionByHash', [gethOne.tx.hash]),
            ]);
            assertTxKeysetParity(s, g);
        });
    });

    describe('error handling parity with geth', () => {
        it('an unknown hash returns null on both chains', async () => {
            const unknown = '0x' + 'ab'.repeat(32);
            const [s, g] = await Promise.all([
                sei.send('eth_getTransactionByHash', [unknown]),
                geth.send('eth_getTransactionByHash', [unknown]),
            ]);
            expect(s, 'Sei null').to.equal(null);
            expect(g, 'geth null').to.equal(null);
        });

        it('empty params fail identically (-32602, missing argument 0)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getTransactionByHash', []),
                rawGeth('eth_getTransactionByHash', []),
            ]);
            expectSameError(s, g);
        });

        it('a wrong-length hash fails identically (-32602, common.Hash)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getTransactionByHash', ['0x1234']),
                rawGeth('eth_getTransactionByHash', ['0x1234']),
            ]);
            expectSameError(s, g);
        });

        it('a non-hex hash fails identically', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getTransactionByHash', ['nope']),
                rawGeth('eth_getTransactionByHash', ['nope']),
            ]);
            expectSameError(s, g);
        });
    });
});
