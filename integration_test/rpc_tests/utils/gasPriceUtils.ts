import { ethers } from 'ethers';
import { expect } from 'chai';
import { waitUntil, blockGasInfo } from './chainUtils';

/**
 * Helpers for the eth_gasPrice parity spec: read the current gas price, sample it
 * against a stable block height, and assert it tracks Sei's base-fee-plus-buffer formula.
 */

export async function gasPrice(provider: ethers.JsonRpcProvider): Promise<bigint> {
    return BigInt(await provider.send('eth_gasPrice', []));
}

/** Sei's idle-chain default priority fee (1 gwei), returned when the latest block is uncongested. */
export const DEFAULT_PRIORITY_FEE_WEI = ethers.parseUnits('1', 'gwei');

/** Sei treats a block as congested once EVM receipt gas burns > 80% of the block gas limit. */
export const CONGESTION_THRESHOLD = 0.8;

export async function maxPriorityFeePerGas(provider: ethers.JsonRpcProvider): Promise<bigint> {
    return BigInt(await provider.send('eth_maxPriorityFeePerGas', []));
}

export interface PriorityFeeSample {
    tip: bigint;
    block: number;
    gasUsed: bigint;
    evmGasUsed: bigint;
    gasLimit: bigint;
    ratio: number;
    evmRatio: number;
}

/**
 * Read eth_maxPriorityFeePerGas and the latest block it was derived from without
 * crossing a block boundary. The fee RPC has no block tag, so callers that assert
 * against latest-block gas usage must pin a stable head themselves.
 */
export async function maxPriorityFeePerGasAtStableBlock(
    provider: ethers.JsonRpcProvider,
): Promise<PriorityFeeSample> {
    for (let i = 0; i < 50; i++) {
        const b1 = await provider.getBlockNumber();
        const tip = await maxPriorityFeePerGas(provider);
        const b2 = await provider.getBlockNumber();
        if (b1 !== b2) continue;

        const info = await blockGasInfo(provider, b1);
        const evmGasUsed = await evmGasUsedForBlock(provider, b1);
        return {
            tip,
            block: info.number,
            gasUsed: info.gasUsed,
            evmGasUsed,
            gasLimit: info.gasLimit,
            ratio: Number(info.gasUsed) / Number(info.gasLimit),
            evmRatio: Number(evmGasUsed) / Number(info.gasLimit),
        };
    }
    throw new Error('maxPriorityFeePerGasAtStableBlock: block kept advancing across the sample');
}

export async function evmGasUsedForBlock(
    provider: ethers.JsonRpcProvider,
    block: number,
): Promise<bigint> {
    const receipts = await provider.send('eth_getBlockReceipts', [ethers.toQuantity(block)]);
    return (receipts ?? []).reduce(
        (sum: bigint, receipt: { gasUsed: string }) => sum + BigInt(receipt.gasUsed),
        0n,
    );
}

export function isCongested(sample: { evmGasUsed: bigint; gasLimit: bigint }): boolean {
    return sample.evmGasUsed > (sample.gasLimit * 80n) / 100n;
}

/**
 * Read eth_gasPrice with the latest height pinned: only accept a reading where no new
 * block landed across the call, so the returned price provably derives from
 * GetNextBaseFeePerGas(B). Returns the price and that block height B.
 */
export async function gasPriceAtStableBlock(
    provider: ethers.JsonRpcProvider,
): Promise<{ gasPrice: bigint; block: number }> {
    for (let i = 0; i < 20; i++) {
        const b1 = await provider.getBlockNumber();
        const price = await gasPrice(provider);
        const b2 = await provider.getBlockNumber();
        if (b1 === b2) return { gasPrice: price, block: b1 };
    }
    throw new Error('gasPriceAtStableBlock: block kept advancing across the gas price call');
}

/**
 * Assert a Sei gas price reading tracks the base fee: uncongested it is exactly 1.1x
 * (floor, matching the node's integer Mul/Div) the base fee of some block in the immediate
 * neighbourhood of B, congested it at least covers the base fee.
 */
export async function assertSeiGasPriceTracks(
    provider: ethers.JsonRpcProvider,
    gasPriceWei: bigint,
    block: number,
): Promise<void> {
    await waitUntil(async () => ((await provider.getBlockNumber()) >= block + 2 ? true : null), {
        timeoutMs: 15_000,
        label: 'two blocks after sample',
    });
    const head = await provider.getBlockNumber();
    const heights = [block - 1, block, block + 1, block + 2].filter(h => h >= 0 && h <= head);
    const infos = await Promise.all(heights.map(h => blockGasInfo(provider, h)));
    const evmGasUsed = await Promise.all(heights.map(h => evmGasUsedForBlock(provider, h)));

    if (infos.some(b => (b.baseFee * 110n) / 100n === gasPriceWei)) return;

    const congested = infos.some((b, i) => evmGasUsed[i] > (b.gasLimit * 80n) / 100n);
    const minBase = infos.reduce((m, b) => (b.baseFee < m ? b.baseFee : m), infos[0].baseFee);
    expect(
        congested && gasPriceWei >= minBase,
        `gasPrice ${gasPriceWei} is not 1.1x any base fee near block ${block} ` +
            `(bases: ${infos.map(b => b.baseFee).join(', ')})`,
    ).to.equal(true);
}
