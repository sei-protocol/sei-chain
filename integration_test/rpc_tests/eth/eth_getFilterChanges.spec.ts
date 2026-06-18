import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, claimPool } from '../utils/testUtils';
import { EvmAccount } from '../utils/evmUtils';
import {
    deployLogToken,
    drainFilterChanges,
    assertFilterChangesMatchGetLogs,
    emitLogScene,
    LogScene,
    addressTopic,
    expectLogShape,
    TRANSFER_TOPIC,
    APPROVAL_TOPIC,
} from '../utils/logsUtils';
import { HASH32 } from '../utils/format';

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

    describe('topic filtering (order-dependent matching)', () => {
        let scene: LogScene;
        let base: { fromBlock: string; toBlock: string; address: string };
        let emitterTopic: string;
        let aliceTopic: string;
        let bobTopic: string;

        before(async function () {
            this.timeout(120 * 1000);
            scene = await emitLogScene(
                emitter,
                ethers.Wallet.createRandom().address,
                ethers.Wallet.createRandom().address,
            );
            base = {
                fromBlock: ethers.toQuantity(scene.firstEventBlock),
                toBlock: ethers.toQuantity(scene.lastEventBlock),
                address: scene.erc20,
            };
            emitterTopic = addressTopic(scene.emitter.address);
            aliceTopic = addressTopic(scene.alice);
            bobTopic = addressTopic(scene.bob);
        });

        it('[] (empty array) matches anything', async () => {
            const got = await assertFilterChangesMatchGetLogs(sei, { ...base, topics: [] }, '[]');
            expect(got.length, 'all four scene events').to.equal(scene.totalCount);
        });

        it('omitting topics entirely matches anything', async () => {
            const got = await assertFilterChangesMatchGetLogs(sei, { ...base }, 'no topics');
            expect(got.length, 'all four scene events').to.equal(scene.totalCount);
        });

        it('[A] matches A in the first position (the event signature)', async () => {
            const got = await assertFilterChangesMatchGetLogs(
                sei,
                { ...base, topics: [TRANSFER_TOPIC] },
                '[A]',
            );
            expect(got.length, 'the three Transfers').to.equal(scene.transferCount);
            got.forEach((l: any) => expect(l.topics[0]).to.equal(TRANSFER_TOPIC));
        });

        it('[null, B] matches B in the second position regardless of the first', async () => {
            // from == emitter ⇒ the two transfers + the approval (the mint is from 0x0).
            const got = await assertFilterChangesMatchGetLogs(
                sei,
                { ...base, topics: [null, emitterTopic] },
                '[null, B]',
            );
            expect(got.length, 'logs whose pos1 is the emitter').to.equal(3);
            got.forEach((l: any) => expect(l.topics[1]).to.equal(emitterTopic));
        });

        it('[A, B] matches A in the first AND B in the second position', async () => {
            const got = await assertFilterChangesMatchGetLogs(
                sei,
                { ...base, topics: [TRANSFER_TOPIC, emitterTopic] },
                '[A, B]',
            );
            expect(got.length, 'Transfers from the emitter (excludes the mint)').to.equal(2);
            got.forEach((l: any) => {
                expect(l.topics[0]).to.equal(TRANSFER_TOPIC);
                expect(l.topics[1]).to.equal(emitterTopic);
            });
        });

        it('[[A, B]] matches (A OR B) in the first position', async () => {
            const got = await assertFilterChangesMatchGetLogs(
                sei,
                { ...base, topics: [[TRANSFER_TOPIC, APPROVAL_TOPIC]] },
                '[[A, B]]',
            );
            expect(got.length, 'Transfer OR Approval == every event').to.equal(scene.totalCount);
            got.forEach((l: any) =>
                expect([TRANSFER_TOPIC, APPROVAL_TOPIC]).to.include(l.topics[0]),
            );
        });

        it('[A, null, [X, Y]] matches A in pos0 AND (X OR Y) in pos2', async () => {
            // Transfers whose recipient is alice OR bob (excludes the mint, whose to == emitter).
            const got = await assertFilterChangesMatchGetLogs(
                sei,
                { ...base, topics: [TRANSFER_TOPIC, null, [aliceTopic, bobTopic]] },
                '[A, null, [X, Y]]',
            );
            expect(got.length, 'transfers to alice or bob').to.equal(2);
            got.forEach((l: any) => {
                expect(l.topics[0]).to.equal(TRANSFER_TOPIC);
                expect([aliceTopic, bobTopic]).to.include(l.topics[2]);
            });
        });
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

            const hashes = await sei.send('eth_getFilterChanges', [id]);
            expect(hashes, 'an array of block hashes').to.be.an('array');
            expect(hashes.length).to.be.greaterThan(0);
            hashes.forEach((h: string) => expect(h).to.match(HASH32));
            expect(hashes, 'includes the block our tx landed in').to.include(
                receipt!.blockHash,
            );

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
