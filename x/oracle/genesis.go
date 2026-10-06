package oracle

import (
	"fmt"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"

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
