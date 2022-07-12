package keeper

import (
	"strconv"

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
		}

		winCount := votePeriodsPerWindow - (votePenaltyCounter.MissCount + votePenaltyCounter.AbstainCount)
		ctx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeEndSlashWindow,
				sdk.NewAttribute(types.AttributeKeyOperator, operator.String()),
				sdk.NewAttribute(types.AttributeKeyMissCount, strconv.FormatUint(votePenaltyCounter.MissCount, 10)),
				sdk.NewAttribute(types.AttributeKeyAbstainCount, strconv.FormatUint(votePenaltyCounter.AbstainCount, 10)),
				sdk.NewAttribute(types.AttributeKeyWinCount, strconv.FormatUint(winCount, 10)),
			),
		)

		k.DeleteVotePenaltyCounter(ctx, operator)
		return false
	})
}
