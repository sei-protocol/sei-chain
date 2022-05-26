package oracle

import (
	"time"

	"github.com/sei-protocol/sei-chain/x/oracle/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func EndBlocker(ctx sdk.Context, k keeper.Keeper) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyEndBlocker)

	params := k.GetParams(ctx)
	if utils.IsPeriodLastBlock(ctx, params.VotePeriod) {

		// Build claim map over all validators in active set
		validatorClaimMap := make(map[string]types.Claim)

		maxValidators := k.StakingKeeper.MaxValidators(ctx)
		iterator := k.StakingKeeper.ValidatorsPowerStoreIterator(ctx)
		defer iterator.Close()

		powerReduction := k.StakingKeeper.PowerReduction(ctx)

		i := 0
		for ; iterator.Valid() && i < int(maxValidators); iterator.Next() {
			validator := k.StakingKeeper.Validator(ctx, iterator.Value())

			// Exclude not bonded validator
			if validator.IsBonded() {
				valAddr := validator.GetOperator()
				validatorClaimMap[valAddr.String()] = types.NewClaim(validator.GetConsensusPower(powerReduction), 0, 0, valAddr)
				i++
			}
		}

		voteTargets := make(map[string]types.Denom)
		totalTargets := 0
		k.IterateVoteTargets(ctx, func(denom string, denomInfo types.Denom) bool {
			voteTargets[denom] = denomInfo
			totalTargets++
			return false
		})

		// Organize votes to ballot by denom
		// NOTE: **Filter out inactive or jailed validators**
		// NOTE: **Make abstain votes to have zero vote power**
		voteMap := k.OrganizeBallotByDenom(ctx, validatorClaimMap)
		// belowThresholdVoteMap has assets that failed to meet threshold
		referenceDenom, belowThresholdVoteMap := pickReferenceDenom(ctx, k, voteTargets, voteMap)

		if referenceDenom != "" {
			// make voteMap of Reference denom to calculate cross exchange rates
			ballotRD := voteMap[referenceDenom]
			voteMapRD := ballotRD.ToMap()

			var exchangeRateRD sdk.Dec = ballotRD.WeightedMedianWithAssertion()

			// Iterate through ballots and update exchange rates; drop if not enough votes have been achieved.
			for denom, ballot := range voteMap {

				// Convert ballot to cross exchange rates
				if denom != referenceDenom {

					ballot = ballot.ToCrossRateWithSort(voteMapRD)
				}

				// Get weighted median of cross exchange rates
				exchangeRate := Tally(ctx, ballot, params.RewardBand, validatorClaimMap)

				// Transform into the original form base/quote
				if denom != referenceDenom {
					exchangeRate = exchangeRateRD.Quo(exchangeRate)
				}

				// Set the exchange rate, emit ABCI event
				k.SetBaseExchangeRateWithEvent(ctx, denom, exchangeRate)
			}

			for _, ballot := range belowThresholdVoteMap {
				// perform tally for below threshold assets to calculate total win count
				Tally(ctx, ballot, params.RewardBand, validatorClaimMap)
			}
		} else {
			// in this case, all assets would be in the belowThresholdVoteMap
			for _, ballot := range belowThresholdVoteMap {
				// perform tally for below threshold assets to calculate total win count
				Tally(ctx, ballot, params.RewardBand, validatorClaimMap)
			}
		}

		//---------------------------
		// Do miss counting & slashing
		for _, claim := range validatorClaimMap {
			// Skip abstain & valid voters
			// we require validator to have submitted in-range data
			// for all assets to not be counted as a miss
			if int(claim.WinCount) == totalTargets {
				continue
			}

			// Increase miss counter
			k.SetMissCounter(ctx, claim.Recipient, k.GetMissCounter(ctx, claim.Recipient)+1)
		}

		// Clear the ballot
		k.ClearBallots(ctx, params.VotePeriod)

		// Update vote targets
		k.ApplyWhitelist(ctx, params.Whitelist, voteTargets)
	}

	// Do slash who did miss voting over threshold and
	// reset miss counters of all validators at the last block of slash window
	if utils.IsPeriodLastBlock(ctx, params.SlashWindow) {
		k.SlashAndResetMissCounters(ctx)
	}
}
