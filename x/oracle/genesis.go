package oracle

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/sei-protocol/sei-chain/x/oracle/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
)

// InitGenesis initialize default parameters
// and the keeper's address to pubkey map
func InitGenesis(ctx sdk.Context, keeper keeper.Keeper, data *types.GenesisState) {
	keeper.SetParams(ctx, data.Params)
	for _, d := range data.FeederDelegations {
		voter, err := sdk.ValAddressFromBech32(d.ValidatorAddress)
		if err != nil {
			panic(err)
		}

		feeder, err := sdk.AccAddressFromBech32(d.FeederAddress)
		if err != nil {
			panic(err)
		}

		keeper.SetFeederDelegation(ctx, voter, feeder)
	}

	for _, ex := range data.ExchangeRates {
		keeper.SetBaseExchangeRate(ctx, ex.Denom, ex.ExchangeRate)
	}

	for _, pc := range data.PenaltyCounters {
		operator, err := sdk.ValAddressFromBech32(pc.ValidatorAddress)
		if err != nil {
			panic(err)
		}

		keeper.SetVotePenaltyCounter(ctx, operator, pc.VotePenaltyCounter.MissCount, pc.VotePenaltyCounter.AbstainCount, pc.VotePenaltyCounter.SuccessCount)
	}

	for _, av := range data.AggregateExchangeRateVotes {
		valAddr, err := sdk.ValAddressFromBech32(av.Voter)
		if err != nil {
			panic(err)
		}

		keeper.SetAggregateExchangeRateVote(ctx, valAddr, av)
	}

	for _, priceSnapshot := range data.PriceSnapshots {
		keeper.AddPriceSnapshot(ctx, priceSnapshot)
	}

	// check if the module account exists
	moduleAcc := keeper.GetOracleAccount(ctx)
	if moduleAcc == nil {
		panic(fmt.Sprintf("%s module account has not been set", types.ModuleName))
	}
}

// ExportGenesis writes the current store values
// to a genesis file, which can be imported again
// with InitGenesis
func ExportGenesis(ctx sdk.Context, keeper keeper.Keeper) *types.GenesisState {
	params := keeper.GetParams(ctx)
	feederDelegations := []types.FeederDelegation{}
	keeper.IterateFeederDelegations(ctx, func(valAddr sdk.ValAddress, feederAddr sdk.AccAddress) (stop bool) {
		feederDelegations = append(feederDelegations, types.FeederDelegation{
			FeederAddress:    feederAddr.String(),
			ValidatorAddress: valAddr.String(),
		})
		return false
	})

	exchangeRates := []types.ExchangeRateTuple{}
	keeper.IterateBaseExchangeRates(ctx, func(denom string, rate types.OracleExchangeRate) (stop bool) {
		exchangeRates = append(exchangeRates, types.ExchangeRateTuple{Denom: denom, ExchangeRate: rate.ExchangeRate})
		return false
	})

	penaltyCounters := []types.PenaltyCounter{}
	keeper.IterateVotePenaltyCounters(ctx, func(operator sdk.ValAddress, votePenaltyCounter types.VotePenaltyCounter) (stop bool) {
		penaltyCounters = append(penaltyCounters, types.PenaltyCounter{
			ValidatorAddress:   operator.String(),
			VotePenaltyCounter: &votePenaltyCounter,
		})
		return false
	})

	aggregateExchangeRateVotes := []types.AggregateExchangeRateVote{}
	keeper.IterateAggregateExchangeRateVotes(ctx, func(_ sdk.ValAddress, aggregateVote types.AggregateExchangeRateVote) bool {
		aggregateExchangeRateVotes = append(aggregateExchangeRateVotes, aggregateVote)
		return false
	})

	priceSnapshots := types.PriceSnapshots{}
	keeper.IteratePriceSnapshots(ctx, func(snapshot types.PriceSnapshot) bool {
		priceSnapshots = append(priceSnapshots, snapshot)
		return false
	})

	return types.NewGenesisState(
		params,
		exchangeRates,
		feederDelegations,
		penaltyCounters,
		aggregateExchangeRateVotes,
		priceSnapshots,
	)
}
