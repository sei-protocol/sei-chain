import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, claimPool, expectSameError } from '../utils/testUtils';
import { EvmAccount } from '../utils/evmUtils';

// eth_getTransactionCount: the nonce query — how many transactions an account has sent.
// Bugs here break every client that derives nonces from the node rather than tracking
// them locally. We check: canonical format, historical accuracy, pending vs latest
// semantics, EIP-1898 block specifiers, and geth parity.

describe('eth_getTransactionCount', function () {
    this.timeout(120 * 1000);

    const { sei, geth } = bothProviders();

    let runtime: RuntimeState;
    let seiAdmin: string;
    let gethAdmin: string;
    let sender: EvmAccount;
    let gethSender: EvmAccount;

    before(async () => {
        runtime = readRuntimeState();
        seiAdmin = runtime.funded.admin;
        gethAdmin = runtime.funded.gethAdmin.address;
        [sender] = claimPool(runtime, sei, 1, 'eth_getTransactionCount');
        gethSender = EvmAccount.fromPrivateKey(runtime.funded.gethAdmin.privateKey, geth);
    });

    // ── schema / happy path ──────────────────────────────────────────────────────────

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

        it('all block tags return a canonical quantity for a funded account', async () => {
            const tags = ['earliest', 'safe', 'finalized', 'pending', 'latest'] as const;
            const results = await Promise.all(
                tags.map(t => rawSei<string>('eth_getTransactionCount', [seiAdmin, t])),
            );
            results.forEach((res, i) => {
                expect(res.error, `${tags[i]}: ${JSON.stringify(res.error)}`).to.equal(undefined);
                expect(res.result, `${tags[i]} is canonical`).to.match(
                    /^0x(0|[1-9a-f][0-9a-f]*)$/,
                );
            });
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

    // ── nonce increments with transactions ───────────────────────────────────────────

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

            // The historical read must not reflect the later transaction.
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

        it('[divergence-probe] pending nonce >= latest nonce (pending includes mempool txs)', async () => {
            // On geth, pending nonce includes in-mempool but unmined txs.
            // Sei's pending semantics may differ — this test documents the actual behaviour.
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
            // pending >= latest is the Ethereum spec; assert it and surface divergences.
            expect(
                BigInt(pending.result!) >= BigInt(latest.result!),
                '[divergence-probe] pending nonce must be >= latest nonce',
            ).to.equal(true);
        });
    });

    // ── EIP-1898 block specifiers ────────────────────────────────────────────────────

    describe('EIP-1898 block specifiers', () => {
        let knownNonce: bigint;
        let txBlock: number;
        let txBlockHash: string;

        before(async () => {
            // Record the nonce just before a known tx.
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

    // ── geth parity ──────────────────────────────────────────────────────────────────

    describe('geth parity', () => {
        it('both agree that a fresh address starts at nonce 0x0', async () => {
            const fresh = ethers.Wallet.createRandom().address;
            const [s, g] = await Promise.all([
                rawSei<string>('eth_getTransactionCount', [fresh, 'latest']),
                rawGeth<string>('eth_getTransactionCount', [fresh, 'latest']),
            ]);
            expect(s.result).to.equal('0x0');
            expect(g.result).to.equal('0x0');
        });

        it('nonce increments by 1 on geth after a single tx (sanity baseline)', async () => {
            const nonceBefore = BigInt(
                (
                    await rawGeth<string>('eth_getTransactionCount', [
                        gethSender.address,
                        'latest',
                    ])
                ).result!,
            );
            const receipt = await (
                await gethSender.wallet.sendTransaction({
                    to: ethers.Wallet.createRandom().address,
                    value: 0n,
                })
            ).wait();
            expect(receipt!.status).to.equal(1);
            const nonceAfter = BigInt(
                (
                    await rawGeth<string>('eth_getTransactionCount', [
                        gethSender.address,
                        'latest',
                    ])
                ).result!,
            );
            expect(nonceAfter).to.equal(nonceBefore + 1n);
        });
    });

    // ── error handling parity ────────────────────────────────────────────────────────

    describe('wrong params / error handling (parity with geth)', () => {
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

        it('an unknown future block returns an error or null (does not panic)', async () => {
            // The exact behaviour is implementation-defined; we just verify it does not
            // return a non-null result as if the block existed.
            const future = ethers.toQuantity((await sei.getBlockNumber()) + 10_000_000);
            const res = await rawSei('eth_getTransactionCount', [seiAdmin, future]);
            // Either an error OR a null result is acceptable; a non-null result is not.
            if (res.error === undefined) {
                expect(res.result, 'future block should return null, not a count').to.equal(null);
            }
        });
    });
});
