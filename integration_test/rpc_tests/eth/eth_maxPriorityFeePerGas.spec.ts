import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError, blockGasInfo } from '../utils/chainUtils';
import { HEX_QUANTITY } from '../utils/format';
import {
    maxPriorityFeePerGas,
    gasPrice,
    DEFAULT_PRIORITY_FEE_WEI,
    CONGESTION_THRESHOLD,
} from '../utils/gasPriceUtils';

describe('eth_maxPriorityFeePerGas', function () {
    this.timeout(60 * 1000);

    const { sei, geth } = bothProviders();

    describe('queries', () => {
        it('returns a canonical, non-negative hex quantity', async () => {
            const raw = await sei.send('eth_maxPriorityFeePerGas', []);
            expect(raw, 'canonical quantity').to.match(HEX_QUANTITY);
            expect(BigInt(raw) >= 0n, 'non-negative tip').to.equal(true);
        });

        it('stays within a sane bound (never more than the total gas price)', async () => {
            // The suggested tip is one component of the gas price, so on an idle chain (where the
            // gas price is base*1.1 and the tip is the flat default) it must not exceed eth_gasPrice.
            const [tip, price] = await Promise.all([maxPriorityFeePerGas(sei), gasPrice(sei)]);
            expect(tip <= price, `tip ${tip} must be <= gasPrice ${price}`).to.equal(true);
        });

        it('falls back to the 1-gwei default while the latest block is uncongested', async () => {
            // Sei returns the flat default tip unless the latest block burned > 80% of the gas
            // limit. Local devnets idle far below that, so the default must surface exactly.
            const head = await blockGasInfo(sei, 'latest');
            const ratio = Number(head.gasUsed) / Number(head.gasLimit);
            if (ratio >= CONGESTION_THRESHOLD) return; // congested sample; the default does not apply
            const tip = await maxPriorityFeePerGas(sei);
            expect(tip, 'uncongested tip == 1 gwei default').to.equal(DEFAULT_PRIORITY_FEE_WEI);
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
