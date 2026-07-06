import { ethers } from 'ethers';
import { expect } from 'chai';
import { bothProviders, rawSei, rawGeth, expectJsonRpcError } from '../utils/chainUtils';
import { readRuntimeState, RuntimeState, claimPool, expectSameError } from '../utils/testUtils';
import { EvmAccount } from '../utils/evmUtils';
import { signRawTransfer, sendRaw, signBelowIntrinsicTx, assertRawTxMatches } from '../utils/txUtils';

const TX_TYPES = [0, 1, 2] as const;
const TYPE_NAME: Record<number, string> = { 0: 'legacy', 1: 'access-list', 2: 'EIP-1559' };

describe('eth_sendRawTransaction', function () {
    this.timeout(180 * 1000);

    const { sei, geth } = bothProviders();

    let runtime: RuntimeState;
    let sender: EvmAccount;
    let nonceTester: EvmAccount;
    let intrinsicTester: EvmAccount;
    let gethSender: EvmAccount;

    before(async () => {
        runtime = readRuntimeState();
        [sender, nonceTester, intrinsicTester] = claimPool(runtime, sei, 3, 'eth_sendRawTransaction');
        gethSender = EvmAccount.fromPrivateKey(runtime.funded.gethAdmin.privateKey, geth);
    });

    describe('happy path: accepts the canonical encoding of every tx type', () => {
        for (const type of TX_TYPES) {
            it(`accepts a ${TYPE_NAME[type]} (type ${type}) transfer and returns its hash`, async () => {
                const signed = await signRawTransfer(sei, sender, type);
                const returned = await sendRaw(sei, signed.raw);
                expect(returned, 'returned hash == keccak256(raw)').to.equal(signed.hash);

                const receipt = await sei.waitForTransaction(signed.hash, 1, 60_000);
                expect(receipt!.status, 'mined successfully').to.equal(1);

                const txObject = await sei.send('eth_getTransactionByHash', [signed.hash]);
                expect(Number(txObject.type), 'tx type preserved').to.equal(type);
                assertRawTxMatches(signed.raw, txObject);
            });
        }
    });

    describe('wrong params / error handling', () => {
        it('[divergence] both reject a below-intrinsic-gas tx; geth is descriptive, Sei is generic', async () => {
            const [seiTx, gethTx] = await Promise.all([
                signBelowIntrinsicTx(sei, intrinsicTester),
                signBelowIntrinsicTx(geth, gethSender),
            ]);
            const [s, g] = await Promise.all([
                rawSei('eth_sendRawTransaction', [seiTx.raw]),
                rawGeth('eth_sendRawTransaction', [gethTx.raw]),
            ]);
            // geth rejects pre-execution with the exact reason; Sei rejects too (same -32000 code)
            // but its ante surfaces a generic ABCI error rather than the descriptive message.
            expectJsonRpcError(g, -32000, /intrinsic gas too low/);
            expect(s.error, 'Sei rejects the below-intrinsic tx').to.not.equal(undefined);
            expect(s.error!.code, 'both use -32000').to.equal(g.error!.code);
            expect(s.error!.message, '[divergence] Sei does not surface the geth reason').to.not.equal(
                g.error!.message,
            );
        });

        it('rejects malformed transaction bytes identically to geth', async () => {
            const [s, g] = await Promise.all([
                rawSei('eth_sendRawTransaction', ['0xdeadbeef']),
                rawGeth('eth_sendRawTransaction', ['0xdeadbeef']),
            ]);
            expect(s.error, 'sei rejects garbage bytes').to.not.equal(undefined);
            expectSameError(s, g);
        });

        it('rejects a tx whose nonce is already used (stale nonce)', async () => {
            // Consume nonce 0, then re-submit a freshly signed tx pinned to nonce 0. Sei's Cosmos
            // ante reports "incorrect account sequence" where geth would say "nonce too low".
            const first = await signRawTransfer(sei, nonceTester, 2, { nonce: 0 });
            await sendRaw(sei, first.raw);
            await sei.waitForTransaction(first.hash, 1, 60_000);

            const stale = await signRawTransfer(sei, nonceTester, 2, { nonce: 0 });
            const res = await rawSei('eth_sendRawTransaction', [stale.raw]);
            expect(res.error, JSON.stringify(res)).to.not.equal(undefined);
            expect(res.error!.message, 'stale-nonce signature').to.match(
                /incorrect account sequence|nonce too low|already known/i,
            );
        });

        it('a missing transaction argument is rejected with -32602', async () => {
            const res = await rawSei('eth_sendRawTransaction', []);
            expectJsonRpcError(res, -32602, /missing value for required argument 0/);
        });

        it('[replay-protection] the same signed tx rejected on second submission', async () => {
            // After the tx mines, re-submitting the identical raw bytes must hit replay protection.
            const signed = await signRawTransfer(sei, sender, 2);
            await sendRaw(sei, signed.raw);
            await sei.waitForTransaction(signed.hash, 1, 60_000);

            const replayRes = await rawSei('eth_sendRawTransaction', [signed.raw]);
            expect(replayRes.error, 'replay must be rejected').to.not.equal(undefined);
            // Sei dedups in the mempool cache ("tx already exists in cache") before the nonce
            // check, so accept that alongside the canonical nonce/replay rejection reasons.
            expect(replayRes.error!.message, 'replay rejection reason').to.match(
                /incorrect account sequence|nonce too low|already known|tx already exists in cache/i,
            );
        });

        it('[chain-id-mismatch] a tx signed for chain ID 1 (mainnet) is rejected', async () => {
            // A transfer signed for chain ID 1 (Ethereum mainnet): EIP-155 replay protection must make Sei reject it.
            const feeData = await sei.getFeeData();
            const network = await sei.getNetwork();
            const wallet = ethers.Wallet.createRandom().connect(sei);
            // Fund the wallet first so the tx could otherwise be valid.
            await (await sender.wallet.sendTransaction({ to: wallet.address, value: ethers.parseEther('0.1') })).wait();

            const wrongChainId = network.chainId === 1n ? 2n : 1n;
            const tx = await wallet.signTransaction({
                to: ethers.Wallet.createRandom().address,
                value: 0n,
                type: 2,
                chainId: wrongChainId,
                nonce: 0,
                gasLimit: 21000n,
                maxFeePerGas: feeData.maxFeePerGas! * 2n,
                maxPriorityFeePerGas: feeData.maxPriorityFeePerGas!,
            });
            const res = await rawSei('eth_sendRawTransaction', [tx]);
            expect(res.error, 'wrong chain ID must be rejected').to.not.equal(undefined);
        });
    });
});
