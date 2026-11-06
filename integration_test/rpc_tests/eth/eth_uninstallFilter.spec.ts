import { expect } from 'chai';
import { bothProviders, rawSei, expectJsonRpcError, sleep } from '../utils/chainUtils';
import { FILTER_ID } from '../utils/logsUtils';
import { FILTER_LIFETIME, filterLifetimeProbe } from '../utils/filterLifetime';

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

    describe('removal after filter_timeout', function () {
        // The probe filters are installed when the run starts (utils/filterLifetime.ts), and this
        // spec runs last, so the sweep has usually happened by now and the wait below is short.
        this.timeout(FILTER_LIFETIME.sweptByMs + 60_000);

        it('a filter left unread past filter_timeout is removed, while one read within it is kept', async () => {
            const probe = await filterLifetimeProbe();
            await sleep(Math.max(0, probe.startedAt + FILTER_LIFETIME.sweptByMs - Date.now()));

            expect(probe.failedReads, 'every periodic read of the kept filter answered').to.deep.equal([]);
            expect(await sei.send('eth_getFilterLogs', [probe.kept]), 'the read filter still answers').to.deep.equal(
                await sei.send('eth_getLogs', [probe.criteria]),
            );
            expectJsonRpcError(await rawSei('eth_getFilterLogs', [probe.idle]), -32000, /filter does not exist/);
            await sei.send('eth_uninstallFilter', [probe.kept]);
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
