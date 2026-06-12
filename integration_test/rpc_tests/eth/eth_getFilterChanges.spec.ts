import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, claimPool } from '../utils/testUtils';
import { EvmAccount } from '../utils/evmUtils';
import {
    deployLogToken,
    drainFilterChanges,
    expectLogShape,
    TRANSFER_TOPIC,
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
            // Deploy first, then install the filter, so it observes events produced
            // strictly after creation. fromBlock is pinned to the current head: a Sei
            // log filter that lets fromBlock default to "latest" freezes its range to
            // the creation block and never reports later logs, so an explicit numeric
            // fromBlock (with toBlock defaulting to the advancing latest) is required to
            // track new events.
            const { address, token } = await deployLogToken(emitter);
            const fromBlock = ethers.toQuantity(await sei.getBlockNumber());
            const id = await sei.send('eth_newFilter', [
                { address, topics: [TRANSFER_TOPIC], fromBlock },
            ]);

            // Nothing has happened on this token since the filter was installed.
            expect(await sei.send('eth_getFilterChanges', [id]), 'no events yet').to.deep.equal([]);

            // Drain across polls: the freshly emitted log may land a poll after the
            // receipt on Sei's continuously-producing chain, but it is delivered once.
            await (await token.mint(emitter.address, ethers.parseEther('100'))).wait();
            const afterMint = await drainFilterChanges(sei, id, 1);
            expect(afterMint.length, 'the mint Transfer').to.equal(1);
            expectLogShape(afterMint[0], 'afterMint[0]');
            expect(afterMint[0].topics[0]).to.equal(TRANSFER_TOPIC);

            await (await token.transfer(ethers.Wallet.createRandom().address, 1n)).wait();
            const afterTransfer = await drainFilterChanges(sei, id, 1);
            expect(afterTransfer.length, 'only the new Transfer').to.equal(1);

            // No further events ⇒ an empty delta.
            expect(await sei.send('eth_getFilterChanges', [id]), 'drained').to.deep.equal([]);

            await sei.send('eth_uninstallFilter', [id]);
        });

        it('[spec] a filter with default (omitted) fromBlock still tracks new logs', async () => {
            // execution-apis: omitting fromBlock/toBlock defaults them to "latest", and an
            // installed filter must report logs produced AFTER creation as the chain
            // advances. On Sei a default-"latest" filter freezes its range to the creation
            // block and never reports later logs — assert the standard so the bug surfaces.
            const { address, token } = await deployLogToken(emitter);
            const id = await sei.send('eth_newFilter', [{ address, topics: [TRANSFER_TOPIC] }]);

            // Nothing yet.
            expect(await sei.send('eth_getFilterChanges', [id]), 'no events yet').to.deep.equal([]);

            await (await token.mint(emitter.address, ethers.parseEther('100'))).wait();
            const delivered = await drainFilterChanges(sei, id, 1);
            expect(delivered.length, 'default-fromBlock filter must deliver the new log').to.equal(1);
            expect(delivered[0].topics[0]).to.equal(TRANSFER_TOPIC);

            await sei.send('eth_uninstallFilter', [id]);
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
