import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, claimPool, expectSameError } from '../utils/testUtils';
import { EvmAccount, fundFromUnlocked } from '../utils/evmUtils';
import { ADDRESS } from '../utils/format';
import { cosmosBankSend, generateSeiAddress, bankBalanceUsei, CosmosBankSend } from '../utils/cosmosUtils';
import {
    sharedRichBlock,
    sendSingleTx,
    sendRevertingTx,
    computeLogsBloom,
    expectedTransferGas,
    STAKING_PRECOMPILE_ADDRESS,
    RichBlock,
    SentTx,
    USEI,
    TX_RECEIPT_SHARED_FIELDS,
    blockReceipts,
    assertCanonicalReceipt,
    assertCumulativeGasSeries,
    expectedEffectiveGasPrice,
    richFailedTxs,
    assertFailedReceipt,
    assertRawTxMatches,
    RAW_TX_BY_HASH,
    RAW_TX_BY_BLOCK_HASH_AND_INDEX,
    RAW_TX_BY_BLOCK_NUMBER_AND_INDEX,
} from '../utils/txUtils';

// eth_getBlockReceipts: one Sei block with every tx type; assert receipt fields,
// cross-reference block/tx/receipt views, reconcile gas/fees/balances, check geth parity.
describe('eth_getBlockReceipts', function () {
    this.timeout(300 * 1000);

    const { sei, geth } = bothProviders();

    let runtime: RuntimeState;
    let richSei: RichBlock;
    let baseFee: bigint;
    let seiOne: { number: number; hash: string; tx: SentTx };
    let gethOne: { number: number; hash: string; tx: SentTx };
    let seiFailed: SentTx;
    let gethFailed: SentTx;
    let gethSigner: EvmAccount;
    let gethCreate: ethers.TransactionReceipt;

    before(async function () {
        this.timeout(300 * 1000);
        runtime = readRuntimeState();
        const extra = claimPool(runtime, sei, 2, 'eth_getBlockReceipts');

        const gethDev: string = (await geth.send('eth_accounts', []))[0];
        gethSigner = EvmAccount.fromPrivateKey(ethers.Wallet.createRandom().privateKey, geth);
        await fundFromUnlocked(geth, gethDev, gethSigner.address, ethers.parseEther('10'));

        richSei = await sharedRichBlock(sei, runtime);
        seiOne = await sendSingleTx(sei, extra[0]);
        gethOne = await sendSingleTx(geth, gethSigner);
        seiFailed = await sendRevertingTx(sei, extra[1], runtime.contracts.erc20);
        gethFailed = await sendRevertingTx(geth, gethSigner, runtime.contracts.erc20Geth);

        // A minimal contract creation on geth (init code returns empty runtime) so we can
        // compare a creation receipt against Sei's (which omits the `to` field).
        const created = await gethSigner.wallet.sendTransaction({ data: '0x60006000f3', gasLimit: 100_000n });
        gethCreate = (await created.wait(1, 60_000))!;

        const blk = await sei.send('eth_getBlockByNumber', [ethers.toQuantity(richSei.number), false]);
        baseFee = BigInt(blk.baseFeePerGas);
    });

    describe('receipt array shape (populated Sei block)', () => {
        it('returns one canonical receipt per transaction, in transaction-index order', async () => {
            const receipts = await blockReceipts(sei, richSei.number);
            expect(receipts.length, 'one receipt per sent tx').to.equal(richSei.txs.length);
            receipts.forEach((rc, i) => assertCanonicalReceipt(rc, richSei.hash, richSei.number, i));
            const hashes = receipts.map(r => r.transactionHash);
            for (const sent of richSei.txs) {
                expect(hashes, `receipt present for ${sent.kind}`).to.include(sent.hash);
            }
        });

        it('the by-number and by-hash lookups return byte-identical receipt arrays', async () => {
            const [viaNumber, viaHash] = await Promise.all([
                blockReceipts(sei, richSei.number),
                blockReceipts(sei, richSei.hash),
            ]);
            expect(viaHash).to.deep.equal(viaNumber);
        });

        it('the two included-but-failed txs surface as status-0 receipts in the block', async () => {
            const receipts = await blockReceipts(sei, richSei.number);
            const byHash = new Map<string, any>(receipts.map(r => [r.transactionHash, r]));
            const { outOfGas, revertErc20 } = richFailedTxs(richSei);
            for (const sent of [outOfGas, revertErc20]) {
                const rc = byHash.get(sent.hash);
                expect(rc, `${sent.kind} receipt present in the block`).to.not.equal(undefined);
                assertFailedReceipt(rc, sent);
            }
        });
    });

    describe('cross-reference: receipts ⇄ block transactions (byNumber & byHash)', () => {
        it('byNumber, byHash and the receipts all describe the same ordered transaction set', async () => {
            const [bn, bh, receipts] = await Promise.all([
                sei.send('eth_getBlockByNumber', [ethers.toQuantity(richSei.number), true]),
                sei.send('eth_getBlockByHash', [richSei.hash, true]),
                blockReceipts(sei, richSei.number),
            ]);
            const fromNumber = bn.transactions.map((t: any) => t.hash);
            const fromHash = bh.transactions.map((t: any) => t.hash);
            const fromReceipts = receipts.map(r => r.transactionHash);
            expect(fromHash, 'byHash tx order == byNumber tx order').to.deep.equal(fromNumber);
            expect(fromReceipts, 'receipt order == block tx order').to.deep.equal(fromNumber);
        });

        it('each receipt lines up with its transaction object on every shared field', async () => {
            const [block, receipts] = await Promise.all([
                sei.send('eth_getBlockByNumber', [ethers.toQuantity(richSei.number), true]),
                blockReceipts(sei, richSei.number),
            ]);
            const txByHash = new Map<string, any>(block.transactions.map((t: any) => [t.hash, t]));
            for (const rc of receipts) {
                const tx = txByHash.get(rc.transactionHash);
                expect(tx, `tx object for receipt ${rc.transactionHash}`).to.not.equal(undefined);
                expect(rc.from.toLowerCase(), 'from matches the tx').to.equal(tx.from.toLowerCase());
                expect((rc.to ?? null)?.toLowerCase() ?? null, 'to matches the tx').to.equal(
                    (tx.to ?? null)?.toLowerCase() ?? null,
                );
                expect(BigInt(rc.transactionIndex), 'index matches the tx').to.equal(
                    BigInt(tx.transactionIndex),
                );
                expect(rc.blockHash, 'blockHash matches the tx').to.equal(tx.blockHash);
                expect(BigInt(rc.blockNumber), 'blockNumber matches the tx').to.equal(
                    BigInt(tx.blockNumber),
                );
                expect(BigInt(rc.type), 'type matches the tx').to.equal(BigInt(tx.type));
                expect(BigInt(rc.effectiveGasPrice), 'effectiveGasPrice == tx.gasPrice').to.equal(
                    BigInt(tx.gasPrice),
                );
            }
        });

        it('every receipt is byte-identical to its standalone eth_getTransactionReceipt', async () => {
            const receipts = await blockReceipts(sei, richSei.number);
            for (const rc of receipts) {
                const single = await sei.send('eth_getTransactionReceipt', [rc.transactionHash]);
                expect(single, `block receipt == single receipt for ${rc.transactionHash}`).to.deep.equal(
                    rc,
                );
            }
        });
    });

    describe('cross-reference: receipts ⇄ eth_getTransactionBy* lookups', () => {
        it('byHash, byBlockHashAndIndex and byBlockNumberAndIndex return byte-identical objects', async () => {
            const block = await sei.send('eth_getBlockByNumber', [
                ethers.toQuantity(richSei.number),
                true,
            ]);
            for (const txInBlock of block.transactions) {
                const i = ethers.toQuantity(txInBlock.transactionIndex);
                const [byHash_, byBH, byBN] = await Promise.all([
                    sei.send('eth_getTransactionByHash', [txInBlock.hash]),
                    sei.send('eth_getTransactionByBlockHashAndIndex', [richSei.hash, i]),
                    sei.send('eth_getTransactionByBlockNumberAndIndex', [
                        ethers.toQuantity(richSei.number),
                        i,
                    ]),
                ]);
                expect(byBH, `byBlockHashAndIndex == byHash @${i}`).to.deep.equal(byHash_);
                expect(byBN, `byBlockNumberAndIndex == byHash @${i}`).to.deep.equal(byHash_);
                expect(byHash_, `block.transactions[${i}] == byHash`).to.deep.equal(txInBlock);
            }
        });

        it('the tx object and its receipt converge on every shared identity field', async () => {
            const receipts = await blockReceipts(sei, richSei.number);
            for (const rc of receipts) {
                const tx = await sei.send('eth_getTransactionByHash', [rc.transactionHash]);
                expect(tx.hash, 'tx.hash == receipt.transactionHash').to.equal(rc.transactionHash);
                expect(tx.from.toLowerCase(), 'from').to.equal(rc.from.toLowerCase());
                expect((tx.to ?? null)?.toLowerCase() ?? null, 'to').to.equal(
                    (rc.to ?? null)?.toLowerCase() ?? null,
                );
                expect(BigInt(tx.transactionIndex), 'transactionIndex').to.equal(
                    BigInt(rc.transactionIndex),
                );
                expect(tx.blockHash, 'blockHash').to.equal(rc.blockHash);
                expect(BigInt(tx.blockNumber), 'blockNumber').to.equal(BigInt(rc.blockNumber));
                expect(BigInt(tx.type), 'type').to.equal(BigInt(rc.type));
                // The one non-identity convergence: the tx's realised price (block-stamped
                // gasPrice) equals the receipt's effectiveGasPrice.
                expect(BigInt(tx.gasPrice), 'tx.gasPrice == receipt.effectiveGasPrice').to.equal(
                    BigInt(rc.effectiveGasPrice),
                );
            }
        });

        it('tx-only and receipt-only fields are disjoint — overlap is exactly the identity set', async () => {
            const receipts = await blockReceipts(sei, richSei.number);
            for (const rc of receipts) {
                const tx = await sei.send('eth_getTransactionByHash', [rc.transactionHash]);
                // Shared keys are identity/position fields, minus `to` for creations (Sei drops it).
                // Everything else partitions: signed-intent on the tx, outcome on the receipt,
                // realised price renamed gasPrice → effectiveGasPrice.
                const expectedShared = TX_RECEIPT_SHARED_FIELDS.filter(f => f in rc);
                const actualShared = Object.keys(tx).filter(k => k in rc);
                expect(actualShared.sort(), `overlap for ${rc.transactionHash}`).to.deep.equal(
                    [...expectedShared].sort(),
                );
                // signed-intent fields never leak into the receipt, and outcome fields never into the tx.
                for (const f of ['nonce', 'value', 'input', 'gas', 'gasPrice', 'r', 's', 'v']) {
                    expect(tx, `tx has ${f}`).to.have.property(f);
                    expect(rc, `receipt lacks ${f}`).to.not.have.property(f);
                }
                for (const f of ['gasUsed', 'cumulativeGasUsed', 'status', 'logsBloom', 'effectiveGasPrice']) {
                    expect(rc, `receipt has ${f}`).to.have.property(f);
                    expect(tx, `tx lacks ${f}`).to.not.have.property(f);
                }
            }
        });

        it('[divergence] geth stamps blockTimestamp on the tx object; Sei does not', async () => {
            const [s, g] = await Promise.all([
                sei.send('eth_getTransactionByHash', [seiOne.tx.hash]),
                geth.send('eth_getTransactionByHash', [gethOne.tx.hash]),
            ]);
            const sKeys = Object.keys(s).sort();
            const gKeys = Object.keys(g).sort();
            // geth carries exactly one extra field, the including block's timestamp.
            expect(gKeys.filter(k => !sKeys.includes(k)), 'geth-only tx fields').to.deep.equal([
                'blockTimestamp',
            ]);
            expect(sKeys.filter(k => !gKeys.includes(k)), 'Sei-only tx fields').to.deep.equal([]);
            // It is purely informational: it equals the block's own timestamp, so it adds no
            // state — just saves a second round-trip. Sei omitting it is consistent with the
            // receipt (which also has no timestamp); query the block for the time instead.
            const gBlock = await geth.send('eth_getBlockByNumber', [g.blockNumber, false]);
            expect(g.blockTimestamp, 'blockTimestamp == block.timestamp').to.equal(gBlock.timestamp);
        });
    });

    describe('eth_getTransactionBy* — null & error semantics (parity with geth)', () => {
        it('byHash: empty params fail identically (-32602, missing argument 0)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getTransactionByHash', []),
                rawGeth('eth_getTransactionByHash', []),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 0/);
            expectSameError(s, g);
        });

        it('byHash: a wrong-length hash fails identically (-32602, common.Hash)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getTransactionByHash', ['0x1234']),
                rawGeth('eth_getTransactionByHash', ['0x1234']),
            ]);
            expectJsonRpcError(s, -32602, /hex string has length 4, want 64 for common\.Hash/);
            expectSameError(s, g);
        });

        it('byHash: an unknown hash returns null on both chains', async () => {
            const unknown = '0x' + 'ab'.repeat(32);
            const [s, g] = await Promise.all([
                sei.send('eth_getTransactionByHash', [unknown]),
                geth.send('eth_getTransactionByHash', [unknown]),
            ]);
            expect(s, 'Sei null').to.equal(null);
            expect(g, 'geth null').to.equal(null);
        });

        it('byBlockHashAndIndex: a missing index fails identically (-32602, argument 1)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getTransactionByBlockHashAndIndex', [richSei.hash]),
                rawGeth('eth_getTransactionByBlockHashAndIndex', [gethOne.hash]),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 1/);
            expectSameError(s, g);
        });

        it('byBlockHashAndIndex: an out-of-range index returns null on both chains', async () => {
            const [s, g] = await Promise.all([
                sei.send('eth_getTransactionByBlockHashAndIndex', [richSei.hash, '0xffff']),
                geth.send('eth_getTransactionByBlockHashAndIndex', [gethOne.hash, '0xffff']),
            ]);
            expect(s, 'Sei null').to.equal(null);
            expect(g, 'geth null').to.equal(null);
        });

        it('byBlockHashAndIndex on an unknown block returns null on both chains', async () => {
            const unknown = '0x' + 'ab'.repeat(32);
            const [s, g] = await Promise.all([
                rawSei('eth_getTransactionByBlockHashAndIndex', [unknown, '0x0']),
                rawGeth('eth_getTransactionByBlockHashAndIndex', [unknown, '0x0']),
            ]);
            // Both treat an absent block as "no such tx": a null result rather than an error.
            expect(g.result, 'geth returns null for an unknown block').to.equal(null);
            expect(g.error, 'geth does not error').to.equal(undefined);
            expect(s.result, 'Sei returns null for an unknown block').to.equal(null);
            expect(s.error, 'Sei does not error').to.equal(undefined);
        });

        it('byBlockNumberAndIndex: an out-of-range index returns null on both chains', async () => {
            const [s, g] = await Promise.all([
                sei.send('eth_getTransactionByBlockNumberAndIndex', [
                    ethers.toQuantity(richSei.number),
                    '0xffff',
                ]),
                geth.send('eth_getTransactionByBlockNumberAndIndex', [
                    ethers.toQuantity(gethOne.number),
                    '0xffff',
                ]),
            ]);
            expect(s, 'Sei null').to.equal(null);
            expect(g, 'geth null').to.equal(null);
        });

        it('byBlockNumberAndIndex on a future block returns null on both chains', async () => {
            const future = ethers.toQuantity((await sei.getBlockNumber()) + 10_000_000);
            const [s, g] = await Promise.all([
                rawSei('eth_getTransactionByBlockNumberAndIndex', [future, '0x0']),
                rawGeth('eth_getTransactionByBlockNumberAndIndex', [future, '0x0']),
            ]);
            // A not-yet-mined height has no such tx: both return a null result, not an error.
            expect(g.result, 'geth returns null for a future block').to.equal(null);
            expect(g.error, 'geth does not error').to.equal(undefined);
            expect(s.result, 'Sei returns null for a future block').to.equal(null);
            expect(s.error, 'Sei does not error').to.equal(undefined);
        });
    });

    describe('eth_getRawTransaction* — raw signed tx (geth) & Sei divergence', () => {
        it('[divergence] Sei does not implement the raw-transaction endpoints (-32601)', async () => {
            const envelopes = await Promise.all([
                rawSei(RAW_TX_BY_HASH, [seiOne.tx.hash]),
                rawSei(RAW_TX_BY_BLOCK_HASH_AND_INDEX, [seiOne.hash, '0x0']),
                rawSei(RAW_TX_BY_BLOCK_NUMBER_AND_INDEX, [ethers.toQuantity(seiOne.number), '0x0']),
            ]);
            // geth serves these; Sei has not registered them, so every one is method-not-found.
            for (const env of envelopes) {
                expectJsonRpcError(env, -32601, /does not exist\/is not available/);
            }
        });

        it('[geth] the three raw lookups return the identical raw signed transaction', async () => {
            const [byHashRaw, byBlockHashRaw, byBlockNumberRaw] = await Promise.all([
                geth.send(RAW_TX_BY_HASH, [gethOne.tx.hash]),
                geth.send(RAW_TX_BY_BLOCK_HASH_AND_INDEX, [gethOne.hash, '0x0']),
                geth.send(RAW_TX_BY_BLOCK_NUMBER_AND_INDEX, [
                    ethers.toQuantity(gethOne.number),
                    '0x0',
                ]),
            ]);
            expect(byHashRaw, 'raw is non-empty 0x data').to.match(/^0x[0-9a-f]+$/i);
            expect(byBlockHashRaw, 'byBlockHashAndIndex == byHash').to.equal(byHashRaw);
            expect(byBlockNumberRaw, 'byBlockNumberAndIndex == byHash').to.equal(byHashRaw);
        });

        it('[geth] the raw bytes decode to exactly the reported transaction (transfer)', async () => {
            const [raw, txObject] = await Promise.all([
                geth.send(RAW_TX_BY_HASH, [gethOne.tx.hash]),
                geth.send('eth_getTransactionByHash', [gethOne.tx.hash]),
            ]);
            assertRawTxMatches(raw, txObject);
        });

        it('[geth] the raw bytes decode to exactly the reported transaction (creation)', async () => {
            const [raw, txObject] = await Promise.all([
                geth.send(RAW_TX_BY_HASH, [gethCreate.hash]),
                geth.send('eth_getTransactionByHash', [gethCreate.hash]),
            ]);
            const decoded = assertRawTxMatches(raw, txObject);
            expect(decoded.to, 'a creation has no recipient in the signed bytes').to.equal(null);
        });

        it('an unknown hash: geth returns empty "0x", Sei still answers -32601', async () => {
            const unknown = '0x' + 'ab'.repeat(32);
            const [g, s] = await Promise.all([
                geth.send(RAW_TX_BY_HASH, [unknown]),
                rawSei(RAW_TX_BY_HASH, [unknown]),
            ]);
            expect(g, 'geth returns empty bytes for an unknown tx').to.equal('0x');
            expectJsonRpcError(s, -32601, /does not exist\/is not available/);
        });
    });

    describe('gas, fees, tip and balances reconcile (multiple users)', () => {
        it('block.gasUsed equals Σ receipt gasUsed and cumulativeGasUsed is consistent', async () => {
            const [block, receipts] = await Promise.all([
                sei.send('eth_getBlockByNumber', [ethers.toQuantity(richSei.number), false]),
                blockReceipts(sei, richSei.number),
            ]);
            // block.gasUsed (eth) sums only EVM receipts, and eth_getBlockReceipts lists only
            // EVM receipts, so this equality is exact.
            const summed = receipts.reduce((acc, rc) => acc + BigInt(rc.gasUsed), 0n);
            expect(summed, 'block.gasUsed == Σ EVM-visible receipt.gasUsed').to.equal(BigInt(block.gasUsed));

            const ordered = [...receipts]
                .map(rc => ({
                    index: Number(BigInt(rc.transactionIndex)),
                    gasUsed: BigInt(rc.gasUsed),
                    cumulativeGasUsed: BigInt(rc.cumulativeGasUsed),
                }))
                .sort((a, b) => a.index - b.index);
            assertCumulativeGasSeries(ordered, BigInt(block.gasUsed), richSei.cosmosShellGas);
        });

        it('pure transfers burn exactly the intrinsic gas', async () => {
            const [block, receipts] = await Promise.all([
                sei.send('eth_getBlockByNumber', [ethers.toQuantity(richSei.number), true]),
                blockReceipts(sei, richSei.number),
            ]);
            const txByHash = new Map<string, any>(block.transactions.map((t: any) => [t.hash, t]));
            const rcByHash = new Map<string, any>(receipts.map(r => [r.transactionHash, r]));
            for (const kind of ['legacy', 'accessList', 'eip1559'] as const) {
                const sent = richSei.txs.find(t => t.kind === kind)!;
                const rc = rcByHash.get(sent.hash);
                expect(BigInt(rc.gasUsed), `${kind} intrinsic gas`).to.equal(
                    expectedTransferGas(txByHash.get(sent.hash)),
                );
            }
        });

        it('each receipt effectiveGasPrice equals base fee + the capped tip exactly', async () => {
            const receipts = await blockReceipts(sei, richSei.number);
            const rcByHash = new Map<string, any>(receipts.map(r => [r.transactionHash, r]));
            for (const sent of richSei.txs) {
                const rc = rcByHash.get(sent.hash);
                const expected = expectedEffectiveGasPrice(sent, baseFee);
                expect(BigInt(rc.effectiveGasPrice), `${sent.kind} effectiveGasPrice`).to.equal(expected);
                // The surfaced priority fee (tip) is effectiveGasPrice - baseFee.
                const tip = BigInt(rc.effectiveGasPrice) - baseFee;
                if (sent.maxPriorityFeePerGas !== undefined) {
                    const room = sent.maxFeePerGas! - baseFee;
                    const cappedTip = sent.maxPriorityFeePerGas < room ? sent.maxPriorityFeePerGas : room;
                    expect(tip, `${sent.kind} effective tip`).to.equal(cappedTip);
                }
            }
        });

        it('each sender is debited gasUsed×effectiveGasPrice + value, each recipient credited value', async () => {
            const receipts = await blockReceipts(sei, richSei.number);
            const rcByHash = new Map<string, any>(receipts.map(r => [r.transactionHash, r]));
            for (const sent of richSei.txs) {
                const rc = rcByHash.get(sent.hash);
                const fee = BigInt(rc.gasUsed) * BigInt(rc.effectiveGasPrice);
                const [before, after] = await Promise.all([
                    sei.getBalance(sent.sender, richSei.number - 1),
                    sei.getBalance(sent.sender, richSei.number),
                ]);
                const spent = before - after;
                const want = sent.value + fee;
                const drift = spent > want ? spent - want : want - spent;
                expect(
                    drift <= USEI,
                    `${sent.kind}: spent ${spent} vs value+fee ${want} (drift ${drift})`,
                ).to.equal(true);
            }
            for (const kind of ['legacy', 'accessList', 'eip1559'] as const) {
                const sent = richSei.txs.find(t => t.kind === kind)!;
                const [before, after] = await Promise.all([
                    sei.getBalance(sent.to!, richSei.number - 1),
                    sei.getBalance(sent.to!, richSei.number),
                ]);
                expect(before, `${kind} recipient started empty`).to.equal(0n);
                expect(after, `${kind} recipient credited exactly the value`).to.equal(sent.value);
            }
        });
    });

    describe('logs & bloom', () => {
        it('the erc20 receipt emitted a Transfer log and its bloom matches its own logs', async () => {
            const receipts = await blockReceipts(sei, richSei.number);
            const rc = receipts.find(
                r => r.transactionHash === richSei.txs.find(t => t.kind === 'erc20')!.hash,
            )!;
            expect(rc.logs.length >= 1, 'erc20 transfer emitted at least one log').to.equal(true);
            const transferTopic = ethers.id('Transfer(address,address,uint256)');
            expect(
                rc.logs.some((l: any) => l.topics[0] === transferTopic),
                'a Transfer event is present',
            ).to.equal(true);
            expect(rc.logsBloom, 'receipt bloom == Bloom(its logs)').to.equal(
                computeLogsBloom([rc] as any),
            );
        });

        it('the block logsBloom equals the OR of every receipt bloom', async () => {
            const [block, receipts] = await Promise.all([
                sei.send('eth_getBlockByNumber', [ethers.toQuantity(richSei.number), false]),
                blockReceipts(sei, richSei.number),
            ]);
            expect(block.logsBloom, 'block bloom == Bloom(all receipts logs)').to.equal(
                computeLogsBloom(receipts as any),
            );
        });
    });

    describe('contract deployment & precompile receipts', () => {
        it('the deployment receipt carries the created contractAddress with live code', async () => {
            const receipts = await blockReceipts(sei, richSei.number);
            const sent = richSei.txs.find(t => t.kind === 'deploy')!;
            const rc = receipts.find(r => r.transactionHash === sent.hash)!;
            expect(rc.to ?? null, 'creation receipt has no recipient').to.equal(null);
            expect(rc.contractAddress, 'contractAddress is set').to.match(ADDRESS);
            expect(rc.contractAddress!.toLowerCase(), 'matches the local receipt').to.equal(
                sent.receipt.contractAddress!.toLowerCase(),
            );
            const code = await sei.getCode(rc.contractAddress!, richSei.number);
            expect(code.length, 'deployed code is non-empty').to.be.greaterThan(2);
        });

        it('the precompile receipt succeeded and has no contractAddress', async () => {
            const receipts = await blockReceipts(sei, richSei.number);
            const sent = richSei.txs.find(t => t.kind === 'precompile')!;
            const rc = receipts.find(r => r.transactionHash === sent.hash)!;
            expect(rc.status, 'precompile call succeeded').to.equal('0x1');
            expect(rc.contractAddress, 'no contract created').to.equal(null);
            expect(rc.to!.toLowerCase(), 'targets the staking precompile').to.equal(
                STAKING_PRECOMPILE_ADDRESS,
            );
        });
    });

    describe('failed transactions are still included', () => {
        it('[Sei] a reverted tx appears with status 0x0 and is counted in the block', async () => {
            expect(seiFailed.receipt.status, 'tx reverted').to.equal(0);
            const receipts = await blockReceipts(sei, seiFailed.receipt.blockNumber);
            const rc = receipts.find(r => r.transactionHash === seiFailed.hash);
            expect(rc, 'failed tx present in block receipts').to.not.equal(undefined);
            expect(rc!.status, 'status reflects the revert').to.equal('0x0');
            expect(BigInt(rc!.gasUsed) > 0n, 'a reverted tx still burns gas').to.equal(true);
            const single = await sei.send('eth_getTransactionReceipt', [seiFailed.hash]);
            expect(single).to.deep.equal(rc);
        });
    });

    describe('geth parity (single transaction)', () => {
        it('both chains expose the identical receipt field set', async () => {
            const [s, g] = await Promise.all([blockReceipts(sei, seiOne.number), blockReceipts(geth, gethOne.number)]);
            expect(s.length, 'Sei single-tx block').to.equal(1);
            expect(g.length, 'geth single-tx block').to.equal(1);
            assertCanonicalReceipt(s[0], seiOne.hash, seiOne.number, 0);
            assertCanonicalReceipt(g[0], gethOne.hash, gethOne.number, 0);
            expect(Object.keys(s[0]).sort(), 'identical key set').to.deep.equal(Object.keys(g[0]).sort());
            expect(BigInt(s[0].type), 'both EIP-1559').to.equal(2n);
            expect(BigInt(g[0].type), 'both EIP-1559').to.equal(2n);
        });

        it('by-number and by-hash agree on geth too', async () => {
            const [viaNumber, viaHash] = await Promise.all([
                blockReceipts(geth, gethOne.number),
                blockReceipts(geth, gethOne.hash),
            ]);
            expect(viaHash).to.deep.equal(viaNumber);
        });

        it('creation receipts identify the creation via contractAddress (to, if present, is null)', async () => {
            // execution-apis ReceiptInfo: `to` is NOT required (oneOf[null,address]); contractAddress
            // is the canonical creation signal. geth emits `to: null`, Sei omits the key — both are
            // spec-compliant, so assert the creation address and only check `to` where present.
            const seiReceipts = await blockReceipts(sei, richSei.number);
            const seiDeploy = seiReceipts.find(
                r => r.transactionHash === richSei.txs.find(t => t.kind === 'deploy')!.hash,
            )!;
            const gethReceipts = await blockReceipts(geth, gethCreate.blockNumber);
            const gethDeploy = gethReceipts.find(r => r.transactionHash === gethCreate.hash)!;

            expect(seiDeploy.contractAddress, 'Sei creation contractAddress').to.match(ADDRESS);
            expect(gethDeploy.contractAddress, 'geth creation contractAddress').to.match(ADDRESS);
        });
    });

    describe('dual-VM: a Cosmos bank send sharing the block', () => {
        // Sei executes native Cosmos txs and EVM txs in the same blocks, but the EVM
        // JSON-RPC surface must only ever expose the EVM half. Land a bank MsgSend in the
        // same height as an EVM transfer and prove the receipts list ignores the Cosmos tx.
        let height: number | undefined;
        let cosmos: CosmosBankSend;
        let evm: { number: number; hash: string; tx: SentTx };
        let recipientSei: string;
        const AMOUNT_USEI = 123_456n;

        before(async function () {
            this.timeout(180 * 1000);
            const evmSigner = claimPool(runtime, sei, 1, 'eth_getBlockReceipts:cosmos')[0];
            for (let attempt = 0; attempt < 4 && height === undefined; attempt++) {
                const recipient = await generateSeiAddress();
                const [cos, ev] = await Promise.all([
                    cosmosBankSend(runtime.funded.adminMnemonic, recipient, AMOUNT_USEI),
                    sendSingleTx(sei, evmSigner),
                ]);
                if (cos.code === 0 && cos.height === ev.number) {
                    height = cos.height;
                    cosmos = cos;
                    evm = ev;
                    recipientSei = recipient;
                }
            }
            if (height === undefined) this.skip();
        });

        it('the Cosmos bank send and the EVM tx really share one Sei block', () => {
            expect(cosmos.code, 'bank send succeeded').to.equal(0);
            expect(evm.number, 'EVM tx mined at the shared height').to.equal(height);
            expect(cosmos.height, 'Cosmos tx mined at the shared height').to.equal(height);
        });

        it('eth_getBlockReceipts includes the EVM tx but never the Cosmos tx', async () => {
            const receipts = await blockReceipts(sei, height!);
            const cosmosAsEvmHash = '0x' + cosmos.hash.toLowerCase();
            const hashes = receipts.map(r => r.transactionHash.toLowerCase());
            expect(hashes, 'EVM tx present in receipts').to.include(evm.tx.hash.toLowerCase());
            expect(hashes, 'Cosmos tx absent from receipts').to.not.include(cosmosAsEvmHash);
            for (const rc of receipts) {
                const tx = await sei.send('eth_getTransactionByHash', [rc.transactionHash]);
                expect(tx, `EVM tx exists for receipt ${rc.transactionHash}`).to.not.equal(null);
                expect(BigInt(tx.blockNumber)).to.equal(BigInt(height!));
            }
        });

        it('receipt count equals the EVM block tx count (Cosmos tx is not counted)', async () => {
            const [block, receipts] = await Promise.all([
                sei.send('eth_getBlockByNumber', [ethers.toQuantity(height!), false]),
                blockReceipts(sei, height!),
            ]);
            const cosmosAsEvmHash = '0x' + cosmos.hash.toLowerCase();
            expect(receipts.length, 'one receipt per EVM tx').to.equal(block.transactions.length);
            expect(
                block.transactions.map((h: string) => h.toLowerCase()),
                'Cosmos tx absent from the EVM block tx list',
            ).to.not.include(cosmosAsEvmHash);
        });

        it('the Cosmos tx hash is unknown to the EVM tx/receipt endpoints', async () => {
            const cosmosAsEvmHash = '0x' + cosmos.hash.toLowerCase();
            const [tx, rc] = await Promise.all([
                sei.send('eth_getTransactionByHash', [cosmosAsEvmHash]),
                sei.send('eth_getTransactionReceipt', [cosmosAsEvmHash]),
            ]);
            expect(tx, 'no EVM tx for the Cosmos hash').to.equal(null);
            expect(rc, 'no EVM receipt for the Cosmos hash').to.equal(null);
        });

        it('the bank send actually moved usei in that block (state changed off the EVM)', async () => {
            const [before, after] = await Promise.all([
                bankBalanceUsei(recipientSei, height! - 1),
                bankBalanceUsei(recipientSei, height!),
            ]);
            expect(before, 'recipient started empty').to.equal(0n);
            expect(after, 'recipient credited the exact usei amount').to.equal(AMOUNT_USEI);
        });
    });

    describe('lookup semantics', () => {
        it('the latest tag returns canonical, correctly indexed receipts', async () => {
            const receipts: any[] = await sei.send('eth_getBlockReceipts', ['latest']);
            expect(receipts, 'latest receipts is an array').to.be.an('array');
            receipts.forEach((rc, i) =>
                assertCanonicalReceipt(rc, rc.blockHash, Number(BigInt(rc.blockNumber)), i),
            );
        });

        it('the earliest (genesis) block returns an empty receipt array', async () => {
            const receipts = await sei.send('eth_getBlockReceipts', ['earliest']);
            expect(receipts, 'genesis has no transactions').to.deep.equal([]);
        });

        it('an unknown block hash returns null on both chains', async () => {
            const unknown = '0x' + 'ab'.repeat(32);
            const [s, g] = await Promise.all([
                sei.send('eth_getBlockReceipts', [unknown]),
                geth.send('eth_getBlockReceipts', [unknown]),
            ]);
            expect(s, 'Sei unknown hash is null').to.equal(null);
            expect(g, 'geth unknown hash is null').to.equal(null);
        });
    });

    describe('wrong params / error handling (parity with geth)', () => {
        it('empty params fail identically (-32602, missing required argument 0)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockReceipts', []),
                rawGeth('eth_getBlockReceipts', []),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 0/);
            expectSameError(s, g);
        });

        it('too many positional args fail identically (-32602, want at most 1)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockReceipts', [richSei.hash, {}]),
                rawGeth('eth_getBlockReceipts', [gethOne.hash, {}]),
            ]);
            expectJsonRpcError(s, -32602, /too many arguments, want at most 1/);
            expectSameError(s, g);
        });

        it('non-array params fail identically (-32602, non-array args)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockReceipts', { block: 'latest' } as any),
                rawGeth('eth_getBlockReceipts', { block: 'latest' } as any),
            ]);
            expectJsonRpcError(s, -32602, /^non-array args$/);
            expectSameError(s, g);
        });

        it('a far-future block returns null on both chains', async () => {
            const future = ethers.toQuantity((await sei.getBlockNumber()) + 10_000_000);
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockReceipts', [future]),
                rawGeth('eth_getBlockReceipts', [future]),
            ]);
            // A not-yet-mined height resolves to an absent block: both return null, not an error.
            expect(g.error, 'geth does not error on a future block').to.equal(undefined);
            expect(g.result, 'geth returns null for a future block').to.equal(null);
            expect(s.error, 'Sei does not error on a future block').to.equal(undefined);
            expect(s.result, 'Sei returns null for a future block').to.equal(null);
        });
    });
});
