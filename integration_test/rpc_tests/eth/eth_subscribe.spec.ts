import { expect } from 'chai';
import { ethers } from 'ethers';
import { Endpoints } from '../config/endpoints';
import { seiRpc } from '../utils/chainUtils';
import { readRuntimeState, claimPool } from '../utils/testUtils';
import { EvmAccount } from '../utils/evmUtils';
import { deployLogToken, expectLogShape, TRANSFER_TOPIC } from '../utils/logsUtils';
import {
    WsClient,
    SUBSCRIPTION_ID,
    assertNewHeadSchema,
    assertNewHeadInapplicableZeros,
    assertNewHeadMatchesBlock,
} from '../utils/subscribeUtils';

/**
 * eth_subscribe is a push method that only works over a bidirectional transport, so these tests
 * drive Sei's WebSocket endpoint directly (no geth comparison). Sei supports two subscription
 * kinds — `newHeads` and `logs` — and every notification field is asserted strictly and
 * cross-checked against the canonical HTTP RPC (eth_getBlockByNumber / eth_getLogs).
 */
describe('eth_subscribe (WebSocket)', function () {
    this.timeout(120 * 1000);

    const sei = seiRpc();
    let ws: WsClient;
    let emitter: EvmAccount;

    before(async function () {
        ws = await WsClient.open(Endpoints.sei.evmWs);
        const runtime = readRuntimeState();
        [emitter] = claimPool(runtime, sei, 1, 'eth_subscribe');
    });

    after(() => {
        ws.close();
    });

    describe('newHeads', () => {
        it('returns an opaque hex subscription id', async () => {
            const subId = await ws.subscribe(['newHeads']);
            expect(subId, 'subscription id is opaque hex').to.match(SUBSCRIPTION_ID);
            expect(await ws.unsubscribe(subId), 'unsubscribe of a live sub returns true').to.equal(true);
        });

        it('pushes a fully-formed header that matches the canonical block', async () => {
            const subId = await ws.subscribe(['newHeads']);
            const heads = await ws.waitFor(subId, 2);

            for (const head of heads) {
                assertNewHeadSchema(head);
                assertNewHeadInapplicableZeros(head);
                const block = await sei.send('eth_getBlockByNumber', [head.number, false]);
                expect(block, `canonical block exists for ${head.number}`).to.not.equal(null);
                assertNewHeadMatchesBlock(head, block);
            }

            await ws.unsubscribe(subId);
        });

        it('delivers strictly increasing block numbers tracking the chain head', async () => {
            const subId = await ws.subscribe(['newHeads']);
            const heads = await ws.waitFor(subId, 3);

            const numbers = heads.map(h => BigInt(h.number));
            for (let i = 1; i < numbers.length; i++) {
                expect(numbers[i] > numbers[i - 1], `head ${i} is newer than head ${i - 1}`).to.equal(
                    true,
                );
            }
            // The stream is at the live head: the last pushed number is at/above an independently
            // observed height taken before we read it.
            const observed = BigInt(await sei.send('eth_blockNumber', []));
            expect(numbers[numbers.length - 1] + 2n >= observed, 'stream tracks the live head').to.equal(
                true,
            );

            await ws.unsubscribe(subId);
        });

        it('rejects a repeat unsubscribe of an already-removed id', async () => {
            const subId = await ws.subscribe(['newHeads']);
            await ws.waitFor(subId, 1);
            expect(await ws.unsubscribe(subId), 'first unsubscribe returns true').to.equal(true);

            let err: any;
            try {
                await ws.unsubscribe(subId);
            } catch (e) {
                err = e;
            }
            expect(err, 'second unsubscribe is rejected').to.not.equal(undefined);
            expect(err.message, 'reports subscription not found').to.match(/subscription not found/i);
        });
    });

    describe('logs', () => {
        it('streams only logs matching the filter, with the canonical log schema', async () => {
            const { address, token } = await deployLogToken(emitter);
            const subId = await ws.subscribe(['logs', { address, topics: [TRANSFER_TOPIC] }]);

            const sink = ethers.Wallet.createRandom().address;
            await (await token.mint(emitter.address, ethers.parseEther('100'))).wait();
            await (await token.transfer(sink, 1n)).wait();

            const logs = await ws.waitFor(subId, 2);
            for (const log of logs) {
                expectLogShape(log, 'subscription log');
                expect(log.address, 'log address is the filtered token').to.equal(address.toLowerCase());
                expect(log.topics[0], 'log is a Transfer').to.equal(TRANSFER_TOPIC);
                expect(log.removed, 'live log is not removed').to.equal(false);
            }

            // The mint topics0->emitter, then emitter->sink: subscription order is emission order.
            expect(BigInt(logs[0].topics[2]), 'first Transfer credits the emitter').to.equal(
                BigInt(emitter.address.toLowerCase()),
            );

            // Cross-check the pushed logs against eth_getLogs over the same filter.
            const viaGetLogs = await sei.send('eth_getLogs', [
                { address, topics: [TRANSFER_TOPIC], fromBlock: '0x1', toBlock: 'latest' },
            ]);
            expect(viaGetLogs.length, 'eth_getLogs sees the same Transfers').to.be.gte(2);
            const pushedKeys = logs.map(l => `${l.blockHash}:${l.logIndex}`);
            const httpKeys = viaGetLogs.map((l: any) => `${l.blockHash}:${l.logIndex}`);
            pushedKeys.forEach(k =>
                expect(httpKeys, `pushed log ${k} is also reported by eth_getLogs`).to.include(k),
            );

            await ws.unsubscribe(subId);
        });

        it('a blockHash filter replays exactly that block\'s matching logs', async () => {
            const { address, token } = await deployLogToken(emitter);
            const mint = await (await token.mint(emitter.address, ethers.parseEther('5'))).wait();
            const blockHash: string = mint!.blockHash;

            const subId = await ws.subscribe(['logs', { blockHash, address }]);
            const logs = await ws.waitFor(subId, 1);

            expect(logs.length, 'one Transfer in the mint block').to.equal(1);
            expectLogShape(logs[0], 'blockHash log');
            expect(logs[0].blockHash, 'log is from the requested block').to.equal(blockHash);
            expect(logs[0].address, 'log is from the requested token').to.equal(address.toLowerCase());

            await ws.unsubscribe(subId);
        });
    });
});
