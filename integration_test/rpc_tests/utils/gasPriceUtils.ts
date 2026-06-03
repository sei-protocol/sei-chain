import { ethers } from 'ethers';
import { expect } from 'chai';
import { waitUntil, blockGasInfo } from './chainUtils';

/**
 * Helpers for the eth_gasPrice parity spec: read the current gas price, sample it
 * against a stable block height, and assert it tracks Sei's base-fee-plus-buffer
 * formula.
 */

/** eth_gasPrice as a bigint. */
export async function gasPrice(provider: ethers.JsonRpcProvider): Promise<bigint> {
    return BigInt(await provider.send('eth_gasPrice', []));
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
 * the base fee of some block in the immediate neighbourhood of B (the block the node
 * sampled for GetNextBaseFeePerGas can drift by one under active load); congested it at
 * least covers the base fee.
 */
export async function assertSeiGasPriceTracks(
    provider: ethers.JsonRpcProvider,
    gasPriceWei: bigint,
    block: number,
): Promise<void> {
    await waitUntil(async () => ((await provider.getBlockNumber()) > block ? true : null), {
        timeoutMs: 15_000,
        label: 'block after sample',
    });
    const head = await provider.getBlockNumber();
    const heights = [block - 1, block, block + 1, block + 2].filter(h => h >= 0 && h <= head);
    const infos = await Promise.all(heights.map(h => blockGasInfo(provider, h)));

    if (infos.some(b => (b.baseFee * 110n) / 100n === gasPriceWei)) return;

    const congested = infos.some(b => b.gasUsed > (b.gasLimit * 80n) / 100n);
    const minBase = infos.reduce((m, b) => (b.baseFee < m ? b.baseFee : m), infos[0].baseFee);
    expect(
        congested && gasPriceWei >= minBase,
        `gasPrice ${gasPriceWei} is not 1.1x any base fee near block ${block} ` +
            `(bases: ${infos.map(b => b.baseFee).join(', ')})`,
    ).to.equal(true);
}
