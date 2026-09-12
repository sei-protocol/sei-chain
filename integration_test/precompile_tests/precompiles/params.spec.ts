/**
 * params precompile (0x…1013) — subspace parameter lookups against a live Sei chain.
 *
 * One view method: params(subspace, key) returns the stored value as a string.
 * The Go unit test pins staking/MaxValidators as a decimal encoding of the
 * staking keeper's MaxValidators; this spec uses the same key and asserts
 * parity against cosmosQuery().staking.params().
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc, rawSei } from '../utils/chainUtils';
import { cosmosQuery } from '../utils/cosmosUtils';
import { EvmAccount } from '../utils/evmUtils';
import {
    PRECOMPILE_ADDRESSES,
    precompileContract,
    precompileInterface,
    callerContract,
    expectExecutionReverted,
} from '../utils/precompileUtils';
import { readRuntimeState, RuntimeState } from '../utils/testUtils';

describe('params precompile (0x1013)', function () {
    this.timeout(120 * 1000);

    const provider = seiRpc();
    const paramsIface = precompileInterface('params');

    let runtime: RuntimeState;
    let admin: EvmAccount;
    let params: ethers.Contract;
    let caller: ethers.Contract;

    before(() => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
        params = precompileContract('params', admin.wallet);
        caller = callerContract(runtime, admin.wallet);
    });

    describe('happy path & state parity', () => {
        it("params('staking', 'MaxValidators') matches the staking module", async () => {
            const [value, qc] = await Promise.all([
                params.params('staking', 'MaxValidators') as Promise<string>,
                cosmosQuery(),
            ]);
            const expected = String((await qc.staking.params()).params!.maxValidators);
            expect(value).to.match(/^\d+$/);
            expect(value).to.equal(expected);
        });
    });

    describe('error handling', () => {
        it('unknown subspace reverts', async () => {
            await expectExecutionReverted(
                params.params('notasubspace', 'NotAKey'),
                "params.params with subspace 'notasubspace'",
            );
        });

        it('empty subspace reverts', async () => {
            await expectExecutionReverted(
                params.params('', 'MaxValidators'),
                'params.params with an empty subspace',
            );
        });

        it('rejects value (non-payable)', async () => {
            const envelope = await rawSei('eth_call', [
                {
                    from: admin.address,
                    to: PRECOMPILE_ADDRESSES.params,
                    data: paramsIface.encodeFunctionData('params', ['staking', 'MaxValidators']),
                    value: '0x1',
                },
                'latest',
            ]);
            expect(envelope.error, 'value-bearing call must revert').to.not.equal(undefined);
            expect(envelope.error!.message).to.match(/execution reverted|revert/i);
        });
    });

    describe('dispatch semantics (via PrecompileCaller)', () => {
        it('responds under STATICCALL', async () => {
            const data = paramsIface.encodeFunctionData('params', ['staking', 'MaxValidators']);
            const ret: string = await caller.staticcallTarget.staticCall(
                PRECOMPILE_ADDRESSES.params,
                data,
            );
            const [decoded] = paramsIface.decodeFunctionResult('params', ret);
            const direct: string = await params.params('staking', 'MaxValidators');
            expect(decoded).to.equal(direct);
        });
    });
});
