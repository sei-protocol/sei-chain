import { expect } from 'chai';
import { bothProviders, rawSei, expectJsonRpcError } from '../utils/chainUtils';
import { FILTER_ID } from '../utils/logsUtils';

const UNKNOWN_ID = '0xdeadbeefdeadbeefdeadbeefdeadbeef';

describe('eth_uninstallFilter', function () {
    this.timeout(120 * 1000);

    const { sei, geth } = bothProviders();

    describe('lifecycle', () => {
        it('uninstalls a log filter once (true), then reports it gone (false)', async () => {
            const id = await sei.send('eth_newFilter', [{}]);
            expect(id, 'log filter id').to.match(FILTER_ID);
            expect(await sei.send('eth_uninstallFilter', [id]), 'first uninstall').to.equal(true);
            expect(await sei.send('eth_uninstallFilter', [id]), 'second uninstall').to.equal(false);
        });

        it('uninstalls a block filter once (true), then reports it gone (false)', async () => {
            const id = await sei.send('eth_newBlockFilter', []);
            expect(await sei.send('eth_uninstallFilter', [id]), 'first uninstall').to.equal(true);
            expect(await sei.send('eth_uninstallFilter', [id]), 'second uninstall').to.equal(false);
        });

        it('an uninstalled filter can no longer be polled (-32000 filter does not exist)', async () => {
            const id = await sei.send('eth_newFilter', [{}]);
            await sei.send('eth_uninstallFilter', [id]);
            const res = await rawSei('eth_getFilterChanges', [id]);
            expectJsonRpcError(res, -32000, /filter does not exist/);
        });
    });

    describe('schema matching (parity with geth)', () => {
        it('returns false for an unknown id on both nodes (never an error)', async () => {
            const [s, g] = await Promise.all([
                sei.send('eth_uninstallFilter', [UNKNOWN_ID]),
                geth.send('eth_uninstallFilter', [UNKNOWN_ID]),
            ]);
            expect(s, 'sei: unknown id => false').to.equal(false);
            expect(g, 'geth: unknown id => false').to.equal(false);
        });
    });

    describe('wrong params / error handling', () => {
        it('a missing id argument is rejected with -32602', async () => {
            const res = await rawSei('eth_uninstallFilter', []);
            expectJsonRpcError(res, -32602, /missing value for required argument 0/);
        });
    });
});
