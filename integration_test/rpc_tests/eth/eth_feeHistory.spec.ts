import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders } from '../utils/providers';
import { rawSei, rawGeth, expectJsonRpcError, JsonRpcEnvelope } from '../utils/rpc';
import { readRuntimeState, RuntimeState } from '../utils/state';
import { abiOf, deployContract } from '../utils/deploy';
import { EvmAccount } from '../utils/wallet';
import { HEX_QUANTITY } from '../utils/format';
import {
    Eip1559Params,
    queryEip1559Params,
    nextBaseFeeSei,
    nextBaseFeeGeth,
} from '../utils/eip1559';

// eth_feeHistory parity against a local `geth --dev` reference. Every field returned
// (baseFeePerGas, gasUsedRatio, reward) is cross-checked against the underlying
// blocks, and the base-fee series is replayed through each chain's own fee-market
// formula after we deliberately raise the base fee with a gas burner.
describe('eth_feeHistory', function () {
    this.timeout(240 * 1000);

    const { sei, geth } = bothProviders();
    const burnerIface = new ethers.Interface(abiOf('GasBurner.sol', 'RealGasBurner'));

    let runtime: RuntimeState;
    let seiBurner: string;
    let spammers: EvmAccount[];
    let seiParams: Eip1559Params | null;

    interface ParsedFeeHistory {
        oldest: number;
        baseFeePerGas: bigint[];
        gasUsedRatio: number[];
        reward?: bigint[][];
    }

    const feeHistory = (
        provider: ethers.JsonRpcProvider,
        count: number,
        newest: string,
        percentiles: number[],
    ) => provider.send('eth_feeHistory', [ethers.toQuantity(count), newest, percentiles]);

    function parse(raw: any): ParsedFeeHistory {
        return {
            oldest: Number(raw.oldestBlock),
            baseFeePerGas: (raw.baseFeePerGas as string[]).map(BigInt),
            gasUsedRatio: (raw.gasUsedRatio as number[]).map(Number),
            reward: raw.reward
                ? (raw.reward as string[][]).map(row => row.map(BigInt))
                : undefined,
        };
    }

    async function blockInfo(provider: ethers.JsonRpcProvider, n: number) {
        const b = await provider.send('eth_getBlockByNumber', [ethers.toQuantity(n), false]);
        return {
            gasUsed: BigInt(b.gasUsed),
            gasLimit: BigInt(b.gasLimit),
            baseFee: BigInt(b.baseFeePerGas ?? '0x0'),
        };
    }

    /**
     * Assert the whole envelope is internally consistent: array lengths, oldestBlock,
     * every baseFeePerGas/gasUsedRatio entry against its block, ascending rewards, and
     * the base-fee transition between each pair replayed through the chain's formula
     * (exact on geth's integer math, within rounding tolerance on Sei's decimal math).
     */
    async function verifySeries(
        provider: ethers.JsonRpcProvider,
        fh: ParsedFeeHistory,
        newest: number,
        percentiles: number[],
        chain: 'sei' | 'geth',
    ): Promise<void> {
        const count = fh.gasUsedRatio.length;
        expect(fh.baseFeePerGas.length, 'baseFeePerGas is blockCount + 1').to.equal(count + 1);
        expect(fh.oldest, 'oldestBlock = newest - count + 1').to.equal(newest - count + 1);
        if (percentiles.length > 0) {
            expect(fh.reward, 'reward present when percentiles requested').to.not.equal(undefined);
            expect(fh.reward!.length, 'one reward row per block').to.equal(count);
        }

        const head = await provider.getBlockNumber();
        // Sei reports gasUsedRatio quantized to 4 decimal places; geth is full precision.
        const ratioTol = chain === 'sei' ? 1.01e-4 : 1e-9;
        for (let i = 0; i < count; i++) {
            const blk = await blockInfo(provider, fh.oldest + i);

            expect(fh.baseFeePerGas[i], `baseFeePerGas[${i}] equals block base fee`).to.equal(
                blk.baseFee,
            );
            const ratio = Number(blk.gasUsed) / Number(blk.gasLimit);
            expect(fh.gasUsedRatio[i], `gasUsedRatio[${i}] equals gasUsed/gasLimit`).to.be.closeTo(
                ratio,
                ratioTol,
            );

            if (fh.reward) {
                const row = fh.reward[i];
                expect(row.length, `reward[${i}] has one entry per percentile`).to.equal(
                    percentiles.length,
                );
                for (let p = 1; p < row.length; p++) {
                    expect(row[p] >= row[p - 1], `reward[${i}] percentiles ascending`).to.equal(true);
                }
            }

            if (chain === 'geth') {
                const predicted = nextBaseFeeGeth(fh.baseFeePerGas[i], blk.gasUsed, blk.gasLimit);
                expect(fh.baseFeePerGas[i + 1], `geth base-fee transition ${i}`).to.equal(predicted);
            } else if (seiParams) {
                const predicted = nextBaseFeeSei(
                    Number(fh.baseFeePerGas[i]),
                    Number(blk.gasUsed),
                    seiParams,
                );
                expect(
                    Number(fh.baseFeePerGas[i + 1]),
                    `sei base-fee transition ${i}`,
                ).to.be.closeTo(predicted, 5);
            }
        }

        // The trailing element forecasts newest+1's base fee. A block's base fee depends
        // only on its parent, so once newest+1 is mined the forecast must equal it exactly.
        const forecastBlock = fh.oldest + count;
        if (forecastBlock <= head) {
            const nb = await blockInfo(provider, forecastBlock);
            expect(fh.baseFeePerGas[count], 'forecast equals the real next block base fee').to.equal(
                nb.baseFee,
            );
        }
    }

    function claimPool(count: number, salt: string): EvmAccount[] {
        const pool = runtime.funded.pool;
        let h = 0;
        for (const ch of salt) h = (h * 31 + ch.charCodeAt(0)) >>> 0;
        const start = h % pool.length;
        return Array.from({ length: count }, (_, i) =>
            EvmAccount.fromPrivateKey(pool[(start + i) % pool.length].privateKey, sei),
        );
    }

    before(async () => {
        runtime = readRuntimeState();
        seiBurner = runtime.contracts.gasBurner;
        spammers = claimPool(5, 'eth_feeHistory');
        seiParams = await queryEip1559Params();
    });

    describe('schema / structure', () => {
        it('returns well-formed, length-consistent arrays on Sei', async () => {
            const newest = await sei.getBlockNumber();
            const percentiles = [5, 25, 50, 75, 95];
            const fh = parse(await feeHistory(sei, 5, ethers.toQuantity(newest), percentiles));
            await verifySeries(sei, fh, newest, percentiles, 'sei');
        });

        it('returns well-formed, length-consistent arrays on geth', async () => {
            const newest = await geth.getBlockNumber();
            const count = Math.min(newest, 4);
            const percentiles = [10, 50, 90];
            const fh = parse(await feeHistory(geth, count, ethers.toQuantity(newest), percentiles));
            await verifySeries(geth, fh, newest, percentiles, 'geth');
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
        const getBaseFee = async (): Promise<bigint> =>
            BigInt((await sei.send('eth_getBlockByNumber', ['latest', false])).baseFeePerGas ?? '0x0');

        async function burnBurst(): Promise<{ before: bigint; minBlock: number; maxBlock: number }> {
            const before = await getBaseFee();
            const GAS_LIMIT = 6_000_000n;
            const ITERATIONS = 200n;
            const tip = ethers.parseUnits('2', 'gwei');
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
            const fh = parse(await feeHistory(sei, count, ethers.toQuantity(newest), percentiles));

            await verifySeries(sei, fh, newest, percentiles, 'sei');

            const peak = fh.baseFeePerGas.reduce((m, v) => (v > m ? v : m), 0n);
            expect(peak > before, `base fee should rise above ${before}, peaked at ${peak}`).to.equal(
                true,
            );

            // At least one block was pushed over target, so at least one transition rose.
            const rose = fh.baseFeePerGas.some((v, i) => i > 0 && v > fh.baseFeePerGas[i - 1]);
            expect(rose, 'at least one block raised the base fee').to.equal(true);

            // We paid a 2 gwei tip, so the top percentile of some burst block is non-zero.
            const topRewards = fh.reward!.map(r => r[r.length - 1]);
            expect(
                topRewards.some(r => r > 0n),
                'a paid tip must surface in the reward percentiles',
            ).to.equal(true);
        });

        it('a single over-target block reports gasUsedRatio above the target ratio', async function () {
            if (!seiParams) this.skip();
            const data = burnerIface.encodeFunctionData('burnGasIterations', [777n, 200n]);
            const tip = ethers.parseUnits('1', 'gwei');
            const baseNow = await getBaseFee();
            const tx = await spammers[0].wallet.sendTransaction({
                to: seiBurner,
                data,
                gasLimit: 6_000_000n,
                maxFeePerGas: baseNow * 4n + tip,
                maxPriorityFeePerGas: tip,
                type: 2,
            });
            const receipt = await tx.wait();
            const blk = await blockInfo(sei, receipt!.blockNumber);
            const targetRatio = seiParams!.targetGasUsedPerBlock / Number(blk.gasLimit);

            const fh = parse(
                await feeHistory(sei, 1, ethers.toQuantity(receipt!.blockNumber), []),
            );
            const reportedRatio = fh.gasUsedRatio[0];
            expect(reportedRatio).to.be.closeTo(Number(blk.gasUsed) / Number(blk.gasLimit), 1.01e-4);
            expect(reportedRatio > targetRatio, 'over-target block exceeds the target ratio').to.equal(
                true,
            );
        });
    });

    describe('base fee manipulation (geth)', () => {
        let gethBurner: ethers.Contract;
        let gethSigner: EvmAccount;
        let gethNonce: number;
        const TIP = ethers.parseUnits('1', 'gwei');

        before(async () => {
            gethSigner = EvmAccount.fromPrivateKey(runtime.funded.gethAdmin.privateKey, geth);
            const dep = await deployContract(gethSigner, 'GasBurner.sol', [], 'RealGasBurner');
            gethBurner = new ethers.Contract(dep.address, burnerIface, gethSigner.wallet);
            // geth --dev instamines, so manage the nonce explicitly to avoid the
            // pending-count lag racing successive heavy burns.
            gethNonce = await gethSigner.nonce('latest');
        });

        // Each heavy burn is its own block (one tx per dev block). Burn ~60% of the
        // parent gas limit (over geth's 50% target so the next base fee rises) while
        // capping the tx gas limit at 80% — comfortably under the block limit, which
        // geth nudges +/-1/1024 per block, so the tx is always minable.
        async function heavyGethBlock(salt: number): Promise<number> {
            const parent = await blockInfo(geth, await geth.getBlockNumber());
            const iterations = (parent.gasLimit * 60n) / 100n / 22_300n;
            const tx = await gethBurner.burnGasIterations(salt, iterations, {
                gasLimit: (parent.gasLimit * 80n) / 100n,
                maxFeePerGas: parent.baseFee * 4n + TIP,
                maxPriorityFeePerGas: TIP,
                nonce: gethNonce++,
            });
            const receipt = await tx.wait(1, 30_000);
            expect(receipt!.status, 'heavy geth burn must succeed').to.equal(1);
            return receipt!.blockNumber;
        }

        it('raises the base fee monotonically and replays exactly through geth CalcBaseFee', async () => {
            const blocks: number[] = [];
            for (let i = 0; i < 4; i++) blocks.push(await heavyGethBlock(i));

            const minBlock = blocks[0];
            const newest = blocks[blocks.length - 1];
            const count = newest - minBlock + 1;
            const percentiles = [10, 50, 90];
            const fh = parse(await feeHistory(geth, count, ethers.toQuantity(newest), percentiles));

            await verifySeries(geth, fh, newest, percentiles, 'geth');

            // Each block was > 50% full, so every transition strictly increased the base fee.
            for (let i = 1; i <= count; i++) {
                expect(
                    fh.baseFeePerGas[i] > fh.baseFeePerGas[i - 1],
                    `geth base fee strictly rose at ${i}`,
                ).to.equal(true);
            }
            // Each block was over the 50% target.
            fh.gasUsedRatio.forEach((r, i) =>
                expect(r > 0.5, `geth block ${i} over 50% target (got ${r})`).to.equal(true),
            );
            // A single tx per block, all paying the same tip, so every reward percentile is that tip.
            fh.reward!.forEach((row, i) =>
                row.forEach((r, p) =>
                    expect(r, `geth reward[${i}][${p}] equals the paid tip`).to.equal(TIP),
                ),
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
            const percentiles = [25, 75];
            const fh = parse(await feeHistory(sei, 3, ethers.toQuantity(newest), percentiles));
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
        function expectSameError(s: JsonRpcEnvelope, g: JsonRpcEnvelope): void {
            expect(g.error, `geth must error, got ${JSON.stringify(g.result)}`).to.not.equal(undefined);
            expect(s.error, `sei must error, got ${JSON.stringify(s.result)}`).to.not.equal(undefined);
            expect(s.error!.code, 'error.code parity').to.equal(g.error!.code);
            expect(s.error!.message, 'error.message parity').to.equal(g.error!.message);
            expect(s.error!.data, 'error.data parity').to.deep.equal(g.error!.data);
        }

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
