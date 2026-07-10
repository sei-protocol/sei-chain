package keeper

import (
	"fmt"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/distribution/types"
	stakingtypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/staking/types"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("cosmos", "x", "distribution", "keeper")

// initialize starting info for a new delegation
func (k Keeper) initializeDelegation(ctx sdk.Context, val sdk.ValAddress, del sdk.AccAddress) {
	// period has already been incremented - we want to store the period ended by this delegation action
	previousPeriod := k.GetValidatorCurrentRewards(ctx, val).Period - 1

	// increment reference count for the period we're going to track
	k.incrementReferenceCount(ctx, val, previousPeriod)

	validator := k.stakingKeeper.Validator(ctx, val)
	delegation := k.stakingKeeper.Delegation(ctx, del, val)

	// calculate delegation stake in tokens
	// we don't store directly, so multiply delegation shares * (tokens per share)
	// note: necessary to truncate so we don't allow withdrawing more rewards than owed
	stake := validator.TokensFromSharesTruncated(delegation.GetShares())
	k.SetDelegatorStartingInfo(ctx, val, del, types.NewDelegatorStartingInfo(previousPeriod, stake, uint64(ctx.BlockHeight()))) //nolint:gosec // block heights are always non-negative
}

// rewardsFromRatios returns stake * (endingRatio - startingRatio), truncated. It
// centralizes the ratio math shared by the store-backed reward calculation and the
// read-only calculation used by queries (which supply an in-memory ending ratio).
func rewardsFromRatios(startingRatio, endingRatio sdk.DecCoins, stake sdk.Dec) sdk.DecCoins {
	difference := endingRatio.Sub(startingRatio)
	if difference.IsAnyNegative() {
		panic("negative rewards should not be possible")
	}
	// note: necessary to truncate so we don't allow withdrawing more rewards than owed
	return difference.MulDecTruncate(stake)
}

// calculate the rewards accrued by a delegation between two periods
func (k Keeper) calculateDelegationRewardsBetween(ctx sdk.Context, val stakingtypes.ValidatorI,
	startingPeriod, endingPeriod uint64, stake sdk.Dec) (rewards sdk.DecCoins) {
	// sanity check
	if startingPeriod > endingPeriod {
		panic("startingPeriod cannot be greater than endingPeriod")
	}

	// sanity check
	if stake.IsNegative() {
		panic("stake should not be negative")
	}

	// return staking * (ending - starting)
	starting := k.GetValidatorHistoricalRewards(ctx, val.GetOperator(), startingPeriod)
	ending := k.GetValidatorHistoricalRewards(ctx, val.GetOperator(), endingPeriod)
	return rewardsFromRatios(starting.CumulativeRewardRatio, ending.CumulativeRewardRatio, stake)
}

// CalculateDelegationRewards calculates the total rewards accrued by a delegation
// up to endingPeriod, reading that period's cumulative reward ratio from the
// store. State-changing callers (e.g. withdrawals) persist endingPeriod via
// IncrementValidatorPeriod before calling this. Passing a nil ending ratio makes
// the core read endingPeriod from the store lazily (only if the final period is
// reached), preserving this path's exact store-access pattern and gas cost.
func (k Keeper) CalculateDelegationRewards(ctx sdk.Context, val stakingtypes.ValidatorI, del stakingtypes.DelegationI, endingPeriod uint64) (rewards sdk.DecCoins) {
	return k.calculateDelegationRewards(ctx, val, del, endingPeriod, nil)
}

// currentRewardsEndingPeriodAndRatio returns the ending period and its cumulative
// reward ratio that IncrementValidatorPeriod WOULD produce for the validator's
// current (open) period — computed in memory, WITHOUT writing to the store. This
// lets read-only reward queries avoid the state mutation (historical/current
// rewards, reference counts, and the zero-token community-pool transfer) that
// IncrementValidatorPeriod performs. The returned ratio is identical to the one
// IncrementValidatorPeriod would persist for that period, so the resulting reward
// figure matches the state-changing path exactly.
func (k Keeper) currentRewardsEndingPeriodAndRatio(ctx sdk.Context, val stakingtypes.ValidatorI) (uint64, sdk.DecCoins) {
	rewards := k.GetValidatorCurrentRewards(ctx, val.GetOperator())

	var current sdk.DecCoins
	if val.GetTokens().IsZero() {
		// The state-changing path routes a zero-token validator's rewards to the
		// community pool and uses a zero ratio; a read-only query only needs the
		// (zero) ratio, not the transfer.
		current = sdk.DecCoins{}
	} else {
		// note: necessary to truncate so we don't allow withdrawing more rewards than owed
		current = rewards.Rewards.QuoDecTruncate(val.GetTokens().ToDec())
	}

	historical := k.GetValidatorHistoricalRewards(ctx, val.GetOperator(), rewards.Period-1).CumulativeRewardRatio
	return rewards.Period, historical.Add(current...)
}

// CalculateDelegationRewardsReadOnly computes a delegation's outstanding rewards
// without mutating any state. It mirrors the "IncrementValidatorPeriod then
// CalculateDelegationRewards" sequence used by the withdraw path, but derives the
// ending period's cumulative reward ratio in memory instead of persisting it, so
// it is safe to call from read-only queries.
func (k Keeper) CalculateDelegationRewardsReadOnly(ctx sdk.Context, val stakingtypes.ValidatorI, del stakingtypes.DelegationI) sdk.DecCoins {
	endingPeriod, endingRatio := k.currentRewardsEndingPeriodAndRatio(ctx, val)
	return k.calculateDelegationRewards(ctx, val, del, endingPeriod, &endingRatio)
}

// calculateDelegationRewards is the shared core of the reward calculation. When
// endingRatio is non-nil it is used as the cumulative reward ratio at endingPeriod
// for the final period, so read-only callers can pass an in-memory ratio and avoid
// persisting the period. When endingRatio is nil the ratio is read from the store
// (the state-changing withdraw path, which persists it via IncrementValidatorPeriod
// beforehand); reading it lazily here — only if the final period is reached —
// preserves that path's exact store-access pattern and gas cost. Intermediate
// slash periods are always read from the store (they are always persisted).
func (k Keeper) calculateDelegationRewards(ctx sdk.Context, val stakingtypes.ValidatorI, del stakingtypes.DelegationI, endingPeriod uint64, endingRatio *sdk.DecCoins) (rewards sdk.DecCoins) {
	// fetch starting info for delegation
	startingInfo := k.GetDelegatorStartingInfo(ctx, del.GetValidatorAddr(), del.GetDelegatorAddr())

	if startingInfo.Height == uint64(ctx.BlockHeight()) { //nolint:gosec // block heights are always non-negative
		// started this height, no rewards yet
		return
	}

	startingPeriod := startingInfo.PreviousPeriod
	stake := startingInfo.Stake

	// Iterate through slashes and withdraw with calculated staking for
	// distribution periods. These period offsets are dependent on *when* slashes
	// happen - namely, in BeginBlock, after rewards are allocated...
	// Slashes which happened in the first block would have been before this
	// delegation existed, UNLESS they were slashes of a redelegation to this
	// validator which was itself slashed (from a fault committed by the
	// redelegation source validator) earlier in the same BeginBlock.
	startingHeight := startingInfo.Height
	// Slashes this block happened after reward allocation, but we have to account
	// for them for the stake sanity check below.
	endingHeight := uint64(ctx.BlockHeight()) //nolint:gosec // block heights are always non-negative
	if endingHeight > startingHeight {
		k.IterateValidatorSlashEventsBetween(ctx, del.GetValidatorAddr(), startingHeight, endingHeight,
			func(height uint64, event types.ValidatorSlashEvent) (stop bool) {
				endingPeriod := event.ValidatorPeriod
				if endingPeriod > startingPeriod {
					rewards = rewards.Add(k.calculateDelegationRewardsBetween(ctx, val, startingPeriod, endingPeriod, stake)...)

					// Note: It is necessary to truncate so we don't allow withdrawing
					// more rewards than owed.
					stake = stake.MulTruncate(sdk.OneDec().Sub(event.Fraction))
					startingPeriod = endingPeriod
				}
				return false
			},
		)
	}

	// A total stake sanity check; Recalculated final stake should be less than or
	// equal to current stake here. We cannot use Equals because stake is truncated
	// when multiplied by slash fractions (see above). We could only use equals if
	// we had arbitrary-precision rationals.
	currentStake := val.TokensFromShares(del.GetShares())

	if stake.GT(currentStake) {
		// AccountI for rounding inconsistencies between:
		//
		//     currentStake: calculated as in staking with a single computation
		//     stake:        calculated as an accumulation of stake
		//                   calculations across validator's distribution periods
		//
		// These inconsistencies are due to differing order of operations which
		// will inevitably have different accumulated rounding and may lead to
		// the smallest decimal place being one greater in stake than
		// currentStake. When we calculated slashing by period, even if we
		// round down for each slash fraction, it's possible due to how much is
		// being rounded that we slash less when slashing by period instead of
		// for when we slash without periods. In other words, the single slash,
		// and the slashing by period could both be rounding down but the
		// slashing by period is simply rounding down less, thus making stake >
		// currentStake
		//
		// A small amount of this error is tolerated and corrected for,
		// however any greater amount should be considered a breach in expected
		// behaviour.
		marginOfErr := sdk.SmallestDec().MulInt64(3)
		if stake.LTE(currentStake.Add(marginOfErr)) {
			stake = currentStake
		} else {
			panic(fmt.Sprintf("calculated final stake for delegator %s greater than current usei"+
				"\n\tfinal stake:\t%s"+
				"\n\tcurrent stake:\t%s",
				del.GetDelegatorAddr(), stake, currentStake))
		}
	}

	// calculate rewards for the final period (mirrors calculateDelegationRewardsBetween:
	// same sanity checks and same two historical-rewards reads for the nil path).
	if startingPeriod > endingPeriod {
		panic("startingPeriod cannot be greater than endingPeriod")
	}
	if stake.IsNegative() {
		panic("stake should not be negative")
	}
	startingRatio := k.GetValidatorHistoricalRewards(ctx, val.GetOperator(), startingPeriod).CumulativeRewardRatio
	finalRatio := endingRatio
	if finalRatio == nil {
		// state-changing path: read the persisted ending ratio here (lazily, only
		// once the final period is reached) to preserve the original gas cost.
		ratio := k.GetValidatorHistoricalRewards(ctx, val.GetOperator(), endingPeriod).CumulativeRewardRatio
		finalRatio = &ratio
	}
	rewards = rewards.Add(rewardsFromRatios(startingRatio, *finalRatio, stake)...)

	return rewards
}

func (k Keeper) withdrawDelegationRewards(ctx sdk.Context, val stakingtypes.ValidatorI, del stakingtypes.DelegationI) (sdk.Coins, error) {
	// check existence of delegator starting info
	if !k.HasDelegatorStartingInfo(ctx, del.GetValidatorAddr(), del.GetDelegatorAddr()) {
		return nil, types.ErrEmptyDelegationDistInfo
	}

	// end current period and calculate rewards
	endingPeriod := k.IncrementValidatorPeriod(ctx, val)
	rewardsRaw := k.CalculateDelegationRewards(ctx, val, del, endingPeriod)
	outstanding := k.GetValidatorOutstandingRewardsCoins(ctx, del.GetValidatorAddr())

	// defensive edge case may happen on the very final digits
	// of the decCoins due to operation order of the distribution mechanism.
	rewards := rewardsRaw.Intersect(outstanding)
	if !rewards.IsEqual(rewardsRaw) {

		logger.Info(
			"rounding error withdrawing rewards from validator",
			"delegator", del.GetDelegatorAddr().String(),
			"validator", val.GetOperator().String(),
			"got", rewards.String(),
			"expected", rewardsRaw.String(),
		)
	}

	// truncate reward dec coins, return remainder to community pool
	finalRewards, remainder := rewards.TruncateDecimal()

	// add coins to user account
	if !finalRewards.IsZero() {
		withdrawAddr := k.GetDelegatorWithdrawAddr(ctx, del.GetDelegatorAddr())
		err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, withdrawAddr, finalRewards)
		if err != nil {
			return nil, err
		}
	}

	// update the outstanding rewards and the community pool only if the
	// transaction was successful
	k.SetValidatorOutstandingRewards(ctx, del.GetValidatorAddr(), types.ValidatorOutstandingRewards{Rewards: outstanding.Sub(rewards)})
	feePool := k.GetFeePool(ctx)
	feePool.CommunityPool = feePool.CommunityPool.Add(remainder...)
	k.SetFeePool(ctx, feePool)

	// decrement reference count of starting period
	startingInfo := k.GetDelegatorStartingInfo(ctx, del.GetValidatorAddr(), del.GetDelegatorAddr())
	startingPeriod := startingInfo.PreviousPeriod
	k.decrementReferenceCount(ctx, del.GetValidatorAddr(), startingPeriod)

	// remove delegator starting info
	k.DeleteDelegatorStartingInfo(ctx, del.GetValidatorAddr(), del.GetDelegatorAddr())

	if finalRewards.IsZero() {
		baseDenom, _ := sdk.GetBaseDenom()
		if baseDenom == "" {
			baseDenom = sdk.DefaultBondDenom
		}

		// Note, we do not call the NewCoins constructor as we do not want the zero
		// coin removed.
		finalRewards = sdk.Coins{sdk.NewCoin(baseDenom, sdk.ZeroInt())}
	}

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeWithdrawRewards,
			sdk.NewAttribute(sdk.AttributeKeyAmount, finalRewards.String()),
			sdk.NewAttribute(types.AttributeKeyValidator, val.GetOperator().String()),
		),
	)

	return finalRewards, nil
}
