import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, expectJsonRpcError, sleep } from '../utils/chainUtils';
import { FILTER_LIMITS } from '../utils/constants';
import { readRuntimeState, RuntimeState, claimPool } from '../utils/testUtils';
import {
    BURST_TOPIC,
    BurstScene,
    burstBatchTopic,
    burstsInRange,
    emitBurstScene,
    emitLogScene,
    expectBurstLogs,
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

    describe('large results and server limits', function () {
        // Bursts 0-3 carry 2500 logs each and burst 4 one more, each in a later block, so a block
        // range ending at burst 3 holds exactly FILTER_LIMITS.maxLogs logs and one ending at
        // burst 4 a single log over it.
        this.timeout(5 * 60 * 1000);
        let bursts: BurstScene;
        const rangeFilter = (from: number, to: number, extra: object = {}) =>
            sei.send('eth_newFilter', [
                { fromBlock: ethers.toQuantity(from), toBlock: ethers.toQuantity(to), address: bursts.address, ...extra },
            ]);

        before('emit 10,001 logs over five blocks', async () => {
            const [burster] = claimPool(runtime, sei, 1, 'eth_getFilterLogs:limits');
            bursts = await emitBurstScene(burster, [...Array(4).fill(FILTER_LIMITS.maxLogs / 4), 1]);
            expect(bursts.total).to.equal(FILTER_LIMITS.maxLogs + 1);
        });

        it('returns more than 1000 logs in one call, complete, in order, and equal to eth_getLogs', async () => {
            const [b0, b1] = bursts.batches;
            const id = await rangeFilter(b0.blockNumber, b1.blockNumber);
            const logs = await sei.send('eth_getFilterLogs', [id]);
            expectBurstLogs(logs, [b0, b1], bursts.address, 'getFilterLogs');
            expect(logs, 'getFilterLogs == getLogs for the same range').to.deep.equal(await sei.send('eth_getLogs', [{
                fromBlock: ethers.toQuantity(b0.blockNumber), toBlock: ethers.toQuantity(b1.blockNumber), address: bursts.address,
            }]));
            await sei.send('eth_uninstallFilter', [id]);
        });

        it(`returns exactly max_log_no_block (${FILTER_LIMITS.maxLogs}) logs`, async () => {
            const lastAtCap = bursts.batches[3];
            const id = await rangeFilter(bursts.firstBlock, lastAtCap.blockNumber);
            const logs = await sei.send('eth_getFilterLogs', [id]);
            expectBurstLogs(logs, burstsInRange(bursts, bursts.firstBlock, lastAtCap.blockNumber), bursts.address, 'at the cap');
            expect(logs.length).to.equal(FILTER_LIMITS.maxLogs);
            await sei.send('eth_uninstallFilter', [id]);
        });

        it('one log over max_log_no_block fails with the log-cap error, on every call and as eth_getLogs does, instead of truncating', async () => {
            const id = await rangeFilter(bursts.firstBlock, bursts.lastBlock);
            const capError = new RegExp(`query matches too many logs: result exceeds the maximum of ${FILTER_LIMITS.maxLogs} logs`);
            expectJsonRpcError(await rawSei('eth_getFilterLogs', [id]), -32000, capError);
            expectJsonRpcError(await rawSei('eth_getFilterLogs', [id]), -32000, capError);
            expectJsonRpcError(await rawSei('eth_getLogs', [{
                fromBlock: ethers.toQuantity(bursts.firstBlock), toBlock: ethers.toQuantity(bursts.lastBlock), address: bursts.address,
            }]), -32000, capError);
            await sei.send('eth_uninstallFilter', [id]);
        });

        it('the same over-cap range answers once a topic narrows the match under the cap', async () => {
            for (const b of [bursts.batches[1], bursts.batches[4]]) {
                const id = await rangeFilter(bursts.firstBlock, bursts.lastBlock, { topics: [BURST_TOPIC, burstBatchTopic(b.batch)] });
                expectBurstLogs(await sei.send('eth_getFilterLogs', [id]), [b], bursts.address, `batch ${b.batch}`);
                await sei.send('eth_uninstallFilter', [id]);
            }
        });

        it(`accepts a ${FILTER_LIMITS.maxBlocks}-block range and refuses ${FILTER_LIMITS.maxBlocks + 1} blocks`, async function () {
            const head = await sei.getBlockNumber();
            if (head < FILTER_LIMITS.maxBlocks) {
                console.log(`chain height ${head} is below ${FILTER_LIMITS.maxBlocks} blocks; skipping`);
                this.skip();
            }
            // An address that never emitted keeps the log count at zero, so only the range can fail.
            const quiet = { address: ethers.Wallet.createRandom().address, toBlock: ethers.toQuantity(head) };
            const atCap = await sei.send('eth_newFilter', [{ ...quiet, fromBlock: ethers.toQuantity(head - FILTER_LIMITS.maxBlocks + 1) }]);
            expect(await sei.send('eth_getFilterLogs', [atCap])).to.deep.equal([]);
            // eth_newFilter only checks that fromBlock is retained; the range is checked on read.
            const overCap = await sei.send('eth_newFilter', [{ ...quiet, fromBlock: ethers.toQuantity(head - FILTER_LIMITS.maxBlocks) }]);
            expectJsonRpcError(await rawSei('eth_getFilterLogs', [overCap]), -32000,
                new RegExp(`block range too large \\(${FILTER_LIMITS.maxBlocks + 1}\\), maximum allowed is ${FILTER_LIMITS.maxBlocks} blocks`));
            await Promise.all([atCap, overCap].map(id => sei.send('eth_uninstallFilter', [id])));
        });
    });

    describe('filter lifetime', function () {
        // cleanupLoop sweeps every timeout / 2 and removes a filter idle for longer than timeout, so
        // an unread filter is gone within 1.5 x timeout, and one read more often than that is kept.
        this.timeout(FILTER_LIMITS.timeoutMs * 2 + 60_000);
        const SWEPT_BY = FILTER_LIMITS.timeoutMs * 1.5 + 10_000;
        const READ_EVERY = FILTER_LIMITS.timeoutMs / 3;

        it('a filter left unread past filter_timeout is removed, while one read within it is kept', async () => {
            const idle = await newSceneFilter();
            const kept = await newSceneFilter();
            const start = Date.now();
            while (Date.now() - start < SWEPT_BY) {
                await sleep(Math.min(READ_EVERY, SWEPT_BY - (Date.now() - start)));
                expect(await sei.send('eth_getFilterLogs', [kept]), 'the read filter still answers').to.have.lengthOf(scene.totalCount);
            }
            expectJsonRpcError(await rawSei('eth_getFilterLogs', [idle]), -32000, /filter does not exist/);
            await sei.send('eth_uninstallFilter', [kept]);
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
