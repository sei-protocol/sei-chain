package keeper

import (
	"strconv"

	cosmostelemetry "github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

// SlashAndResetCounters do slash any operator who over criteria & clear all operators miss counter to zero
func (k Keeper) SlashAndResetCounters(ctx sdk.Context) {
	height := ctx.BlockHeight()
	distributionHeight := height - sdk.ValidatorUpdateDelay - 1

	minValidPerWindow := k.MinValidPerWindow(ctx)
	slashFraction := k.SlashFraction(ctx)
	powerReduction := k.StakingKeeper.PowerReduction(ctx)

	k.IterateVotePenaltyCounters(ctx, func(operator sdk.ValAddress, votePenaltyCounter types.VotePenaltyCounter) bool {
		// Calculate valid vote rate; (totalVotes - (MissCounter + AbstainCounter))/totalVotes
		// this accounts for changes in vote period within a window, and will take the overall success rate
		// as opposed to the one expected based on the number of vote period expected based on the ending slash window or vote period
		totalVotes := votePenaltyCounter.SuccessCount + votePenaltyCounter.AbstainCount + votePenaltyCounter.MissCount
		if totalVotes == 0 {
			ctx.Logger().Error("zero votes in penalty counter, this should never happen")
			return false
		}
		validVoteRate := sdk.NewDecFromInt(
			sdk.NewInt(int64(votePenaltyCounter.SuccessCount))).
			QuoInt64(int64(totalVotes))

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
				cosmostelemetry.IncrValidatorSlashedCounter(consAddr.String(), "oracle")
			}
		}

		ctx.EventManager().EmitEvent(
			sdk.NewEvent(types.EventTypeEndSlashWindow,
				sdk.NewAttribute(types.AttributeKeyOperator, operator.String()),
				sdk.NewAttribute(types.AttributeKeyMissCount, strconv.FormatUint(votePenaltyCounter.MissCount, 10)),
				sdk.NewAttribute(types.AttributeKeyAbstainCount, strconv.FormatUint(votePenaltyCounter.AbstainCount, 10)),
				sdk.NewAttribute(types.AttributeKeySuccessCount, strconv.FormatUint(votePenaltyCounter.SuccessCount, 10)),
			),
		)

		k.DeleteVotePenaltyCounter(ctx, operator)
		return false
	})
}
