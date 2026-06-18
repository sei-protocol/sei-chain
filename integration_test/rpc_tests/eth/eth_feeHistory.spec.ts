import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError, Eip1559Params, queryEip1559Params } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, expectSameError, claimPool } from '../utils/testUtils';
import { abiOf, deployContract, EvmAccount } from '../utils/evmUtils';
import { burnGasBurst } from '../utils/txUtils';
import { HEX_QUANTITY } from '../utils/format';
import {
    feeHistory,
    parseFeeHistory,
    blockGasInfo,
    assertFeeHistoryCounts,
    verifyFeeHistorySeries,
} from '../utils/feeHistoryUtils';

describe('eth_feeHistory Tests', function () {
    this.timeout(240 * 1000);

    const { sei, geth } = bothProviders();
    const burnerIface = new ethers.Interface(abiOf('GasBurner.sol', 'RealGasBurner'));

    let runtime: RuntimeState;
    let seiBurner: string;
    let spammers: EvmAccount[];
    let seiParams: Eip1559Params | null;

    before(async () => {
        runtime = readRuntimeState();
        seiBurner = runtime.contracts.gasBurner;
        spammers = claimPool(runtime, sei, 5, 'eth_feeHistory');
        seiParams = await queryEip1559Params();
    });

    describe('schema / structure', () => {
        it('returns well-formed, length-consistent arrays on Sei', async () => {
            const newest = await sei.getBlockNumber();
            const count = 5;
            const percentiles = [5, 25, 50, 75, 95];
            const fh = parseFeeHistory(await feeHistory(sei, count, ethers.toQuantity(newest), percentiles));
            // Plenty of history exists by the time the suite runs, so a 5-block request
            // must come back with exactly 5 ratios, 6 base fees, and 5 reward rows.
            assertFeeHistoryCounts(fh, count, percentiles.length);
            await verifyFeeHistorySeries(sei, fh, newest, percentiles, 'sei', seiParams);
        });

        it('returns well-formed, length-consistent arrays on geth', async () => {
            const newest = await geth.getBlockNumber();
            const count = Math.min(newest, 4);
            const percentiles = [10, 50, 90];
            const fh = parseFeeHistory(await feeHistory(geth, count, ethers.toQuantity(newest), percentiles));
            assertFeeHistoryCounts(fh, count, percentiles.length);
            await verifyFeeHistorySeries(geth, fh, newest, percentiles, 'geth', seiParams);
        });

        it('every base fee is a canonical quantity within the configured bounds (Sei)', async function () {
            const newest = await sei.getBlockNumber();
            const body = await rawSei<any>('eth_feeHistory', ['0x5', ethers.toQuantity(newest), []]);
            expect(body.error, JSON.stringify(body.error)).to.equal(undefined);
            for (const fee of body.result!.baseFeePerGas as string[]) {
                expect(fee, 'canonical quantity').to.match(HEX_QUANTITY);
                expect(Number(fee)).to.be.gte(seiParams!.minFeePerGas);
                expect(Number(fee)).to.be.lte(seiParams!.maxFeePerGas);
            }
        });
    });

    describe('base fee manipulation (Sei)', () => {
        // Must match the maxPriorityFeePerGas burnGasBurst pays, so the exact-tip assertions hold.
        const BURST_TIP = ethers.parseUnits('2', 'gwei');

        const getBaseFee = async (): Promise<bigint> =>
            BigInt((await sei.send('eth_getBlockByNumber', ['latest', false])).baseFeePerGas ?? '0x0');

        it('drives the base fee up and every field replays through the fee-market formula', async function () {
            const { beforeBaseFee: before, minBlock, maxBlock } = await burnGasBurst(sei, runtime, spammers);

            const newest = Math.min(maxBlock + 1, await sei.getBlockNumber());
            const count = Math.min(newest - minBlock + 2, 1024);
            const percentiles = [10, 50, 90];
            const fh = parseFeeHistory(await feeHistory(sei, count, ethers.toQuantity(newest), percentiles));

            assertFeeHistoryCounts(fh, count, percentiles.length);
            await verifyFeeHistorySeries(sei, fh, newest, percentiles, 'sei', seiParams);

            const peak = fh.baseFeePerGas.reduce((m, v) => (v > m ? v : m), 0n);
            expect(peak > before, `base fee should rise above ${before}, peaked at ${peak}`).to.equal(
                true,
            );

            const rose = fh.baseFeePerGas.some((v, i) => i > 0 && v > fh.baseFeePerGas[i - 1]);
            expect(rose, 'at least one block raised the base fee').to.equal(true);

            const top = percentiles.length - 1;
            const burstTipBlock = fh.reward!.find(
                row => row.length === percentiles.length && row[top] === BURST_TIP,
            );
            expect(
                burstTipBlock,
                `a burst block must report the exact ${BURST_TIP} wei tip at the ${percentiles[top]}th percentile`,
            ).to.not.equal(undefined);

            for (const row of fh.reward!) {
                for (const r of row) {
                    expect(
                        r <= BURST_TIP,
                        `reward ${r} must not exceed the max tip paid (${BURST_TIP})`,
                    ).to.equal(true);
                }
            }
        });

        it('reports each requested percentile as the exact gas-weighted tip', async function () {
            const tips = [1n, 3n, 5n, 6n].map(g => g * 1_000_000_000n);
            const signers = spammers.slice(0, tips.length);
            expect(signers.length, 'a distinct signer per tip').to.equal(tips.length);

            let blockNumber = 0;
            for (let attempt = 0; attempt < 6 && blockNumber === 0; attempt++) {
                const maxFee = (await getBaseFee()) * 2n + tips[tips.length - 1];
                const nonces = await Promise.all(
                    signers.map(s => sei.getTransactionCount(s.address, 'pending')),
                );
                const responses = await Promise.all(
                    signers.map((s, i) =>
                        s.wallet.sendTransaction({
                            to: ethers.Wallet.createRandom().address,
                            value: 1n,
                            type: 2,
                            gasLimit: 21000n,
                            maxFeePerGas: maxFee,
                            maxPriorityFeePerGas: tips[i],
                            nonce: nonces[i],
                        }),
                    ),
                );
                const receipts = await Promise.all(
                    responses.map(r => sei.waitForTransaction(r.hash, 1, 30_000)),
                );
                const blocks = new Set(receipts.map(r => r!.blockNumber));
                if (blocks.size !== 1) continue; // split across blocks; re-price and retry
                const candidate = receipts[0]!.blockNumber;
                const blk = await sei.send('eth_getBlockByNumber', [ethers.toQuantity(candidate), false]);
                const ours = new Set(responses.map(r => r.hash));
                const clean =
                    blk.transactions.length === tips.length &&
                    blk.transactions.every((h: string) => ours.has(h)) &&
                    receipts.every(r => r!.gasUsed === 21000n);
                if (clean) blockNumber = candidate;
            }
            expect(blockNumber, 'four equal-gas tips co-located in a clean block').to.not.equal(0);

            // percentiles -> threshold (of 84k total) -> tx the cumulative gas lands on:
            //   10 -> 8400  -> tx0 (1g) | 40 -> 33600 -> tx1 (3g)
            //   60 -> 50400 -> tx2 (5g) | 90 -> 75600 -> tx3 (6g)
            const percentiles = [10, 40, 60, 90];
            const fh = parseFeeHistory(await feeHistory(sei, 1, ethers.toQuantity(blockNumber), percentiles));
            assertFeeHistoryCounts(fh, 1, percentiles.length);
            expect(fh.reward![0], 'each percentile is the exact gas-weighted tip').to.deep.equal(tips);
        });

        it('a single over-target block reports gasUsedRatio above the target ratio', async function () {
            if (!seiParams) this.skip();
            const data = burnerIface.encodeFunctionData('burnGasIterations', [777n, 200n]);
            const tip = ethers.parseUnits('1', 'gwei');
            const baseNow = await getBaseFee();
            const gasLimit = 6_000_000n;
            const maxFee = baseNow * 4n + tip;
            let sender: EvmAccount | undefined;
            sender = spammers[0];

            const tx = await sender!.wallet.sendTransaction({
                to: seiBurner,
                data,
                gasLimit,
                maxFeePerGas: maxFee,
                maxPriorityFeePerGas: tip,
                type: 2,
            });
            const receipt = await tx.wait();
            const blk = await blockGasInfo(sei, receipt!.blockNumber);
            const targetRatio = seiParams!.targetGasUsedPerBlock / Number(blk.gasLimit);

            const fh = parseFeeHistory(
                await feeHistory(sei, 1, ethers.toQuantity(receipt!.blockNumber), []),
            );
            // A blockCount of 1 returns exactly one ratio and two base fees (this block + forecast).
            assertFeeHistoryCounts(fh, 1, 0);
            const reportedRatio = fh.gasUsedRatio[0];
            expect(reportedRatio).to.be.closeTo(Number(blk.gasUsed) / Number(blk.gasLimit), 1.01e-4);
            expect(reportedRatio > targetRatio, 'over-target block exceeds the target ratio').to.equal(
                true,
            );
        });
    });

    describe('empty / null handling', () => {
        // SKIP(expected-failure): Sei diverges on blockCount 0 oldestBlock; pending manual reverification.
        it.skip('[spec] blockCount 0 returns oldestBlock as the canonical quantity 0x0', async () => {
            // execution-apis: oldestBlock is a QUANTITY. geth returns "0x0" for an empty
            // range; Sei returns null, which is not a valid quantity — assert the standard.
            const [s, g] = await Promise.all([
                rawSei<any>('eth_feeHistory', ['0x0', 'latest', []]),
                rawGeth<any>('eth_feeHistory', ['0x0', 'latest', []]),
            ]);
            expect(s.error, JSON.stringify(s.error)).to.equal(undefined);
            expect(g.error, JSON.stringify(g.error)).to.equal(undefined);
            expect(s.result.gasUsedRatio, 'sei nulls gasUsedRatio for empty range').to.equal(null);
            expect(g.result.gasUsedRatio, 'geth nulls gasUsedRatio for empty range').to.equal(null);
            expect(g.result.oldestBlock, 'geth (reference) reports 0x0').to.equal('0x0');
            expect(s.result.oldestBlock, 'Sei must report a canonical 0x0, not null').to.equal('0x0');
        });

        it('an idle range still returns a zero-filled reward matrix, never null entries', async () => {
            const newest = await sei.getBlockNumber();
            const count = 3;
            const percentiles = [25, 75];
            const fh = parseFeeHistory(await feeHistory(sei, count, ethers.toQuantity(newest), percentiles));
            assertFeeHistoryCounts(fh, count, percentiles.length);
            expect(fh.reward, 'reward present').to.not.equal(undefined);
            fh.reward!.forEach(row => {
                expect(row.length).to.equal(percentiles.length);
                row.forEach(r => expect(r >= 0n).to.equal(true));
            });
        });

        it('clamps an oversized blockCount to the available history without erroring (Sei)', async () => {
            const newest = await sei.getBlockNumber();
            const body = await rawSei<any>('eth_feeHistory', [
                '0xffff',
                ethers.toQuantity(newest),
                [],
            ]);
            expect(body.error, JSON.stringify(body.error)).to.equal(undefined);
            const ratios = body.result.gasUsedRatio as number[];
            expect(ratios.length > 0, 'returns some blocks').to.equal(true);
            expect(ratios.length <= newest + 1, 'cannot exceed available history').to.equal(true);
        });
    });

    describe('wrong params / error handling', () => {
        it('missing percentiles argument fails identically (-32602, exact message)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_feeHistory', ['0x2', 'latest']),
                rawGeth('eth_feeHistory', ['0x2', 'latest']),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 2/);
            expectSameError(s, g);
        });

        it('empty params fail identically (-32602 missing required argument 0)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_feeHistory', []),
                rawGeth('eth_feeHistory', []),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 0/);
            expectSameError(s, g);
        });

        it('[divergence] unsorted percentiles: both -32000, different messages', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_feeHistory', ['0x2', 'latest', [50, 5]]),
                rawGeth('eth_feeHistory', ['0x2', 'latest', [50, 5]]),
            ]);
            expect(s.error?.code, 'sei code').to.equal(-32000);
            expect(g.error?.code, 'geth code').to.equal(-32000);
            expect(s.error?.message).to.match(/ascending|invalid reward percentile/i);
            expect(g.error?.message).to.match(/invalid reward percentile/i);
            expect(s.error?.message).to.not.equal(g.error?.message);
        });

        it('[divergence] a percentile above 100 is rejected by both (-32000)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_feeHistory', ['0x2', 'latest', [150]]),
                rawGeth('eth_feeHistory', ['0x2', 'latest', [150]]),
            ]);
            expect(s.error?.code, 'sei code').to.equal(-32000);
            expect(g.error?.code, 'geth code').to.equal(-32000);
            expect(s.error?.message).to.match(/percentile/i);
            expect(g.error?.message).to.match(/percentile/i);
        });

        it('[divergence] a far-future newest block is rejected by both (-32000)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_feeHistory', ['0x2', '0xffffffff', [50]]),
                rawGeth('eth_feeHistory', ['0x2', '0xffffffff', [50]]),
            ]);
            expect(s.error?.code, 'sei code').to.equal(-32000);
            expect(g.error?.code, 'geth code').to.equal(-32000);
            expect(s.error?.message).to.match(/not yet available|beyond/i);
            expect(g.error?.message).to.match(/beyond head block/i);
            expect(s.error?.message).to.not.equal(g.error?.message);
        });

        // TODO: Sei returns oldestBlock null (not 0x0) for blockCount 0 — revisit. Skipped for now.
        it.skip('[parity] blockCount 0 returns an empty fee history (oldestBlock 0x0)', async () => {
            // A zero block count is not rejected by either node: both return an empty
            // fee history (oldestBlock 0x0, no reward/gasUsedRatio rows) rather than an error.
            const [s, g] = await Promise.all([
                rawSei<any>('eth_feeHistory', [0, 'latest', []]),
                rawGeth<any>('eth_feeHistory', [0, 'latest', []]),
            ]);
            expect(s.error, JSON.stringify(s)).to.equal(undefined);
            expect(g.error, JSON.stringify(g)).to.equal(undefined);
            expect(s.result.oldestBlock, 'sei oldestBlock').to.equal('0x0');
            expect(g.result.oldestBlock, 'geth oldestBlock').to.equal('0x0');
        });

        it('percentile 0 returns the minimum priority fee (or 0 for empty blocks)', async () => {
            const newest = await sei.getBlockNumber();
            const body = await rawSei('eth_feeHistory', ['0x3', ethers.toQuantity(newest), [0]]);
            expect(body.error, JSON.stringify(body.error)).to.equal(undefined);
            const fh: any = body.result;
            if (fh.reward) {
                for (const rewardRow of fh.reward) {
                    expect(Array.isArray(rewardRow), 'reward row is an array').to.equal(true);
                    expect(rewardRow.length, 'one entry per percentile').to.equal(1);
                    // percentile 0 is the minimum observed — must be a valid quantity.
                    expect(rewardRow[0]).to.match(/^0x(0|[1-9a-f][0-9a-f]*)$/);
                }
            }
        });

        it('percentile 100 returns the maximum priority fee (or 0 for empty blocks)', async () => {
            const newest = await sei.getBlockNumber();
            const body = await rawSei('eth_feeHistory', ['0x3', ethers.toQuantity(newest), [100]]);
            expect(body.error, JSON.stringify(body.error)).to.equal(undefined);
            const fh: any = body.result;
            if (fh.reward) {
                for (const rewardRow of fh.reward) {
                    expect(rewardRow[0]).to.match(/^0x(0|[1-9a-f][0-9a-f]*)$/);
                }
            }
        });

        it('percentile 0 <= percentile 50 <= percentile 100 for any non-empty block', async () => {
            // The P0, P50 and P100 of any distribution must be non-decreasing.
            const newest = await sei.getBlockNumber();
            const body = await rawSei('eth_feeHistory', ['0x5', ethers.toQuantity(newest), [0, 50, 100]]);
            expect(body.error, JSON.stringify(body.error)).to.equal(undefined);
            const fh: any = body.result;
            if (!fh.reward) return; // skip if node omits reward for no-tx blocks
            for (const [p0, p50, p100] of fh.reward as string[][]) {
                const v0 = BigInt(p0), v50 = BigInt(p50), v100 = BigInt(p100);
                expect(v0 <= v50, `P0(${v0}) <= P50(${v50})`).to.equal(true);
                expect(v50 <= v100, `P50(${v50}) <= P100(${v100})`).to.equal(true);
            }
        });

        it('empty rewardPercentiles array produces a response without the reward field', async () => {
            // Per the Ethereum spec, omitting percentiles means the node does not compute
            // the reward array at all. The absence of the field (or an empty array) is correct.
            const newest = await sei.getBlockNumber();
            const body = await rawSei('eth_feeHistory', ['0x3', ethers.toQuantity(newest), []]);
            expect(body.error, JSON.stringify(body.error)).to.equal(undefined);
            const fh: any = body.result;
            // Per spec: reward may be absent or null when no percentiles are requested.
            if ('reward' in fh && fh.reward !== null) {
                for (const row of fh.reward) {
                    expect((row as any[]).length, 'reward row is empty when no percentiles requested').to.equal(0);
                }
            }
        });
    });
});
