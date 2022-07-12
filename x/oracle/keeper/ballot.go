package keeper

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sei-protocol/sei-chain/x/oracle/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// OrganizeBallotByDenom collects all oracle votes for the period, categorized by the votes' denom parameter
func (k Keeper) OrganizeBallotByDenom(ctx sdk.Context, validatorClaimMap map[string]types.Claim) (votes map[string]types.ExchangeRateBallot) {
	votes = map[string]types.ExchangeRateBallot{}

	// Organize aggregate votes
	aggregateHandler := func(voterAddr sdk.ValAddress, vote types.AggregateExchangeRateVote) (stop bool) {
		// organize ballot only for the active validators
		claim, ok := validatorClaimMap[vote.Voter]

		if ok {
			power := claim.Power
			for _, tuple := range vote.ExchangeRateTuples {
				tmpPower := power
				if !tuple.ExchangeRate.IsPositive() {
					// Make the power of abstain vote zero
					tmpPower = 0
				}

				votes[tuple.Denom] = append(votes[tuple.Denom],
					types.NewVoteForTally(
						tuple.ExchangeRate,
						tuple.Denom,
						voterAddr,
						tmpPower,
					),
				)
			}

		}

		return false
	}

	k.IterateAggregateExchangeRateVotes(ctx, aggregateHandler)

	// sort created ballot
	for denom, ballot := range votes {
		sort.Sort(ballot)
		votes[denom] = ballot
	}

	return votes
}

// ClearBallots clears all tallied prevotes and votes from the store
func (k Keeper) ClearBallots(ctx sdk.Context, votePeriod uint64) {
	// Clear all aggregate prevotes
	k.IterateAggregateExchangeRatePrevotes(ctx, func(voterAddr sdk.ValAddress, aggregatePrevote types.AggregateExchangeRatePrevote) (stop bool) {
		if ctx.BlockHeight() > int64(aggregatePrevote.SubmitBlock+votePeriod) {
			k.DeleteAggregateExchangeRatePrevote(ctx, voterAddr)
		}

		return false
	})

	// Clear all aggregate votes
	k.IterateAggregateExchangeRateVotes(ctx, func(voterAddr sdk.ValAddress, aggregateVote types.AggregateExchangeRateVote) (stop bool) {
		k.DeleteAggregateExchangeRateVote(ctx, voterAddr)
		return false
	})
}

// ApplyWhitelist update vote target denom list with params whitelist
func (k Keeper) ApplyWhitelist(ctx sdk.Context, whitelist types.DenomList, voteTargets map[string]types.Denom) {
	// check is there any update in whitelist params
	updateRequired := false
	if len(voteTargets) != len(whitelist) {
		updateRequired = true
	} else {
		for _, item := range whitelist {
			if _, ok := voteTargets[item.Name]; !ok {
				updateRequired = true
				break
			}
		}
	}

	if updateRequired {
		k.ClearVoteTargets(ctx)

		for _, item := range whitelist {
			k.SetVoteTarget(ctx, item.Name)

			// Register meta data to bank module
			if _, ok := k.bankKeeper.GetDenomMetaData(ctx, item.Name); !ok {
				base := item.Name
				display := base[1:]

				k.bankKeeper.SetDenomMetaData(ctx, banktypes.Metadata{
					Description: display,
					DenomUnits: []*banktypes.DenomUnit{
						{Denom: "u" + display, Exponent: uint32(0), Aliases: []string{"micro" + display}},
						{Denom: "m" + display, Exponent: uint32(3), Aliases: []string{"milli" + display}},
						{Denom: display, Exponent: uint32(6), Aliases: []string{}},
					},
					Base:    base,
					Display: display,
					Name:    fmt.Sprintf("%s TERRA", strings.ToUpper(display)),
					Symbol:  fmt.Sprintf("%sT", strings.ToUpper(display[:len(display)-1])),
				})
			}
		}
	}
}
