import { expect } from 'chai';
import { ethers } from 'ethers';
import { Endpoints } from '../config/endpoints';
import { seiRpc, waitUntil } from '../utils/chainUtils';
import { readRuntimeState, claimPool } from '../utils/testUtils';
import { EvmAccount } from '../utils/evmUtils';
import {
    deployLogToken,
    expectLogShape,
    emitLogScene,
    LogScene,
    LOG_FILTER_MATRIX,
    TRANSFER_TOPIC,
    addressTopic,
} from '../utils/logsUtils';
import {
    WsClient,
    SUBSCRIPTION_ID,
    assertNewHeadSchema,
    assertNewHeadInapplicableZeros,
    assertNewHeadMatchesBlock,
    assertNewHeadBaseFeeTracksChain,
} from '../utils/subscribeUtils';

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
        if (ws) ws.close();
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
                await assertNewHeadBaseFeeTracksChain(head, sei);
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
            const mint = await (await token.mint(emitter.address, ethers.parseEther('100'))).wait();
            const xfer = await (await token.transfer(sink, 1n)).wait();

            const logs = await ws.waitFor(subId, 2);
            for (const log of logs) {
                expectLogShape(log, 'subscription log');
                expect(log.address, 'log address is the filtered token').to.equal(address.toLowerCase());
                expect(log.topics[0], 'log is a Transfer').to.equal(TRANSFER_TOPIC);
                expect(log.removed, 'live log is not removed').to.equal(false);
            }

            // The mint topics0->emitter, then emitter->sink: subscription order is emission order.
            expect(logs[0].topics[2], 'first Transfer credits the emitter').to.equal(
                addressTopic(emitter.address),
            );

            const viaGetLogs = await sei.send('eth_getLogs', [
                {
                    address,
                    topics: [TRANSFER_TOPIC],
                    fromBlock: ethers.toQuantity(mint!.blockNumber),
                    toBlock: ethers.toQuantity(xfer!.blockNumber),
                },
            ]);
            expect(viaGetLogs.length, 'eth_getLogs sees the same Transfers').to.equal(2);
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

            await waitUntil(
                async () => {
                    const seen = await sei.send('eth_getLogs', [{ blockHash, address }]);
                    return seen.length >= 1 ? seen : null;
                },
                { timeoutMs: 30_000, label: `eth_getLogs indexes the mint block ${blockHash}` },
            );

            const subId = await ws.subscribe(['logs', { blockHash, address }]);
            const logs = await ws.waitFor(subId, 1);

            expect(logs.length, 'one Transfer in the mint block').to.equal(1);
            expectLogShape(logs[0], 'blockHash log');
            expect(logs[0].blockHash, 'log is from the requested block').to.equal(blockHash);
            expect(logs[0].address, 'log is from the requested token').to.equal(address.toLowerCase());

            await ws.unsubscribe(subId);
        });
    });

    describe('rich log filter criteria (multi-address / multi-topic), parity with eth_getLogs', () => {
        let scene: LogScene;

        before(async () => {
            const alice = ethers.Wallet.createRandom().address;
            const bob = ethers.Wallet.createRandom().address;
            scene = await emitLogScene(emitter, alice, bob);
        });

        const assertSubLogsMatchGetLogs = async (criteria: object, ctx: string): Promise<any[]> => {
            const oracle = await sei.send('eth_getLogs', [criteria]);
            expect(oracle.length, `${ctx}: scene produced matching logs`).to.be.greaterThan(0);

            const subId = await ws.subscribe(['logs', criteria]);
            try {
                const got = await ws.waitFor(subId, oracle.length);
                // A bounded, address-scoped subscription must not over-deliver: poll for one extra
                // log and require the wait to time out (no straggler beyond the eth_getLogs set).
                let overDelivered = true;
                try {
                    await ws.waitFor(subId, oracle.length + 1, 2_000);
                } catch {
                    overDelivered = false;
                }
                expect(overDelivered, `${ctx}: no logs beyond the eth_getLogs set`).to.equal(false);

                const key = (l: any) => `${l.blockHash}:${l.logIndex}`;
                const gotByKey = new Map(got.map(l => [key(l), l]));
                oracle.forEach((o: any) => {
                    const g = gotByKey.get(key(o));
                    expect(g, `${ctx}: subscription delivered ${key(o)}`).to.not.equal(undefined);
                    expectLogShape(g, `${ctx} sub log`);
                    expect(g.address, `${ctx}: address`).to.equal(o.address);
                    expect(g.topics, `${ctx}: topics`).to.deep.equal(o.topics);
                    expect(g.data, `${ctx}: data`).to.equal(o.data);
                    expect(g.removed, `${ctx}: canonical (not removed)`).to.equal(false);
                });
                return got;
            } finally {
                await ws.unsubscribe(subId);
            }
        };

        for (const c of LOG_FILTER_MATRIX) {
            it(c.title, async () => {
                const { criteria, expectedCount, check } = c.build(scene);
                const logs = await assertSubLogsMatchGetLogs(criteria, c.title);
                expect(logs.length, `${c.title}: match count`).to.equal(expectedCount);
                check?.(logs);
            });
        }
    });

    describe('subscription type support matrix (divergence from geth)', () => {
        it('rejects an unknown subscription type identically (-32601)', async () => {
            let err: any;
            try {
                await ws.subscribe(['definitelyNotARealSubscription']);
            } catch (e) {
                err = e;
            }
            expect(err, 'unknown type is rejected').to.not.equal(undefined);
            expect(err.code, 'JSON-RPC method-not-found code').to.equal(-32601);
            expect(err.message, 'namespaced "no subscription" message').to.match(
                /no "definitelyNotARealSubscription" subscription in eth namespace/,
            );
        });
    });
});
