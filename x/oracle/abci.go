package oracle

import (
	"sort"
	"time"

	"github.com/sei-protocol/sei-chain/utils/metrics"
	"github.com/sei-protocol/sei-chain/x/oracle/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"

	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func MidBlocker(ctx sdk.Context, k keeper.Keeper) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyMidBlocker)

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
				validatorClaimMap[valAddr.String()] = types.NewClaim(validator.GetConsensusPower(powerReduction), 0, 0, valAddr, false)
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

			exchangeRateRD := ballotRD.WeightedMedianWithAssertion()

			// Iterate through ballots and update exchange rates; drop if not enough votes have been achieved.
			keys := make([]string, len(voteMap))
			j := 0
			for denom := range voteMap {
				keys[j] = denom
				j++
			}
			sort.Strings(keys)
			for _, denom := range keys {
				ballot := voteMap[denom]
				// Convert ballot to cross exchange rates
				if denom != referenceDenom {
					ballot = ballot.ToCrossRateWithSort(voteMapRD)
				}

				// Get weighted median of cross exchange rates
				exchangeRate := Tally(ctx, ballot, params.RewardBand, validatorClaimMap)

				// if exchange rate is somehow 0, exclude it from ballot?
				if exchangeRate.IsZero() {
					// skip this denom
					continue
				}

				// Transform into the original form base/quote
				if denom != referenceDenom {
					exchangeRate = exchangeRateRD.Quo(exchangeRate)
				}

				// Set the exchange rate, emit ABCI event
				metrics.IncrPriceUpdateDenom(denom)
				k.SetBaseExchangeRateWithEvent(ctx, denom, exchangeRate)
			}
		}

		belowThresholdKeys := make([]string, len(belowThresholdVoteMap))
		n := 0
		for denom := range belowThresholdVoteMap {
			belowThresholdKeys[n] = denom
			n++
		}
		sort.Strings(belowThresholdKeys)
		// in this case, all assets would be in the belowThresholdVoteMap

		for _, denom := range belowThresholdKeys {
			ballot := belowThresholdVoteMap[denom]
			// perform tally for below threshold assets to calculate total win count
			UpdateDidVote(ctx, ballot, validatorClaimMap)
		}

		//---------------------------
		// Do miss counting & slashing
		for _, claim := range validatorClaimMap {
			// we require validator to have submitted in-range data
			// for all assets to not be counted as a miss
			if int(claim.WinCount) == totalTargets {
				k.IncrementSuccessCount(ctx, claim.Recipient)
				continue
			}
			if !claim.DidVote {
				k.IncrementAbstainCount(ctx, claim.Recipient)
				continue
			}

			// Increase miss counter
			k.IncrementMissCount(ctx, claim.Recipient)
		}

		// Clear the ballot
		k.ClearBallots(ctx, params.VotePeriod)

		// Update vote targets
		k.ApplyWhitelist(ctx, params.Whitelist, voteTargets)

		priceSnapshotItems := []types.PriceSnapshotItem{}
		k.IterateBaseExchangeRates(ctx, func(denom string, exchangeRate types.OracleExchangeRate) bool {
			priceSnapshotItem := types.PriceSnapshotItem{
				Denom:              denom,
				OracleExchangeRate: exchangeRate,
			}
			priceSnapshotItems = append(priceSnapshotItems, priceSnapshotItem)
			return false
		})
		if len(priceSnapshotItems) > 0 {
			priceSnapshot := types.PriceSnapshot{
				SnapshotTimestamp:  ctx.BlockTime().Unix(),
				PriceSnapshotItems: priceSnapshotItems,
			}
			k.AddPriceSnapshot(ctx, priceSnapshot)
		}
	}

}

func EndBlocker(ctx sdk.Context, k keeper.Keeper) {
	defer telemetry.ModuleMeasureSince(types.ModuleName, time.Now(), telemetry.MetricKeyEndBlocker)

	params := k.GetParams(ctx)
	// Do slash who did miss voting over threshold and
	// reset miss counters of all validators at the last block of slash window
	if utils.IsPeriodLastBlock(ctx, params.SlashWindow) {
		k.SlashAndResetCounters(ctx)
		// Compare vote targets and actives and remove excess feeds
		k.RemoveExcessFeeds(ctx)
	}
}
