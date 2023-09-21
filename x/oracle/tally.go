package oracle

import (
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/oracle/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

// Tally calculates the median and returns it. Sets the set of voters to be rewarded, i.e. voted within
// a reasonable spread from the weighted median to the store
// CONTRACT: pb must be sorted
func Tally(_ sdk.Context, pb types.ExchangeRateBallot, rewardBand sdk.Dec, validatorClaimMap map[string]types.Claim) (weightedMedian sdk.Dec) {
	weightedMedian = pb.WeightedMedianWithAssertion()

	standardDeviation := pb.StandardDeviation(weightedMedian)
	rewardSpread := weightedMedian.Mul(rewardBand.QuoInt64(2))

	if standardDeviation.GT(rewardSpread) {
		rewardSpread = standardDeviation
	}

	for _, vote := range pb {
		// Filter ballot winners
		key := vote.Voter.String()
		claim := validatorClaimMap[key]
		if vote.ExchangeRate.GTE(weightedMedian.Sub(rewardSpread)) &&
			vote.ExchangeRate.LTE(weightedMedian.Add(rewardSpread)) {

			claim.Weight += vote.Power
			claim.WinCount++
		}
		claim.DidVote = true
		validatorClaimMap[key] = claim
	}

	return
}

// ballot for the asset is passing the threshold amount of voting power
func ballotIsPassing(ballot types.ExchangeRateBallot, thresholdVotes sdk.Int) (sdk.Int, bool) {
	ballotPower := sdk.NewInt(ballot.Power())
	return ballotPower, !ballotPower.IsZero() && ballotPower.GTE(thresholdVotes)
}

// choose reference denom with the highest voter turnout
// If the voting power of the two denominations is the same,
// select reference denom in alphabetical order.
func pickReferenceDenom(ctx sdk.Context, k keeper.Keeper, voteTargets map[string]types.Denom, voteMap map[string]types.ExchangeRateBallot) (referenceDenom string, belowThresholdVoteMap map[string]types.ExchangeRateBallot) {
	largestBallotPower := int64(0)
	referenceDenom = ""
	belowThresholdVoteMap = map[string]types.ExchangeRateBallot{}

	totalBondedPower := sdk.TokensToConsensusPower(k.StakingKeeper.TotalBondedTokens(ctx), k.StakingKeeper.PowerReduction(ctx))
	voteThreshold := k.VoteThreshold(ctx)
	thresholdVotes := voteThreshold.MulInt64(totalBondedPower).RoundInt()

	for denom, ballot := range voteMap {
		// If denom is not in the voteTargets, or the ballot for it has failed, then skip
		// and remove it from voteMap for iteration efficiency
		if _, exists := voteTargets[denom]; !exists {
			delete(voteMap, denom)
			continue
		}

		ballotPower := int64(0)

		// If the ballot is not passed, remove it from the voteTargets array
		// to prevent slashing validators who did valid vote.
		if power, ok := ballotIsPassing(ballot, thresholdVotes); ok {
			ballotPower = power.Int64()
		} else {
			// add assets below threshold to separate map for tally evaluation
			belowThresholdVoteMap[denom] = voteMap[denom]
			delete(voteTargets, denom)
			delete(voteMap, denom)
			continue
		}

		if ballotPower > largestBallotPower || largestBallotPower == 0 {
			referenceDenom = denom
			largestBallotPower = ballotPower
		} else if largestBallotPower == ballotPower && referenceDenom > denom {
			referenceDenom = denom
		}
	}
	return referenceDenom, belowThresholdVoteMap
}
