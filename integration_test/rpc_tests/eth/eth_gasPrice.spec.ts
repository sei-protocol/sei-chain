import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders } from '../utils/chainUtils';
import { rawSei, rawGeth, expectJsonRpcError, blockGasInfo } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, expectSameError, claimPool } from '../utils/testUtils';
import { abiOf } from '../utils/evmUtils';
import { EvmAccount } from '../utils/evmUtils';
import { HEX_QUANTITY } from '../utils/format';
import { Eip1559Params, queryEip1559Params, waitUntil } from '../utils/chainUtils';
import { gasPrice, gasPriceAtStableBlock, assertSeiGasPriceTracks } from '../utils/gasPriceUtils';

describe('eth_gasPrice', function () {
    this.timeout(240 * 1000);

    const { sei, geth } = bothProviders();
    const burnerIface = new ethers.Interface(abiOf('GasBurner.sol', 'RealGasBurner'));

    let runtime: RuntimeState;
    let seiBurner: string;
    let spammers: EvmAccount[];
    let seiParams: Eip1559Params | null;
    let floorBase: bigint;
    let floorGasPrice: bigint;

    before(async () => {
        runtime = readRuntimeState();
        seiBurner = runtime.contracts.gasBurner;
        spammers = claimPool(runtime, sei, 5, 'eth_gasPrice');
        seiParams = await queryEip1559Params();
        floorBase = seiParams ? BigInt(seiParams.minFeePerGas) : 1_000_000_000n;
        floorGasPrice = (floorBase * 110n) / 100n;
    });

    describe('happy path / schema', () => {
        it('returns a positive canonical quantity on Sei', async () => {
            const body = await rawSei<string>('eth_gasPrice', []);
            expect(body.error, JSON.stringify(body.error)).to.equal(undefined);
            expect(body.result).to.match(HEX_QUANTITY);
            expect(BigInt(body.result!) > 0n).to.equal(true);
        });

        it('returns a positive canonical quantity on geth', async () => {
            const body = await rawGeth<string>('eth_gasPrice', []);
            expect(body.error, JSON.stringify(body.error)).to.equal(undefined);
            expect(body.result).to.match(HEX_QUANTITY);
            expect(BigInt(body.result!) > 0n).to.equal(true);
        });

        it('is at least the current base fee, so a tx priced at it is includable (both)', async () => {
            const [sGas, sBlk, gGas, gBlk] = await Promise.all([
                gasPrice(sei),
                blockGasInfo(sei, 'latest'),
                gasPrice(geth),
                blockGasInfo(geth, 'latest'),
            ]);
            expect(sGas >= sBlk.baseFee, `sei gasPrice ${sGas} < base ${sBlk.baseFee}`).to.equal(true);
            expect(gGas >= gBlk.baseFee, `geth gasPrice ${gGas} < base ${gBlk.baseFee}`).to.equal(true);
        });
    });

    describe('relationship to base fee and priority fee', () => {
        it('[Sei] an uncongested gas price is exactly the base fee plus the 10% buffer', async () => {
            const { gasPrice: price, block } = await gasPriceAtStableBlock(sei);
            await assertSeiGasPriceTracks(sei, price, block);
        });

        it('[geth] the gas price equals the base fee plus the suggested priority fee (exact)', async () => {
            const [price, tip, blk] = await Promise.all([
                gasPrice(geth),
                BigInt(await geth.send('eth_maxPriorityFeePerGas', [])),
                blockGasInfo(geth, 'latest'),
            ]);
            expect(price, 'geth gasPrice = baseFee + tip').to.equal(blk.baseFee + tip);
        });

        it('[Sei] maxPriorityFeePerGas defaults to 1 gwei while the chain is uncongested', async () => {
            // Quiescent runs sit well under the 80% congestion threshold.
            const tip = BigInt(await sei.send('eth_maxPriorityFeePerGas', []));
            expect(tip).to.equal(1_000_000_000n);
        });

        it('[divergence] Sei multiplies the base fee by 1.1; geth adds a flat tip', async () => {
            const [sGas, sBlk, gGas, gTip, gBlk] = await Promise.all([
                gasPrice(sei),
                blockGasInfo(sei, 'latest'),
                gasPrice(geth),
                BigInt(await geth.send('eth_maxPriorityFeePerGas', [])),
                blockGasInfo(geth, 'latest'),
            ]);
            // geth's suggestion is purely additive.
            expect(gGas - gBlk.baseFee, 'geth premium is the flat tip').to.equal(gTip);
            // Sei's premium scales with the base fee (10%), not a flat 1 gwei tip.
            expect(sGas, 'sei does not use base + 1gwei').to.not.equal(sBlk.baseFee + 1_000_000_000n);
        });
    });

    describe('reflects base fee increases (Sei)', () => {
        async function burstAndSample(): Promise<{
            samples: { gasPrice: bigint; block: number }[];
            peakGasPrice: bigint;
            peakBase: bigint;
        }> {
            const samples: { gasPrice: bigint; block: number }[] = [];
            let peakGasPrice = 0n;
            let peakBase = 0n;
            const GAS_LIMIT = 6_000_000n;
            const ITERATIONS = 200n;
            const tip = ethers.parseUnits('2', 'gwei');

            for (let round = 0; round < 10; round++) {
                const baseNow = (await blockGasInfo(sei, 'latest')).baseFee;
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
                            .then(() => undefined)
                            .catch(() => undefined),
                    );
                }
                if (sends.length === 0) break;
                await Promise.all(sends);

                const sample = await gasPriceAtStableBlock(sei);
                samples.push(sample);
                const base = (await blockGasInfo(sei, sample.block)).baseFee;
                if (sample.gasPrice > peakGasPrice) peakGasPrice = sample.gasPrice;
                if (base > peakBase) peakBase = base;
            }
            return { samples, peakGasPrice, peakBase };
        }

        it('gas price rises with the base fee and keeps tracking nextBaseFee * 1.1', async function () {
            if (!seiParams) this.skip();
            const { samples, peakGasPrice, peakBase } = await burstAndSample();
            // Skip when the environment cannot raise the base fee (e.g. the shared pool
            // accounts have been drained by earlier runs) rather than fail spuriously.
            if (samples.length === 0 || peakBase <= floorBase) this.skip();

            expect(
                peakGasPrice > floorGasPrice,
                `gas price should rise above the idle ${floorGasPrice}`,
            ).to.equal(true);

            // Every reading must track the live base fee through Sei's formula.
            for (const s of samples) {
                await assertSeiGasPriceTracks(sei, s.gasPrice, s.block);
            }
        });

        it('gas price decays back to the floor buffer once the load stops', async function () {
            if (!seiParams) this.skip();
            const settled = await waitUntil(
                async () => ((await gasPrice(sei)) === floorGasPrice ? true : null),
                { timeoutMs: 60_000, intervalMs: 500, label: 'gas price decays to floor' },
            );
            expect(settled).to.equal(true);
        });
    });

    describe('wrong params / error handling', () => {
        it('an extra positional argument fails identically (-32602, want at most 0)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_gasPrice', ['latest']),
                rawGeth('eth_gasPrice', ['latest']),
            ]);
            expectJsonRpcError(s, -32602, /too many arguments, want at most 0/);
            expectSameError(s, g);
        });

        it('an object argument fails identically (-32602, want at most 0)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_gasPrice', [{}]),
                rawGeth('eth_gasPrice', [{}]),
            ]);
            expectJsonRpcError(s, -32602, /too many arguments, want at most 0/);
            expectSameError(s, g);
        });
    });
});
