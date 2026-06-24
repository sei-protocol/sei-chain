import { ethers } from 'ethers';
import { expect } from 'chai';
import { Eip1559Params, nextBaseFeeSei, nextBaseFeeGeth, blockGasInfo } from './chainUtils';

export { blockGasInfo };

/**
 * Helpers for the eth_feeHistory parity spec: a caller, a parser into native types,
 * per-block gas lookups, and assertions that replay the EIP-1559 fee market and check
 * the array shapes the RPC promises.
 */

export interface ParsedFeeHistory {
    oldest: number;
    baseFeePerGas: bigint[];
    gasUsedRatio: number[];
    reward?: bigint[][];
}

/** Call eth_feeHistory with blockCount as a quantity, a newest tag, and reward percentiles. */
export function feeHistory(
    provider: ethers.JsonRpcProvider,
    count: number,
    newest: string,
    percentiles: number[],
): Promise<any> {
    return provider.send('eth_feeHistory', [ethers.toQuantity(count), newest, percentiles]);
}

/** Parse a raw eth_feeHistory result into native bigint/number arrays. */
export function parseFeeHistory(raw: any): ParsedFeeHistory {
    return {
        oldest: ethers.toNumber(raw.oldestBlock),
        baseFeePerGas: (raw.baseFeePerGas as string[]).map(BigInt),
        gasUsedRatio: (raw.gasUsedRatio as number[]).map(Number),
        reward: raw.reward
            ? (raw.reward as string[][]).map(row => row.map(BigInt))
            : undefined,
    };
}

/**
 * Assert the array lengths line up with a `blockCount`-block request:
 *   - gasUsedRatio:  exactly `expectedCount` entries (one per block)
 *   - baseFeePerGas: `expectedCount + 1` (the extra slot forecasts newest+1's base fee)
 *   - reward:        one row per block, each with one entry per requested percentile
 *                    (only when percentiles were requested)
 */
export function assertFeeHistoryCounts(
    fh: ParsedFeeHistory,
    expectedCount: number,
    percentilesLength: number,
): void {
    expect(fh.gasUsedRatio.length, 'gasUsedRatio has exactly blockCount entries').to.equal(
        expectedCount,
    );
    expect(fh.baseFeePerGas.length, 'baseFeePerGas has blockCount + 1 entries').to.equal(
        expectedCount + 1,
    );
    // typeof NaN === 'number', so assert a real, non-negative integer height here.
    expect(
        Number.isInteger(fh.oldest) && fh.oldest >= 0,
        `oldestBlock is a valid block height (got ${fh.oldest})`,
    ).to.equal(true);
    if (percentilesLength > 0) {
        expect(fh.reward, 'reward present when percentiles requested').to.not.equal(undefined);
        expect(fh.reward!.length, 'one reward row per block').to.equal(expectedCount);
        fh.reward!.forEach((row, i) =>
            expect(row.length, `reward[${i}] has one entry per requested percentile`).to.equal(
                percentilesLength,
            ),
        );
    }
}

/**
 * Replay the whole fee-history window: verify the array shapes, that each baseFeePerGas
 * and gasUsedRatio matches the on-chain block, that consecutive base fees obey the
 * chain's fee-market formula, and that the trailing forecast equals newest+1 once mined.
 */
export async function verifyFeeHistorySeries(
    provider: ethers.JsonRpcProvider,
    fh: ParsedFeeHistory,
    newest: number,
    percentiles: number[],
    chain: 'sei' | 'geth',
    seiParams: Eip1559Params | null,
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
        const blk = await blockGasInfo(provider, fh.oldest + i);

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
        const nb = await blockGasInfo(provider, forecastBlock);
        expect(fh.baseFeePerGas[count], 'forecast equals the real next block base fee').to.equal(
            nb.baseFee,
        );
    }
}
