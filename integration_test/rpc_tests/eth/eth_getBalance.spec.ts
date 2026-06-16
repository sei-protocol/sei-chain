import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, claimPool, expectSameError } from '../utils/testUtils';
import { EvmAccount } from '../utils/evmUtils';
import { HEX_QUANTITY as CANONICAL_QUANTITY, EARLY_STATE_ERROR } from '../utils/format';

describe('eth_getBalance', function () {
    this.timeout(120 * 1000);

    const { sei } = bothProviders();

    let runtime: RuntimeState;
    let seiAdmin: string;
    let gethAdmin: string;
    let erc20Sei: string;
    let spender: EvmAccount;
    let unassociated: string;

    before(async () => {
        runtime = readRuntimeState();
        seiAdmin = runtime.funded.admin;
        gethAdmin = runtime.funded.gethAdmin.address;
        erc20Sei = runtime.contracts.erc20;
        [spender] = claimPool(runtime, sei, 1, 'eth_getBalance');
        unassociated = ethers.Wallet.createRandom().address;
    });

    describe('happy path / schema', () => {
        it('returns the funded admin balance as a positive canonical quantity at latest', async () => {
            const res = await rawSei<string>('eth_getBalance', [seiAdmin, 'latest']);
            expect(res.error, JSON.stringify(res.error)).to.equal(undefined);
            expect(res.result).to.match(CANONICAL_QUANTITY);
            expect(BigInt(res.result!) > 0n, 'admin holds a spendable balance').to.equal(true);
        });

        it('reports 0x0 for a fresh, unassociated account — identically on Sei and geth', async () => {
            const [s, g] = await Promise.all([
                rawSei<string>('eth_getBalance', [unassociated, 'latest']),
                rawGeth<string>('eth_getBalance', [unassociated, 'latest']),
            ]);
            expect(s.result, 'Sei: empty account is zero').to.equal('0x0');
            expect(g.result, 'geth: empty account is zero').to.equal('0x0');
        });

        it('returns a canonical quantity for every supported block tag, equal across the recent tags', async () => {
            // The recent tags must resolve to a live state and succeed.
            const recent = ['safe', 'finalized', 'pending', 'latest'] as const;
            const results = await Promise.all(
                recent.map(t => rawSei<string>('eth_getBalance', [seiAdmin, t])),
            );
            results.forEach((res, i) => {
                expect(res.error, `${recent[i]}: ${JSON.stringify(res.error)}`).to.equal(undefined);
                expect(res.result, `${recent[i]} is canonical`).to.match(CANONICAL_QUANTITY);
            });

            // The admin does not transact in this spec, so the recent tags must agree.
            const byTag = Object.fromEntries(recent.map((t, i) => [t, results[i].result]));
            expect(byTag.safe, 'safe == latest while idle').to.equal(byTag.latest);
            expect(byTag.finalized, 'finalized == latest while idle').to.equal(byTag.latest);
            expect(byTag.pending, 'pending == latest while idle').to.equal(byTag.latest);

            // earliest reads genesis: a full-history node serves a canonical balance, but a
            // pruning node / one whose EVM module postdates genesis rejects it with -32000
            // (same divergence eth_call's earliest case handles). Tolerate both.
            const earliest = await rawSei<string>('eth_getBalance', [seiAdmin, 'earliest']);
            if (earliest.error) {
                expect(earliest.error.code, `earliest: ${JSON.stringify(earliest.error)}`).to.equal(-32000);
                expect(earliest.error.message).to.match(EARLY_STATE_ERROR);
            } else {
                expect(earliest.result, 'earliest is canonical').to.match(CANONICAL_QUANTITY);
            }
        });

        it('returns the (zero) native balance of a contract address', async () => {
            // TestERC20 is never sent value, so it holds no native balance.
            const balance = await sei.getBalance(erc20Sei, 'latest');
            expect(balance).to.equal(0n);
        });
    });

    describe('balance tracks transfers across historical state', () => {
        it('debits the sender by value + gas while the pre-send block keeps the old balance', async () => {
            const recipient = ethers.Wallet.createRandom().address;
            const value = ethers.parseEther('0.01');

            const blockBefore = await sei.getBlockNumber();
            const balanceBefore = await sei.getBalance(spender.address, blockBefore);
            expect(balanceBefore > value, 'pool account is pre-funded').to.equal(true);

            const receipt = await (
                await spender.wallet.sendTransaction({ to: recipient, value })
            ).wait();
            expect(receipt!.status).to.equal(1);

            const balanceAfter = await sei.getBalance(spender.address, 'latest');
            expect(balanceAfter < balanceBefore, 'sender was debited').to.equal(true);
            expect(
                balanceBefore - balanceAfter >= value,
                'at least the transferred value (plus gas) left the account',
            ).to.equal(true);

            // The historical read must not be rewritten by the later transfer.
            const balanceAtOldBlock = await sei.getBalance(spender.address, blockBefore);
            expect(balanceAtOldBlock, 'historical balance is immutable').to.equal(balanceBefore);
        });

        it('credits a fresh recipient and the credit is invisible before the funding block', async () => {
            const recipient = ethers.Wallet.createRandom().address;
            const value = ethers.parseEther('0.02');

            expect(await sei.getBalance(recipient, 'latest'), 'recipient starts empty').to.equal(0n);

            const receipt = await (
                await spender.wallet.sendTransaction({ to: recipient, value })
            ).wait();
            const fundingBlock = receipt!.blockNumber;

            expect(await sei.getBalance(recipient, fundingBlock), 'credited exactly value').to.equal(
                value,
            );
            expect(
                await sei.getBalance(recipient, fundingBlock - 1),
                'no balance one block before the funding tx',
            ).to.equal(0n);
        });
    });

    describe('block specifiers (EIP-1898)', () => {
        // Fund a fresh wallet with a known amount so every specifier form can be checked
        // against an exact, predictable balance rather than just against each other.
        let knownWallet: string;
        let knownBalance: bigint;
        let fundingBlock: number;
        let fundingBlockHash: string;

        before(async () => {
            knownWallet = ethers.Wallet.createRandom().address;
            knownBalance = ethers.parseEther('0.03');
            const receipt = await (
                await spender.wallet.sendTransaction({ to: knownWallet, value: knownBalance })
            ).wait();
            fundingBlock = receipt!.blockNumber;
            const block = await sei.getBlock(fundingBlock);
            expect(block, 'funding block should exist').to.not.equal(null);
            fundingBlockHash = block!.hash!;
        });

        it('a blockNumber object matches the numeric tag and the known funded balance', async () => {
            const [viaTag, viaObject] = await Promise.all([
                rawSei<string>('eth_getBalance', [knownWallet, ethers.toQuantity(fundingBlock)]),
                rawSei<string>('eth_getBalance', [
                    knownWallet,
                    { blockNumber: ethers.toQuantity(fundingBlock) },
                ]),
            ]);
            expect(viaObject.result, 'blockNumber object == numeric tag').to.equal(viaTag.result);
            expect(BigInt(viaObject.result!), 'resolves to the exact funded balance').to.equal(
                knownBalance,
            );
        });

        it('a blockHash object matches the numeric tag and the known funded balance', async () => {
            const [viaNumber, viaHash] = await Promise.all([
                rawSei<string>('eth_getBalance', [knownWallet, ethers.toQuantity(fundingBlock)]),
                rawSei<string>('eth_getBalance', [knownWallet, { blockHash: fundingBlockHash }]),
            ]);
            expect(viaHash.result, 'blockHash object == numeric tag').to.equal(viaNumber.result);
            expect(BigInt(viaHash.result!), 'resolves to the exact funded balance').to.equal(
                knownBalance,
            );
        });
    });

    describe('wrong params / error handling (parity with geth)', () => {
        it('empty params fail identically (-32602, missing required argument 0)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBalance', []),
                rawGeth('eth_getBalance', []),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 0/);
            expectSameError(s, g);
        });

        it('omitting the block argument fails identically (-32602, missing required argument 1)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBalance', [seiAdmin]),
                rawGeth('eth_getBalance', [gethAdmin]),
            ]);
            expectJsonRpcError(s, -32602, /missing value for required argument 1/);
            expectSameError(s, g);
        });

        it('too many positional args fail identically (-32602, want at most 2)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBalance', [seiAdmin, 'latest', {}]),
                rawGeth('eth_getBalance', [gethAdmin, 'latest', {}]),
            ]);
            expectJsonRpcError(s, -32602, /too many arguments, want at most 2/);
            expectSameError(s, g);
        });

        it('non-array params fail identically (-32602, non-array args)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBalance', { address: seiAdmin }),
                rawGeth('eth_getBalance', { address: gethAdmin }),
            ]);
            expectJsonRpcError(s, -32602, /^non-array args$/);
            expectSameError(s, g);
        });

        it('a malformed (too short) address fails identically (-32602, exact length message)', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_getBalance', ['0x1234', 'latest']),
                rawGeth('eth_getBalance', ['0x1234', 'latest']),
            ]);
            expectJsonRpcError(s, -32602, /hex string has length 4, want 40 for common\.Address/);
            expectSameError(s, g);
        });
    });
});
