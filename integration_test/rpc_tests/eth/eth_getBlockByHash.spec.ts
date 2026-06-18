import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError, Eip1559Params, queryEip1559Params, nextBaseFeeSei } from '../utils/chainUtils';
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
    ZERO_HASH,
    RichBlock,
    SentTx,
} from '../utils/txUtils';

describe('eth_getBlockByHash', function () {
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

    const byHash = (p: ethers.JsonRpcProvider, hash: string, full: boolean): Promise<any> =>
        p.send('eth_getBlockByHash', [hash, full]);
    const byNumber = (p: ethers.JsonRpcProvider, n: number, full: boolean): Promise<any> =>
        p.send('eth_getBlockByNumber', [ethers.toQuantity(n), full]);

    before(async function () {
        this.timeout(300 * 1000);
        runtime = readRuntimeState();
        signers = claimPool(runtime, sei, 12, 'eth_getBlockByHash');
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
        it('returns every canonical header field and echoes the requested hash', async () => {
            const block = await byHash(sei, richSei.hash, false);
            assertCanonicalHeader(block, { hasTxs: true });
            expect(block.hash).to.equal(richSei.hash);
            expect(BigInt(block.number)).to.equal(BigInt(richSei.number));
        });

        it('is byte-identical to the same block fetched by number', async () => {
            const [viaHash, viaNumber] = await Promise.all([
                byHash(sei, richSei.hash, true),
                byNumber(sei, richSei.number, true),
            ]);
            const strip = ({ totalDifficulty, ...rest }: any) => rest;
            expect(strip(viaHash)).to.deep.equal(strip(viaNumber));
        });
    });

    describe('transactions array (hashes vs full objects)', () => {
        it('fullTx=false lists exactly the transaction hashes we sent', async () => {
            const block = await byHash(sei, richSei.hash, false);
            const hashes: string[] = block.transactions;
            expect(hashes.every(h => typeof h === 'string')).to.equal(true);
            for (const sent of richSei.txs) {
                expect(hashes, `missing ${sent.kind}`).to.include(sent.hash);
            }
        });

        it('fullTx=true returns canonical, correctly indexed transaction objects', async () => {
            const block = await byHash(sei, richSei.hash, true);
            block.transactions.forEach((tx: any, i: number) => {
                assertCanonicalTx(tx, block);
                expect(BigInt(tx.transactionIndex), `index ${i}`).to.equal(BigInt(i));
            });
            const seen = new Map(block.transactions.map((t: any) => [t.hash, t]));
            for (const sent of richSei.txs) {
                expect(seen.has(sent.hash), `full object for ${sent.kind}`).to.equal(true);
            }
        });
    });

    describe('every transaction type lands in the single block', () => {
        it('exposes legacy (0), access-list (1), EIP-1559 (2) and set-code (4) together', async () => {
            const block = await byHash(sei, richSei.hash, true);
            const seen = new Map<string, any>(block.transactions.map((t: any) => [t.hash, t]));
            for (const kind of ['legacy', 'accessList', 'eip1559', 'setCode'] as const) {
                const sent = richSei.txs.find(t => t.kind === kind)!;
                const tx = seen.get(sent.hash);
                expect(tx, `${kind} present`).to.not.equal(undefined);
                expect(BigInt(tx.type), `${kind} type byte`).to.equal(BigInt(sent.type));
            }
        });

        it('the access-list transaction carries its exact access list in the block', async () => {
            const block = await byHash(sei, richSei.hash, true);
            const sent = richSei.txs.find(t => t.kind === 'accessList')!;
            const tx = block.transactions.find((t: any) => t.hash === sent.hash);
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
        it('records a creation transaction (to=null) with live code afterwards', async () => {
            const block = await byHash(sei, richSei.hash, true);
            const sent = richSei.txs.find(t => t.kind === 'deploy')!;
            const tx = block.transactions.find((t: any) => t.hash === sent.hash);
            expect(tx.to, 'creation tx has null to').to.equal(null);
            expect(sent.receipt.contractAddress).to.match(/^0x[0-9a-fA-F]{40}$/);
            const code = await sei.getCode(sent.receipt.contractAddress!, richSei.number);
            expect(code.length, 'deployed code is non-empty').to.be.greaterThan(2);
        });
    });

    describe('EOA transfers in the block', () => {
        it('records plain value transfers with empty input, exact value, recipient and intrinsic gas', async () => {
            const block = await byHash(sei, richSei.hash, true);
            for (const kind of ['legacy', 'accessList', 'eip1559'] as const) {
                const sent = richSei.txs.find(t => t.kind === kind)!;
                const tx = block.transactions.find((t: any) => t.hash === sent.hash);
                expect(tx.input, `${kind} echoes the exact (empty) input`).to.equal(sent.data);
                expect(tx.to.toLowerCase(), `${kind} echoes the exact recipient`).to.equal(
                    sent.to!.toLowerCase(),
                );
                expect(BigInt(tx.value), `${kind} value`).to.equal(sent.value);
                expect(sent.receipt.gasUsed, `${kind} burned exactly the intrinsic gas`).to.equal(
                    expectedTransferGas(tx),
                );
            }
        });
    });

    describe('precompile call in the block', () => {
        it('records a transaction to the staking precompile that succeeded', async () => {
            const block = await byHash(sei, richSei.hash, true);
            const sent = richSei.txs.find(t => t.kind === 'precompile')!;
            const tx = block.transactions.find((t: any) => t.hash === sent.hash);
            expect(tx.to.toLowerCase()).to.equal(STAKING_PRECOMPILE_ADDRESS);
            expect(sent.receipt.status).to.equal(1);
        });
    });

    describe('included-but-failed transactions in the block', () => {
        it('lists the out-of-gas and reverted-ERC20 txs and surfaces them as status-0 receipts', async () => {
            const block = await byHash(sei, richSei.hash, true);
            const byTx = new Map<string, any>(block.transactions.map((t: any) => [t.hash, t]));
            const { outOfGas, revertErc20 } = richFailedTxs(richSei);
            for (const sent of [outOfGas, revertErc20]) {
                const tx = byTx.get(sent.hash);
                expect(tx, `${sent.kind} present in the block tx list`).to.not.equal(undefined);
                expect(tx.to.toLowerCase(), `${sent.kind} target`).to.equal(sent.to!.toLowerCase());
                const rc = await sei.send('eth_getTransactionReceipt', [sent.hash]);
                assertFailedReceipt(rc, sent);
            }
        });
    });

    describe('gas + fees reconcile against the block (multiple users)', () => {
        it('block.gasUsed equals Σ receipt gasUsed and cumulativeGasUsed is consistent', async () => {
            const block = await byHash(sei, richSei.hash, true);
            await assertGasAccounting(sei, block, richSei.cosmosShellGas);
        });

        it('every reported transaction re-encodes to its hash and block.size covers the bytes', async () => {
            const block = await byHash(sei, richSei.hash, true);
            const { verified } = assertActualBytesAndSize(block);
            expect(verified, 'at least the 3 transfers re-encoded byte-for-byte').to.be.greaterThanOrEqual(3);
        });

        it('each transaction echoes the exact input bytes it was sent', async () => {
            const block = await byHash(sei, richSei.hash, true);
            const seen = new Map<string, any>(block.transactions.map((t: any) => [t.hash, t]));
            for (const sent of richSei.txs) {
                const tx = seen.get(sent.hash);
                expect(tx.input, `${sent.kind} input bytes round-trip`).to.equal(sent.data);
            }
        });

        it('each transaction echoes the exact sender, nonce, chainId and fee caps it was signed with', async () => {
            const block = await byHash(sei, richSei.hash, true);
            const chainId = (await sei.getNetwork()).chainId;
            const seen = new Map<string, any>(block.transactions.map((t: any) => [t.hash, t]));
            for (const sent of richSei.txs) {
                assertReportedSendFields(seen.get(sent.hash), sent, chainId);
            }
        });

        it('the header logsBloom is exactly the Bloom of every emitted log', async () => {
            const block = await byHash(sei, richSei.hash, false);
            await assertLogsBloom(sei, block);
        });

        it("each sender's effective gas price and fee match the block", async () => {
            const block = await byHash(sei, richSei.hash, true);
            const seen = new Map<string, any>(block.transactions.map((t: any) => [t.hash, t]));
            for (const sent of richSei.txs) {
                const tx = seen.get(sent.hash);
                expect(BigInt(tx.gasPrice), `${sent.kind} effective gas price`).to.equal(
                    sent.receipt.gasPrice,
                );
                const [before, after] = await Promise.all([
                    sei.getBalance(sent.sender, richSei.number - 1),
                    sei.getBalance(sent.sender, richSei.number),
                ]);
                const fee = sent.receipt.gasUsed * BigInt(tx.gasPrice);
                const spent = before - after;
                const drift = spent > sent.value + fee ? spent - (sent.value + fee) : sent.value + fee - spent;
                expect(drift <= USEI, `${sent.kind}: drift ${drift}`).to.equal(true);
            }
        });
    });

    describe('base fee responds to gas pressure (Sei fee market)', () => {
        let params: Eip1559Params | null = null;
        let burst: { beforeBaseFee: bigint; minBlock: number; maxBlock: number } | null = null;

        before(async function () {
            this.timeout(180 * 1000);
            params = await queryEip1559Params();
            if (!params) return;
            burst = await burnGasBurst(sei, runtime, signers);
        });

        // Resolve by number, then re-read by hash so the fee-market check hits the by-hash endpoint.
        const byHashAt = async (n: number): Promise<any> => {
            const ref = await byNumber(sei, n, false);
            return ref ? byHash(sei, ref.hash, false) : null;
        };

        it("each over-target block raises the next block's baseFeePerGas exactly per the formula", async function () {
            if (!params || !burst || burst.maxBlock === 0) this.skip();
            let transitions = 0;
            let rose = 0;
            for (let n = burst!.minBlock; n <= burst!.maxBlock; n++) {
                const [blk, child] = await Promise.all([byHashAt(n), byHashAt(n + 1)]);
                if (!blk || !child) continue;
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
                const blk = await byHashAt(n);
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
                byHash(sei, seiOne.hash, true),
                byHash(geth, gethOne.hash, true),
            ]);
            assertCanonicalHeader(s, { hasTxs: true });
            assertCanonicalHeader(g, { hasTxs: true });

            const sKeys = Object.keys(s);
            const gKeys = Object.keys(g);
            for (const f of CORE_BLOCK_FIELDS) {
                expect(sKeys, `Sei header has ${f}`).to.include(f);
                expect(gKeys, `geth header has ${f}`).to.include(f);
            }
            sKeys
                .filter(k => !gKeys.includes(k))
                .forEach(k =>
                    expect(SEI_ONLY_BLOCK_FIELDS as readonly string[], `unexpected Sei-only ${k}`).to.include(k),
                );
            gKeys
                .filter(k => !sKeys.includes(k))
                .forEach(k =>
                    expect(GETH_ONLY_BLOCK_FIELDS as readonly string[], `unexpected geth-only ${k}`).to.include(k),
                );
        });

        it('both single transactions expose the core tx field set, with only the documented divergences', async () => {
            const [s, g] = await Promise.all([
                byHash(sei, seiOne.hash, true),
                byHash(geth, gethOne.hash, true),
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
            gKeys
                .filter(k => !sKeys.includes(k))
                .forEach(k =>
                    expect(GETH_ONLY_TX_FIELDS as readonly string[], `unexpected geth-only tx ${k}`).to.include(k),
                );
        });
    });

    describe('failed transactions are still included', () => {
        it('[Sei] a reverted tx is listed in its block (by hash) with status 0', async () => {
            expect(seiFailed.receipt.status).to.equal(0);
            const block = await byHash(sei, seiFailed.receipt.blockHash, true);
            const tx = block.transactions.find((t: any) => t.hash === seiFailed.hash);
            expect(tx, 'failed tx is present').to.not.equal(undefined);
            assertCanonicalTx(tx, block);
            await assertGasAccounting(sei, block);
        });

        it('[geth] a reverted tx is listed in its block (by hash) with status 0', async () => {
            expect(gethFailed.receipt.status).to.equal(0);
            const block = await byHash(geth, gethFailed.receipt.blockHash, true);
            const tx = block.transactions.find((t: any) => t.hash === gethFailed.hash);
            expect(tx, 'failed tx is present').to.not.equal(undefined);
            assertCanonicalTx(tx, block);
            await assertGasAccounting(geth, block);
        });
    });

    describe('lookup semantics', () => {
        it('an unknown block hash returns null on both chains', async () => {
            const unknown = '0x' + 'ab'.repeat(32);
            const [s, g] = await Promise.all([
                sei.send('eth_getBlockByHash', [unknown, false]),
                geth.send('eth_getBlockByHash', [unknown, false]),
            ]);
            expect(s, 'Sei unknown hash is null').to.equal(null);
            expect(g, 'geth unknown hash is null').to.equal(null);
        });

        it('the zero hash returns null', async () => {
            const block = await sei.send('eth_getBlockByHash', [ZERO_HASH, false]);
            expect(block).to.equal(null);
        });

        it('the fullTx flag only changes the transactions field, not the header', async () => {
            const [lite, full] = await Promise.all([
                byHash(sei, richSei.hash, false),
                byHash(sei, richSei.hash, true),
            ]);
            const stripTx = (b: any) => {
                const { transactions, totalDifficulty, ...header } = b;
                return header;
            };
            expect(stripTx(lite)).to.deep.equal(stripTx(full));
            expect(full.transactions.map((t: any) => t.hash)).to.deep.equal(lite.transactions);
        });
    });

    describe('wrong params / error handling (parity with geth)', () => {
        it('empty params fail identically (-32602, missing required argument 0)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockByHash', []),
                rawGeth('eth_getBlockByHash', []),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 0/);
            expectSameError(s, g);
        });

        it('omitting the fullTx flag fails identically (-32602, missing required argument 1)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockByHash', [richSei.hash]),
                rawGeth('eth_getBlockByHash', [gethOne.hash]),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 1/);
            expectSameError(s, g);
        });

        it('too many positional args fail identically (-32602, want at most 2)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockByHash', [richSei.hash, false, {}]),
                rawGeth('eth_getBlockByHash', [gethOne.hash, false, {}]),
            ]);
            expectJsonRpcError(s, -32602, /too many arguments, want at most 2/);
            expectSameError(s, g);
        });

        it('non-array params fail identically (-32602, non-array args)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockByHash', { hash: richSei.hash }),
                rawGeth('eth_getBlockByHash', { hash: gethOne.hash }),
            ]);
            expectJsonRpcError(s, -32602, /^non-array args$/);
            expectSameError(s, g);
        });

        it('a malformed (too short) block hash fails identically (-32602, exact length message)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockByHash', ['0x1234', false]),
                rawGeth('eth_getBlockByHash', ['0x1234', false]),
            ]);
            expectJsonRpcError(s, -32602, /hex string has length 4, want 64 for common\.Hash/);
            expectSameError(s, g);
        });

        it('a non-boolean fullTx flag fails identically (-32602, cannot unmarshal)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBlockByHash', [richSei.hash, 'notabool']),
                rawGeth('eth_getBlockByHash', [gethOne.hash, 'notabool']),
            ]);
            expectJsonRpcError(s, -32602, /cannot unmarshal/);
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
            const [s, g] = await Promise.all([
                rawSei('eth_sendRawTransaction', ['0xdeadbeef']),
                rawGeth('eth_sendRawTransaction', ['0xdeadbeef']),
            ]);
            expect(s.error, 'Sei rejects garbage bytes').to.not.equal(undefined);
            expectSameError(s, g);
        });
    });
});
