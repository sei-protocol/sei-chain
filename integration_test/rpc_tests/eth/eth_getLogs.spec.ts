import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, claimPool, expectSameError } from '../utils/testUtils';
import { HEX_QUANTITY } from '../utils/format';
import { EvmAccount } from '../utils/evmUtils';
import { sharedRichBlock, RichBlock, richFailedTxs } from '../utils/txUtils';
import {
    emitLogScene,
    LogScene,
    expectLogShape,
    logKeys,
    addressTopic,
    TRANSFER_TOPIC,
    APPROVAL_TOPIC,
    CORE_LOG_KEYS,
    ERC20_LOG_IFACE,
    STAKING_IFACE,
    STAKING_PRECOMPILE_ADDRESS,
    DELEGATE_TOPIC,
    firstBondedValidator,
    delegateViaPrecompile,
} from '../utils/logsUtils';

describe('eth_getLogs', function () {
    this.timeout(180 * 1000);

    const { sei, geth } = bothProviders();

    let runtime: RuntimeState;
    let scene: LogScene;
    let erc20Lower: string;

    before(async () => {
        runtime = readRuntimeState();
        const [emitter] = claimPool(runtime, sei, 1, 'eth_getLogs');
        const alice = ethers.Wallet.createRandom().address;
        const bob = ethers.Wallet.createRandom().address;
        scene = await emitLogScene(emitter, alice, bob);
        erc20Lower = scene.erc20.toLowerCase();
    });

    const getLogs = (filter: object) => sei.send('eth_getLogs', [filter]);
    const range = () => ({
        fromBlock: ethers.toQuantity(scene.firstEventBlock),
        toBlock: ethers.toQuantity(scene.lastEventBlock),
    });

    describe('happy path / schema', () => {
        it('returns every event emitted against the contract over its block range', async () => {
            const logs = await getLogs({ ...range(), address: scene.erc20 });
            expect(logs.length, 'mint + 2 transfers + approval').to.equal(scene.totalCount);
            logs.forEach((log: any, i: number) => {
                expectLogShape(log, `logs[${i}]`);
                expect(log.address, `logs[${i}].address`).to.equal(erc20Lower);
            });
        });

        it('orders logs by (blockNumber, logIndex); the mint comes first', async () => {
            const logs = await getLogs({ ...range(), address: scene.erc20 });
            const blockNums = logs.map((l: any) => Number(l.blockNumber));
            const sorted = [...blockNums].sort((a, b) => a - b);
            expect(blockNums, 'ascending by block').to.deep.equal(sorted);

            expect(logs[0].topics[0], 'first event is a Transfer').to.equal(TRANSFER_TOPIC);
            expect(logs[0].topics[1], 'minted from the zero address').to.equal(
                addressTopic(ethers.ZeroAddress),
            );
        });
    });

    describe('topic filtering', () => {
        it('filters by event signature (topic0): only Transfers', async () => {
            const logs = await getLogs({ ...range(), address: scene.erc20, topics: [TRANSFER_TOPIC] });
            expect(logs.length).to.equal(scene.transferCount);
            logs.forEach((l: any) => expect(l.topics[0]).to.equal(TRANSFER_TOPIC));
        });

        it('filters by event signature (topic0): only the Approval', async () => {
            const logs = await getLogs({ ...range(), address: scene.erc20, topics: [APPROVAL_TOPIC] });
            expect(logs.length).to.equal(scene.approvalCount);
            expect(logs[0].topics[0]).to.equal(APPROVAL_TOPIC);
            // Approval(emitter, alice, 5)
            expect(BigInt(logs[0].data)).to.equal(5n);
        });

        it('filters by an indexed sender (topic1): the two emitter transfers', async () => {
            const logs = await getLogs({
                ...range(),
                address: scene.erc20,
                topics: [TRANSFER_TOPIC, addressTopic(scene.emitter.address)],
            });
            expect(logs.length, 'transfer to alice + transfer to bob').to.equal(2);
            logs.forEach((l: any) =>
                expect(l.topics[1]).to.equal(addressTopic(scene.emitter.address)),
            );
        });

        it('filters by an indexed recipient (topic2): only the transfer to alice', async () => {
            const logs = await getLogs({
                ...range(),
                address: scene.erc20,
                topics: [TRANSFER_TOPIC, null, addressTopic(scene.alice)],
            });
            expect(logs.length).to.equal(1);
            expect(logs[0].topics[2]).to.equal(addressTopic(scene.alice));
            expect(BigInt(logs[0].data), 'transferred 10').to.equal(10n);
        });

        it('supports topic OR-sets (topic0 = Transfer OR Approval): every event', async () => {
            const logs = await getLogs({
                ...range(),
                address: scene.erc20,
                topics: [[TRANSFER_TOPIC, APPROVAL_TOPIC]],
            });
            expect(logs.length).to.equal(scene.totalCount);
        });

        it('supports an OR-set on topic1 (mint zero-address OR emitter): all three transfers', async () => {
            const logs = await getLogs({
                ...range(),
                address: scene.erc20,
                topics: [
                    TRANSFER_TOPIC,
                    [addressTopic(ethers.ZeroAddress), addressTopic(scene.emitter.address)],
                ],
            });
            expect(logs.length).to.equal(scene.transferCount);
        });

        it('treats a null topic0 as a wildcard: every event whose first indexed arg is the emitter', async () => {
            // emitter is the `from` of both transfers and the `owner` of the approval.
            const logs = await getLogs({
                ...range(),
                address: scene.erc20,
                topics: [null, addressTopic(scene.emitter.address)],
            });
            expect(logs.length, '2 transfers + 1 approval').to.equal(3);
        });
    });

    describe('block range & address filtering', () => {
        it('restricts to a single block (fromBlock == toBlock)', async () => {
            const logs = await getLogs({
                fromBlock: ethers.toQuantity(scene.aliceBlock),
                toBlock: ethers.toQuantity(scene.aliceBlock),
                address: scene.erc20,
            });
            expect(logs.length).to.equal(1);
            expect(logs[0].topics[2]).to.equal(addressTopic(scene.alice));
        });

        it('honours a partial range (mint .. alice transfer)', async () => {
            const logs = await getLogs({
                fromBlock: ethers.toQuantity(scene.mintBlock),
                toBlock: ethers.toQuantity(scene.aliceBlock),
                address: scene.erc20,
            });
            expect(logs.length, 'mint + alice transfer').to.equal(2);
        });

        it('resolves a blockHash filter to that block only', async () => {
            const block = await sei.getBlock(scene.aliceBlock);
            expect(block, 'alice-transfer block exists').to.not.equal(null);
            const logs = await getLogs({ blockHash: block!.hash!, address: scene.erc20 });
            expect(logs.length).to.equal(1);
            expect(logs[0].blockHash).to.equal(block!.hash);
        });

        it('returns an empty set for an address that never emitted', async () => {
            const logs = await getLogs({
                ...range(),
                address: ethers.Wallet.createRandom().address,
            });
            expect(logs).to.deep.equal([]);
        });

        it('returns an empty set when no event of that type is in range', async () => {
            // The approval lands after the bob transfer, so a mint..bob range has none.
            const logs = await getLogs({
                fromBlock: ethers.toQuantity(scene.mintBlock),
                toBlock: ethers.toQuantity(scene.bobBlock),
                address: scene.erc20,
                topics: [APPROVAL_TOPIC],
            });
            expect(logs).to.deep.equal([]);
        });
    });

    describe('multiple-address filter', () => {
        it('accepts the array form and unions across addresses (empty co-addresses contribute nothing)', async () => {
            const [single, withEmpty] = await Promise.all([
                getLogs({ ...range(), address: [scene.erc20] }),
                getLogs({ ...range(), address: [scene.erc20, ethers.Wallet.createRandom().address] }),
            ]);
            expect(single.length, 'array-of-one == the scene total').to.equal(scene.totalCount);
            expect(withEmpty.length, 'a non-emitting co-address adds nothing').to.equal(
                scene.totalCount,
            );
        });
    });

    describe('non-EVM log sources (dual-VM & precompiles)', () => {
        it('indexes a CW20 ERC20 pointer transfer as a standard Transfer log', async function () {
            if (!runtime.wasm?.cw20Pointer) {
                this.skip();
            }
            // The pointer's actor holds the CW20 balance; admin is association-safe.
            const actor = EvmAccount.fromPrivateKey(runtime.wasm!.actor.privateKey, sei);
            const pointer = new ethers.Contract(
                runtime.wasm!.cw20Pointer,
                ERC20_LOG_IFACE,
                actor.wallet,
            );
            const receipt = await (await pointer.transfer(runtime.funded.admin, 1n)).wait();

            const logs = await getLogs({
                address: runtime.wasm!.cw20Pointer,
                fromBlock: ethers.toQuantity(receipt!.blockNumber),
                toBlock: ethers.toQuantity(receipt!.blockNumber),
            });
            const transfer = logs.find((l: any) => l.topics[0] === TRANSFER_TOPIC);
            expect(transfer, 'pointer emits a Transfer log').to.not.equal(undefined);
            expectLogShape(transfer, 'pointer transfer');
            expect(transfer.address).to.equal(runtime.wasm!.cw20Pointer.toLowerCase());
            expect(transfer.topics[2], 'recipient is admin').to.equal(
                addressTopic(runtime.funded.admin),
            );
        });

        it('indexes a staking-precompile delegate as a Delegate log under the precompile address', async function () {
            const [delegator] = claimPool(runtime, sei, 1, 'eth_getLogs-staking');
            let validator: string;
            try {
                validator = await firstBondedValidator();
            } catch {
                this.skip();
                return;
            }
            // Associate the EVM account, then delegate 0.01 SEI.
            await (await delegator.wallet.sendTransaction({ to: delegator.address, value: 0 })).wait();
            const receipt = await delegateViaPrecompile(
                delegator,
                validator,
                ethers.parseEther('0.01'),
            );

            const logs = await getLogs({
                address: STAKING_PRECOMPILE_ADDRESS,
                fromBlock: ethers.toQuantity(receipt.blockNumber),
                toBlock: ethers.toQuantity(receipt.blockNumber),
            });

            const delegate = logs.find((l: any) => l.topics[0] === DELEGATE_TOPIC);
            expect(delegate, 'precompile emits a Delegate log').to.not.equal(undefined);
            expectLogShape(delegate, 'delegate');
            expect(delegate.address).to.equal(STAKING_PRECOMPILE_ADDRESS.toLowerCase());
            expect(delegate.topics[1], 'indexed delegator').to.equal(
                addressTopic(delegator.address),
            );
            const decoded = STAKING_IFACE.parseLog({
                topics: delegate.topics,
                data: delegate.data,
            });
            expect(decoded!.args.validator, 'non-indexed validator string').to.equal(validator);
            expect(decoded!.args.amount, 'delegated amount').to.equal(ethers.parseEther('0.01'));
        });

        it('numbers multiple logs from one tx contiguously (shared tx hash, incrementing logIndex)', async function () {
            const [delegator] = claimPool(runtime, sei, 1, 'eth_getLogs-multilog');
            let validator: string;
            try {
                validator = await firstBondedValidator();
            } catch {
                this.skip();
                return;
            }
            await (await delegator.wallet.sendTransaction({ to: delegator.address, value: 0 })).wait();
            const receipt = await delegateViaPrecompile(
                delegator,
                validator,
                ethers.parseEther('0.01'),
            );

            const logs = await getLogs({
                address: STAKING_PRECOMPILE_ADDRESS,
                fromBlock: ethers.toQuantity(receipt.blockNumber),
                toBlock: ethers.toQuantity(receipt.blockNumber),
            });
            const txLogs = logs
                .filter((l: any) => l.transactionHash === receipt.hash)
                .sort((a: any, b: any) => Number(a.logIndex) - Number(b.logIndex));
            expect(txLogs.length, 'the delegate tx produced at least one log').to.be.greaterThan(0);
            txLogs.forEach((l: any, i: number) => {
                expect(Number(l.logIndex), `logIndex is contiguous from 0`).to.equal(i);
                expect(l.blockHash, 'same block hash').to.equal(txLogs[0].blockHash);
                expect(l.transactionIndex, 'same tx index').to.equal(txLogs[0].transactionIndex);
            });
        });
    });

    describe('rich block: block-global log index correctness', () => {
        let rich: RichBlock;
        let blockLogs: any[];

        before(async function () {
            this.timeout(300 * 1000);
            rich = await sharedRichBlock(sei, runtime);
            blockLogs = await getLogs({
                fromBlock: ethers.toQuantity(rich.number),
                toBlock: ethers.toQuantity(rich.number),
            });
        });

        it('assigns contiguous, block-global logIndex ordered by transaction index', async () => {
            expect(blockLogs.length, 'the rich block emitted at least one log').to.be.greaterThan(0);
            blockLogs.forEach((l: any) => {
                expectLogShape(l, 'rich log');
                expect(l.blockHash, 'log belongs to the rich block').to.equal(rich.hash);
                expect(Number(l.blockNumber)).to.equal(rich.number);
            });

            const byIndex = [...blockLogs].sort(
                (a, b) => Number(a.logIndex) - Number(b.logIndex),
            );
            // logIndex is 0..n-1 with no gaps, spanning the whole block.
            byIndex.forEach((l: any, i: number) =>
                expect(Number(l.logIndex), 'contiguous block-global index').to.equal(i),
            );
            // ...and that ordering is non-decreasing in transactionIndex (block order).
            const txIdx = byIndex.map((l: any) => Number(l.transactionIndex));
            expect(txIdx, 'logIndex order follows transaction order').to.deep.equal(
                [...txIdx].sort((a, b) => a - b),
            );

            // The ERC20 transfer (value 0) is one of those logs.
            expect(
                blockLogs.some(
                    (l: any) =>
                        l.address === runtime.contracts.erc20.toLowerCase() &&
                        l.topics[0] === TRANSFER_TOPIC,
                ),
                'the rich block ERC20 transfer is indexed',
            ).to.equal(true);
        });

        it('logIndex is block-global and preserved under filtering (not renumbered)', async () => {
            // A topic filter selects a SUBSET of the block's logs; it must never renumber
            // logIndex. Each filtered log keeps the exact block-global index it occupies in
            // the unfiltered result, so the filtered indices can be non-contiguous (have gaps).
            const filtered = await getLogs({ blockHash: rich.hash, topics: [TRANSFER_TOPIC] });
            expect(filtered.length, 'the rich block has Transfer logs').to.be.greaterThan(0);

            // blockLogs is the full, unfiltered block; logIndex == position in that ordering.
            for (const log of filtered) {
                expect(log.topics[0], 'filtered log is a Transfer').to.equal(TRANSFER_TOPIC);
                const atIndex = blockLogs[Number(BigInt(log.logIndex))];
                expect(atIndex, `block has a log at index ${log.logIndex}`).to.not.equal(undefined);
                expect(atIndex.transactionHash, 'same tx at that block-global index').to.equal(
                    log.transactionHash,
                );
                expect(atIndex.logIndex, 'logIndex unchanged by filter').to.equal(log.logIndex);
                expect(atIndex.data, 'same log payload').to.equal(log.data);
            }

            // Server-side filtering must reproduce client-side filtering of the full block
            // exactly: same logs, same order, same block-global indices.
            const clientFiltered = blockLogs.filter((l: any) => l.topics[0] === TRANSFER_TOPIC);
            expect(
                filtered.map((l: any) => l.logIndex),
                'server filter preserves block-global indices',
            ).to.deep.equal(clientFiltered.map((l: any) => l.logIndex));
        });

        it('the two included-but-failed txs contribute no logs to the block', async () => {
            const { outOfGas, revertErc20 } = richFailedTxs(rich);
            const failedHashes = new Set([outOfGas.hash, revertErc20.hash]);
            for (const log of blockLogs) {
                expect(
                    failedHashes.has(log.transactionHash),
                    'no block log originates from a failed tx',
                ).to.equal(false);
            }
            for (const sent of [outOfGas, revertErc20]) {
                const rc = await sei.send('eth_getTransactionReceipt', [sent.hash]);
                expect(rc.logs, `${sent.kind} receipt has no logs`)
                    .to.be.an('array')
                    .that.has.lengthOf(0);
            }
        });

        it('reports identical logIndex across getLogs, getTransactionReceipt and getBlockReceipts', async () => {
            const blockReceipts = await sei.send('eth_getBlockReceipts', [
                ethers.toQuantity(rich.number),
            ]);
            const fromBlockReceipts = blockReceipts.flatMap((r: any) => r.logs);

            for (const log of blockLogs) {
                const receipt = await sei.send('eth_getTransactionReceipt', [log.transactionHash]);
                const inReceipt = receipt.logs.find(
                    (l: any) => l.logIndex === log.logIndex,
                );
                expect(inReceipt, `tx receipt carries log ${log.logIndex}`).to.not.equal(undefined);
                // The same log, byte-for-byte, from the receipt endpoint.
                expect(inReceipt).to.deep.equal(log);

                const inBlockReceipts = fromBlockReceipts.find(
                    (l: any) => l.logIndex === log.logIndex,
                );
                expect(inBlockReceipts, `block receipts carry log ${log.logIndex}`).to.deep.equal(
                    log,
                );
            }
        });
    });

    describe('geth compatibility (log schema)', () => {
        it('returns every canonical Ethereum log field, matching geth', async () => {
            const [seiLogs, gethLogs] = await Promise.all([
                getLogs({ ...range(), address: scene.erc20 }),
                geth.send('eth_getLogs', [
                    { address: runtime.contracts.erc20Geth, fromBlock: 'earliest', toBlock: 'latest' },
                ]),
            ]);
            expect(seiLogs.length, 'Sei produced logs').to.be.greaterThan(0);
            expect(gethLogs.length, 'geth reference has logs').to.be.greaterThan(0);

            const seiKeys = logKeys(seiLogs[0]);
            const gethKeys = logKeys(gethLogs[0]);
            // Both must carry every canonical field...
            CORE_LOG_KEYS.forEach(k => {
                expect(seiKeys, `Sei log has ${k}`).to.include(k);
                expect(gethKeys, `geth log has ${k}`).to.include(k);
            });
            // ...Sei returns exactly the canonical set; geth additionally adds
            // blockTimestamp in recent releases, so it's a superset, never a subset.
            expect(seiKeys, 'Sei returns exactly the canonical fields').to.deep.equal([
                ...CORE_LOG_KEYS,
            ]);
            expect(
                CORE_LOG_KEYS.every(k => gethKeys.includes(k)),
                'geth is a superset of the canonical fields',
            ).to.equal(true);
        });

        // SKIP(expected-failure): Sei omits blockTimestamp on logs; pending manual reverification.
        it.skip('[spec] includes blockTimestamp on every log object', async () => {
            // The execution-apis log schema (and recent geth) include `blockTimestamp`.
            // Sei currently omits it; assert the standard so the gap surfaces as a failure.
            const [seiLogs, gethLogs] = await Promise.all([
                getLogs({ ...range(), address: scene.erc20 }),
                geth.send('eth_getLogs', [
                    { address: runtime.contracts.erc20Geth, fromBlock: 'earliest', toBlock: 'latest' },
                ]),
            ]);
            expect(gethLogs[0], 'geth (reference) log carries blockTimestamp').to.have.property(
                'blockTimestamp',
            );
            seiLogs.forEach((log: any, i: number) => {
                expect(log, `Sei log[${i}] must carry blockTimestamp`).to.have.property(
                    'blockTimestamp',
                );
                expect(log.blockTimestamp, `Sei log[${i}].blockTimestamp is a quantity`).to.match(
                    HEX_QUANTITY,
                );
            });
        });
    });

    // ── Ethereum invariants — log emission rules ─────────────────────────────────────
    //
    // These tests encode invariants that MUST hold for any EVM-compatible chain.
    // Failures here indicate a fundamental divergence from Ethereum semantics.

    describe('log emission invariants', () => {
        let rich: RichBlock;

        before(async function () {
            this.timeout(300 * 1000);
            rich = await sharedRichBlock(sei, runtime);
        });

        it('a reverted transaction produces no logs', async () => {
            // An ERC20 transfer that reverts due to insufficient balance must not emit
            // a Transfer event. This is a critical Ethereum invariant: state changes
            // and their side-effects (logs) are rolled back atomically on revert.
            const { revertErc20 } = richFailedTxs(rich);
            const logs = await sei.send('eth_getLogs', [
                { blockHash: rich.hash, topics: [TRANSFER_TOPIC] },
            ]);
            const revertedLogs = logs.filter(
                (l: any) => l.transactionHash === revertErc20.hash,
            );
            expect(
                revertedLogs.length,
                '[invariant] reverted tx must not emit logs (atomicity)',
            ).to.equal(0);
        });

        it('an out-of-gas transaction produces no logs', async () => {
            const { outOfGas } = richFailedTxs(rich);
            const logs = await sei.send('eth_getLogs', [{ blockHash: rich.hash }]);
            const oogs = logs.filter((l: any) => l.transactionHash === outOfGas.hash);
            expect(
                oogs.length,
                '[invariant] out-of-gas tx must not emit logs',
            ).to.equal(0);
        });

        it('canonical logs carry `removed: false` — they are not from a reorged chain', async () => {
            // Per EIP-234 / the JSON-RPC spec, `removed` is true only for logs from
            // blocks that were later reorged out. All logs from canonical blocks must
            // have `removed: false` (or the field absent, which ethers normalises to false).
            const logs = await sei.send('eth_getLogs', [{ blockHash: rich.hash }]);
            expect(logs.length, 'the rich block has logs').to.be.greaterThan(0);
            for (const log of logs) {
                if ('removed' in log) {
                    expect(log.removed, `log at index ${log.logIndex}: removed must be false`).to.equal(false);
                }
            }
        });

        it('geth canonical logs also carry `removed: false` (parity)', async () => {
            const gethBlock = await geth.send('eth_getBlockByNumber', ['latest', false]);
            if (!gethBlock || !gethBlock.hash) return;
            const logs = await geth.send('eth_getLogs', [{ blockHash: gethBlock.hash }]);
            for (const log of logs) {
                if ('removed' in log) {
                    expect(log.removed, `geth log removed must be false`).to.equal(false);
                }
            }
        });

        it('log indices within a block are contiguous, starting at 0', async () => {
            // All logIndex values across the block must form the sequence 0, 1, 2, …
            // with no gaps. A gap means a log was silently dropped.
            const logs = await sei.send('eth_getLogs', [{ blockHash: rich.hash }]);
            const sorted = [...logs].sort(
                (a: any, b: any) => Number(BigInt(a.logIndex)) - Number(BigInt(b.logIndex)),
            );
            sorted.forEach((log: any, i: number) => {
                expect(
                    Number(BigInt(log.logIndex)),
                    `log[${i}].logIndex must equal ${i} (no gaps)`,
                ).to.equal(i);
            });
        });

        it('transactionIndex in logs matches the order of transactions in the block', async () => {
            // Each log's transactionIndex must equal the index of its transaction in the
            // block's transaction array. A mismatch indicates incorrect index bookkeeping.
            const logs = await sei.send('eth_getLogs', [{ blockHash: rich.hash }]);
            const block = await sei.send('eth_getBlockByHash', [rich.hash, false]);
            const txIndexByHash = new Map<string, number>(
                block.transactions.map((h: string, i: number) => [h, i]),
            );
            for (const log of logs) {
                const expected = txIndexByHash.get(log.transactionHash);
                expect(expected, `log tx ${log.transactionHash} is in the block`).to.not.equal(undefined);
                expect(
                    Number(BigInt(log.transactionIndex)),
                    `log.transactionIndex must match block tx position`,
                ).to.equal(expected!);
            }
        });
    });

    describe('wrong params / error handling (parity with geth)', () => {

        it('empty params fail identically (-32602, missing required argument 0)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getLogs', []),
                rawGeth('eth_getLogs', []),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 0/);
            expectSameError(s, g);
        });

        it('blockHash combined with fromBlock fails identically (-32602)', async () => {
            const filter = { blockHash: '0x' + '0'.repeat(64), fromBlock: '0x1' };
            const [s, g] = await Promise.all([
                rawSei('eth_getLogs', [filter]),
                rawGeth('eth_getLogs', [filter]),
            ]);
            expectJsonRpcError(
                s,
                -32602,
                /cannot specify both BlockHash and FromBlock\/ToBlock/,
            );
            expectSameError(s, g);
        });

        it('a malformed topic (wrong byte length) fails identically (-32602)', async () => {
            const filter = { topics: ['0x1234'] };
            const [s, g] = await Promise.all([
                rawSei('eth_getLogs', [filter]),
                rawGeth('eth_getLogs', [filter]),
            ]);
            expectJsonRpcError(s, -32602, /invalid length 2 after decoding; expected 32 for topic/);
            expectSameError(s, g);
        });
    });
});
