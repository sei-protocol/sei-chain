import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError } from '../utils/chainUtils';
import { FILTER_ID } from '../utils/logsUtils';

describe('eth_newPendingTransactionFilter', function () {
    this.timeout(60 * 1000);

    const { sei, geth } = bothProviders();

    describe('geth reference (the Ethereum standard)', () => {
        it('returns a well-formed, opaque filter handle', async () => {
            const id = await geth.send('eth_newPendingTransactionFilter', []);
            expect(id, 'geth pending-tx filter id').to.match(FILTER_ID);
            await geth.send('eth_uninstallFilter', [id]);
        });
    });

    describe('[divergence] Sei does not support pending-transaction filters', () => {
        // Sei has no public mempool view, so it registers the method but answers -32000 with an
        // explicit unsupported message rather than handing back a filter that never fires.
        it('returns -32000 with an explicit unsupported message', async () => {
            const res = await rawSei('eth_newPendingTransactionFilter', []);
            expectJsonRpcError(
                res,
                -32000,
                /eth_newPendingTransactionFilter is not supported on Sei EVM RPC/,
            );
        });

        it('geth installs a filter while Sei errors for the identical call', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_newPendingTransactionFilter', []),
                rawGeth('eth_newPendingTransactionFilter', []),
            ]);
            expect(s.error, 'Sei errors').to.not.equal(undefined);
            expect(g.error, 'geth does not error').to.equal(undefined);
            expect(g.result, 'geth returns a handle').to.match(FILTER_ID);
            if (typeof g.result === 'string') await geth.send('eth_uninstallFilter', [g.result]);
        });
    });
});
