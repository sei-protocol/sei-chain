import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, claimPool, expectSameError } from '../utils/testUtils';
import { EvmAccount } from '../utils/evmUtils';
import { EARLY_STATE_ERROR } from '../utils/format';

describe('eth_getTransactionCount', function () {
    this.timeout(120 * 1000);

    const { sei, geth } = bothProviders();

    let runtime: RuntimeState;
    let seiAdmin: string;
    let gethAdmin: string;
    let sender: EvmAccount;

    before(async () => {
        runtime = readRuntimeState();
        seiAdmin = runtime.funded.admin;
        gethAdmin = runtime.funded.gethAdmin.address;
        [sender] = claimPool(runtime, sei, 1, 'eth_getTransactionCount');
    });


    describe('happy path / schema', () => {
        it('returns a canonical HEX_QUANTITY for a funded account at latest', async () => {
            const res = await rawSei<string>('eth_getTransactionCount', [seiAdmin, 'latest']);
            expect(res.error, JSON.stringify(res.error)).to.equal(undefined);
            expect(res.result, 'nonce is a canonical quantity').to.match(/^0x(0|[1-9a-f][0-9a-f]*)$/);
        });

        it('returns 0x0 for a brand-new unassociated address', async () => {
            const fresh = ethers.Wallet.createRandom().address;
            const [s, g] = await Promise.all([
                rawSei<string>('eth_getTransactionCount', [fresh, 'latest']),
                rawGeth<string>('eth_getTransactionCount', [fresh, 'latest']),
            ]);
            expect(s.result, 'Sei: fresh account nonce is 0x0').to.equal('0x0');
            expect(g.result, 'geth: fresh account nonce is 0x0').to.equal('0x0');
        });

        it('all block tags return the same live nonce for a funded, idle account', async () => {
            const recent = ['safe', 'finalized', 'pending', 'latest'] as const;
            const results = await Promise.all(
                recent.map(t => rawSei<string>('eth_getTransactionCount', [seiAdmin, t])),
            );

            const byTag: Record<string, bigint> = {};
            results.forEach((res, i) => {
                expect(res.error, `${recent[i]}: ${JSON.stringify(res.error)}`).to.equal(undefined);
                expect(res.result, `${recent[i]} is canonical`).to.match(
                    /^0x(0|[1-9a-f][0-9a-f]*)$/,
                );
                byTag[recent[i]] = BigInt(res.result!);
            });

            const live = byTag.latest;
            for (const t of ['safe', 'finalized', 'pending'] as const) {
                expect(byTag[t], `${t} equals the live (latest) nonce`).to.equal(live);
            }

            // earliest reads genesis: a full-history node serves a nonce that cannot exceed
            // the live one, but a pruning node / one whose EVM module postdates genesis
            // rejects it with -32000 (same divergence eth_call's earliest case handles).
            const earliest = await rawSei<string>('eth_getTransactionCount', [seiAdmin, 'earliest']);
            if (earliest.error) {
                expect(earliest.error.code, `earliest: ${JSON.stringify(earliest.error)}`).to.equal(-32000);
                expect(earliest.error.message).to.match(EARLY_STATE_ERROR);
            } else {
                expect(earliest.result, 'earliest is canonical').to.match(/^0x(0|[1-9a-f][0-9a-f]*)$/);
                expect(
                    BigInt(earliest.result!) <= live,
                    `earliest (${earliest.result}) cannot exceed the live nonce (${live})`,
                ).to.equal(true);
            }
        });

        it('Sei and geth agree on the nonce of the contract address (always 1 after deploy)', async () => {
            // After deployment the contract address has nonce 1 in Ethereum semantics.
            const [s, g] = await Promise.all([
                rawSei<string>('eth_getTransactionCount', [runtime.contracts.erc20, 'latest']),
                rawGeth<string>('eth_getTransactionCount', [runtime.contracts.erc20Geth, 'latest']),
            ]);
            expect(s.result, 'Sei: deployed contract nonce is 0x1').to.equal('0x1');
            expect(g.result, 'geth: deployed contract nonce is 0x1').to.equal('0x1');
        });
    });

    describe('nonce tracks transactions', () => {
        it('increments by 1 after each successfully mined transaction', async () => {
            const nonceBefore = BigInt(
                (await rawSei<string>('eth_getTransactionCount', [sender.address, 'latest'])).result!,
            );

            const recipient = ethers.Wallet.createRandom().address;
            const receipt = await (
                await sender.wallet.sendTransaction({ to: recipient, value: 0n })
            ).wait();
            expect(receipt!.status, 'tx mined').to.equal(1);

            const nonceAfter = BigInt(
                (await rawSei<string>('eth_getTransactionCount', [sender.address, 'latest'])).result!,
            );
            expect(nonceAfter, 'nonce incremented by exactly 1').to.equal(nonceBefore + 1n);
        });

        it('pre-send block retains the old nonce (historical immutability)', async () => {
            const blockBefore = await sei.getBlockNumber();
            const nonceBefore = BigInt(
                (
                    await rawSei<string>('eth_getTransactionCount', [
                        sender.address,
                        ethers.toQuantity(blockBefore),
                    ])
                ).result!,
            );

            const receipt = await (
                await sender.wallet.sendTransaction({
                    to: ethers.Wallet.createRandom().address,
                    value: 0n,
                })
            ).wait();
            expect(receipt!.status, 'tx mined').to.equal(1);

            const historicalNonce = BigInt(
                (
                    await rawSei<string>('eth_getTransactionCount', [
                        sender.address,
                        ethers.toQuantity(blockBefore),
                    ])
                ).result!,
            );
            expect(historicalNonce, 'historical nonce is immutable').to.equal(nonceBefore);
        });

        it('latest nonce (pending includes mempool txs)', async () => {
            const [latest, pending] = await Promise.all([
                rawSei<string>('eth_getTransactionCount', [sender.address, 'latest']),
                rawSei<string>('eth_getTransactionCount', [sender.address, 'pending']),
            ]);
            expect(latest.result, 'latest nonce is a quantity').to.match(
                /^0x(0|[1-9a-f][0-9a-f]*)$/,
            );
            expect(pending.result, 'pending nonce is a quantity').to.match(
                /^0x(0|[1-9a-f][0-9a-f]*)$/,
            );
            expect(
                BigInt(pending.result!) >= BigInt(latest.result!),
                'pending nonce must be >= latest nonce',
            ).to.equal(true);
        });
    });

    describe('EIP-1898 block specifiers', () => {
        let knownNonce: bigint;
        let txBlock: number;
        let txBlockHash: string;

        before(async () => {
            knownNonce = BigInt(
                (await rawSei<string>('eth_getTransactionCount', [sender.address, 'latest'])).result!,
            );
            const receipt = await (
                await sender.wallet.sendTransaction({
                    to: ethers.Wallet.createRandom().address,
                    value: 0n,
                })
            ).wait();
            txBlock = receipt!.blockNumber;
            const block = await sei.getBlock(txBlock);
            txBlockHash = block!.hash!;
        });

        it('blockNumber object matches numeric tag and reflects the post-tx nonce', async () => {
            const tag = ethers.toQuantity(txBlock);
            const [viaTag, viaObject] = await Promise.all([
                rawSei<string>('eth_getTransactionCount', [sender.address, tag]),
                rawSei<string>('eth_getTransactionCount', [
                    sender.address,
                    { blockNumber: tag },
                ]),
            ]);
            expect(viaObject.result, 'blockNumber object == numeric tag').to.equal(viaTag.result);
            expect(BigInt(viaObject.result!), 'nonce incremented after tx').to.equal(
                knownNonce + 1n,
            );
        });

        it('blockHash object matches numeric tag', async () => {
            const tag = ethers.toQuantity(txBlock);
            const [viaNumber, viaHash] = await Promise.all([
                rawSei<string>('eth_getTransactionCount', [sender.address, tag]),
                rawSei<string>('eth_getTransactionCount', [
                    sender.address,
                    { blockHash: txBlockHash },
                ]),
            ]);
            expect(viaHash.result, 'blockHash object == numeric tag').to.equal(viaNumber.result);
        });

        it('nonce at the block before the tx equals the pre-tx nonce', async () => {
            const preTxNonce = BigInt(
                (
                    await rawSei<string>('eth_getTransactionCount', [
                        sender.address,
                        ethers.toQuantity(txBlock - 1),
                    ])
                ).result!,
            );
            expect(preTxNonce, 'nonce at block before tx == pre-tx nonce').to.equal(knownNonce);
        });
    });

    describe('wrong params / error handling (parity with geth)', () => {
        it('both agree that a fresh address starts at nonce 0x0', async () => {
            const fresh = ethers.Wallet.createRandom().address;
            const [s, g] = await Promise.all([
                rawSei<string>('eth_getTransactionCount', [fresh, 'latest']),
                rawGeth<string>('eth_getTransactionCount', [fresh, 'latest']),
            ]);
            expect(s.result).to.equal('0x0');
            expect(g.result).to.equal('0x0');
        });

        it('empty params fail identically (-32602, missing required argument 0)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getTransactionCount', []),
                rawGeth('eth_getTransactionCount', []),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 0/);
            expectSameError(s, g);
        });

        it('omitting the block argument fails identically (-32602, missing required argument 1)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getTransactionCount', [seiAdmin]),
                rawGeth('eth_getTransactionCount', [gethAdmin]),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 1/);
            expectSameError(s, g);
        });

        it('too many positional args fail identically (-32602, want at most 2)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getTransactionCount', [seiAdmin, 'latest', {}]),
                rawGeth('eth_getTransactionCount', [gethAdmin, 'latest', {}]),
            ]);
            expectJsonRpcError(s, -32602, /too many arguments, want at most 2/);
            expectSameError(s, g);
        });

        it('non-array params fail identically (-32602, non-array args)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getTransactionCount', { address: seiAdmin }),
                rawGeth('eth_getTransactionCount', { address: gethAdmin }),
            ]);
            expectJsonRpcError(s, -32602, /^non-array args$/);
            expectSameError(s, g);
        });

        it('a malformed (too short) address fails identically (-32602, exact length message)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getTransactionCount', ['0x1234', 'latest']),
                rawGeth('eth_getTransactionCount', ['0x1234', 'latest']),
            ]);
            expectJsonRpcError(s, -32602, /hex string has length 4, want 40 for common\.Address/);
            expectSameError(s, g);
        });

        it('an unknown future block returns undefined (does not panic)', async () => {
            const future = ethers.toQuantity((await sei.getBlockNumber()) + 10_000_000);
            const res = await rawSei('eth_getTransactionCount', [seiAdmin, future]);
            expect(res.error!.message).to.contain('is not yet available');
        });
    });
});
