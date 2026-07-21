/**
 * p256 precompile (0x…1011) — RIP-7212-style secp256r1 verification.
 *
 * The ABI wraps the raw RIP-7212 layout: verify(bytes) where the argument is
 * exactly 160 bytes = hash(32) || r(32) || s(32) || x(32) || y(32). A VALID
 * signature returns the 32-byte word 1; an INVALID signature returns EMPTY
 * output from a SUCCESSFUL call (not a revert) — so assertions go through raw
 * eth_call: ethers' decoder would throw BAD_DATA on the empty case.
 *
 * Test vectors are generated with node:crypto (P-256 keygen + ieee-p1363
 * signatures); the precompile does no hashing itself — the first 32 bytes are
 * the already-computed message digest.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { generateKeyPairSync, sign, createHash } from 'node:crypto';
import { seiRpc, rawSei } from '../utils/chainUtils';
import { EvmAccount } from '../utils/evmUtils';
import {
    PRECOMPILE_ADDRESSES,
    precompileInterface,
    callerContract,
    expectVmError,
} from '../utils/precompileUtils';
import { readRuntimeState, RuntimeState } from '../utils/testUtils';

const VALID_WORD = '0x' + '00'.repeat(31) + '01';

/** A fresh valid 160-byte verify() input: sha256(msg) || r || s || x || y. */
function validVector(): Buffer {
    const { publicKey, privateKey } = generateKeyPairSync('ec', { namedCurve: 'P-256' });
    const message = Buffer.from('sei p256 precompile e2e');
    const rs = sign('sha256', message, { key: privateKey, dsaEncoding: 'ieee-p1363' });
    const hash = createHash('sha256').update(message).digest();
    const jwk = publicKey.export({ format: 'jwk' });
    return Buffer.concat([
        hash,
        rs,
        Buffer.from(jwk.x!, 'base64url'),
        Buffer.from(jwk.y!, 'base64url'),
    ]);
}

describe('p256 precompile (0x1011)', function () {
    this.timeout(120 * 1000);

    const provider = seiRpc();
    const p256Iface = precompileInterface('p256');

    let runtime: RuntimeState;
    let admin: EvmAccount;
    let caller: ethers.Contract;

    const rawVerify = async (input: Buffer): Promise<string> => {
        const envelope = await rawSei<string>('eth_call', [
            {
                to: PRECOMPILE_ADDRESSES.p256,
                data: p256Iface.encodeFunctionData('verify', [input]),
            },
            'latest',
        ]);
        if (envelope.error) {
            throw new Error(`verify eth_call failed: ${envelope.error.message}`);
        }
        return envelope.result!;
    };

    before(() => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
        caller = callerContract(runtime, admin.wallet);
    });

    describe('happy path', () => {
        it('a valid signature verifies to the 32-byte word 1', async () => {
            const result = await rawVerify(validVector());
            const [decoded] = p256Iface.decodeFunctionResult('verify', result);
            expect(decoded).to.equal(VALID_WORD);
        });
    });

    describe('invalid signatures return empty output, not a revert', () => {
        it('a tampered message hash yields empty output', async () => {
            const input = validVector();
            input[0] ^= 0xff;
            expect(await rawVerify(input)).to.equal('0x');
        });

        it('a tampered s component yields empty output', async () => {
            const input = validVector();
            input[80] ^= 0xff; // middle of s (bytes 64..96)
            expect(await rawVerify(input)).to.equal('0x');
        });

        it('a public key not on the curve yields empty output', async () => {
            const input = validVector();
            input[159] ^= 0x01; // nudge y off the curve
            expect(await rawVerify(input)).to.equal('0x');
        });
    });

    describe('error handling', () => {
        it('input shorter than 160 bytes reverts', async () => {
            const envelope = await rawSei<string>('eth_call', [
                {
                    to: PRECOMPILE_ADDRESSES.p256,
                    data: p256Iface.encodeFunctionData('verify', [validVector().subarray(0, 159)]),
                },
                'latest',
            ]);
            expect(envelope.error, 'short input must revert').to.not.equal(undefined);
            expect(envelope.error!.message).to.match(/execution reverted|revert/i);
        });

        it('a mined failing tx carries the exact Go error in its VmError', async () => {
            const data = p256Iface.encodeFunctionData('verify', [validVector().subarray(0, 100)]);
            await expectVmError(
                admin.wallet.sendTransaction({
                    to: PRECOMPILE_ADDRESSES.p256,
                    data,
                    gasLimit: 200_000,
                }),
                'invalid input length',
            );
        });

        it('rejects value (non-payable)', async () => {
            const envelope = await rawSei('eth_call', [
                {
                    from: admin.address,
                    to: PRECOMPILE_ADDRESSES.p256,
                    data: p256Iface.encodeFunctionData('verify', [validVector()]),
                    value: '0x1',
                },
                'latest',
            ]);
            expect(envelope.error, 'value-bearing call must revert').to.not.equal(undefined);
            expect(envelope.error!.message).to.match(/execution reverted|revert/i);
        });
    });

    describe('dispatch semantics (via PrecompileCaller)', () => {
        it('verifies under STATICCALL (no readOnly guard)', async () => {
            const data = p256Iface.encodeFunctionData('verify', [validVector()]);
            const ret: string = await caller.staticcallTarget.staticCall(
                PRECOMPILE_ADDRESSES.p256,
                data,
            );
            const [decoded] = p256Iface.decodeFunctionResult('verify', ret);
            expect(decoded).to.equal(VALID_WORD);
        });

        it('is rejected under DELEGATECALL', async () => {
            const data = p256Iface.encodeFunctionData('verify', [validVector()]);
            await expectVmError(
                caller.getFunction('delegatecallTarget').send(PRECOMPILE_ADDRESSES.p256, data, {
                    gasLimit: 500_000,
                }),
                'cannot delegatecall P256Verify',
            );
        });
    });
});
