package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

// SlashAndResetCounters do slash any operator who over criteria & clear all operators miss counter to zero
func (k Keeper) SlashAndResetCounters(ctx sdk.Context) {
	height := ctx.BlockHeight()
	distributionHeight := height - sdk.ValidatorUpdateDelay - 1

	// slash_window / vote_period
	votePeriodsPerWindow := uint64(
		sdk.NewDec(int64(k.SlashWindow(ctx))).
			QuoInt64(int64(k.VotePeriod(ctx))).
			TruncateInt64(),
	)
	minValidPerWindow := k.MinValidPerWindow(ctx)
	slashFraction := k.SlashFraction(ctx)
	powerReduction := k.StakingKeeper.PowerReduction(ctx)

	k.IterateVotePenaltyCounters(ctx, func(operator sdk.ValAddress, votePenaltyCounter types.VotePenaltyCounter) bool {

		// Calculate valid vote rate; (SlashWindow - MissCounter)/SlashWindow
		validVoteRate := sdk.NewDecFromInt(
			sdk.NewInt(int64(votePeriodsPerWindow - votePenaltyCounter.MissCount))).
			QuoInt64(int64(votePeriodsPerWindow))

		// Calculate valid vote rate; (SlashWindow - AbstainCounter)/SlashWindow
		validNonAbstainVoteRate := sdk.NewDecFromInt(
			sdk.NewInt(int64(votePeriodsPerWindow - votePenaltyCounter.AbstainCount))).
			QuoInt64(int64(votePeriodsPerWindow))

		// Penalize the validator whose the valid vote rate is smaller than min threshold
		if validVoteRate.LT(minValidPerWindow) {
			validator := k.StakingKeeper.Validator(ctx, operator)
			if validator.IsBonded() && !validator.IsJailed() {
				consAddr, err := validator.GetConsAddr()
				if err != nil {
					panic(err)
				}

				k.StakingKeeper.Slash(
					ctx, consAddr,
					distributionHeight, validator.GetConsensusPower(powerReduction), slashFraction,
				)
				k.StakingKeeper.Jail(ctx, consAddr)
			}
		} else if validNonAbstainVoteRate.LT(minValidPerWindow) {
			// if we dont slash + jail for missing, we still need to evaluate for abstaining
			// this way, we dont penalize for both misses and abstaining in one vote period
			validator := k.StakingKeeper.Validator(ctx, operator)
			if validator.IsBonded() && !validator.IsJailed() {
				consAddr, err := validator.GetConsAddr()
				if err != nil {
					panic(err)
				}

				k.StakingKeeper.Slash(
					ctx, consAddr,
					distributionHeight, validator.GetConsensusPower(powerReduction), slashFraction,
				)
			}
		}

		k.DeleteVotePenaltyCounter(ctx, operator)
		return false
	})
}
