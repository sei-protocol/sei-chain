import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, expectSameError } from '../utils/testUtils';
import {
    RichBlock,
    blockReceipts,
    assertCanonicalReceipt,
    expectedEffectiveGasPrice,
    computeLogsBloom,
    richFailedTxs,
    assertFailedReceipt,
} from '../utils/txUtils';
import {
    sharedRichBlock,
    sharedGethTx,
    assertTxReceiptConsistency,
    logsByBlockHash,
} from '../utils/txLookupUtils';

describe('eth_getTransactionReceipt', function () {
    this.timeout(300 * 1000);

    const { sei, geth } = bothProviders();

    let runtime: RuntimeState;
    let rich: RichBlock;
    let blockByHash: any;
    let baseFee: bigint;
    let receipts: any[];
    let gethOne: { number: number; hash: string; tx: any };

    before(async function () {
        this.timeout(300 * 1000);
        runtime = readRuntimeState();
        rich = await sharedRichBlock(sei, runtime);
        [blockByHash, receipts, gethOne] = await Promise.all([
            sei.send('eth_getBlockByHash', [rich.hash, true]),
            blockReceipts(sei, rich.number),
            sharedGethTx(geth),
        ]);
        baseFee = BigInt(blockByHash.baseFeePerGas);
    });

    describe('schema + field correctness', () => {
        it('returns a canonical receipt for every tx, linked to its block + index', async () => {
            for (const t of blockByHash.transactions) {
                const rc = await sei.send('eth_getTransactionReceipt', [t.hash]);
                expect(rc, `receipt exists for ${t.hash}`).to.not.equal(null);
                assertCanonicalReceipt(rc, rich.hash, rich.number, Number(BigInt(t.transactionIndex)));
            }
        });

        it('each receipt logsBloom is exactly the Bloom we recompute from its own logs', async () => {
            for (const t of blockByHash.transactions) {
                const rc = await sei.send('eth_getTransactionReceipt', [t.hash]);
                expect(rc.logsBloom, `logsBloom == Bloom(own logs) for ${t.hash}`).to.equal(
                    computeLogsBloom([rc] as any),
                );
            }
        });

        it('effectiveGasPrice equals base fee + realised tip for each tx type', async () => {
            for (const sent of rich.txs) {
                const rc = await sei.send('eth_getTransactionReceipt', [sent.hash]);
                expect(BigInt(rc.effectiveGasPrice), `effectiveGasPrice for ${sent.kind}`).to.equal(
                    expectedEffectiveGasPrice(sent, baseFee),
                );
            }
        });

        it('gasUsed matches the mined receipt and cumulativeGasUsed accumulates in index order', async () => {
            // Canonical invariant: cumulative[i] == cumulative[i-1] + gasUsed[i].
            const ordered = [...receipts].sort(
                (a, b) => Number(BigInt(a.transactionIndex)) - Number(BigInt(b.transactionIndex)),
            );
            let running = 0n;
            for (const rc of ordered) {
                running += BigInt(rc.gasUsed);
                expect(BigInt(rc.cumulativeGasUsed), `cumulative at idx ${rc.transactionIndex}`).to.equal(
                    running,
                );
            }
            // Each per-tx gasUsed equals what the mined transaction actually burned.
            for (const sent of rich.txs) {
                const rc = await sei.send('eth_getTransactionReceipt', [sent.hash]);
                expect(BigInt(rc.gasUsed), `gasUsed for ${sent.kind}`).to.equal(sent.receipt.gasUsed);
            }
        });

        it('every receipt status matches the transaction outcome (0x1 success / 0x0 failed)', async () => {
            for (const sent of rich.txs) {
                const rc = await sei.send('eth_getTransactionReceipt', [sent.hash]);
                expect(rc.status, `status for ${sent.kind}`).to.equal(
                    sent.status === 0 ? '0x0' : '0x1',
                );
            }
        });

        it('contractAddress is populated only for the contract-creation tx and holds real code', async () => {
            for (const sent of rich.txs) {
                const rc = await sei.send('eth_getTransactionReceipt', [sent.hash]);
                if (sent.to === null) {
                    expect(rc.contractAddress, `${sent.kind} created an address`).to.match(
                        /^0x[0-9a-fA-F]{40}$/,
                    );
                    const code = await sei.send('eth_getCode', [rc.contractAddress, 'latest']);
                    expect(code, 'created contract has runtime code').to.not.equal('0x');
                } else {
                    expect(rc.contractAddress, `${sent.kind} is not a creation`).to.equal(null);
                }
            }
        });
    });

    describe('cross-reference: block receipts, logs, tx object', () => {
        it('is byte-identical to the matching eth_getBlockReceipts entry', async () => {
            for (const rc of receipts) {
                const single = await sei.send('eth_getTransactionReceipt', [rc.transactionHash]);
                expect(single, `single == block receipt for ${rc.transactionHash}`).to.deep.equal(rc);
            }
        });

        it("receipt.logs match eth_getLogs for that transaction (logIndex/address/topics)", async () => {
            const blockLogs = await logsByBlockHash(sei, rich.hash);
            const byTx = new Map<string, any[]>();
            for (const l of blockLogs) {
                const arr = byTx.get(l.transactionHash) ?? [];
                arr.push(l);
                byTx.set(l.transactionHash, arr);
            }
            for (const rc of receipts) {
                const expected = byTx.get(rc.transactionHash) ?? [];
                expect(rc.logs.length, `log count for ${rc.transactionHash}`).to.equal(expected.length);
                const norm = (l: any) => ({
                    logIndex: l.logIndex,
                    address: l.address.toLowerCase(),
                    topics: l.topics,
                    data: l.data,
                });
                expect(rc.logs.map(norm), `logs match getLogs for ${rc.transactionHash}`).to.deep.equal(
                    expected.map(norm),
                );
            }
        });

        it('transactionHash / index agree with eth_getTransactionByHash and the block', async () => {
            for (const rc of receipts) {
                const tx = await sei.send('eth_getTransactionByHash', [rc.transactionHash]);
                expect(tx.hash, 'tx.hash == receipt.transactionHash').to.equal(rc.transactionHash);
                expect(tx.transactionIndex, 'index agrees').to.equal(rc.transactionIndex);
                expect(tx.blockHash, 'blockHash agrees').to.equal(rc.blockHash);
            }
        });
    });

    describe('consistency with eth_getTransactionByHash', () => {
        it('keeps the tx/receipt field partition disjoint and identity fields equal', async () => {
            for (const sent of rich.txs) {
                const [tx, rc] = await Promise.all([
                    sei.send('eth_getTransactionByHash', [sent.hash]),
                    sei.send('eth_getTransactionReceipt', [sent.hash]),
                ]);
                assertTxReceiptConsistency(tx, rc);
            }
        });
    });

    describe('included-but-failed transactions', () => {
        it('the out-of-gas receipt consumed the whole gas limit, with no logs', async () => {
            const { outOfGas } = richFailedTxs(rich);
            const rc = await sei.send('eth_getTransactionReceipt', [outOfGas.hash]);
            assertFailedReceipt(rc, outOfGas);
        });

        it('the insufficient-balance ERC20 transfer reverted with gas refunded, no logs', async () => {
            const { revertErc20 } = richFailedTxs(rich);
            const rc = await sei.send('eth_getTransactionReceipt', [revertErc20.hash]);
            assertFailedReceipt(rc, revertErc20);
        });
    });

    describe('contract-creation receipt', () => {
        it('identifies the creation via contractAddress, and `to` (if present) is null', async () => {
            const creation = rich.txs.find(t => t.to === null)!;
            const rc = await sei.send('eth_getTransactionReceipt', [creation.hash]);
            expect(rc.contractAddress, 'creation receipt sets contractAddress').to.match(
                /^0x[0-9a-fA-F]{40}$/,
            );
            if ('to' in rc) {
                expect(rc.to, 'creation receipt to, when present, is null').to.equal(null);
            }
        });
    });

    describe('geth schema parity', () => {
        it('exposes the same key set as geth for an EIP-1559 receipt', async () => {
            const sent1559 = rich.txs.find(t => t.type === 2 && t.to !== null)!;
            const [s, g] = await Promise.all([
                sei.send('eth_getTransactionReceipt', [sent1559.hash]),
                geth.send('eth_getTransactionReceipt', [gethOne.tx.hash]),
            ]);
            expect(Object.keys(s).sort(), 'receipt key set parity').to.deep.equal(
                Object.keys(g).sort(),
            );
        });
    });

    describe('error handling parity with geth', () => {
        it('an unknown hash returns null on both chains', async () => {
            const unknown = '0x' + 'cd'.repeat(32);
            const [s, g] = await Promise.all([
                sei.send('eth_getTransactionReceipt', [unknown]),
                geth.send('eth_getTransactionReceipt', [unknown]),
            ]);
            expect(s, 'Sei null').to.equal(null);
            expect(g, 'geth null').to.equal(null);
        });

        it('empty params fail identically (-32602, missing argument 0)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getTransactionReceipt', []),
                rawGeth('eth_getTransactionReceipt', []),
            ]);
            expectSameError(s, g);
        });

        it('a wrong-length hash fails identically (-32602, common.Hash)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getTransactionReceipt', ['0xabcd']),
                rawGeth('eth_getTransactionReceipt', ['0xabcd']),
            ]);
            expectSameError(s, g);
        });
    });
});
