/**
 * staking precompile (0x…1005) — end-to-end semantics against a live Sei chain.
 *
 * Boundary note: rpc_tests already asserts how staking-precompile txs surface
 * through eth_* endpoints (validators() decode via eth_call, Delegate logs via
 * eth_getLogs, estimateGas). This spec owns the Cosmos-side state effects of
 * delegate/undelegate/redelegate/createValidator/editValidator, query-method
 * parity vs the staking module, error handling, and dispatch guards.
 *
 * Exact Go error strings only surface via eth_getVMError on mined failing txs
 * (eth_call rewrites precompile errors to a bare "execution reverted"), so the
 * error-handling section mines deliberate failures with explicit gas limits.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { generateKeyPairSync } from 'node:crypto';
import { fromBech32, toBech32 } from '@cosmjs/encoding';
import { seiRpc, waitUntil } from '../utils/chainUtils';
import { EvmAccount, associateViaTx } from '../utils/evmUtils';
import { bondedValidators, cosmosQuery, cosmosRest, bankBalance } from '../utils/cosmosUtils';
import {
    PRECOMPILE_ADDRESSES,
    precompileContract,
    precompileInterface,
    callerContract,
    expectExecutionReverted,
    expectVmError,
    getVmError,
    traceTransaction,
} from '../utils/precompileUtils';
import { readRuntimeState, claimPool, RuntimeState } from '../utils/testUtils';
import { WEI_PER_USEI } from '../utils/constants';

const DELEGATE_WEI = ethers.parseEther('1'); // == 1_000_000 usei exactly
const DELEGATE_USEI = DELEGATE_WEI / WEI_PER_USEI;

describe('staking precompile (0x1005)', function () {
    this.timeout(180 * 1000);

    const provider = seiRpc();
    const stakingIface = precompileInterface('staking');

    let runtime: RuntimeState;
    let admin: EvmAccount;
    let staking: ethers.Contract;
    let caller: ethers.Contract;
    let validators: string[];
    let delegator: EvmAccount;

    before(async () => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
        staking = precompileContract('staking', admin.wallet);
        caller = callerContract(runtime, admin.wallet);
        validators = await bondedValidators();
        expect(validators.length, 'devnet must have >=2 bonded validators').to.be.greaterThan(1);
        [delegator] = claimPool(runtime, provider, 1, 'staking:delegator');
        await associateViaTx(delegator);
    });

    describe('happy path & state parity', () => {
        it('delegate stakes msg.value and the staking module sees the delegation', async () => {
            const tx = await (staking.connect(delegator.wallet) as ethers.Contract).delegate(
                validators[0],
                { value: DELEGATE_WEI, gasLimit: 1_000_000 },
            );
            const receipt = await tx.wait();
            expect(receipt!.status, 'delegate tx must succeed').to.equal(1);

            // Legacy scar tissue: the delegation query can race right after the
            // delegate tx on CI — poll instead of asserting immediately.
            const qc = await cosmosQuery();
            const cosmosDelegation = await waitUntil(
                async () => {
                    const d = await qc.staking.delegation(delegator.seiAddress(), validators[0]);
                    return d.delegationResponse?.balance ?? null;
                },
                { timeoutMs: 30_000, label: 'cosmos delegation after delegate' },
            );
            expect(cosmosDelegation.amount, 'staking module records the usei amount').to.equal(
                DELEGATE_USEI.toString(),
            );
            expect(cosmosDelegation.denom).to.equal('usei');
        });

        it('delegate emits the Delegate event (amount in wei) plus a rewards-withdrawn log', async () => {
            const tx = await (staking.connect(delegator.wallet) as ethers.Contract).delegate(
                validators[0],
                { value: DELEGATE_WEI, gasLimit: 1_000_000 },
            );
            const receipt = await tx.wait();

            const parsed = receipt!.logs.map((l: ethers.Log) => {
                try {
                    return stakingIface.parseLog({ topics: [...l.topics], data: l.data });
                } catch {
                    return null;
                }
            });
            const delegateEvent = parsed.find((p: any) => p?.name === 'Delegate');
            expect(delegateEvent, 'Delegate event must be emitted').to.not.equal(undefined);
            expect(delegateEvent!.args[0]).to.equal(delegator.address);
            expect(delegateEvent!.args[1]).to.equal(validators[0]);
            // The Delegate event reports the raw wei msg.value, not usei.
            expect(delegateEvent!.args[2]).to.equal(DELEGATE_WEI);

            const rewardsEvent = parsed.find((p: any) => p?.name === 'DelegationRewardsWithdrawn');
            expect(
                rewardsEvent,
                'delegate implicitly withdraws rewards and logs it',
            ).to.not.equal(undefined);
        });

        it('delegation query matches the staking module exactly', async () => {
            const [viaPrecompile, qc] = await Promise.all([
                staking.delegation(delegator.address, validators[0]),
                cosmosQuery(),
            ]);
            const viaCosmos = await qc.staking.delegation(delegator.seiAddress(), validators[0]);

            expect(viaPrecompile.balance.amount).to.equal(
                BigInt(viaCosmos.delegationResponse!.balance!.amount),
            );
            expect(viaPrecompile.balance.denom).to.equal('usei');
            expect(viaPrecompile.delegation.delegator_address).to.equal(delegator.seiAddress());
            expect(viaPrecompile.delegation.validator_address).to.equal(validators[0]);
        });

        it('undelegate creates an unbonding entry and funds return after unbonding_time', async () => {
            const half = DELEGATE_USEI; // one of the two 1-SEI delegations above

            // Legacy scar tissue: undelegate gas fluctuates block-to-block on CI
            // (staking queue writes) — give it a generous explicit limit up front.
            const tx = await (staking.connect(delegator.wallet) as ethers.Contract).undelegate(
                validators[0],
                half,
                { gasLimit: 2_000_000 },
            );
            const receipt = await tx.wait();
            expect(receipt!.status, 'undelegate tx must succeed').to.equal(1);

            // Baseline AFTER the undelegate (its gas and implicit reward
            // withdrawal already applied) but before the 10s maturity.
            const balanceBeforeMaturity = await bankBalance(delegator.seiAddress());

            // The devnet's unbonding_time is 10s — poll for the entry before it
            // matures (the RPC node can lag the block that included the tx).
            // NB: the component is named `entries`, which collides with
            // Array.prototype.entries on ethers' Result — use getValue().
            const entries = await waitUntil(
                async () => {
                    const ubd = await staking.unbondingDelegation(
                        delegator.address,
                        validators[0],
                    );
                    const list = ubd.getValue('entries') as ethers.Result;
                    return list.length > 0 ? list : null;
                },
                { timeoutMs: 8_000, intervalMs: 200, label: 'unbonding entry before maturity' },
            );
            expect(BigInt(entries[0].balance), 'unbonding entry carries the amount').to.equal(half);

            // After ~10s the staking EndBlocker credits the delegator's bank balance.
            await waitUntil(
                async () => {
                    const b = await bankBalance(delegator.seiAddress());
                    return b >= balanceBeforeMaturity + half ? b : null;
                },
                { timeoutMs: 45_000, label: 'unbonded funds returned to bank balance' },
            );
        });

        it('redelegate moves stake to a second validator', async () => {
            const moved = DELEGATE_USEI / 2n;
            const tx = await (staking.connect(delegator.wallet) as ethers.Contract).redelegate(
                validators[0],
                validators[1],
                moved,
                { gasLimit: 2_000_000 },
            );
            const receipt = await tx.wait();
            expect(receipt!.status, 'redelegate tx must succeed').to.equal(1);

            const qc = await cosmosQuery();
            const dst = await waitUntil(
                async () => {
                    const d = await qc.staking.delegation(delegator.seiAddress(), validators[1]);
                    return d.delegationResponse?.balance ?? null;
                },
                { timeoutMs: 30_000, label: 'destination delegation after redelegate' },
            );
            expect(BigInt(dst.amount)).to.equal(moved);
        });

        it('pool and params queries match the staking module', async () => {
            const qc = await cosmosQuery();
            const [poolP, paramsP, poolC, paramsC] = await Promise.all([
                staking.pool(),
                staking.params(),
                qc.staking.pool(),
                qc.staking.params(),
            ]);
            expect(BigInt(poolP.bondedTokens)).to.equal(BigInt(poolC.pool!.bondedTokens));
            expect(paramsP.bondDenom).to.equal(paramsC.params!.bondDenom);
            expect(Number(paramsP.maxValidators)).to.equal(paramsC.params!.maxValidators);
        });

        it('createValidator registers a new validator and editValidator updates its moniker', async () => {
            const [operator] = claimPool(runtime, provider, 1, 'staking:validator-creator');
            await associateViaTx(operator);

            // The operator address is the caller's own sei address under the
            // seivaloper prefix — derive it instead of searching by moniker
            // (monikers collide across suite re-runs on a long-lived devnet).
            const valoper = toBech32('seivaloper', fromBech32(operator.seiAddress()).data);
            // Unique moniker per run for the same reason.
            const moniker = `e2e-val-${Date.now().toString(36)}`;

            // Bare-hex ed25519 consensus pubkey (32 bytes, no 0x).
            const { publicKey } = generateKeyPairSync('ed25519');
            const pubKeyHex = (publicKey.export({ format: 'der', type: 'spki' }) as Buffer)
                .subarray(-32)
                .toString('hex');

            const createTx = await (staking.connect(operator.wallet) as ethers.Contract).createValidator(
                pubKeyHex,
                moniker,
                '0.05', // devnet min_commission_rate
                '0.20',
                '0.01',
                1n,
                { value: ethers.parseEther('1'), gasLimit: 2_000_000 },
            );
            const createReceipt = await createTx.wait();
            expect(createReceipt!.status, 'createValidator tx must succeed').to.equal(1);

            // validator() packs the whole Description as one string field.
            const created = await waitUntil(
                async () => {
                    const v = await staking.validator(valoper);
                    return v.description.includes(moniker) ? v : null;
                },
                { timeoutMs: 30_000, label: 'new validator visible via the precompile' },
            );
            expect(created.operatorAddress).to.equal(valoper);

            const qc = await cosmosQuery();
            const viaCosmos = await qc.staking.validator(valoper);
            expect(viaCosmos.validator?.description?.moniker, 'staking module parity').to.equal(
                moniker,
            );

            // Commission cannot change within 24h of creation: pass '' (untouched)
            // and minSelfDelegation 0 (untouched) — only the moniker updates.
            const editTx = await (staking.connect(operator.wallet) as ethers.Contract).editValidator(
                `${moniker}-edited`,
                '',
                0n,
                { gasLimit: 1_000_000 },
            );
            const editReceipt = await editTx.wait();
            expect(editReceipt!.status, 'editValidator tx must succeed').to.equal(1);

            await waitUntil(
                async () => {
                    const v = await staking.validator(valoper);
                    return v.description.includes(`${moniker}-edited`) ? v : null;
                },
                { timeoutMs: 30_000, label: 'edited moniker visible' },
            );

            // The operator address is derived from the caller, so a second
            // createValidator from the same account must fail.
            await expectVmError(
                (staking.connect(operator.wallet) as ethers.Contract).createValidator(
                    pubKeyHex,
                    `${moniker}-2`,
                    '0.05',
                    '0.20',
                    '0.01',
                    1n,
                    { value: ethers.parseEther('1'), gasLimit: 2_000_000 },
                ),
                'validator already exist',
            );
        });
    });

    describe('error handling', () => {
        it('delegate from an unassociated caller reverts (via eth_call)', async () => {
            // Mining a tx auto-associates its sender, so the association error
            // can never surface from a real tx — only eth_call (which does not
            // associate) reaches the precompile with an unassociated caller.
            const [unassociated] = claimPool(runtime, provider, 1, 'staking:unassociated');
            await expectExecutionReverted(
                (staking.connect(unassociated.wallet) as ethers.Contract).delegate.staticCall(
                    validators[0],
                    { value: DELEGATE_WEI },
                ),
                'staking.delegate from an unassociated caller',
            );
        });

        it('delegate rejects a zero value', async () => {
            await expectExecutionReverted(
                (staking.connect(delegator.wallet) as ethers.Contract).delegate.staticCall(
                    validators[0],
                    { value: 0n },
                ),
                'staking.delegate with value 0',
            );
        });

        it('delegate rejects a value with a non-zero wei remainder', async () => {
            await expectVmError(
                (staking.connect(delegator.wallet) as ethers.Contract).delegate(validators[0], {
                    value: DELEGATE_WEI + 1n,
                    gasLimit: 1_000_000,
                }),
                'non-zero wei remainder',
            );
        });

        it('undelegate without a delegation fails with the staking module error', async () => {
            const [bystander] = claimPool(runtime, provider, 1, 'staking:no-delegation');
            await associateViaTx(bystander);
            await expectVmError(
                (staking.connect(bystander.wallet) as ethers.Contract).undelegate(
                    validators[0],
                    1n,
                    { gasLimit: 2_000_000 },
                ),
                'no delegation for (address, validator) tuple',
            );
        });

        it('redelegate to the same validator is rejected', async () => {
            await expectVmError(
                (staking.connect(delegator.wallet) as ethers.Contract).redelegate(
                    validators[1],
                    validators[1],
                    1n,
                    { gasLimit: 2_000_000 },
                ),
                'cannot redelegate to the same validator',
            );
        });

        it('delegation query for an unknown validator reverts', async () => {
            // A checksummed-but-never-seen operator address (random 20 bytes).
            const unknownValoper = toBech32('seivaloper', ethers.randomBytes(20));
            await expectExecutionReverted(
                staking.delegation(delegator.address, unknownValoper),
                'staking.delegation with an unknown validator',
            );
        });

        it('delegation query for a malformed bech32 reverts', async () => {
            await expectExecutionReverted(
                staking.delegation(delegator.address, 'seivaloper1not-a-real-address'),
                'staking.delegation with a malformed bech32',
            );
        });

        it('out-of-gas fails cleanly, never as a panic (legacy guard)', async () => {
            // Unlike bank/addr (whose executors convert the gas-meter panic to
            // "execution reverted"), a starved delegate runs out inside the
            // cosmos store layer and surfaces as a location-tagged out-of-gas
            // error. The load-bearing property is the same: no Go panic leaks.
            const tx = await (staking.connect(delegator.wallet) as ethers.Contract).delegate(
                validators[0],
                { value: DELEGATE_WEI, gasLimit: 60_000 },
            );
            const receipt = await tx.wait().catch((e: any) => e.receipt);
            expect(receipt.status, 'tx must fail').to.equal(0);

            const vmError = await getVmError(receipt.hash);
            expect(vmError).to.include('out of gas');
            expect(vmError, 'no panic may leak into the VM error').to.not.include('panic');

            const trace = await traceTransaction(receipt.hash);
            expect(trace.error ?? '', 'trace carries a non-panic error').to.not.equal('');
            expect(trace.error, 'no panic may leak into traces').to.not.include('panic');
        });
    });

    describe('dispatch semantics (via PrecompileCaller)', () => {
        it('query methods respond through a real CALL from contract bytecode', async () => {
            const data = stakingIface.encodeFunctionData('delegation', [
                delegator.address,
                validators[1],
            ]);
            const ret: string = await caller.callTarget.staticCall(
                PRECOMPILE_ADDRESSES.staking,
                data,
            );
            const [decoded] = stakingIface.decodeFunctionResult('delegation', ret);
            expect(decoded.delegation.validator_address).to.equal(validators[1]);
        });

        it('query methods are callable via STATICCALL', async () => {
            const data = stakingIface.encodeFunctionData('params', []);
            const ret: string = await caller.staticcallTarget.staticCall(
                PRECOMPILE_ADDRESSES.staking,
                data,
            );
            const [decoded] = stakingIface.decodeFunctionResult('params', ret);
            expect(decoded.bondDenom).to.equal('usei');
        });

        it('write methods are rejected under STATICCALL (readOnly guard)', async () => {
            const data = stakingIface.encodeFunctionData('undelegate', [validators[0], 1n]);
            await expectVmError(
                caller.getFunction('staticcallTarget').send(PRECOMPILE_ADDRESSES.staking, data, {
                    gasLimit: 1_000_000,
                }),
                'cannot call staking precompile from staticcall',
            );
        });

        it('every method is rejected under DELEGATECALL (staking-wide guard)', async () => {
            // The delegatecall guard sits above method dispatch, so even a view
            // method like params() rejects — unlike json/pointerview.
            const data = stakingIface.encodeFunctionData('params', []);
            await expectVmError(
                caller.getFunction('delegatecallTarget').send(PRECOMPILE_ADDRESSES.staking, data, {
                    gasLimit: 1_000_000,
                }),
                'cannot delegatecall staking',
            );
        });

        it('grantStakingAuthorization is rejected under STATICCALL', async () => {
            const expiration = BigInt(Math.floor(Date.now() / 1000) + 86400);
            const data = stakingIface.encodeFunctionData('grantStakingAuthorization', [
                delegator.address,
                [validators[0], validators[1]],
                DELEGATE_USEI,
                expiration,
            ]);
            await expectVmError(
                caller.getFunction('staticcallTarget').send(PRECOMPILE_ADDRESSES.staking, data, {
                    gasLimit: 1_000_000,
                }),
                'cannot call staking precompile from staticcall',
            );
        });

        it('validators is callable via STATICCALL', async () => {
            const data = stakingIface.encodeFunctionData('validators', [
                'BOND_STATUS_BONDED',
                new Uint8Array(),
            ]);
            const ret: string = await caller.staticcallTarget.staticCall(
                PRECOMPILE_ADDRESSES.staking,
                data,
            );
            const [decoded] = stakingIface.decodeFunctionResult('validators', ret);
            const ops = [...decoded.validators].map((v: { operatorAddress: string }) => v.operatorAddress);
            expect(ops, 'STATICCALL validators includes the first bonded validator').to.include(
                validators[0],
            );
        });
    });

    describe('authz (grant / execute / revoke)', () => {
        it('grantStakingAuthorization lets the grantee delegateWithAuthorization; revoke then reverts', async () => {
            const [granter, grantee] = claimPool(runtime, provider, 2, 'staking:authz');
            await associateViaTx(granter);
            await associateViaTx(grantee);

            const expiration = BigInt(Math.floor(Date.now() / 1000) + 86400);
            const maxTokens = DELEGATE_USEI * 10n;
            const grantTx = await (
                staking.connect(granter.wallet) as ethers.Contract
            ).grantStakingAuthorization(
                grantee.address,
                [validators[0], validators[1]],
                maxTokens,
                expiration,
                { gasLimit: 1_000_000 },
            );
            expect((await grantTx.wait())!.status, 'grantStakingAuthorization tx must succeed').to.equal(
                1,
            );

            // Staking.sol: the CALLER (grantee) supplies msg.value; the delegation
            // is recorded on the granter.
            const delegateTx = await (
                staking.connect(grantee.wallet) as ethers.Contract
            ).delegateWithAuthorization(granter.address, validators[0], {
                value: DELEGATE_WEI,
                gasLimit: 1_000_000,
            });
            expect(
                (await delegateTx.wait())!.status,
                'delegateWithAuthorization tx must succeed',
            ).to.equal(1);

            const qc = await cosmosQuery();
            const cosmosDelegation = await waitUntil(
                async () => {
                    const d = await qc.staking.delegation(granter.seiAddress(), validators[0]);
                    return d.delegationResponse?.balance ?? null;
                },
                { timeoutMs: 30_000, label: 'cosmos delegation after delegateWithAuthorization' },
            );
            expect(cosmosDelegation.amount).to.equal(DELEGATE_USEI.toString());
            expect(cosmosDelegation.denom).to.equal('usei');

            const revokeTx = await (
                staking.connect(granter.wallet) as ethers.Contract
            ).revokeStakingAuthorization(grantee.address, { gasLimit: 1_000_000 });
            expect((await revokeTx.wait())!.status, 'revokeStakingAuthorization tx must succeed').to.equal(
                1,
            );

            await expectVmError(
                (staking.connect(grantee.wallet) as ethers.Contract).delegateWithAuthorization(
                    granter.address,
                    validators[0],
                    { value: DELEGATE_WEI, gasLimit: 1_000_000 },
                ),
                'authorization not found',
            );
        });

        it('redelegateWithAuthorization and undelegateWithAuthorization consume the grant', async () => {
            const [granter, grantee] = claimPool(runtime, provider, 2, 'staking:authz-redelegate');
            await associateViaTx(granter);
            await associateViaTx(grantee);

            const expiration = BigInt(Math.floor(Date.now() / 1000) + 86400);
            const grantTx = await (
                staking.connect(granter.wallet) as ethers.Contract
            ).grantStakingAuthorization(
                grantee.address,
                [validators[0], validators[1]],
                DELEGATE_USEI * 10n,
                expiration,
                { gasLimit: 1_000_000 },
            );
            expect((await grantTx.wait())!.status).to.equal(1);

            const delegateTx = await (
                staking.connect(grantee.wallet) as ethers.Contract
            ).delegateWithAuthorization(granter.address, validators[0], {
                value: DELEGATE_WEI,
                gasLimit: 1_000_000,
            });
            expect((await delegateTx.wait())!.status).to.equal(1);

            const qc = await cosmosQuery();
            await waitUntil(
                async () => {
                    const d = await qc.staking.delegation(granter.seiAddress(), validators[0]);
                    return d.delegationResponse?.balance ?? null;
                },
                { timeoutMs: 30_000, label: 'authz granter delegation before redelegate' },
            );

            const moved = DELEGATE_USEI / 4n;
            const redelegateTx = await (
                staking.connect(grantee.wallet) as ethers.Contract
            ).redelegateWithAuthorization(
                granter.address,
                validators[0],
                validators[1],
                moved,
                { gasLimit: 2_000_000 },
            );
            expect((await redelegateTx.wait())!.status, 'redelegateWithAuthorization').to.equal(1);

            const dst = await waitUntil(
                async () => {
                    const d = await qc.staking.delegation(granter.seiAddress(), validators[1]);
                    return d.delegationResponse?.balance ?? null;
                },
                { timeoutMs: 30_000, label: 'authz destination delegation after redelegate' },
            );
            expect(BigInt(dst.amount)).to.equal(moved);

            const undelegateTx = await (
                staking.connect(grantee.wallet) as ethers.Contract
            ).undelegateWithAuthorization(granter.address, validators[0], moved, {
                gasLimit: 2_000_000,
            });
            expect((await undelegateTx.wait())!.status, 'undelegateWithAuthorization').to.equal(1);
        });
    });

    describe('query methods (remaining surface)', () => {
        const emptyPageKey = new Uint8Array();

        it('validators(BOND_STATUS_BONDED, empty) includes validators[0]', async () => {
            const resp = await staking.validators('BOND_STATUS_BONDED', emptyPageKey);
            const list = resp.validators as ethers.Result;
            expect(list.length, 'bonded validator set is non-empty').to.be.greaterThan(0);
            const ops = [...list].map((v: { operatorAddress: string }) => v.operatorAddress);
            expect(ops).to.include(validators[0]);
        });

        it('delegatorDelegations / delegatorValidators / delegatorValidator see a fresh delegation', async () => {
            const [account] = claimPool(runtime, provider, 1, 'staking:query-delegator');
            await associateViaTx(account);
            const tx = await (staking.connect(account.wallet) as ethers.Contract).delegate(
                validators[0],
                { value: DELEGATE_WEI, gasLimit: 1_000_000 },
            );
            expect((await tx.wait())!.status).to.equal(1);

            const dels = await waitUntil(
                async () => {
                    const resp = await staking.delegatorDelegations(account.address, emptyPageKey);
                    const list = resp.delegations as ethers.Result;
                    return [...list].some(
                        (d: any) => d.delegation.validator_address === validators[0],
                    )
                        ? list
                        : null;
                },
                { timeoutMs: 30_000, label: 'delegatorDelegations after fresh delegate' },
            );
            expect(
                [...dels].some((d: any) => d.delegation.delegator_address === account.seiAddress()),
            ).to.equal(true);

            const dvals = await staking.delegatorValidators(account.address, emptyPageKey);
            const ops = [...(dvals.validators as ethers.Result)].map(
                (v: { operatorAddress: string }) => v.operatorAddress,
            );
            expect(ops).to.include(validators[0]);

            const dv = await staking.delegatorValidator(account.address, validators[0]);
            expect(dv.operatorAddress).to.equal(validators[0]);

            const valDels = await staking.validatorDelegations(validators[0], emptyPageKey);
            expect(
                (valDels.delegations as ethers.Result).length,
                'validatorDelegations is non-empty',
            ).to.be.greaterThan(0);
            expect(
                [...(valDels.delegations as ethers.Result)].every(
                    (d: any) => d.delegation.validator_address === validators[0],
                ),
            ).to.equal(true);
        });

        it('unbondingDelegation / delegatorUnbondingDelegations / validatorUnbondingDelegations after undelegate', async () => {
            const [account] = claimPool(runtime, provider, 1, 'staking:query-unbond');
            await associateViaTx(account);
            const delegateTx = await (staking.connect(account.wallet) as ethers.Contract).delegate(
                validators[0],
                { value: DELEGATE_WEI, gasLimit: 1_000_000 },
            );
            expect((await delegateTx.wait())!.status).to.equal(1);

            const amount = DELEGATE_USEI / 2n;
            const undelegateTx = await (staking.connect(account.wallet) as ethers.Contract).undelegate(
                validators[0],
                amount,
                { gasLimit: 2_000_000 },
            );
            expect((await undelegateTx.wait())!.status).to.equal(1);

            const ubd = await waitUntil(
                async () => {
                    const u = await staking.unbondingDelegation(account.address, validators[0]);
                    const entries = u.getValue('entries') as ethers.Result;
                    return entries.length > 0 ? u : null;
                },
                { timeoutMs: 8_000, intervalMs: 200, label: 'unbondingDelegation after undelegate' },
            );
            expect(ubd.delegatorAddress).to.equal(account.seiAddress());
            expect(ubd.validatorAddress).to.equal(validators[0]);
            expect(BigInt((ubd.getValue('entries') as ethers.Result)[0].balance)).to.equal(amount);

            const byDelegator = await staking.delegatorUnbondingDelegations(
                account.address,
                emptyPageKey,
            );
            const duList = byDelegator.unbondingDelegations as ethers.Result;
            expect(
                [...duList].some((u: any) => u.validatorAddress === validators[0]),
                'delegatorUnbondingDelegations includes this validator',
            ).to.equal(true);

            const byValidator = await waitUntil(
                async () => {
                    const resp = await staking.validatorUnbondingDelegations(
                        validators[0],
                        emptyPageKey,
                    );
                    const list = resp.unbondingDelegations as ethers.Result;
                    return [...list].some((u: any) => u.delegatorAddress === account.seiAddress())
                        ? list
                        : null;
                },
                { timeoutMs: 8_000, intervalMs: 200, label: 'validatorUnbondingDelegations' },
            );
            expect(byValidator.length).to.be.greaterThan(0);
        });

        it('redelegations sees a fresh redelegate', async () => {
            const [account] = claimPool(runtime, provider, 1, 'staking:query-redelegate');
            await associateViaTx(account);
            const delegateTx = await (staking.connect(account.wallet) as ethers.Contract).delegate(
                validators[0],
                { value: DELEGATE_WEI, gasLimit: 1_000_000 },
            );
            expect((await delegateTx.wait())!.status).to.equal(1);

            const moved = DELEGATE_USEI / 2n;
            const redelegateTx = await (staking.connect(account.wallet) as ethers.Contract).redelegate(
                validators[0],
                validators[1],
                moved,
                { gasLimit: 2_000_000 },
            );
            expect((await redelegateTx.wait())!.status).to.equal(1);

            const resp = await waitUntil(
                async () => {
                    const r = await staking.redelegations(
                        account.seiAddress(),
                        validators[0],
                        validators[1],
                        emptyPageKey,
                    );
                    const list = r.redelegations as ethers.Result;
                    return list.length > 0 ? r : null;
                },
                { timeoutMs: 30_000, label: 'redelegations after redelegate' },
            );
            const redels = resp.redelegations as ethers.Result;
            expect(redels[0].delegatorAddress).to.equal(account.seiAddress());
            expect(redels[0].validatorSrcAddress).to.equal(validators[0]);
            expect(redels[0].validatorDstAddress).to.equal(validators[1]);
        });

        // The staking module only keeps historical info for the last
        // `historical_entries` heights, so whether a given height answers is a
        // property of the chain, not of the precompile. The LCD is therefore the
        // oracle: the precompile must agree with it about the SAME height, both
        // on the answer and on the absence of one.
        it('historicalInfo agrees with the staking module about a recent height', async () => {
            const height = BigInt(Math.max((await provider.getBlockNumber()) - 2, 1));
            const lcd = await cosmosRest<{
                hist?: { valset?: Array<{ operator_address: string }> } | null;
                code?: number;
            }>(`/cosmos/staking/v1beta1/historical_info/${height}`).catch(() => undefined);

            if (lcd?.hist == null) {
                // Not retained (historical_entries=0, or the height aged out):
                // the precompile must refuse it rather than invent an answer. The
                // reason surfaced is the staking querier's, not the executor's own
                // "historical info not found" fallback, which a NotFound status
                // error means is unreachable.
                await expectVmError(
                    admin.wallet.sendTransaction({
                        to: PRECOMPILE_ADDRESSES.staking,
                        data: stakingIface.encodeFunctionData('historicalInfo', [height]),
                        gasLimit: 1_000_000,
                    }),
                    `historical info for height ${height} not found`,
                );
                return;
            }

            const info = await staking.historicalInfo(height);
            expect(info.height, 'historicalInfo echoes the requested height').to.equal(height);
            const ops = [...(info.validators as ethers.Result)].map(
                (v: { operatorAddress: string }) => v.operatorAddress,
            );
            expect(ops.slice().sort(), 'historical valset matches the staking module').to.deep.equal(
                (lcd.hist.valset ?? []).map(v => v.operator_address).sort(),
            );
        });
    });
});
