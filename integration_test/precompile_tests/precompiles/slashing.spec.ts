/**
 * slashing precompile (0x…1014) — end-to-end semantics against a live Sei chain.
 *
 * Query methods are checked against the slashing module's LCD. Write methods
 * that would jail a validator are out of scope: grant/revoke of unjail
 * authorization are exercised from a pool account, and unjail is only asserted
 * to revert when the caller is not a jailed validator.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc, rawSei } from '../utils/chainUtils';
import { EvmAccount, associateViaTx } from '../utils/evmUtils';
import { cosmosRest } from '../utils/cosmosUtils';
import {
    PRECOMPILE_ADDRESSES,
    precompileContract,
    precompileInterface,
    callerContract,
    expectExecutionReverted,
    expectVmError,
} from '../utils/precompileUtils';
import { readRuntimeState, claimPool, RuntimeState } from '../utils/testUtils';

const EMPTY_PAGE = new Uint8Array();

interface LcdSlashingParams {
    params: {
        signed_blocks_window: string;
        min_signed_per_window: string;
        downtime_jail_duration: string;
        slash_fraction_double_sign: string;
        slash_fraction_downtime: string;
    };
}

/** protojson duration (`"600s"`) to whole seconds, matching the precompile's `.Seconds()`. */
function protoDurationSeconds(raw: string): bigint {
    const m = /^(-?[0-9]+(?:\.[0-9]+)?)s$/.exec(raw);
    if (!m) {
        throw new Error(`unexpected protojson duration: ${raw}`);
    }
    return BigInt(Math.trunc(Number(m[1])));
}

function asSigningInfo(info: ethers.Result) {
    return {
        validatorAddress: String(info.validatorAddress),
        startHeight: BigInt(info.startHeight),
        indexOffset: BigInt(info.indexOffset),
        jailedUntil: BigInt(info.jailedUntil),
        tombstoned: Boolean(info.tombstoned),
        missedBlocksCounter: BigInt(info.missedBlocksCounter),
    };
}

describe('slashing precompile (0x1014)', function () {
    this.timeout(180 * 1000);

    const provider = seiRpc();
    const slashingIface = precompileInterface('slashing');

    let runtime: RuntimeState;
    let admin: EvmAccount;
    let slashing: ethers.Contract;
    let caller: ethers.Contract;
    let associated: EvmAccount;

    before(async () => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
        slashing = precompileContract('slashing', admin.wallet);
        caller = callerContract(runtime, admin.wallet);
        [associated] = claimPool(runtime, provider, 1, 'slashing:associated');
        await associateViaTx(associated);
    });

    describe('happy path & state parity', () => {
        it('params() matches LCD /cosmos/slashing/v1beta1/params', async () => {
            const [viaPrecompile, lcd] = await Promise.all([
                slashing.params() as Promise<ethers.Result>,
                cosmosRest<LcdSlashingParams>('/cosmos/slashing/v1beta1/params'),
            ]);
            const p = lcd.params;
            expect(viaPrecompile.signedBlocksWindow).to.equal(BigInt(p.signed_blocks_window));
            expect(viaPrecompile.minSignedPerWindow).to.equal(p.min_signed_per_window);
            expect(viaPrecompile.downtimeJailDuration).to.equal(
                protoDurationSeconds(p.downtime_jail_duration),
            );
            expect(viaPrecompile.slashFractionDoubleSign).to.equal(p.slash_fraction_double_sign);
            expect(viaPrecompile.slashFractionDowntime).to.equal(p.slash_fraction_downtime);
        });

        it('signingInfos(empty) is non-empty and signingInfo(that cons address) matches', async () => {
            const listed: ethers.Result = await slashing.signingInfos(EMPTY_PAGE);
            expect(listed.signingInfos.length, 'devnet validators must have signing info').to.be.greaterThan(
                0,
            );
            const first = listed.signingInfos[0];
            const viaOne: ethers.Result = await slashing.signingInfo(first.validatorAddress);
            expect(asSigningInfo(viaOne)).to.deep.equal(asSigningInfo(first));
        });

        it('grantUnjailAuthorization then revokeUnjailAuthorization succeed from an associated pool account', async () => {
            const expiration = BigInt(Math.floor(Date.now() / 1000) + 86_400);
            const grantTx = await (
                slashing.connect(associated.wallet) as ethers.Contract
            ).grantUnjailAuthorization(admin.address, expiration, { gasLimit: 1_000_000 });
            expect((await grantTx.wait())!.status, 'grantUnjailAuthorization tx must succeed').to.equal(
                1,
            );

            const revokeTx = await (
                slashing.connect(associated.wallet) as ethers.Contract
            ).revokeUnjailAuthorization(admin.address, { gasLimit: 1_000_000 });
            expect((await revokeTx.wait())!.status, 'revokeUnjailAuthorization tx must succeed').to.equal(
                1,
            );
        });
    });

    describe('error handling', () => {
        // The caller is associated but is not a validator operator, so the
        // MsgUnjail is rejected by the slashing keeper rather than by the
        // precompile's association check.
        it('unjail from an account that owns no validator reverts', async () => {
            await expectVmError(
                (slashing.connect(associated.wallet) as ethers.Contract).unjail({
                    gasLimit: 1_000_000,
                }),
                'address is not associated with any known validator',
            );
        });

        it('signingInfo of a garbage cons address reverts', async () => {
            await expectExecutionReverted(
                slashing.signingInfo('notanaddress'),
                'slashing.signingInfo with a garbage cons address',
            );
        });

        it('unjailWithAuthorization without a grant reverts', async () => {
            await expectVmError(
                (slashing.connect(associated.wallet) as ethers.Contract).unjailWithAuthorization(
                    admin.address,
                    { gasLimit: 1_000_000 },
                ),
                'authorization not found',
            );
        });

        it('rejects value on a view method (non-payable)', async () => {
            const envelope = await rawSei('eth_call', [
                {
                    from: admin.address,
                    to: PRECOMPILE_ADDRESSES.slashing,
                    data: slashingIface.encodeFunctionData('params', []),
                    value: '0x1',
                },
                'latest',
            ]);
            expect(envelope.error, 'value-bearing call must revert').to.not.equal(undefined);
            expect(envelope.error!.message).to.match(/execution reverted|revert/i);
        });

        it('unjail via STATICCALL reverts', async () => {
            const data = slashingIface.encodeFunctionData('unjail', []);
            await expectVmError(
                caller.getFunction('staticcallTarget').send(PRECOMPILE_ADDRESSES.slashing, data, {
                    gasLimit: 1_000_000,
                }),
                'cannot call slashing precompile from staticcall',
            );
        });

        // The delegatecall guard is the security-relevant one: it stops a
        // contract from unjailing on behalf of whoever called *it*. The
        // executor checks it before the readOnly check, so this reports the
        // delegatecall reason rather than the staticcall one.
        it('unjail via DELEGATECALL reverts', async () => {
            const data = slashingIface.encodeFunctionData('unjail', []);
            await expectVmError(
                caller.getFunction('delegatecallTarget').send(PRECOMPILE_ADDRESSES.slashing, data, {
                    gasLimit: 1_000_000,
                }),
                'cannot delegatecall slashing',
            );
        });
    });

    describe('dispatch semantics (via PrecompileCaller)', () => {
        it('params responds under STATICCALL', async () => {
            const data = slashingIface.encodeFunctionData('params', []);
            const ret: string = await caller.staticcallTarget.staticCall(
                PRECOMPILE_ADDRESSES.slashing,
                data,
            );
            const [decoded] = slashingIface.decodeFunctionResult('params', ret);
            const direct: ethers.Result = await slashing.params();
            expect(decoded.signedBlocksWindow).to.equal(direct.signedBlocksWindow);
            expect(decoded.minSignedPerWindow).to.equal(direct.minSignedPerWindow);
            expect(decoded.downtimeJailDuration).to.equal(direct.downtimeJailDuration);
            expect(decoded.slashFractionDoubleSign).to.equal(direct.slashFractionDoubleSign);
            expect(decoded.slashFractionDowntime).to.equal(direct.slashFractionDowntime);
        });
    });
});
