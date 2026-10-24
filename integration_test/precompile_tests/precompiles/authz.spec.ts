/**
 * authz precompile (0x…100E) — retirement assertion.
 *
 * The authz module is removed and its precompile is retired: all three grant
 * queries unconditionally revert with "authz precompile is retired; authz grant
 * queries are disabled". Like the oracle precompile it returns REAL
 * Error(string) revert data, so the reason is visible directly in eth_call
 * errors — this spec pins that too.
 *
 * One ordering nuance: the non-payable check runs BEFORE the retirement
 * revert, so a value-bearing call reverts WITHOUT the retirement reason.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc, rawSei } from '../utils/chainUtils';
import { EvmAccount } from '../utils/evmUtils';
import {
    PRECOMPILE_ADDRESSES,
    PRECOMPILE_CALLER_ABI,
    precompileInterface,
    getVmError,
} from '../utils/precompileUtils';
import { readRuntimeState, RuntimeState } from '../utils/testUtils';

const RETIRED = /authz precompile is retired/;
const emptyPage = new Uint8Array();

describe('authz precompile (0x100E)', function () {
    this.timeout(120 * 1000);

    const provider = seiRpc();
    const authzIface = precompileInterface('authz');

    let runtime: RuntimeState;
    let admin: EvmAccount;

    const expectRetired = async (data: string, to: string = PRECOMPILE_ADDRESSES.authz) => {
        const envelope = await rawSei('eth_call', [{ to, data }, 'latest']);
        expect(envelope.error, 'retired method must revert').to.not.equal(undefined);
        expect(envelope.error!.message, 'retirement reason surfaces in eth_call').to.match(RETIRED);
    };

    before(() => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
    });

    describe('error handling (retirement)', () => {
        it('grants reverts with the retirement reason', async () => {
            await expectRetired(
                authzIface.encodeFunctionData('grants', [admin.address, admin.address, '', emptyPage]),
            );
        });

        it('granterGrants reverts with the retirement reason', async () => {
            await expectRetired(authzIface.encodeFunctionData('granterGrants', [admin.address, emptyPage]));
        });

        it('granteeGrants reverts with the retirement reason', async () => {
            await expectRetired(authzIface.encodeFunctionData('granteeGrants', [admin.address, emptyPage]));
        });

        it('a value-bearing call reverts on the non-payable check, without the retirement reason', async () => {
            const envelope = await rawSei('eth_call', [
                {
                    from: admin.address,
                    to: PRECOMPILE_ADDRESSES.authz,
                    data: authzIface.encodeFunctionData('granterGrants', [admin.address, emptyPage]),
                    value: '0x1',
                },
                'latest',
            ]);
            expect(envelope.error, 'value-bearing call must revert').to.not.equal(undefined);
            expect(envelope.error!.message).to.match(/execution reverted|revert/i);
            expect(
                envelope.error!.message,
                'the non-payable check fires before the retirement revert',
            ).to.not.match(RETIRED);
        });

        it('a mined tx fails with the retirement reason in its VmError', async () => {
            const tx = await admin.wallet.sendTransaction({
                to: PRECOMPILE_ADDRESSES.authz,
                data: authzIface.encodeFunctionData('granterGrants', [admin.address, emptyPage]),
                gasLimit: 200_000,
            });
            const receipt = await tx.wait().catch((e: any) => e.receipt);
            expect(receipt.status, 'tx must fail').to.equal(0);
            expect(await getVmError(receipt.hash)).to.match(RETIRED);
        });
    });

    describe('dispatch semantics (via PrecompileCaller)', () => {
        it('the retirement reason bubbles through CALL, STATICCALL and DELEGATECALL', async () => {
            const inner = authzIface.encodeFunctionData('granterGrants', [admin.address, emptyPage]);
            const callerIface = new ethers.Interface(PRECOMPILE_CALLER_ABI as unknown as string[]);
            for (const fn of ['callTarget', 'staticcallTarget', 'delegatecallTarget']) {
                await expectRetired(
                    callerIface.encodeFunctionData(fn, [PRECOMPILE_ADDRESSES.authz, inner]),
                    runtime.contracts.precompileCaller,
                );
            }
        });
    });
});
