/**
 * upgrade precompile (0x…1015) — x/upgrade queries against a live Sei chain.
 *
 * All four methods are views. A local cluster has no scheduled upgrade and no
 * applied plan, so currentPlan is the zero plan, appliedPlan of an unknown
 * name is 0, and upgradedConsensusState is empty bytes. moduleVersions reads
 * the app's real module consensus versions. currentPlan is checked against
 * LCD /cosmos/upgrade/v1beta1/current_plan.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc, rawSei } from '../utils/chainUtils';
import { cosmosRest } from '../utils/cosmosUtils';
import { EvmAccount } from '../utils/evmUtils';
import {
    PRECOMPILE_ADDRESSES,
    precompileContract,
    precompileInterface,
    callerContract,
    expectExecutionReverted,
} from '../utils/precompileUtils';
import { readRuntimeState, RuntimeState } from '../utils/testUtils';

interface LcdPlan {
    name?: string;
    height?: string;
    info?: string;
}

interface LcdCurrentPlan {
    plan?: LcdPlan | null;
}

describe('upgrade precompile (0x1015)', function () {
    this.timeout(120 * 1000);

    const provider = seiRpc();
    const upgradeIface = precompileInterface('upgrade');

    let runtime: RuntimeState;
    let admin: EvmAccount;
    let upgrade: ethers.Contract;
    let caller: ethers.Contract;

    before(() => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
        upgrade = precompileContract('upgrade', admin.wallet);
        caller = callerContract(runtime, admin.wallet);
    });

    describe('happy path & state parity', () => {
        it('currentPlan is the zero plan when none is scheduled (LCD parity)', async () => {
            const [plan, lcd] = await Promise.all([
                upgrade.currentPlan(),
                cosmosRest<LcdCurrentPlan>('/cosmos/upgrade/v1beta1/current_plan'),
            ]);
            const lcdPlan = lcd.plan;
            if (lcdPlan == null || lcdPlan.name == null || lcdPlan.name === '') {
                expect(plan.name).to.equal('');
                expect(plan.height).to.equal(0n);
                expect(plan.info).to.equal('');
            } else {
                expect(plan.name).to.equal(lcdPlan.name);
                expect(plan.height).to.equal(BigInt(lcdPlan.height ?? 0));
                expect(plan.info).to.equal(lcdPlan.info ?? '');
            }
        });

        it("moduleVersions('') returns a non-empty list", async () => {
            const versions: Array<{ name: string; version: bigint }> =
                await upgrade.moduleVersions('');
            expect(versions.length).to.be.greaterThan(0);
        });

        it("moduleVersions('bank') is a 1-element list named bank", async () => {
            // Go unit test (TestModuleVersions): a specific module name returns
            // exactly one entry. An unknown name reverts (strict filter).
            const versions: Array<{ name: string; version: bigint }> =
                await upgrade.moduleVersions('bank');
            expect(versions).to.have.length(1);
            expect(versions[0].name).to.equal('bank');
            expect(versions[0].version > 0n).to.equal(true);
        });

        it("appliedPlan('definitely-not-an-upgrade') returns 0", async () => {
            const height: bigint = await upgrade.appliedPlan('definitely-not-an-upgrade');
            expect(height).to.equal(0n);
        });

        it('upgradedConsensusState(1) returns empty bytes', async () => {
            const state: string = await upgrade.upgradedConsensusState(1n);
            expect(state).to.equal('0x');
        });
    });

    describe('error handling', () => {
        it('unknown module name reverts', async () => {
            await expectExecutionReverted(
                upgrade.moduleVersions('notamodule'),
                "upgrade.moduleVersions('notamodule')",
            );
        });

        it('rejects value (non-payable)', async () => {
            const envelope = await rawSei('eth_call', [
                {
                    from: admin.address,
                    to: PRECOMPILE_ADDRESSES.upgrade,
                    data: upgradeIface.encodeFunctionData('currentPlan', []),
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
            const data = upgradeIface.encodeFunctionData('currentPlan', []);
            const ret: string = await caller.staticcallTarget.staticCall(
                PRECOMPILE_ADDRESSES.upgrade,
                data,
            );
            const [decoded] = upgradeIface.decodeFunctionResult('currentPlan', ret);
            const direct = await upgrade.currentPlan();
            expect(decoded.name).to.equal(direct.name);
            expect(decoded.height).to.equal(direct.height);
            expect(decoded.info).to.equal(direct.info);
        });
    });
});
