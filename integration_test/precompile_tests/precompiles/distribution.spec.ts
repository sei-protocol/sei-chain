/**
 * distribution precompile (0x…1007) — end-to-end semantics against a live Sei chain.
 *
 * Fixture: a pool account delegates via the staking precompile, then rewards
 * accrue per block. Reward amounts can never be asserted as exact equality
 * across blocks — the withdrawal test instead pins the bank-balance delta to
 * the amount decoded from the tx's own DelegationRewardsWithdrawn log.
 *
 * Guard quirk worth knowing: rewards() is the one phase-2 view WITHOUT a
 * non-payable check (a value-bearing call would succeed and strand the funds),
 * so this spec deliberately has no "view rejects value" test.
 */
import { ethers } from 'ethers';
import { expect } from 'chai';
import { seiRpc, waitUntil } from '../utils/chainUtils';
import { EvmAccount, associateViaTx } from '../utils/evmUtils';
import { bondedValidators, cosmosQuery, bankBalance } from '../utils/cosmosUtils';
import {
    decString,
    delegatorWithdrawAddress,
    distributionParams,
    validatorSlashes,
} from '../utils/moduleQueries';
import {
    PRECOMPILE_ADDRESSES,
    precompileContract,
    precompileInterface,
    callerContract,
    expectExecutionReverted,
    expectVmError,
} from '../utils/precompileUtils';
import { readRuntimeState, claimPool, RuntimeState } from '../utils/testUtils';

type DistrCoin = { amount: bigint; decimals: bigint; denom: string };

/**
 * Asserts the shape of a decoded DecCoin array. Non-emptiness is required by
 * default: the per-coin loop never runs on an empty array, which would leave
 * the whole assertion vacuous. `mayBeEmpty` opts out where emptiness is a
 * legitimate chain state rather than a defect.
 */
function expectCoinArray(
    coins: readonly DistrCoin[],
    label: string,
    { mayBeEmpty = false }: { mayBeEmpty?: boolean } = {},
): void {
    expect(coins, `${label} is an array`).to.be.an('array');
    if (!mayBeEmpty) {
        expect(coins.length, `${label} must not be empty`).to.be.greaterThan(0);
    }
    for (const coin of coins) {
        expect(coin.denom, `${label} denom`).to.be.a('string').and.not.equal('');
        expect(typeof coin.amount, `${label} amount`).to.equal('bigint');
        expect(coin.decimals, `${label} decimals`).to.equal(18n);
    }
}

describe('distribution precompile (0x1007)', function () {
    this.timeout(180 * 1000);

    const provider = seiRpc();
    const distrIface = precompileInterface('distribution');

    let runtime: RuntimeState;
    let admin: EvmAccount;
    let distribution: ethers.Contract;
    let staking: ethers.Contract;
    let caller: ethers.Contract;
    let validator: string;
    let delegator: EvmAccount;
    let withdrawTarget: EvmAccount;

    before(async () => {
        runtime = readRuntimeState();
        admin = EvmAccount.fromMnemonic(runtime.funded.adminMnemonic, provider);
        distribution = precompileContract('distribution', admin.wallet);
        staking = precompileContract('staking', admin.wallet);
        caller = callerContract(runtime, admin.wallet);
        [validator] = await bondedValidators();

        [delegator, withdrawTarget] = claimPool(runtime, provider, 2, 'distribution:fixture');
        await associateViaTx(delegator);
        await associateViaTx(withdrawTarget);

        // Delegate so rewards start accruing; poll until they are visible.
        const tx = await (staking.connect(delegator.wallet) as ethers.Contract).delegate(
            validator,
            { value: ethers.parseEther('1'), gasLimit: 1_000_000 },
        );
        expect((await tx.wait())!.status, 'fixture delegation must succeed').to.equal(1);

        const qc = await cosmosQuery();
        await waitUntil(
            async () => {
                const { rewards } = await qc.distribution.delegationRewards(
                    delegator.seiAddress(),
                    validator,
                );
                return rewards.some(c => BigInt(c.amount) > 0n) ? true : null;
            },
            { timeoutMs: 60_000, label: 'delegation rewards to accrue' },
        );
    });

    describe('happy path & state parity', () => {
        it('rewards query reports the accrued rewards for the delegation', async () => {
            const result = await distribution.rewards(delegator.address);
            expect(result.rewards.length, 'one delegation, one rewards entry').to.equal(1);
            expect(result.rewards[0].validator_address).to.equal(validator);

            const usei = result.rewards[0].coins.find((c: any) => c.denom === 'usei');
            expect(usei, 'rewards carry a usei coin').to.not.equal(undefined);
            // Amounts are sdk.Dec fixed-point integers scaled by 10^decimals (18).
            expect(usei.decimals).to.equal(18n);
            expect(usei.amount > 0n, 'accrued rewards are positive').to.equal(true);
        });

        it('setWithdrawAddress redirects future withdrawals, visible to the distribution module', async () => {
            const tx = await (
                distribution.connect(delegator.wallet) as ethers.Contract
            ).setWithdrawAddress(withdrawTarget.address, { gasLimit: 500_000 });
            expect((await tx.wait())!.status).to.equal(1);

            const qc = await cosmosQuery();
            const stored = await waitUntil(
                async () =>
                    (await qc.distribution.delegatorWithdrawAddress(delegator.seiAddress()))
                        .withdrawAddress || null,
                { timeoutMs: 15_000, label: 'withdraw address stored' },
            );
            expect(stored).to.equal(withdrawTarget.seiAddress());
        });

        it('withdrawDelegationRewards pays the withdraw address exactly the logged amount', async () => {
            const targetBefore = await bankBalance(withdrawTarget.seiAddress());

            const tx = await (
                distribution.connect(delegator.wallet) as ethers.Contract
            ).withdrawDelegationRewards(validator, { gasLimit: 2_000_000 });
            const receipt = await tx.wait();
            expect(receipt!.status, 'withdraw tx must succeed').to.equal(1);

            const parsed = receipt!.logs
                .map((l: ethers.Log) => {
                    try {
                        return distrIface.parseLog({ topics: [...l.topics], data: l.data });
                    } catch {
                        return null;
                    }
                })
                .find((p: any) => p?.name === 'DelegationRewardsWithdrawn');
            expect(parsed, 'DelegationRewardsWithdrawn log emitted').to.not.equal(undefined);
            const withdrawnUsei: bigint = parsed!.args[2];
            expect(withdrawnUsei > 0n, 'withdrawn amount is positive').to.equal(true);

            // The delta lands at the withdraw address, not the delegator.
            await waitUntil(
                async () => {
                    const b = await bankBalance(withdrawTarget.seiAddress());
                    return b === targetBefore + withdrawnUsei ? b : null;
                },
                { timeoutMs: 30_000, label: 'withdraw address credited with logged amount' },
            );
        });

        it('withdrawMultipleDelegationRewards succeeds for a list of validators', async () => {
            const tx = await (
                distribution.connect(delegator.wallet) as ethers.Contract
            ).withdrawMultipleDelegationRewards([validator], { gasLimit: 2_000_000 });
            expect((await tx.wait())!.status).to.equal(1);
        });
    });

    describe('error handling', () => {
        it('setWithdrawAddress rejects an unassociated withdraw address', async () => {
            const [unassociated] = claimPool(runtime, provider, 1, 'distribution:unassoc-target');
            await expectVmError(
                (distribution.connect(delegator.wallet) as ethers.Contract).setWithdrawAddress(
                    unassociated.address,
                    { gasLimit: 500_000 },
                ),
                'unassociated address',
            );
        });

        it('setWithdrawAddress rejects the zero address', async () => {
            await expectExecutionReverted(
                (distribution.connect(delegator.wallet) as ethers.Contract)
                    .setWithdrawAddress.staticCall(ethers.ZeroAddress),
                'distribution.setWithdrawAddress with the zero address',
            );
        });

        it('withdrawDelegationRewards without a delegation reverts', async () => {
            const [bystander] = claimPool(runtime, provider, 1, 'distribution:no-delegation');
            await associateViaTx(bystander);
            const tx = await (
                distribution.connect(bystander.wallet) as ethers.Contract
            ).withdrawDelegationRewards(validator, { gasLimit: 2_000_000 });
            const receipt = await tx.wait().catch((e: any) => e.receipt);
            expect(receipt.status, 'tx must fail').to.equal(0);
        });

        it('withdrawValidatorCommission from a non-validator fails', async () => {
            await expectVmError(
                (distribution.connect(delegator.wallet) as ethers.Contract)
                    .withdrawValidatorCommission({ gasLimit: 500_000 }),
                'no validator commission to withdraw',
            );
        });

        it('rewards query rejects an unassociated delegator', async () => {
            await expectExecutionReverted(
                distribution.rewards(EvmAccount.random(provider).address),
                'distribution.rewards for an unassociated address',
            );
        });
    });

    describe('query methods', () => {
        it('params matches the distribution module', async () => {
            const [viaPrecompile, params] = await Promise.all([
                distribution.params(),
                distributionParams(),
            ]);
            expect(params, 'the distribution module must report params').to.not.equal(undefined);
            expect(viaPrecompile.communityTax).to.equal(decString(params!.community_tax));
            expect(viaPrecompile.baseProposerReward).to.equal(
                decString(params!.base_proposer_reward),
            );
            expect(viaPrecompile.bonusProposerReward).to.equal(
                decString(params!.bonus_proposer_reward),
            );
            expect(viaPrecompile.withdrawAddrEnabled).to.equal(params!.withdraw_addr_enabled);
        });

        it('delegatorValidators includes the fixture validator', async () => {
            const validators: string[] = await distribution.delegatorValidators(delegator.address);
            expect(validators).to.include(validator);
        });

        it('delegatorWithdrawAddress matches the module rather than assuming the default', async () => {
            const [viaPrecompile, withdrawAddress] = await Promise.all([
                distribution.delegatorWithdrawAddress(delegator.address) as Promise<string>,
                delegatorWithdrawAddress(delegator.seiAddress()),
            ]);
            expect(viaPrecompile).to.equal(withdrawAddress);
        });

        it('delegationRewards, validatorOutstandingRewards and validatorCommission are non-empty', async () => {
            // All three are non-empty by construction: the fixture delegation
            // keeps accruing to `validator` every block, the validator's own
            // rewards are never withdrawn here, and its commission rate cannot
            // be zero (the devnet's min_commission_rate is 5%).
            const [delegation, outstanding, commission] = await Promise.all([
                distribution.delegationRewards(delegator.address, validator),
                distribution.validatorOutstandingRewards(validator),
                distribution.validatorCommission(validator),
            ]);
            expectCoinArray(delegation, 'delegationRewards');
            expectCoinArray(outstanding, 'validatorOutstandingRewards');
            expectCoinArray(commission, 'validatorCommission');
        });

        it('validatorSlashes matches the distribution module over the same height range', async () => {
            const endingHeight = 1_000_000_000;
            const [result, moduleSlashes] = await Promise.all([
                distribution.validatorSlashes(validator, 1, endingHeight, new Uint8Array()),
                validatorSlashes(validator, 1, endingHeight),
            ]);
            const slashes = (result.slashes ?? result[0]) as Array<{
                validatorPeriod: bigint;
                fraction: string;
            }>;
            expect(slashes.length, 'validatorSlashes count vs the module').to.equal(
                moduleSlashes.length,
            );
            for (let i = 0; i < moduleSlashes.length; i++) {
                expect(slashes[i].validatorPeriod).to.equal(
                    BigInt(moduleSlashes[i].validator_period),
                );
                expect(slashes[i].fraction).to.equal(decString(moduleSlashes[i].fraction));
            }
        });

        it('communityPool returns coins, or nothing when community_tax is 0', async () => {
            // sei-cosmos defaults community_tax to 0 and the devnet does not
            // override it, so no reward is ever skimmed into the pool and an
            // empty pool is the correct answer rather than a defect.
            expectCoinArray(await distribution.communityPool(), 'communityPool', {
                mayBeEmpty: true,
            });
        });
    });

    describe('authz', () => {
        let grantee: EvmAccount;
        before(async () => {
            [grantee] = claimPool(runtime, provider, 1, 'distribution:authz-grantee');
            await associateViaTx(grantee);
        });

        it('withdrawValidatorCommission from an account that owns no validator reverts', async () => {
            await expectVmError(
                (distribution.connect(grantee.wallet) as ethers.Contract).withdrawValidatorCommission({
                    gasLimit: 500_000,
                }),
                'no validator commission to withdraw',
            );
        });

        it('withdrawValidatorCommissionWithAuthorization without a grant reverts', async () => {
            await expectVmError(
                (distribution.connect(grantee.wallet) as ethers.Contract)
                    .withdrawValidatorCommissionWithAuthorization(delegator.address, {
                        gasLimit: 500_000,
                    }),
                'authorization not found',
            );
        });

        it('grant lets the grantee withdraw rewards; revoke removes the grant', async () => {
            const expiration = BigInt(Math.floor(Date.now() / 1000) + 86400);

            const grantTx = await (
                distribution.connect(delegator.wallet) as ethers.Contract
            ).grantWithdrawAuthorization(grantee.address, expiration, { gasLimit: 500_000 });
            expect((await grantTx.wait())!.status).to.equal(1);

            // A successful receipt is not proof the rewards moved, so pin the
            // credit the same way the non-authorized withdraw above does: the
            // tx's own DelegationRewardsWithdrawn amount must land at the
            // delegator's configured withdraw address.
            const targetBefore = await bankBalance(withdrawTarget.seiAddress());
            const withdrawTx = await (
                distribution.connect(grantee.wallet) as ethers.Contract
            ).withdrawDelegationRewardsWithAuthorization(delegator.address, validator, {
                gasLimit: 2_000_000,
            });
            const receipt = await withdrawTx.wait();
            expect(receipt!.status).to.equal(1);

            const withdrawn = receipt!.logs
                .map((l: ethers.Log) => {
                    try {
                        return distrIface.parseLog({ topics: [...l.topics], data: l.data });
                    } catch {
                        return null;
                    }
                })
                .find((p: any) => p?.name === 'DelegationRewardsWithdrawn');
            expect(withdrawn, 'DelegationRewardsWithdrawn log emitted').to.not.equal(undefined);
            const withdrawnUsei: bigint = withdrawn!.args[2];
            await waitUntil(
                async () => {
                    const b = await bankBalance(withdrawTarget.seiAddress());
                    return b === targetBefore + withdrawnUsei ? b : null;
                },
                {
                    timeoutMs: 30_000,
                    label: 'withdraw address credited by the authorized withdraw',
                },
            );

            const revokeTx = await (
                distribution.connect(delegator.wallet) as ethers.Contract
            ).revokeWithdrawAuthorization(grantee.address, { gasLimit: 500_000 });
            expect((await revokeTx.wait())!.status).to.equal(1);

            await expectVmError(
                (distribution.connect(grantee.wallet) as ethers.Contract)
                    .withdrawDelegationRewardsWithAuthorization(delegator.address, validator, {
                        gasLimit: 2_000_000,
                    }),
                'authorization not found',
            );
        });
    });

    describe('dispatch semantics (via PrecompileCaller)', () => {
        it('rewards responds through a real CALL and under STATICCALL', async () => {
            const data = distrIface.encodeFunctionData('rewards', [delegator.address]);
            // Rewards grow every block — pin both reads to the same height or
            // the byte-equality below races block production.
            const blockTag = await provider.getBlockNumber();
            const [viaCall, viaStatic] = await Promise.all([
                caller.callTarget.staticCall(PRECOMPILE_ADDRESSES.distribution, data, { blockTag }),
                caller.staticcallTarget.staticCall(PRECOMPILE_ADDRESSES.distribution, data, {
                    blockTag,
                }),
            ]);
            const [decoded] = distrIface.decodeFunctionResult('rewards', viaCall as string);
            expect(decoded.rewards[0].validator_address).to.equal(validator);
            expect(viaStatic, 'CALL and STATICCALL return identical bytes').to.equal(viaCall);
        });

        it('params is callable via STATICCALL', async () => {
            const data = distrIface.encodeFunctionData('params', []);
            const [ret, direct] = await Promise.all([
                caller.staticcallTarget.staticCall(
                    PRECOMPILE_ADDRESSES.distribution,
                    data,
                ) as Promise<string>,
                distribution.params(),
            ]);
            const [decoded] = distrIface.decodeFunctionResult('params', ret);
            expect(decoded.communityTax).to.equal(direct.communityTax);
            expect(decoded.baseProposerReward).to.equal(direct.baseProposerReward);
            expect(decoded.bonusProposerReward).to.equal(direct.bonusProposerReward);
            expect(decoded.withdrawAddrEnabled).to.equal(direct.withdrawAddrEnabled);
        });

        it('write methods are rejected under STATICCALL (readOnly guard)', async () => {
            const data = distrIface.encodeFunctionData('withdrawDelegationRewards', [validator]);
            await expectVmError(
                caller
                    .getFunction('staticcallTarget')
                    .send(PRECOMPILE_ADDRESSES.distribution, data, { gasLimit: 500_000 }),
                'cannot call distr precompile from staticcall',
            );
        });

        it('every method is rejected under DELEGATECALL — including the rewards view', async () => {
            const data = distrIface.encodeFunctionData('rewards', [delegator.address]);
            await expectVmError(
                caller
                    .getFunction('delegatecallTarget')
                    .send(PRECOMPILE_ADDRESSES.distribution, data, { gasLimit: 500_000 }),
                'cannot delegatecall distr',
            );
        });
    });
});
