import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, expectSameError } from '../utils/testUtils';
import { RichBlock, richFailedTxs, assertFailedReceipt } from '../utils/txUtils';
import {
    sharedRichBlock,
    sharedGethTx,
    assertTxObject,
    assertTxKeysetParity,
    logsByBlockHash,
} from '../utils/txLookupUtils';

describe('eth_getTransactionByBlockHashAndIndex', function () {
    this.timeout(300 * 1000);

    const { sei, geth } = bothProviders();

    let runtime: RuntimeState;
    let rich: RichBlock;
    let blockByHash: any;
    let gethOne: { number: number; hash: string; tx: any };

    const rpcCall = 'eth_getTransactionByBlockHashAndIndex';

    before(async function () {
        this.timeout(300 * 1000);
        runtime = readRuntimeState();
        rich = await sharedRichBlock(sei, runtime);
        [blockByHash, gethOne] = await Promise.all([
            sei.send('eth_getBlockByHash', [rich.hash, true]),
            sharedGethTx(geth),
        ]);
    });

    describe('schema + positional correctness', () => {
        it('returns a canonical, type-correct object at every index with the matching transactionIndex', async () => {
            for (let i = 0; i < blockByHash.transactions.length; i++) {
                const tx = await sei.send(rpcCall, [rich.hash, ethers.toQuantity(i)]);
                expect(tx, `tx exists at index ${i}`).to.not.equal(null);
                assertTxObject(tx, rich);
                expect(BigInt(tx.transactionIndex), `transactionIndex == ${i}`).to.equal(BigInt(i));
            }
        });

        it('is byte-identical to eth_getBlockByHash(fullTx=true) at the same index', async () => {
            for (let i = 0; i < blockByHash.transactions.length; i++) {
                const tx = await sei.send(rpcCall, [rich.hash, ethers.toQuantity(i)]);
                expect(tx, `index ${i} matches block entry`).to.deep.equal(
                    blockByHash.transactions[i],
                );
            }
        });

        it('agrees with eth_getTransactionByHash for the same transaction', async () => {
            for (let i = 0; i < blockByHash.transactions.length; i++) {
                const [byIndex, byHash] = await Promise.all([
                    sei.send(rpcCall, [rich.hash, ethers.toQuantity(i)]),
                    sei.send('eth_getTransactionByHash', [blockByHash.transactions[i].hash]),
                ]);
                expect(byIndex, `byIndex == byHash at ${i}`).to.deep.equal(byHash);
            }
        });
    });

    describe('cross-reference with eth_getLogs', () => {
        it('the tx at a log’s transactionIndex carries that log’s transactionHash', async () => {
            const logs = await logsByBlockHash(sei, rich.hash);
            expect(logs.length, 'the rich block emits logs').to.be.greaterThan(0);
            for (const log of logs) {
                const tx = await sei.send(rpcCall, [log.blockHash, log.transactionIndex]);
                expect(tx, `tx at log index ${log.transactionIndex}`).to.not.equal(null);
                expect(tx.hash, 'tx.hash == log.transactionHash').to.equal(log.transactionHash);
            }
        });
    });

    describe('included-but-failed transactions', () => {
        it('exposes the failed txs by index (matching byHash); their receipts are status 0', async () => {
            const { outOfGas, revertErc20 } = richFailedTxs(rich);
            for (const sent of [outOfGas, revertErc20]) {
                const idx = blockByHash.transactions.findIndex((t: any) => t.hash === sent.hash);
                expect(idx, `${sent.kind} present in the block`).to.be.greaterThanOrEqual(0);
                const [byIndex, byHash] = await Promise.all([
                    sei.send(rpcCall, [rich.hash, ethers.toQuantity(idx)]),
                    sei.send('eth_getTransactionByHash', [sent.hash]),
                ]);
                assertTxObject(byIndex, rich);
                expect(byIndex, `byIndex == byHash for ${sent.kind}`).to.deep.equal(byHash);
                const rc = await sei.send('eth_getTransactionReceipt', [sent.hash]);
                assertFailedReceipt(rc, sent);
            }
        });
    });

    describe('geth schema parity', () => {
        it('exposes the same key set as geth for the same (EIP-1559) tx type', async () => {
            // geth's oracle tx is EIP-1559, so compare against the rich block's 1559 tx —
            // index 0 is the legacy tx, whose fee fields differ by design.
            const idx1559 = blockByHash.transactions.findIndex((t: any) => t.type === '0x2');
            const [s, g] = await Promise.all([
                sei.send(rpcCall, [rich.hash, ethers.toQuantity(idx1559)]),
                geth.send(rpcCall, [gethOne.hash, '0x0']),
            ]);
            assertTxKeysetParity(s, g);
        });
    });

    describe('error handling parity with geth', () => {
        it('an out-of-range index returns null on both chains', async () => {
            const [s, g] = await Promise.all([
                sei.send(rpcCall, [rich.hash, '0xffff']),
                geth.send(rpcCall, [gethOne.hash, '0xffff']),
            ]);
            expect(s, 'Sei null').to.equal(null);
            expect(g, 'geth null').to.equal(null);
        });

        it('an unknown block hash returns null on both chains', async () => {
            const unknown = '0x' + 'ab'.repeat(32);
            const [s, g] = await Promise.all([
                sei.send(rpcCall, [unknown, '0x0']),
                geth.send(rpcCall, [unknown, '0x0']),
            ]);
            expect(s, 'Sei null').to.equal(null);
            expect(g, 'geth null').to.equal(null);
        });

        it('a missing index fails identically (-32602, missing argument 1)', async () => {
            const [s, g] = await Promise.all([
                rawSei(rpcCall, [rich.hash]),
                rawGeth(rpcCall, [gethOne.hash]),
            ]);
            expectSameError(s, g);
        });

        it('a wrong-length block hash fails identically (-32602, common.Hash)', async () => {
            const [s, g] = await Promise.all([
                rawSei(rpcCall, ['0x1234', '0x0']),
                rawGeth(rpcCall, ['0x1234', '0x0']),
            ]);
            expectSameError(s, g);
        });

        it('empty params fail identically (-32602, missing argument 0)', async () => {
            const [s, g] = await Promise.all([rawSei(rpcCall, []), rawGeth(rpcCall, [])]);
            expectSameError(s, g);
        });
    });
});
