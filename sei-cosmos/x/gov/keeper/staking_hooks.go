package keeper

import (
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
)

var _ stakingtypes.StakingHooks = StakingHooks{}

// StakingHooks maintains governance vote delegation snapshots as staking state changes.
type StakingHooks struct {
	keeper Keeper
}

// StakingHooks returns the governance staking hooks.
func (keeper Keeper) StakingHooks() StakingHooks {
	return StakingHooks{keeper: keeper}
}

func (StakingHooks) AfterValidatorCreated(sdk.Context, sdk.ValAddress) {}

func (StakingHooks) BeforeValidatorModified(sdk.Context, sdk.ValAddress) {}

func (StakingHooks) AfterValidatorRemoved(sdk.Context, sdk.ConsAddress, sdk.ValAddress) {}

func (StakingHooks) AfterValidatorBonded(sdk.Context, sdk.ConsAddress, sdk.ValAddress) {}

func (StakingHooks) AfterValidatorBeginUnbonding(sdk.Context, sdk.ConsAddress, sdk.ValAddress) {}

func (StakingHooks) BeforeDelegationCreated(sdk.Context, sdk.AccAddress, sdk.ValAddress) {}

func (StakingHooks) BeforeDelegationSharesModified(sdk.Context, sdk.AccAddress, sdk.ValAddress) {}

// BeforeDelegationRemoved removes the outgoing delegation from active vote snapshots.
func (hooks StakingHooks) BeforeDelegationRemoved(
	ctx sdk.Context,
	delegator sdk.AccAddress,
	validator sdk.ValAddress,
) {
	if !hooks.keeper.IncrementalTallyEnabled(ctx) {
		return
	}
	if stakingtypes.IsSlashDelegationModification(ctx) {
		hooks.keeper.QueueVoteDelegationUpdate(ctx, delegator, validator, sdk.ZeroDec())
		return
	}
	hooks.keeper.refreshVoteDelegationSnapshots(ctx, delegator, validator)
}

// AfterDelegationModified refreshes active vote snapshots from the updated delegation state.
func (hooks StakingHooks) AfterDelegationModified(
	ctx sdk.Context,
	delegator sdk.AccAddress,
	validator sdk.ValAddress,
) {
	if !hooks.keeper.IncrementalTallyEnabled(ctx) {
		return
	}
	if stakingtypes.IsSlashDelegationModification(ctx) {
		delegation, found := hooks.keeper.sk.GetDelegation(ctx, delegator, validator)
		if !found {
			hooks.keeper.QueueVoteDelegationUpdate(ctx, delegator, validator, sdk.ZeroDec())
			return
		}
		hooks.keeper.QueueVoteDelegationUpdate(ctx, delegator, validator, delegation.Shares)
		return
	}
	hooks.keeper.refreshVoteDelegationSnapshots(ctx, delegator, nil)
}

func (StakingHooks) BeforeValidatorSlashed(sdk.Context, sdk.ValAddress, sdk.Dec) {}
