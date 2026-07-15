import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError, blockGasInfo, waitUntil } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, claimPool } from '../utils/testUtils';
import { EvmAccount } from '../utils/evmUtils';
import { burnGasBurst } from '../utils/txUtils';
import { HEX_QUANTITY } from '../utils/format';
import { feeHistory } from '../utils/feeHistoryUtils';
import {
    maxPriorityFeePerGas,
    gasPrice,
    DEFAULT_PRIORITY_FEE_WEI,
    CONGESTION_THRESHOLD,
    isCongested,
    maxPriorityFeePerGasAtStableBlock,
} from '../utils/gasPriceUtils';

describe('eth_maxPriorityFeePerGas', function () {
    this.timeout(240 * 1000);

    const { sei, geth } = bothProviders();

    // Burst txs all pay this exact tip; the same value the other fee-market specs use,
    // so a congested block's gas-weighted 50th-percentile reward is exactly this.
    const BURST_TIP = ethers.parseUnits('2', 'gwei');

    let runtime: RuntimeState;
    let spammers: EvmAccount[];

    before(() => {
        runtime = readRuntimeState();
        spammers = claimPool(runtime, sei, 8, 'eth_maxPriorityFeePerGas');
    });

    describe('queries', () => {
        it('returns a canonical, non-negative hex quantity', async () => {
            const raw = await sei.send('eth_maxPriorityFeePerGas', []);
            expect(raw, 'canonical quantity').to.match(HEX_QUANTITY);
            expect(BigInt(raw) >= 0n, 'non-negative tip').to.equal(true);
        });

        it('stays within a sane bound (never more than the total gas price)', async () => {
            const [tip, price] = await Promise.all([maxPriorityFeePerGas(sei), gasPrice(sei)]);
            expect(tip <= price, `tip ${tip} must be <= gasPrice ${price}`).to.equal(true);
        });

        it('falls back to the 1-gwei default while the latest block is uncongested', async () => {
            const sample = await waitUntil(
                async () => {
                    const stable = await maxPriorityFeePerGasAtStableBlock(sei);
                    return isCongested(stable) ? null : stable;
                },
                { timeoutMs: 60_000, intervalMs: 250, label: 'EVM-uncongested priority-fee head' },
            );
            expect(sample.tip, 'uncongested tip == 1 gwei default').to.equal(
                DEFAULT_PRIORITY_FEE_WEI,
            );
        });

        it('returns either the 1-gwei default or the latest-block 50th-pct tip (congestion rule)', async () => {
            const sample = await maxPriorityFeePerGasAtStableBlock(sei);
            const fh = await feeHistory(sei, 1, ethers.toQuantity(sample.block), [50]);
            const reward = fh.reward?.[0]?.[0];
            const congestedTip = reward ? BigInt(reward) : 0n;
            expect(
                sample.tip === DEFAULT_PRIORITY_FEE_WEI || sample.tip === congestedTip,
                `tip ${sample.tip} from block ${sample.block} must be the 1-gwei default ` +
                    `(${DEFAULT_PRIORITY_FEE_WEI}) or that block's 50th-pct tip (${congestedTip})`,
            ).to.equal(true);
        });

        it('switches off the 1-gwei default and reports the burst tip once a block is congested', async function () {
            let burstDone = false;
            const burst = burnGasBurst(sei, runtime, spammers, {
                rounds: 12,
                gasLimit: 4_600_000n,
                tip: BURST_TIP,
            }).finally(() => {
                burstDone = true;
            });

            let sample: { tip: bigint; ratio: number; block: number } | null = null;
            const deadline = Date.now() + 90_000;
            while (Date.now() < deadline && !sample) {
                const b1 = await sei.getBlockNumber();
                const tip = await maxPriorityFeePerGas(sei);
                const info = await blockGasInfo(sei, 'latest');
                const b2 = await sei.getBlockNumber();
                if (b1 === b2 && info.number === b1) {
                    const ratio = Number(info.gasUsed) / Number(info.gasLimit);
                    if (ratio > 0.8) sample = { tip, ratio, block: info.number };
                }
                if (burstDone || sample) break;
                await new Promise(r => setTimeout(r, 50));
            }
            await burst;

            expect(
                sample,
                `no block crossed the ${CONGESTION_THRESHOLD} congestion threshold during the burst`,
            ).to.not.equal(null);
            expect(
                sample!.ratio > CONGESTION_THRESHOLD,
                `sampled head ratio ${sample!.ratio} must exceed the ${CONGESTION_THRESHOLD} threshold`,
            ).to.equal(true);
            expect(
                sample!.tip,
                `congested tip (block ${sample!.block}, ratio ${sample!.ratio}) must be the ` +
                    `burst tip ${BURST_TIP}, not the 1-gwei default`,
            ).to.equal(BURST_TIP);
            expect(sample!.tip, 'congested tip must differ from the idle default').to.not.equal(
                DEFAULT_PRIORITY_FEE_WEI,
            );
        });
    });

    describe('schema matching (parity with geth)', () => {
        it('both nodes return a canonical hex quantity', async () => {
            const [s, g] = await Promise.all([
                sei.send('eth_maxPriorityFeePerGas', []),
                geth.send('eth_maxPriorityFeePerGas', []),
            ]);
            expect(s, 'sei').to.match(HEX_QUANTITY);
            expect(g, 'geth').to.match(HEX_QUANTITY);
        });
    });

    describe('wrong params / error handling (parity with geth)', () => {
        it('rejects extra positional parameters with -32602, identically to geth', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_maxPriorityFeePerGas', ['latest']),
                rawGeth('eth_maxPriorityFeePerGas', ['latest']),
            ]);
            expectJsonRpcError(s, -32602, /too many arguments, want at most 0/);
            expectJsonRpcError(g, -32602, /too many arguments, want at most 0/);
        });

        it('rejects non-array params with -32602 (non-array args) on both', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_maxPriorityFeePerGas', 'latest'),
                rawGeth('eth_maxPriorityFeePerGas', 'latest'),
            ]);
            expectJsonRpcError(s, -32602, /non-array args/);
            expectJsonRpcError(g, -32602, /non-array args/);
        });
    });
});
