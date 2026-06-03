import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders } from '../utils/chainUtils';
import { rawSei, rawGeth, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, expectSameError } from '../utils/testUtils';
import { abiOf, deployContract } from '../utils/evmUtils';
import { EvmAccount } from '../utils/evmUtils';
import { HEX_QUANTITY } from '../utils/format';
import { Eip1559Params, queryEip1559Params } from '../utils/chainUtils';
import { waitUntil } from '../utils/chainUtils';

// eth_gasPrice parity against a local `geth --dev` reference. Sei and geth build the
// suggested gas price differently: geth returns baseFee + suggested tip, while Sei
// returns nextBaseFee * 1.1 when uncongested (or nextBaseFee + median reward when a
// block exceeds 80% of the gas limit). Both must track the base fee as it moves, so
// we drive the base fee up with a gas burner and assert the suggestion follows.
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

    const seiGasPrice = async (): Promise<bigint> => BigInt(await sei.send('eth_gasPrice', []));
    const gethGasPrice = async (): Promise<bigint> => BigInt(await geth.send('eth_gasPrice', []));

    async function blockInfo(provider: ethers.JsonRpcProvider, n: number | 'latest') {
        const tag = typeof n === 'number' ? ethers.toQuantity(n) : n;
        const b = await provider.send('eth_getBlockByNumber', [tag, false]);
        return {
            number: Number(b.number),
            gasUsed: BigInt(b.gasUsed),
            gasLimit: BigInt(b.gasLimit),
            baseFee: BigInt(b.baseFeePerGas ?? '0x0'),
        };
    }

    /**
     * Read eth_gasPrice with the latest height pinned: only accept a reading where no
     * new block landed across the call, so the returned price provably derives from
     * GetNextBaseFeePerGas(B). Returns the price and that block height B.
     */
    async function gasPriceAtStableBlock(): Promise<{ gasPrice: bigint; block: number }> {
        for (let i = 0; i < 20; i++) {
            const b1 = await sei.getBlockNumber();
            const gasPrice = await seiGasPrice();
            const b2 = await sei.getBlockNumber();
            if (b1 === b2) return { gasPrice, block: b1 };
        }
        throw new Error('gasPriceAtStableBlock: block kept advancing across the gas price call');
    }

    /**
     * Assert a Sei gas price reading tracks the base fee: uncongested it is exactly
     * 1.1x the base fee of some block in the immediate neighbourhood of B (which block
     * the node sampled for GetNextBaseFeePerGas can drift by one under active load);
     * congested it at least covers the base fee.
     */
    async function assertSeiGasPriceTracks(gasPrice: bigint, block: number): Promise<void> {
        await waitUntil(async () => ((await sei.getBlockNumber()) > block ? true : null), {
            timeoutMs: 15_000,
            label: 'block after sample',
        });
        const head = await sei.getBlockNumber();
        const heights = [block - 1, block, block + 1, block + 2].filter(h => h >= 0 && h <= head);
        const infos = await Promise.all(heights.map(h => blockInfo(sei, h)));

        if (infos.some(b => (b.baseFee * 110n) / 100n === gasPrice)) return;

        const congested = infos.some(b => b.gasUsed > (b.gasLimit * 80n) / 100n);
        const minBase = infos.reduce((m, b) => (b.baseFee < m ? b.baseFee : m), infos[0].baseFee);
        expect(
            congested && gasPrice >= minBase,
            `gasPrice ${gasPrice} is not 1.1x any base fee near block ${block} ` +
                `(bases: ${infos.map(b => b.baseFee).join(', ')})`,
        ).to.equal(true);
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
        spammers = claimPool(5, 'eth_gasPrice');
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
                seiGasPrice(),
                blockInfo(sei, 'latest'),
                gethGasPrice(),
                blockInfo(geth, 'latest'),
            ]);
            expect(sGas >= sBlk.baseFee, `sei gasPrice ${sGas} < base ${sBlk.baseFee}`).to.equal(true);
            expect(gGas >= gBlk.baseFee, `geth gasPrice ${gGas} < base ${gBlk.baseFee}`).to.equal(true);
        });
    });

    describe('relationship to base fee and priority fee', () => {
        it('[Sei] an uncongested gas price is exactly the base fee plus the 10% buffer', async () => {
            const { gasPrice, block } = await gasPriceAtStableBlock();
            await assertSeiGasPriceTracks(gasPrice, block);
        });

        it('[geth] the gas price equals the base fee plus the suggested priority fee (exact)', async () => {
            const [gasPrice, tip, blk] = await Promise.all([
                gethGasPrice(),
                BigInt(await geth.send('eth_maxPriorityFeePerGas', [])),
                blockInfo(geth, 'latest'),
            ]);
            expect(gasPrice, 'geth gasPrice = baseFee + tip').to.equal(blk.baseFee + tip);
        });

        it('[Sei] maxPriorityFeePerGas defaults to 1 gwei while the chain is uncongested', async () => {
            // Quiescent runs sit well under the 80% congestion threshold.
            const tip = BigInt(await sei.send('eth_maxPriorityFeePerGas', []));
            expect(tip).to.equal(1_000_000_000n);
        });

        it('[divergence] Sei multiplies the base fee by 1.1; geth adds a flat tip', async () => {
            const [sGas, sBlk, gGas, gTip, gBlk] = await Promise.all([
                seiGasPrice(),
                blockInfo(sei, 'latest'),
                gethGasPrice(),
                BigInt(await geth.send('eth_maxPriorityFeePerGas', [])),
                blockInfo(geth, 'latest'),
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
                const baseNow = (await blockInfo(sei, 'latest')).baseFee;
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

                const sample = await gasPriceAtStableBlock();
                samples.push(sample);
                const base = (await blockInfo(sei, sample.block)).baseFee;
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
                await assertSeiGasPriceTracks(s.gasPrice, s.block);
            }
        });

        it('gas price decays back to the floor buffer once the load stops', async function () {
            if (!seiParams) this.skip();
            const settled = await waitUntil(
                async () => ((await seiGasPrice()) === floorGasPrice ? true : null),
                { timeoutMs: 60_000, intervalMs: 500, label: 'gas price decays to floor' },
            );
            expect(settled).to.equal(true);
        });
    });

    describe('reflects base fee increases (geth)', () => {
        let gethBurner: ethers.Contract;
        let gethSigner: EvmAccount;
        let gethNonce: number;
        const TIP = ethers.parseUnits('1', 'gwei');

        before(async () => {
            gethSigner = EvmAccount.fromPrivateKey(runtime.funded.gethAdmin.privateKey, geth);
            const dep = await deployContract(gethSigner, 'GasBurner.sol', [], 'RealGasBurner');
            gethBurner = new ethers.Contract(dep.address, burnerIface, gethSigner.wallet);
            gethNonce = await gethSigner.nonce('latest');
        });

        // Each heavy burn is its own dev block. Burn ~60% of the parent gas limit
        // (over geth's 50% target so the base fee climbs) while capping the tx gas limit
        // at 80% — comfortably under the block limit, which geth nudges +/-1/1024 each
        // block, so the tx is always minable.
        async function heavyGethBlock(salt: number): Promise<number> {
            const parent = await blockInfo(geth, 'latest');
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

        it('a run of heavy blocks raises the base fee and the gas price rises with it, still base + tip', async () => {
            // A block's base fee is set by its parent, so the rise shows up across a run
            // of consecutive over-target blocks rather than within the first one.
            const blocks: number[] = [];
            for (let i = 0; i < 4; i++) blocks.push(await heavyGethBlock(i));
            const first = await blockInfo(geth, blocks[0]);
            const last = await blockInfo(geth, blocks[blocks.length - 1]);
            expect(last.baseFee > first.baseFee, 'base fee climbed across the burst').to.equal(true);

            const [gasPrice, tip, head] = await Promise.all([
                gethGasPrice(),
                BigInt(await geth.send('eth_maxPriorityFeePerGas', [])),
                blockInfo(geth, 'latest'),
            ]);
            expect(gasPrice, 'gas price stays baseFee + tip').to.equal(head.baseFee + tip);
            expect(gasPrice > first.baseFee + tip, 'gas price reflects the raised base fee').to.equal(
                true,
            );
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
