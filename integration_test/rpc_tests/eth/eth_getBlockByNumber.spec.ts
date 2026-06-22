import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError, JsonRpcEnvelope, Eip1559Params, queryEip1559Params, nextBaseFeeSei } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, claimPool, expectSameError } from '../utils/testUtils';
import { EvmAccount, fundFromUnlocked } from '../utils/evmUtils';
import { USEI } from '../utils/constants';
import {
    sharedRichBlock,
    sendSingleTx,
    sendRevertingTx,
    signBelowIntrinsicTx,
    assertCanonicalHeader,
    assertCanonicalTx,
    assertTxTypeSchema,
    assertGasAccounting,
    assertActualBytesAndSize,
    assertReportedSendFields,
    assertLogsBloom,
    burnGasBurst,
    expectedTransferGas,
    richFailedTxs,
    assertFailedReceipt,
    ACCESS_LIST_FIXTURE,
    CORE_BLOCK_FIELDS,
    SEI_ONLY_BLOCK_FIELDS,
    GETH_ONLY_BLOCK_FIELDS,
    CORE_TX_FIELDS,
    GETH_ONLY_TX_FIELDS,
    STAKING_PRECOMPILE_ADDRESS,
    RichBlock,
    SentTx,
} from '../utils/txUtils';

describe('eth_getBlockByNumber', function () {
    this.timeout(300 * 1000);

    const { sei, geth } = bothProviders();

    let runtime: RuntimeState;
    let richSei: RichBlock;
    let seiOne: { number: number; hash: string; tx: SentTx };
    let gethOne: { number: number; hash: string; tx: SentTx };
    let seiFailed: SentTx;
    let gethFailed: SentTx;
    let seiRejectSigner: EvmAccount;
    let gethSigner: EvmAccount;
    let signers: EvmAccount[];

    const getBlock = (p: ethers.JsonRpcProvider, n: number, full: boolean): Promise<any> =>
        p.send('eth_getBlockByNumber', [ethers.toQuantity(n), full]);

    before(async function () {
        this.timeout(300 * 1000);
        runtime = readRuntimeState();
        signers = claimPool(runtime, sei, 12, 'eth_getBlockByNumber');
        seiRejectSigner = signers[9];

        const gethDev: string = (await geth.send('eth_accounts', []))[0];
        gethSigner = EvmAccount.fromPrivateKey(ethers.Wallet.createRandom().privateKey, geth);
        await fundFromUnlocked(geth, gethDev, gethSigner.address, ethers.parseEther('10'));

        richSei = await sharedRichBlock(sei, runtime);
        seiOne = await sendSingleTx(sei, signers[7]);
        gethOne = await sendSingleTx(geth, gethSigner);
        seiFailed = await sendRevertingTx(sei, signers[8], runtime.contracts.erc20);
        gethFailed = await sendRevertingTx(geth, gethSigner, runtime.contracts.erc20Geth);
    });

    describe('header schema (populated Sei block)', () => {
        it('returns every canonical header field and matches the requested number', async () => {
            const block = await getBlock(sei, richSei.number, false);
            assertCanonicalHeader(block, { hasTxs: true });
            expect(block.hash).to.equal(richSei.hash);
            expect(BigInt(block.number)).to.equal(BigInt(richSei.number));
        });

        it('chains to the previous block through parentHash', async () => {
            const [block, parent] = await Promise.all([
                getBlock(sei, richSei.number, false),
                getBlock(sei, richSei.number - 1, false),
            ]);
            expect(block.parentHash).to.equal(parent.hash);
            expect(BigInt(block.timestamp) >= BigInt(parent.timestamp)).to.equal(true);
        });
    });

    describe('transactions array (hashes vs full objects)', () => {
        it('fullTx=false lists exactly the transaction hashes we sent', async () => {
            const block = await getBlock(sei, richSei.number, false);
            const hashes: string[] = block.transactions;
            expect(hashes.every(h => typeof h === 'string')).to.equal(true);
            for (const sent of richSei.txs) {
                expect(hashes, `missing ${sent.kind}`).to.include(sent.hash);
            }
            expect(hashes.length, 'block lists exactly the EVM txs sent').to.equal(
                richSei.txs.length,
            );
        });

        it('fullTx=true returns canonical, correctly indexed transaction objects', async () => {
            const block = await getBlock(sei, richSei.number, true);
            const txs: any[] = block.transactions;
            txs.forEach((tx, i) => {
                assertCanonicalTx(tx, block);
                expect(BigInt(tx.transactionIndex), `index ${i}`).to.equal(BigInt(i));
            });
            const byHash = new Map(txs.map(t => [t.hash, t]));
            for (const sent of richSei.txs) {
                expect(byHash.has(sent.hash), `full object for ${sent.kind}`).to.equal(true);
            }
        });

        it('hash-only and full views describe the same ordered set', async () => {
            const [lite, full] = await Promise.all([
                getBlock(sei, richSei.number, false),
                getBlock(sei, richSei.number, true),
            ]);
            expect(full.transactions.map((t: any) => t.hash)).to.deep.equal(lite.transactions);
        });
    });

    describe('every transaction type lands in the single block', () => {
        it('exposes legacy (0), access-list (1), EIP-1559 (2) and set-code (4) together', async () => {
            const block = await getBlock(sei, richSei.number, true);
            const byHash = new Map<string, any>(block.transactions.map((t: any) => [t.hash, t]));
            const byKind = (k: string) => richSei.txs.find(t => t.kind === k)!;
            for (const kind of ['legacy', 'accessList', 'eip1559', 'setCode'] as const) {
                const sent = byKind(kind);
                const tx = byHash.get(sent.hash);
                expect(tx, `${kind} present`).to.not.equal(undefined);
                expect(BigInt(tx.type), `${kind} type byte`).to.equal(BigInt(sent.type));
            }
        });

        it('each tx exposes exactly its type-specific fields (and none of the others)', async () => {
            const block = await getBlock(sei, richSei.number, true);
            for (const kind of ['legacy', 'accessList', 'eip1559', 'setCode'] as const) {
                const sent = richSei.txs.find(t => t.kind === kind)!;
                const tx = block.transactions.find((t: any) => t.hash === sent.hash);
                expect(tx, `${kind} present`).to.not.equal(undefined);
                assertTxTypeSchema(tx);
            }
        });

        it('the access-list transaction carries its exact access list in the block', async () => {
            const block = await getBlock(sei, richSei.number, true);
            const sent = richSei.txs.find(t => t.kind === 'accessList')!;
            const tx = block.transactions.find((t: any) => t.hash === sent.hash);
            expect(tx.accessList, 'access list survives into the block').to.be.an('array');
            const normalized = tx.accessList.map((e: any) => ({
                address: e.address.toLowerCase(),
                storageKeys: e.storageKeys.map((k: string) => k.toLowerCase()),
            }));
            expect(normalized, 'access list is echoed byte-for-byte').to.deep.equal(
                ACCESS_LIST_FIXTURE.map(e => ({
                    address: e.address.toLowerCase(),
                    storageKeys: e.storageKeys.map(k => k.toLowerCase()),
                })),
            );
        });
    });

    describe('contract deployment in the block', () => {
        it('records a creation transaction (to=null) and the code is live afterwards', async () => {
            const block = await getBlock(sei, richSei.number, true);
            const sent = richSei.txs.find(t => t.kind === 'deploy')!;
            const tx = block.transactions.find((t: any) => t.hash === sent.hash);
            expect(tx.to, 'creation tx has null to').to.equal(null);
            expect(sent.receipt.contractAddress, 'receipt carries the new address').to.match(
                /^0x[0-9a-fA-F]{40}$/,
            );
            const code = await sei.getCode(sent.receipt.contractAddress!, richSei.number);
            expect(code.length, 'deployed code is non-empty').to.be.greaterThan(2);
        });
    });

    describe('EOA transfers in the block', () => {
        it('records plain value transfers with empty input, exact value, recipient and intrinsic gas', async () => {
            const block = await getBlock(sei, richSei.number, true);
            for (const kind of ['legacy', 'accessList', 'eip1559'] as const) {
                const sent = richSei.txs.find(t => t.kind === kind)!;
                const tx = block.transactions.find((t: any) => t.hash === sent.hash);
                expect(tx.input, `${kind} echoes the exact (empty) input`).to.equal(sent.data);
                expect(tx.to.toLowerCase(), `${kind} echoes the exact recipient`).to.equal(
                    sent.to!.toLowerCase(),
                );
                expect(BigInt(tx.value), `${kind} value`).to.equal(sent.value);
                const code = await sei.getCode(tx.to, richSei.number);
                expect(code, `${kind} recipient is an EOA`).to.equal('0x');
                expect(sent.receipt.gasUsed, `${kind} burned exactly the intrinsic gas`).to.equal(
                    expectedTransferGas(tx),
                );
            }
        });
    });

    describe('precompile call in the block', () => {
        it('records a transaction to the staking precompile that succeeded', async () => {
            const block = await getBlock(sei, richSei.number, true);
            const sent = richSei.txs.find(t => t.kind === 'precompile')!;
            const tx = block.transactions.find((t: any) => t.hash === sent.hash);
            expect(tx.to.toLowerCase()).to.equal(STAKING_PRECOMPILE_ADDRESS);
            expect(sent.receipt.status, 'precompile call succeeded').to.equal(1);
        });
    });

    describe('included-but-failed transactions in the block', () => {
        it('lists the out-of-gas and reverted-ERC20 txs and surfaces them as status-0 receipts', async () => {
            const block = await getBlock(sei, richSei.number, true);
            const byHash = new Map<string, any>(block.transactions.map((t: any) => [t.hash, t]));
            const { outOfGas, revertErc20 } = richFailedTxs(richSei);
            for (const sent of [outOfGas, revertErc20]) {
                const tx = byHash.get(sent.hash);
                expect(tx, `${sent.kind} present in the block tx list`).to.not.equal(undefined);
                expect(tx.to.toLowerCase(), `${sent.kind} target`).to.equal(sent.to!.toLowerCase());
                const rc = await sei.send('eth_getTransactionReceipt', [sent.hash]);
                assertFailedReceipt(rc, sent);
            }
        });
    });

    describe('gas + fees reconcile against the block (multiple users)', () => {
        it('block.gasUsed equals Σ receipt gasUsed and cumulativeGasUsed is consistent', async () => {
            const block = await getBlock(sei, richSei.number, true);
            await assertGasAccounting(sei, block, richSei.cosmosShellGas);
        });

        it('every reported transaction re-encodes to its hash and block.size covers the bytes', async () => {
            const block = await getBlock(sei, richSei.number, true);
            const { verified } = assertActualBytesAndSize(block);
            // legacy + access-list + EIP-1559 transfers, the deploy, the erc20 call and
            // the precompile call all re-encode; only the type-4 tx is skipped.
            expect(verified, 'at least the 3 transfers re-encoded byte-for-byte').to.be.greaterThanOrEqual(3);
        });

        it('each transaction echoes the exact input bytes it was sent', async () => {
            const block = await getBlock(sei, richSei.number, true);
            const byHash = new Map<string, any>(block.transactions.map((t: any) => [t.hash, t]));
            for (const sent of richSei.txs) {
                const tx = byHash.get(sent.hash);
                expect(tx.input, `${sent.kind} input bytes round-trip`).to.equal(sent.data);
            }
        });

        it('each transaction echoes the exact sender, nonce, chainId and fee caps it was signed with', async () => {
            const block = await getBlock(sei, richSei.number, true);
            const chainId = (await sei.getNetwork()).chainId;
            const byHash = new Map<string, any>(block.transactions.map((t: any) => [t.hash, t]));
            for (const sent of richSei.txs) {
                assertReportedSendFields(byHash.get(sent.hash), sent, chainId);
            }
        });

        it('the header logsBloom is exactly the Bloom of every emitted log', async () => {
            const block = await getBlock(sei, richSei.number, false);
            await assertLogsBloom(sei, block);
        });

        it("each sender's effective gas price and fee match the block", async () => {
            const block = await getBlock(sei, richSei.number, true);
            const byHash = new Map<string, any>(block.transactions.map((t: any) => [t.hash, t]));
            for (const sent of richSei.txs) {
                const tx = byHash.get(sent.hash);
                expect(BigInt(tx.gasPrice), `${sent.kind} effective gas price`).to.equal(
                    sent.receipt.gasPrice,
                );
                expect(
                    BigInt(tx.gas) >= sent.receipt.gasUsed,
                    `${sent.kind} gas limit bounds gas used`,
                ).to.equal(true);

                const [before, after] = await Promise.all([
                    sei.getBalance(sent.sender, richSei.number - 1),
                    sei.getBalance(sent.sender, richSei.number),
                ]);
                // Fee from the block-reported tx.gasPrice × receipt.gasUsed — the two sources must agree.
                const fee = sent.receipt.gasUsed * BigInt(tx.gasPrice);
                const spent = before - after;
                const drift = spent > sent.value + fee ? spent - (sent.value + fee) : sent.value + fee - spent;
                expect(
                    drift <= USEI,
                    `${sent.kind}: spent ${spent} vs value+fee ${sent.value + fee} (drift ${drift})`,
                ).to.equal(true);
            }
        });

        it('every fresh recipient is credited exactly the transferred value', async () => {
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

    describe('base fee responds to gas pressure (Sei fee market)', () => {
        let params: Eip1559Params | null = null;
        let burst: { beforeBaseFee: bigint; minBlock: number; maxBlock: number } | null = null;

        before(async function () {
            this.timeout(180 * 1000);
            params = await queryEip1559Params();
            if (!params) return; // hosted endpoint: no local seid to read params from
            burst = await burnGasBurst(sei, runtime, signers);
        });

        it("each over-target block raises the next block's baseFeePerGas exactly per the formula", async function () {
            if (!params || !burst || burst.maxBlock === 0) this.skip();
            let transitions = 0;
            let rose = 0;
            for (let n = burst!.minBlock; n <= burst!.maxBlock; n++) {
                const [blk, child] = await Promise.all([
                    getBlock(sei, n, false),
                    getBlock(sei, n + 1, false),
                ]);
                if (!blk || !child) continue;
                // Child base fee is fully determined by this block via Sei's CalculateNextBaseFee (within decimal rounding).
                const predicted = nextBaseFeeSei(
                    Number(BigInt(blk.baseFeePerGas)),
                    Number(BigInt(blk.gasUsed)),
                    params!,
                );
                expect(
                    Number(BigInt(child.baseFeePerGas)),
                    `child of block ${n} follows the fee-market formula`,
                ).to.be.closeTo(predicted, 5);

                if (BigInt(blk.gasUsed) > BigInt(params!.targetGasUsedPerBlock)) {
                    rose++;
                    expect(
                        BigInt(child.baseFeePerGas) > BigInt(blk.baseFeePerGas),
                        `over-target block ${n} (gasUsed ${blk.gasUsed}) raised the base fee`,
                    ).to.equal(true);
                }
                transitions++;
            }
            expect(transitions, 'checked at least one base-fee transition').to.be.greaterThan(0);
            expect(rose, 'at least one over-target block raised the base fee').to.be.greaterThan(0);
        });

        it('the peak base fee across the burst exceeds the pre-burst base fee', async function () {
            if (!params || !burst || burst.maxBlock === 0) this.skip();
            let peak = 0n;
            for (let n = burst!.minBlock; n <= burst!.maxBlock + 1; n++) {
                const blk = await getBlock(sei, n, false);
                if (blk) {
                    const bf = BigInt(blk.baseFeePerGas);
                    if (bf > peak) peak = bf;
                }
            }
            expect(
                peak > burst!.beforeBaseFee,
                `peak base fee ${peak} should exceed pre-burst ${burst!.beforeBaseFee}`,
            ).to.equal(true);
        });
    });

    describe('geth parity (single transaction): block + tx fields match', () => {
        it('both blocks expose the core header field set, with only the documented divergences', async () => {
            const [s, g] = await Promise.all([
                getBlock(sei, seiOne.number, true),
                getBlock(geth, gethOne.number, true),
            ]);
            assertCanonicalHeader(s, { hasTxs: true });
            assertCanonicalHeader(g, { hasTxs: true });

            const sKeys = Object.keys(s);
            const gKeys = Object.keys(g);
            for (const f of CORE_BLOCK_FIELDS) {
                expect(sKeys, `Sei header has ${f}`).to.include(f);
                expect(gKeys, `geth header has ${f}`).to.include(f);
            }
            const seiExtra = sKeys.filter(k => !gKeys.includes(k));
            const gethExtra = gKeys.filter(k => !sKeys.includes(k));
            seiExtra.forEach(k =>
                expect(SEI_ONLY_BLOCK_FIELDS as readonly string[], `unexpected Sei-only ${k}`).to.include(k),
            );
            gethExtra.forEach(k =>
                expect(GETH_ONLY_BLOCK_FIELDS as readonly string[], `unexpected geth-only ${k}`).to.include(k),
            );
        });

        it('both single transactions expose the core tx field set, with only the documented divergences', async () => {
            const [s, g] = await Promise.all([
                getBlock(sei, seiOne.number, true),
                getBlock(geth, gethOne.number, true),
            ]);
            const seiTx = s.transactions.find((t: any) => t.hash === seiOne.tx.hash);
            const gethTx = g.transactions.find((t: any) => t.hash === gethOne.tx.hash);
            assertCanonicalTx(seiTx, s);
            assertCanonicalTx(gethTx, g);

            const sKeys = Object.keys(seiTx);
            const gKeys = Object.keys(gethTx);
            for (const f of CORE_TX_FIELDS) {
                expect(sKeys, `Sei tx has ${f}`).to.include(f);
                expect(gKeys, `geth tx has ${f}`).to.include(f);
            }
            const gethExtra = gKeys.filter(k => !sKeys.includes(k));
            gethExtra.forEach(k =>
                expect(GETH_ONLY_TX_FIELDS as readonly string[], `unexpected geth-only tx ${k}`).to.include(k),
            );
            expect(BigInt(seiTx.type), 'both are EIP-1559').to.equal(2n);
            expect(BigInt(gethTx.type), 'both are EIP-1559').to.equal(2n);
        });
    });

    describe('failed transactions are still included', () => {
        it('[Sei] a reverted tx is listed in its block with status 0 and counted in gasUsed', async () => {
            expect(seiFailed.receipt.status, 'tx reverted').to.equal(0);
            const block = await getBlock(sei, seiFailed.receipt.blockNumber, true);
            const tx = block.transactions.find((t: any) => t.hash === seiFailed.hash);
            expect(tx, 'failed tx is present in the block').to.not.equal(undefined);
            assertCanonicalTx(tx, block);
            await assertGasAccounting(sei, block);
        });

        it('[geth] a reverted tx is listed in its block with status 0 and counted in gasUsed', async () => {
            expect(gethFailed.receipt.status, 'tx reverted').to.equal(0);
            const block = await getBlock(geth, gethFailed.receipt.blockNumber, true);
            const tx = block.transactions.find((t: any) => t.hash === gethFailed.hash);
            expect(tx, 'failed tx is present in the block').to.not.equal(undefined);
            assertCanonicalTx(tx, block);
            await assertGasAccounting(geth, block);
        });
    });

    describe('lookup semantics', () => {
        it('the latest tag returns a canonical, populated-or-empty block', async () => {
            const block = await sei.send('eth_getBlockByNumber', ['latest', false]);
            assertCanonicalHeader(block, { hasTxs: false });
        });

        it('the earliest tag returns the genesis block (number 0x0)', async () => {
            const block = await sei.send('eth_getBlockByNumber', ['earliest', false]);
            expect(block.number).to.equal('0x0');
        });

        it('a far-future block number returns null on both chains', async () => {
            const future = ethers.toQuantity((await sei.getBlockNumber()) + 10_000_000);
            const [s, g] = await Promise.all([
                sei.send('eth_getBlockByNumber', [future, false]),
                geth.send('eth_getBlockByNumber', [future, false]),
            ]);
            expect(s, 'Sei future block is null').to.equal(null);
            expect(g, 'geth future block is null').to.equal(null);
        });
    });

    describe('wrong params / error handling (parity with geth)', () => {
        it('empty params fail identically (-32602, missing required argument 0)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockByNumber', []),
                rawGeth('eth_getBlockByNumber', []),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 0/);
            expectSameError(s, g);
        });

        it('omitting the fullTx flag fails identically (-32602, missing required argument 1)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockByNumber', ['latest']),
                rawGeth('eth_getBlockByNumber', ['latest']),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 1/);
            expectSameError(s, g);
        });

        it('too many positional args fail identically (-32602, want at most 2)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockByNumber', ['latest', false, {}]),
                rawGeth('eth_getBlockByNumber', ['latest', false, {}]),
            ]);
            expectJsonRpcError(s, -32602, /too many arguments, want at most 2/);
            expectSameError(s, g);
        });

        it('non-array params fail identically (-32602, non-array args)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockByNumber', { block: 'latest' }),
                rawGeth('eth_getBlockByNumber', { block: 'latest' }),
            ]);
            expectJsonRpcError(s, -32602, /^non-array args$/);
            expectSameError(s, g);
        });

        it('a non-boolean fullTx flag fails identically (-32602, cannot unmarshal)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockByNumber', ['latest', 'notabool']),
                rawGeth('eth_getBlockByNumber', ['latest', 'notabool']),
            ]);
            expectJsonRpcError(s, -32602, /cannot unmarshal/);
            expectSameError(s, g);
        });

        it('an unparseable block tag fails identically', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockByNumber', ['not-a-block', false]),
                rawGeth('eth_getBlockByNumber', ['not-a-block', false]),
            ]);
            expect(s.error, 'Sei rejects the bad tag').to.not.equal(undefined);
            expectSameError(s, g);
        });
    });

    describe('rejected transactions are refused (parity + documented divergence)', () => {
        it('both reject a tx below the intrinsic gas floor and never mine it', async () => {
            const [seiTx, gethTx] = await Promise.all([
                signBelowIntrinsicTx(sei, seiRejectSigner),
                signBelowIntrinsicTx(geth, gethSigner),
            ]);
            const [s, g] = await Promise.all([
                rawSei('eth_sendRawTransaction', [seiTx.raw]),
                rawGeth('eth_sendRawTransaction', [gethTx.raw]),
            ]);
            // Both reject with code -32000; geth is descriptive while Sei surfaces an
            // opaque ": unknown" from its mempool — a documented message divergence.
            expectJsonRpcError(g, -32000, /intrinsic gas too low/);
            expect(s.error, 'Sei rejects the tx').to.not.equal(undefined);
            expect(s.error!.code, 'both use -32000').to.equal(g.error!.code);
            expect(s.error!.message, '[divergence] Sei does not surface the geth reason').to.not.equal(
                g.error!.message,
            );

            const [seiLookup, gethLookup] = await Promise.all([
                rawSei('eth_getTransactionByHash', [seiTx.hash]),
                rawGeth('eth_getTransactionByHash', [gethTx.hash]),
            ]);
            expect(seiLookup.result, 'Sei: rejected tx is not retrievable').to.equal(null);
            expect(gethLookup.result, 'geth: rejected tx is not retrievable').to.equal(null);
        });

        it('a malformed raw transaction is rejected identically to geth', async () => {
            // Garbage bytes fail in the shared go-ethereum RLP decoder before reaching
            // the mempool, so the error is byte-identical on both chains.
            const [s, g] = await Promise.all([
                rawSei('eth_sendRawTransaction', ['0xdeadbeef']),
                rawGeth('eth_sendRawTransaction', ['0xdeadbeef']),
            ]);
            expect(s.error, 'Sei rejects garbage bytes').to.not.equal(undefined);
            expectSameError(s, g);
        });
    });
});
