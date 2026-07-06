import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, claimPool } from '../utils/testUtils';
import {
    emitLogScene,
    LogScene,
    expectLogShape,
    TRANSFER_TOPIC,
    APPROVAL_TOPIC,
} from '../utils/logsUtils';

describe('eth_getFilterLogs', function () {
    this.timeout(180 * 1000);

    const { sei } = bothProviders();

    let runtime: RuntimeState;
    let scene: LogScene;

    before(async () => {
        runtime = readRuntimeState();
        const [emitter] = claimPool(runtime, sei, 1, 'eth_getFilterLogs');
        const alice = ethers.Wallet.createRandom().address;
        const bob = ethers.Wallet.createRandom().address;
        scene = await emitLogScene(emitter, alice, bob);
    });

    const newSceneFilter = (extra: object = {}) =>
        sei.send('eth_newFilter', [
            {
                fromBlock: ethers.toQuantity(scene.firstEventBlock),
                toBlock: ethers.toQuantity(scene.lastEventBlock),
                address: scene.erc20,
                ...extra,
            },
        ]);

    describe('happy path', () => {
        it('returns every historical log matching the filter', async () => {
            const id = await newSceneFilter();
            const logs = await sei.send('eth_getFilterLogs', [id]);
            expect(logs.length, 'mint + 2 transfers + approval').to.equal(scene.totalCount);
            logs.forEach((l: any, i: number) => {
                expectLogShape(l, `logs[${i}]`);
                expect(l.address).to.equal(scene.erc20.toLowerCase());
            });
            await sei.send('eth_uninstallFilter', [id]);
        });

        it('is idempotent: the full set is returned on every call (unlike getFilterChanges)', async () => {
            const id = await newSceneFilter();
            const first = await sei.send('eth_getFilterLogs', [id]);
            const second = await sei.send('eth_getFilterLogs', [id]);
            expect(first.length).to.equal(scene.totalCount);
            expect(second.length, 'a repeated call returns the same full set').to.equal(
                first.length,
            );
            expect(second.map((l: any) => l.transactionHash)).to.deep.equal(
                first.map((l: any) => l.transactionHash),
            );
            await sei.send('eth_uninstallFilter', [id]);
        });

        it('returns exactly what eth_getLogs returns for the same criteria', async () => {
            const base = {
                fromBlock: ethers.toQuantity(scene.firstEventBlock),
                toBlock: ethers.toQuantity(scene.lastEventBlock),
                address: scene.erc20,
            };
            for (const criteria of [base, { ...base, topics: [TRANSFER_TOPIC] }]) {
                const id = await sei.send('eth_newFilter', [criteria]);
                const [viaFilter, viaGetLogs] = await Promise.all([
                    sei.send('eth_getFilterLogs', [id]),
                    sei.send('eth_getLogs', [criteria]),
                ]);
                expect(viaFilter.length, 'same number of logs as eth_getLogs').to.equal(
                    viaGetLogs.length, 
                );
                expect(viaFilter, 'getFilterLogs == getLogs for identical criteria').to.deep.equal(
                    viaGetLogs,
                );
                await sei.send('eth_uninstallFilter', [id]);
            }
        });

        it('respects the filter topics (Transfers only)', async () => {
            const id = await newSceneFilter({ topics: [TRANSFER_TOPIC] });
            const logs = await sei.send('eth_getFilterLogs', [id]);
            expect(logs.length).to.equal(scene.transferCount);
            logs.forEach((l: any) => expect(l.topics[0]).to.equal(TRANSFER_TOPIC));
            await sei.send('eth_uninstallFilter', [id]);
        });

        it('respects the filter topics (the Approval only)', async () => {
            const id = await newSceneFilter({ topics: [APPROVAL_TOPIC] });
            const logs = await sei.send('eth_getFilterLogs', [id]);
            expect(logs.length).to.equal(scene.approvalCount);
            expect(logs[0].topics[0]).to.equal(APPROVAL_TOPIC);
            await sei.send('eth_uninstallFilter', [id]);
        });
    });

    describe('wrong params / error handling', () => {
        it('an unknown filter id fails with -32000 (filter does not exist)', async () => {
            const res = await rawSei('eth_getFilterLogs', ['0xdeadbeefdeadbeefdeadbeefdeadbeef']);
            expectJsonRpcError(res, -32000, /filter does not exist/);
        });

        it('an uninstalled filter id no longer resolves', async () => {
            const id = await newSceneFilter();
            await sei.send('eth_uninstallFilter', [id]);
            const res = await rawSei('eth_getFilterLogs', [id]);
            expectJsonRpcError(res, -32000, /filter does not exist/);
        });
    });
});
