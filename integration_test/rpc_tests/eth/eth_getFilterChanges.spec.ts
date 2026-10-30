import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, expectJsonRpcError, JsonRpcEnvelope, waitUntil } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, claimPool } from '../utils/testUtils';
import { EvmAccount } from '../utils/evmUtils';
import {
    deployLogBurst,
    deployLogToken,
    drainFilterChanges,
    emitBursts,
    expectBurstLogs,
    pollUntilEmpty,
    waitForLogsServed,
    assertFilterChangesMatchGetLogs,
    emitLogScene,
    LogScene,
    LOG_FILTER_MATRIX,
    expectLogShape,
    TRANSFER_TOPIC,
} from '../utils/logsUtils';
import { HASH32 } from '../utils/format';
import { FILTER_LIMITS } from '../utils/constants';

describe('eth_getFilterChanges', function () {
    this.timeout(180 * 1000);

    const { sei } = bothProviders();

    let runtime: RuntimeState;
    let emitter: EvmAccount;

    before(async () => {
        runtime = readRuntimeState();
        [emitter] = claimPool(runtime, sei, 1, 'eth_getFilterChanges');
    });

    describe('log filter (incremental delivery)', () => {
        it('delivers only the logs that arrived since the previous poll', async () => {
            const { address, token } = await deployLogToken(emitter);
            const fromBlock = ethers.toQuantity(await sei.getBlockNumber());
            const id = await sei.send('eth_newFilter', [
                { address, topics: [TRANSFER_TOPIC], fromBlock },
            ]);

            expect(await sei.send('eth_getFilterChanges', [id]), 'no events yet').to.deep.equal([]);

            await (await token.mint(emitter.address, ethers.parseEther('100'))).wait();
            const afterMint = await drainFilterChanges(sei, id, 1);
            expect(afterMint.length, 'the mint Transfer').to.equal(1);
            expectLogShape(afterMint[0], 'afterMint[0]');
            expect(afterMint[0].topics[0]).to.equal(TRANSFER_TOPIC);

            await (await token.transfer(ethers.Wallet.createRandom().address, 1n)).wait();
            const afterTransfer = await drainFilterChanges(sei, id, 1);
            expect(afterTransfer.length, 'only the new Transfer').to.equal(1);
            expect(await sei.send('eth_getFilterChanges', [id]), 'drained').to.deep.equal([]);

            const delivered = [...afterMint, ...afterTransfer];
            const viaGetLogs = await sei.send('eth_getLogs', [
                { address, topics: [TRANSFER_TOPIC], fromBlock, toBlock: 'latest' },
            ]);
            expect(viaGetLogs.length, 'eth_getLogs sees both Transfers').to.equal(delivered.length);
            expect(delivered, 'getFilterChanges deltas == eth_getLogs result').to.deep.equal(
                viaGetLogs,
            );

            await sei.send('eth_uninstallFilter', [id]);
        });

        it('respects the address filter and returns logs only for that address', async () => {
            // Two emitting tokens in the same range; a filter pinned to token A must ignore
            // token B's logs entirely. Events are mined first, then matched over their range.
            const a = await deployLogToken(emitter);
            const b = await deployLogToken(emitter);
            const sink = ethers.Wallet.createRandom().address;

            const firstRc = await (
                await a.token.mint(emitter.address, ethers.parseEther('1'))
            ).wait();
            await (await b.token.mint(emitter.address, ethers.parseEther('1'))).wait();
            await (await a.token.transfer(sink, 1n)).wait();
            const lastRc = await (await b.token.transfer(sink, 1n)).wait();

            const criteria = {
                fromBlock: ethers.toQuantity(firstRc.blockNumber),
                toBlock: ethers.toQuantity(lastRc.blockNumber),
                address: a.address,
            };
            const got = await assertFilterChangesMatchGetLogs(sei, criteria, 'address filter');
            expect(got.length, 'token A emitted logs').to.be.greaterThan(0);
            got.forEach((l: any) =>
                expect(l.address, 'only token A logs are returned').to.equal(
                    a.address.toLowerCase(),
                ),
            );
        });
    });

    describe('log filter (large backlogs)', function () {
        // A poll scans from its cursor to the head and, when the logs there exceed the caps, halves
        // the block window until they fit (evmrpc/filter.go getLogsByFiltersWithBackoff), so a
        // backlog over the cap arrives over several polls instead of failing.
        this.timeout(5 * 60 * 1000);
        let burster: EvmAccount;

        before(() => {
            [burster] = claimPool(runtime, sei, 1, 'eth_getFilterChanges:backlog');
        });

        it('a filter left unpolled while more than 1000 logs accumulate returns all of them in one poll, then nothing', async () => {
            const contract = await deployLogBurst(burster);
            const address = await contract.getAddress();
            const id = await sei.send('eth_newFilter', [{ address }]);

            const batches = await emitBursts(contract, [600, 600]);
            await waitForLogsServed(sei, batches[batches.length - 1].blockNumber);

            expectBurstLogs(await sei.send('eth_getFilterChanges', [id]), batches, address, 'the single poll');
            expect(await sei.send('eth_getFilterChanges', [id]), 'drained').to.deep.equal([]);
            await sei.send('eth_uninstallFilter', [id]);
        });

        it(`a backlog over max_log_no_block (${FILTER_LIMITS.maxLogs}) arrives across polls, each under the cap, together complete`, async () => {
            const contract = await deployLogBurst(burster);
            const address = await contract.getAddress();
            const id = await sei.send('eth_newFilter', [{ address }]);

            // Four blocks just under the cap together, plus one log: no block alone exceeds it.
            const batches = await emitBursts(contract, [...Array(4).fill(FILTER_LIMITS.maxLogs / 4), 1]);
            await waitForLogsServed(sei, batches[batches.length - 1].blockNumber);

            const deltas = await pollUntilEmpty(sei, id);
            expect(deltas.length, 'more logs than one poll may return take more than one poll').to.be.greaterThan(1);
            deltas.forEach((d, i) => expect(d.length, `poll ${i} stays within the cap`).to.be.at.most(FILTER_LIMITS.maxLogs));
            expectBurstLogs(deltas.flat(), batches, address, 'the polls together');
            await sei.send('eth_uninstallFilter', [id]);
        });

        it(`a single block holding more than max_log_no_block logs fails the poll that reaches it, and the filter moves past it`, async () => {
            // pollLogs: when the poll window has shrunk to one block whose logs alone exceed the
            // caps, the poll fails with an oversizedBlockError and the cursor moves past that
            // block (filter.go:1177-1181), so later polls deliver later logs instead of failing
            // forever. The halving can first reach the empty blocks before it, which poll as [].
            const contract = await deployLogBurst(burster);
            const address = await contract.getAddress();
            const id = await sei.send('eth_newFilter', [{ address }]);

            // ~14.5M gas: one block may hold it (max_gas 35M), but it is over the 10M eth_estimateGas
            // may simulate, so the limit is set by hand.
            const [oversized] = await emitBursts(contract, [FILTER_LIMITS.maxLogs + 1], 0, { gasLimit: 20_000_000n });
            await waitForLogsServed(sei, oversized.blockNumber);
            const capError = `query matches too many logs: result exceeds the maximum of ${FILTER_LIMITS.maxLogs} logs`;

            let failed: JsonRpcEnvelope | undefined;
            for (let poll = 0; poll < 20 && !failed; poll++) {
                const res = await rawSei<any[]>('eth_getFilterChanges', [id]);
                if (res.error) failed = res;
                else expect(res.result, `poll ${poll} before the oversized block`).to.deep.equal([]);
            }
            expect(failed, 'a poll reaches the oversized block').to.not.equal(undefined);
            expectJsonRpcError(failed!, -32000, new RegExp(`logs of block ${oversized.blockNumber} skipped: ${capError}`));

            const [later] = await emitBursts(contract, [3], 1);
            await waitForLogsServed(sei, later.blockNumber);
            expectBurstLogs(await sei.send('eth_getFilterChanges', [id]), [later], address, 'the poll after the oversized block');
            expect(await sei.send('eth_getFilterChanges', [id]), 'drained, without the error again').to.deep.equal([]);

            // A history read over the block fails as a whole, as eth_getLogs does.
            const at = ethers.toQuantity(oversized.blockNumber);
            const history = await sei.send('eth_newFilter', [{ address, fromBlock: at, toBlock: at }]);
            expectJsonRpcError(await rawSei('eth_getFilterLogs', [history]), -32000, new RegExp(capError));
            expectJsonRpcError(await rawSei('eth_getLogs', [{ address, fromBlock: at, toBlock: at }]), -32000, new RegExp(capError));
            await Promise.all([id, history].map(f => sei.send('eth_uninstallFilter', [f])));
        });
    });

    describe('topic & address filtering, parity with eth_getLogs', () => {
        // Parameterised over the shared LOG_FILTER_MATRIX (logsUtils): the same address/topic cases
        // every filter/subscription spec runs. Each case is pinned to eth_getLogs — install a filter,
        // drain its deltas, and require them byte-identical to eth_getLogs(criteria).
        let scene: LogScene;

        before(async function () {
            this.timeout(120 * 1000);
            scene = await emitLogScene(
                emitter,
                ethers.Wallet.createRandom().address,
                ethers.Wallet.createRandom().address,
            );
        });

        for (const c of LOG_FILTER_MATRIX) {
            it(c.title, async () => {
                const { criteria, expectedCount, check } = c.build(scene);
                const got = await assertFilterChangesMatchGetLogs(sei, criteria, c.title);
                expect(got.length, `${c.title}: match count`).to.equal(expectedCount);
                check?.(got);
            });
        }
    });

    describe('range and optional parameters', () => {
        let scene: LogScene;

        before(async function () {
            this.timeout(120 * 1000);
            scene = await emitLogScene(
                emitter,
                ethers.Wallet.createRandom().address,
                ethers.Wallet.createRandom().address,
            );
        });

        it('a range before the contract existed returns nothing', async () => {
            const got = await assertFilterChangesMatchGetLogs(
                sei,
                {
                    fromBlock: ethers.toQuantity(scene.deployBlock - 5),
                    toBlock: ethers.toQuantity(scene.deployBlock - 1),
                    address: scene.erc20,
                },
                'pre-deploy range',
            );
            expect(got.length, 'no logs before the contract was deployed').to.equal(0);
        });

        it('a bounded multi-block window covering the events returns them all', async () => {
            // A window wider than the events but bounded entirely by already-mined blocks
            // (deploy .. last event): never overshoots the chain head, still covers all 4.
            const got = await assertFilterChangesMatchGetLogs(
                sei,
                {
                    fromBlock: ethers.toQuantity(scene.deployBlock),
                    toBlock: ethers.toQuantity(scene.lastEventBlock),
                    address: scene.erc20,
                },
                'multi-block window',
            );
            expect(got.length, 'the whole scene fits in the window').to.equal(scene.totalCount);
        });
    });

    describe('block filter', () => {
        it('delivers the hashes of blocks mined since the previous poll', async () => {
            const id = await sei.send('eth_newBlockFilter', []);

            const receipt = await (
                await emitter.wallet.sendTransaction({ to: emitter.address, value: 0 })
            ).wait();

            // eth_getFilterChanges returns only the hashes new since the previous poll, and the
            // filter cursor can trail the committed block on slow CI. Drain across polls — Sei mines
            // continuously, so our tx's block may arrive after some earlier ones — until it appears.
            const hashes: string[] = [];
            await waitUntil(
                async () => {
                    hashes.push(...(await sei.send('eth_getFilterChanges', [id])));
                    return hashes.includes(receipt!.blockHash) ? hashes : null;
                },
                { timeoutMs: 15_000, intervalMs: 300, label: 'block filter delivers our tx block hash' },
            );

            expect(hashes.length, 'at least one new block hash').to.be.greaterThan(0);
            hashes.forEach((h: string) => expect(h).to.match(HASH32));
            expect(hashes, 'includes the block our tx landed in').to.include(receipt!.blockHash);

            await sei.send('eth_uninstallFilter', [id]);
        });

    });

    describe('wrong params / error handling', () => {
        it('an unknown filter id fails with -32000 (filter does not exist)', async () => {
            const res = await rawSei('eth_getFilterChanges', ['0xdeadbeefdeadbeefdeadbeefdeadbeef']);
            expectJsonRpcError(res, -32000, /filter does not exist/);
        });

        it('an uninstalled filter id no longer resolves', async () => {
            const id = await sei.send('eth_newBlockFilter', []);
            await sei.send('eth_uninstallFilter', [id]);
            const res = await rawSei('eth_getFilterChanges', [id]);
            expectJsonRpcError(res, -32000, /filter does not exist/);
        });
    });
});
