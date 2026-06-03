import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders } from '../utils/chainUtils';
import { rawSei, rawGeth, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, expectSameError, claimPool } from '../utils/testUtils';
import { abiOf, deployContract } from '../utils/evmUtils';
import { EvmAccount } from '../utils/evmUtils';
import { HEX_QUANTITY } from '../utils/format';
import { Eip1559Params, queryEip1559Params } from '../utils/chainUtils';
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
            if (!seiParams) this.skip();
            const newest = await sei.getBlockNumber();
            const body = await rawSei<any>('eth_feeHistory', ['0x5', ethers.toQuantity(newest), []]);
            expect(body.error, JSON.stringify(body.error)).to.equal(undefined);
            for (const fee of body.result!.baseFeePerGas as string[]) {
                expect(fee, 'canonical quantity').to.match(HEX_QUANTITY);
                expect(Number(fee)).to.be.gte(seiParams!.minFeePerGas);
                expect(Number(fee)).to.be.lte(seiParams!.maxFeePerGas);
            }
        });

        it('[divergence] no percentiles: geth omits reward, Sei returns empty reward rows', async () => {
            const [sNewest, gNewest] = await Promise.all([sei.getBlockNumber(), geth.getBlockNumber()]);
            const [s, g] = await Promise.all([
                rawSei<any>('eth_feeHistory', ['0x2', ethers.toQuantity(sNewest), []]),
                rawGeth<any>('eth_feeHistory', ['0x2', ethers.toQuantity(gNewest), []]),
            ]);
            expect(g.result.reward, 'geth omits reward entirely').to.equal(undefined);
            expect(s.result.reward, 'sei returns a reward entry per block').to.be.an('array');
            (s.result.reward as unknown[][]).forEach(row =>
                expect(row, 'each Sei reward row is empty when no percentiles asked').to.deep.equal([]),
            );
            expect(s.result.baseFeePerGas).to.be.an('array');
            expect(g.result.baseFeePerGas).to.be.an('array');
        });
    });

    describe('base fee manipulation (Sei)', () => {
        const BURST_TIP = ethers.parseUnits('2', 'gwei');

        const getBaseFee = async (): Promise<bigint> =>
            BigInt((await sei.send('eth_getBlockByNumber', ['latest', false])).baseFeePerGas ?? '0x0');

        async function burnBurst(): Promise<{ before: bigint; minBlock: number; maxBlock: number }> {
            const before = await getBaseFee();
            const GAS_LIMIT = 6_000_000n;
            const ITERATIONS = 200n;
            const tip = BURST_TIP;
            let minBlock = Number.MAX_SAFE_INTEGER;
            let maxBlock = 0;

            for (let round = 0; round < 10; round++) {
                const baseNow = await getBaseFee();
                const maxFee = baseNow * 4n + tip;
                const sends: Promise<void>[] = [];
                for (let i = 0; i < spammers.length; i++) {
                    const s = spammers[i];
                    if ((await s.balance()) < GAS_LIMIT * maxFee) continue;
                    const data = burnerIface.encodeFunctionData('burnGasIterations', [
                        BigInt(round * 100 + i),
                        ITERATIONS,
                    ]);
                    sends.push(
                        s.wallet
                            .sendTransaction({
                                to: seiBurner,
                                data,
                                gasLimit: GAS_LIMIT,
                                maxFeePerGas: maxFee,
                                maxPriorityFeePerGas: tip,
                                type: 2,
                            })
                            .then(t => t.wait())
                            .then(r => {
                                if (r) {
                                    minBlock = Math.min(minBlock, r.blockNumber);
                                    maxBlock = Math.max(maxBlock, r.blockNumber);
                                }
                            })
                            .catch(() => undefined),
                    );
                }
                if (sends.length === 0) break;
                await Promise.all(sends);
            }
            return { before, minBlock, maxBlock };
        }

        it('drives the base fee up and every field replays through the fee-market formula', async function () {
            if (!seiParams) this.skip();
            const { before, minBlock, maxBlock } = await burnBurst();
            if (maxBlock === 0) this.skip();

            // Cover the whole burst plus one block on each side so the boundary
            // transitions (rise into the burst, decay out of it) are included.
            const newest = Math.min(maxBlock + 1, await sei.getBlockNumber());
            const count = Math.min(newest - minBlock + 2, 1024);
            const percentiles = [10, 50, 90];
            const fh = parseFeeHistory(await feeHistory(sei, count, ethers.toQuantity(newest), percentiles));

            // The whole window is within mined history, so the request returns exactly `count` blocks.
            assertFeeHistoryCounts(fh, count, percentiles.length);
            await verifyFeeHistorySeries(sei, fh, newest, percentiles, 'sei', seiParams);

            const peak = fh.baseFeePerGas.reduce((m, v) => (v > m ? v : m), 0n);
            expect(peak > before, `base fee should rise above ${before}, peaked at ${peak}`).to.equal(
                true,
            );

            // At least one block was pushed over target, so at least one transition rose.
            const rose = fh.baseFeePerGas.some((v, i) => i > 0 && v > fh.baseFeePerGas[i - 1]);
            expect(rose, 'at least one block raised the base fee').to.equal(true);

            // Every burst transaction paid exactly BURST_TIP, and the effective tip is
            // not capped (maxFee = 4*base + tip), so a block made up of burst txs must
            // report that exact tip at *every* requested percentile — not merely a
            // non-zero value. Find such a block and verify the precise amount.
            const exactTipBlock = fh.reward!.find(
                row => row.length === percentiles.length && row.every(r => r === BURST_TIP),
            );
            expect(
                exactTipBlock,
                `a burst block must report the exact ${BURST_TIP} wei tip at every percentile`,
            ).to.not.equal(undefined);

            // And no block may report a tip above what anyone actually paid.
            for (const row of fh.reward!) {
                for (const r of row) {
                    expect(
                        r <= BURST_TIP,
                        `reward ${r} must not exceed the max tip paid (${BURST_TIP})`,
                    ).to.equal(true);
                }
            }
        });

        it('a single over-target block reports gasUsedRatio above the target ratio', async function () {
            if (!seiParams) this.skip();
            const data = burnerIface.encodeFunctionData('burnGasIterations', [777n, 200n]);
            const tip = ethers.parseUnits('1', 'gwei');
            const baseNow = await getBaseFee();
            const gasLimit = 6_000_000n;
            const maxFee = baseNow * 4n + tip;
            const reserve = gasLimit * maxFee;
            // burnBurst (above) may have drained the earlier spammers, so pick the first
            // one that can still cover a full over-target transaction; skip if the pool
            // is exhausted rather than fail on insufficient funds.
            let sender: EvmAccount | undefined;
            for (const s of spammers) {
                if ((await s.balance()) >= reserve) {
                    sender = s;
                    break;
                }
            }
            if (!sender) this.skip();
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
        it('blockCount 0 returns null arrays (Sei) and the geth divergence is documented', async () => {
            const [s, g] = await Promise.all([
                rawSei<any>('eth_feeHistory', ['0x0', 'latest', []]),
                rawGeth<any>('eth_feeHistory', ['0x0', 'latest', []]),
            ]);
            expect(s.error, JSON.stringify(s.error)).to.equal(undefined);
            expect(g.error, JSON.stringify(g.error)).to.equal(undefined);
            expect(s.result.gasUsedRatio, 'sei nulls gasUsedRatio for empty range').to.equal(null);
            expect(g.result.gasUsedRatio, 'geth nulls gasUsedRatio for empty range').to.equal(null);
            // [divergence] Sei reports oldestBlock null; geth reports "0x0".
            expect(s.result.oldestBlock).to.equal(null);
            expect(g.result.oldestBlock).to.equal('0x0');
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
    });
});
