import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, claimPool, expectSameError } from '../utils/testUtils';
import {
    emitLogScene,
    LogScene,
    FILTER_ID,
    TRANSFER_TOPIC,
    APPROVAL_TOPIC,
    addressTopic,
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
        // eth_newFilter must accept the exact same criteria object eth_getLogs does. We pin each
        // filter to its eth_getLogs result: install the filter, drain its historical matches via
        // eth_getFilterLogs, and require them byte-identical to eth_getLogs(criteria). This proves
        // the filter is constructed with the same address/topic matching, not merely "well-formed".
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

        it('unions logs across an address array (a non-emitting co-address adds nothing)', async () => {
            const logs = await assertFilterLogsMatchGetLogs(
                sceneFilter({ address: [scene.erc20, ethers.Wallet.createRandom().address] }),
                'multi-address',
            );
            expect(logs.length, 'array-of-addresses still yields the scene total').to.equal(
                scene.totalCount,
            );
            logs.forEach((l: any) => expect(l.address).to.equal(scene.erc20.toLowerCase()));
        });

        it('matches a topic0 OR-set (Transfer OR Approval)', async () => {
            const logs = await assertFilterLogsMatchGetLogs(
                sceneFilter({ topics: [[TRANSFER_TOPIC, APPROVAL_TOPIC]] }),
                'topic0 OR',
            );
            expect(logs.length, 'every event in the scene').to.equal(scene.totalCount);
        });

        it('matches an indexed positional topic (Transfers sent by the emitter)', async () => {
            const sender = addressTopic(scene.emitter.address);
            const logs = await assertFilterLogsMatchGetLogs(
                sceneFilter({ topics: [TRANSFER_TOPIC, sender] }),
                'topic0 + indexed sender',
            );
            expect(logs.length, 'emitter -> alice and emitter -> bob').to.equal(2);
            logs.forEach((l: any) => expect(l.topics[1]).to.equal(sender));
        });

        it('honours a wildcard slot with a matched recipient topic (only the alice transfer)', async () => {
            const logs = await assertFilterLogsMatchGetLogs(
                sceneFilter({ topics: [TRANSFER_TOPIC, null, addressTopic(scene.alice)] }),
                'wildcard + recipient',
            );
            expect(logs.length, 'only the emitter -> alice transfer').to.equal(1);
            expect(logs[0].topics[2]).to.equal(addressTopic(scene.alice));
        });

        it('matches a nested OR-set in an indexed slot (minted-from-zero OR sent-by-emitter)', async () => {
            const logs = await assertFilterLogsMatchGetLogs(
                sceneFilter({
                    topics: [
                        TRANSFER_TOPIC,
                        [addressTopic(ethers.ZeroAddress), addressTopic(scene.emitter.address)],
                    ],
                }),
                'topic0 + sender OR-set',
            );
            expect(logs.length, 'all three Transfers (mint + 2 sends)').to.equal(scene.transferCount);
        });

        it('combines an address array with a topic0 filter', async () => {
            const logs = await assertFilterLogsMatchGetLogs(
                sceneFilter({
                    address: [scene.erc20, ethers.Wallet.createRandom().address],
                    topics: [TRANSFER_TOPIC],
                }),
                'multi-address + topic0',
            );
            expect(logs.length, 'the three Transfers from the scene token').to.equal(
                scene.transferCount,
            );
            logs.forEach((l: any) => expect(l.topics[0]).to.equal(TRANSFER_TOPIC));
        });
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
