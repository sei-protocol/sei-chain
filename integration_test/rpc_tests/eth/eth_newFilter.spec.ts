import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, claimPool, expectSameError } from '../utils/testUtils';
import {
    emitLogScene,
    LogScene,
    FILTER_ID,
    TRANSFER_TOPIC,
    LOG_FILTER_MATRIX,
} from '../utils/logsUtils';

describe('eth_newFilter', function () {
    this.timeout(180 * 1000);

    const { sei, geth } = bothProviders();

    let runtime: RuntimeState;
    let scene: LogScene;

    before(async () => {
        runtime = readRuntimeState();
        const [emitter] = claimPool(runtime, sei, 1, 'eth_newFilter');
        const alice = ethers.Wallet.createRandom().address;
        const bob = ethers.Wallet.createRandom().address;
        scene = await emitLogScene(emitter, alice, bob);
    });

    const sceneFilter = (extra: object = {}) => ({
        fromBlock: ethers.toQuantity(scene.firstEventBlock),
        toBlock: ethers.toQuantity(scene.lastEventBlock),
        address: scene.erc20,
        ...extra,
    });

    describe('happy path / lifecycle', () => {
        it('creates a log filter and returns a well-formed handle', async () => {
            const id = await sei.send('eth_newFilter', [sceneFilter()]);
            expect(id, 'filter id is opaque hex').to.match(FILTER_ID);
            await sei.send('eth_uninstallFilter', [id]);
        });

        it('hands out distinct ids for separate filters', async () => {
            const [a, b] = await Promise.all([
                sei.send('eth_newFilter', [sceneFilter()]),
                sei.send('eth_newFilter', [sceneFilter()]),
            ]);
            expect(a).to.not.equal(b);
            await Promise.all([
                sei.send('eth_uninstallFilter', [a]),
                sei.send('eth_uninstallFilter', [b]),
            ]);
        });

        it('captures exactly the matching historical logs via eth_getFilterLogs', async () => {
            const id = await sei.send('eth_newFilter', [sceneFilter({ topics: [TRANSFER_TOPIC] })]);
            const logs = await sei.send('eth_getFilterLogs', [id]);
            expect(logs.length, 'the 3 Transfers').to.equal(scene.transferCount);
            logs.forEach((l: any) => {
                expect(l.address).to.equal(scene.erc20.toLowerCase());
                expect(l.topics[0]).to.equal(TRANSFER_TOPIC);
            });
            await sei.send('eth_uninstallFilter', [id]);
        });

        it('uninstalls a filter once: true, then false, then it no longer exists', async () => {
            const id = await sei.send('eth_newFilter', [sceneFilter()]);
            expect(await sei.send('eth_uninstallFilter', [id]), 'first uninstall').to.equal(true);
            expect(await sei.send('eth_uninstallFilter', [id]), 'second uninstall').to.equal(false);

            const after = await rawSei('eth_getFilterLogs', [id]);
            expectJsonRpcError(after, -32000, /filter does not exist/);
        });
    });

    describe('rich criteria (multi-address / multi-topic), parity with eth_getLogs', () => {
        // Parameterised over the shared LOG_FILTER_MATRIX (logsUtils) so a filter-semantics change is
        // updated in one place across every filter/subscription spec. Each case is pinned to
        // eth_getLogs: install the filter, drain its historical matches via eth_getFilterLogs, and
        // require them byte-identical to eth_getLogs(criteria) — proving the filter applies the same
        // address/topic matching, not merely that it is "well-formed".
        const assertFilterLogsMatchGetLogs = async (criteria: object, ctx: string): Promise<any[]> => {
            const oracle = await sei.send('eth_getLogs', [criteria]);
            const id = await sei.send('eth_newFilter', [criteria]);
            expect(id, `${ctx}: filter id is opaque hex`).to.match(FILTER_ID);
            try {
                const logs = await sei.send('eth_getFilterLogs', [id]);
                expect(logs, `${ctx}: eth_getFilterLogs == eth_getLogs`).to.deep.equal(oracle);
                return logs;
            } finally {
                await sei.send('eth_uninstallFilter', [id]);
            }
        };

        for (const c of LOG_FILTER_MATRIX) {
            it(c.title, async () => {
                const { criteria, expectedCount, check } = c.build(scene);
                const logs = await assertFilterLogsMatchGetLogs(criteria, c.title);
                expect(logs.length, `${c.title}: match count`).to.equal(expectedCount);
                check?.(logs);
            });
        }
    });

    describe('eth_newBlockFilter', () => {
        it('returns a well-formed handle distinct from a log filter', async () => {
            const [logId, blockId] = await Promise.all([
                sei.send('eth_newFilter', [sceneFilter()]),
                sei.send('eth_newBlockFilter', []),
            ]);
            expect(blockId, 'block filter id is opaque hex').to.match(FILTER_ID);
            expect(blockId).to.not.equal(logId);
            await Promise.all([
                sei.send('eth_uninstallFilter', [logId]),
                sei.send('eth_uninstallFilter', [blockId]),
            ]);
        });
    });

    describe('wrong params / error handling (parity with geth)', () => {

        it('a malformed topic (wrong byte length) is rejected identically (-32602)', async () => {
            const filter = { topics: ['0xabcd'] };
            const [s, g] = await Promise.all([
                rawSei('eth_newFilter', [filter]),
                rawGeth('eth_newFilter', [filter]),
            ]);
            expectJsonRpcError(s, -32602, /invalid length 2 after decoding; expected 32 for topic/);
            expectSameError(s, g);
        });
    });
});
