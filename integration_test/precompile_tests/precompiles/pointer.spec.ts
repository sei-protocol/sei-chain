/**
 * pointer precompile (0x…100b) — retired.
 *
 * Every method refuses. The ABI and the registration at 0x…100b are kept so the four
 * selectors still decode and revert; at an unregistered address the same call would
 * succeed against empty code and return no data.
 *
 * Fixture: a denom carrying bank metadata, read from the bank module — bank metadata
 * is what the creation path used to require. A refusal here is therefore the
 * retirement rather than the missing-metadata rejection that preceded it.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc } from '../utils/chainUtils';
import { EvmAccount } from '../utils/evmUtils';
import { denomWithMetadata } from '../utils/cosmosUtils';
import {
    PRECOMPILE_ADDRESSES,
    precompileContract,
    precompileInterface,
    callerContract,
    expectExecutionReverted,
    expectVmError,
} from '../utils/precompileUtils';
import { readRuntimeState, RuntimeState } from '../utils/testUtils';

const RETIRED = 'pointer precompile is retired';

describe('pointer precompile (0x100b)', function () {
    this.timeout(180 * 1000);

    const provider = seiRpc();
    const pointerIface = precompileInterface('pointer');

    let runtime: RuntimeState;
    let admin: EvmAccount;
    let pointer: ethers.Contract;
    let pointerview: ethers.Contract;
    let caller: ethers.Contract;
    let denom: string;

    before(async () => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
        pointer = precompileContract('pointer', admin.wallet);
        pointerview = precompileContract('pointerview', provider);
        caller = callerContract(runtime, admin.wallet);
        denom = await denomWithMetadata();
    });

    describe('creation is refused', () => {
        it('addNativePointer reverts for a metadata-backed denom and registers nothing', async () => {
            await expectVmError(
                pointer.addNativePointer(denom, { gasLimit: 5_000_000 }),
                RETIRED,
            );
            const [, , exists] = await pointerview.getNativePointer(denom);
            expect(exists, 'no pointer may be registered').to.equal(false);
        });

        it('the CW methods revert', async () => {
            for (const method of ['addCW20Pointer', 'addCW721Pointer', 'addCW1155Pointer']) {
                await expectExecutionReverted(
                    pointer[method].staticCall(runtime.funded.adminSeiAddress),
                    `pointer.${method}`,
                );
            }
        });

        it('rejects value despite the payable ABI declaration', async () => {
            await expectExecutionReverted(
                pointer.addNativePointer.staticCall(denom, { value: 10n ** 12n }),
                'pointer.addNativePointer with value',
            );
        });
    });

    describe('dispatch semantics (via PrecompileCaller)', () => {
        it('refuses alike under CALL, STATICCALL and DELEGATECALL', async () => {
            const data = pointerIface.encodeFunctionData('addNativePointer', [denom]);
            for (const entrypoint of ['callTarget', 'staticcallTarget', 'delegatecallTarget']) {
                await expectExecutionReverted(
                    caller.getFunction(entrypoint).staticCall(PRECOMPILE_ADDRESSES.pointer, data),
                    `pointer via ${entrypoint}`,
                );
            }
        });
    });
});
