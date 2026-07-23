/**
 * pointerview precompile (0x…100A) — pointer registry lookups.
 *
 * Lookups NEVER revert: unregistered (or garbage) keys return the tuple
 * (0x0, 0, false). This spec registers its own native pointer (for `uatom`,
 * which ships devnet genesis metadata; registration is an upsert, so it is
 * safe and address-stable regardless of what other specs did) — spec files
 * must not depend on each other's execution order.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc, rawSei, waitUntil } from '../utils/chainUtils';
import { EvmAccount } from '../utils/evmUtils';
import {
    PRECOMPILE_ADDRESSES,
    precompileContract,
    precompileInterface,
    callerContract,
} from '../utils/precompileUtils';
import { readRuntimeState, RuntimeState } from '../utils/testUtils';
import { ZERO_ADDRESS } from '../utils/constants';

describe('pointerview precompile (0x100A)', function () {
    this.timeout(120 * 1000);

    const provider = seiRpc();
    const viewIface = precompileInterface('pointerview');

    let runtime: RuntimeState;
    let admin: EvmAccount;
    let pointerview: ethers.Contract;
    let caller: ethers.Contract;
    let uatomPointer: string;

    before(async () => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
        pointerview = precompileContract('pointerview', provider);
        caller = callerContract(runtime, admin.wallet);

        // Own fixture: register (upsert) the native pointer for uatom.
        const pointer = precompileContract('pointer', admin.wallet);
        uatomPointer = await pointer.addNativePointer.staticCall('uatom');
        const tx = await pointer.addNativePointer('uatom', { gasLimit: 5_000_000 });
        expect((await tx.wait())!.status, 'uatom pointer registration must succeed').to.equal(1);
        await waitUntil(
            async () => {
                const [, , exists] = await pointerview.getNativePointer('uatom');
                return exists ? true : null;
            },
            { timeoutMs: 15_000, label: 'uatom pointer visible' },
        );
    });

    describe('happy path & state parity', () => {
        it('getNativePointer returns the registered pointer with a positive version', async () => {
            const [addr, version, exists] = await pointerview.getNativePointer('uatom');
            expect(exists).to.equal(true);
            expect(addr.toLowerCase()).to.equal(uatomPointer.toLowerCase());
            expect(version > 0n, 'registered pointer carries a version').to.equal(true);
        });

        it('unregistered lookups return (0x0, 0, false) without reverting', async () => {
            const cases: Array<[string, string]> = [
                ['getNativePointer', 'factory/sei1nonexistent/nope'],
                ['getCW20Pointer', 'sei1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq'],
                ['getCW721Pointer', 'complete garbage — not even bech32'],
                ['getCW1155Pointer', ''],
            ];
            for (const [method, key] of cases) {
                const [addr, version, exists] = await pointerview[method](key);
                expect(exists, `${method}(${JSON.stringify(key)}) exists flag`).to.equal(false);
                expect(addr, `${method} address`).to.equal(ZERO_ADDRESS);
                expect(version, `${method} version`).to.equal(0n);
            }
        });
    });

    describe('error handling', () => {
        it('rejects value (non-payable)', async () => {
            const envelope = await rawSei('eth_call', [
                {
                    from: admin.address,
                    to: PRECOMPILE_ADDRESSES.pointerview,
                    data: viewIface.encodeFunctionData('getNativePointer', ['uatom']),
                    value: '0x1',
                },
                'latest',
            ]);
            expect(envelope.error, 'value-bearing call must revert').to.not.equal(undefined);
            expect(envelope.error!.message).to.match(/execution reverted|revert/i);
        });
    });

    describe('dispatch semantics (via PrecompileCaller)', () => {
        it('responds through a real CALL, under STATICCALL and under DELEGATECALL', async () => {
            // pointerview has neither a readOnly nor a delegatecall guard —
            // all three dispatch paths return the same tuple (unlike staking
            // and distribution, whose views reject DELEGATECALL).
            const data = viewIface.encodeFunctionData('getNativePointer', ['uatom']);
            const [viaCall, viaStatic, viaDelegate] = await Promise.all([
                caller.callTarget.staticCall(PRECOMPILE_ADDRESSES.pointerview, data),
                caller.staticcallTarget.staticCall(PRECOMPILE_ADDRESSES.pointerview, data),
                caller.delegatecallTarget.staticCall(PRECOMPILE_ADDRESSES.pointerview, data),
            ]);
            expect(viaStatic).to.equal(viaCall);
            expect(viaDelegate).to.equal(viaCall);
            const [addr, , exists] = viewIface.decodeFunctionResult('getNativePointer', viaCall);
            expect(exists).to.equal(true);
            expect(addr.toLowerCase()).to.equal(uatomPointer.toLowerCase());
        });
    });
});
