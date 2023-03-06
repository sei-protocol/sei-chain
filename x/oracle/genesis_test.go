package oracle_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/x/oracle"
	"github.com/sei-protocol/sei-chain/x/oracle/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestExportInitGenesis(t *testing.T) {
	input, _ := setup(t)

	input.OracleKeeper.SetFeederDelegation(input.Ctx, keeper.ValAddrs[0], keeper.Addrs[1])
	input.OracleKeeper.SetBaseExchangeRate(input.Ctx, "denom", sdk.NewDec(123))
	input.OracleKeeper.SetAggregateExchangeRateVote(input.Ctx, keeper.ValAddrs[0], types.NewAggregateExchangeRateVote(types.ExchangeRateTuples{{Denom: "foo", ExchangeRate: sdk.NewDec(123)}}, keeper.ValAddrs[0]))
	input.OracleKeeper.SetVoteTarget(input.Ctx, "denom")
	input.OracleKeeper.SetVoteTarget(input.Ctx, "denom2")
	input.OracleKeeper.SetVotePenaltyCounter(input.Ctx, keeper.ValAddrs[0], 2, 3, 0)
	input.OracleKeeper.SetVotePenaltyCounter(input.Ctx, keeper.ValAddrs[1], 4, 5, 0)
	input.OracleKeeper.AddPriceSnapshot(input.Ctx, types.NewPriceSnapshot(
		types.PriceSnapshotItems{
			{
				Denom: "usei",
				OracleExchangeRate: types.OracleExchangeRate{
					ExchangeRate: sdk.NewDec(12),
					LastUpdate:   sdk.NewInt(3600),
				},
			},
			{
				Denom: "uatom",
				OracleExchangeRate: types.OracleExchangeRate{
					ExchangeRate: sdk.NewDec(10),
					LastUpdate:   sdk.NewInt(3600),
				},
			},
		},
		int64(3600),
	))
	input.OracleKeeper.AddPriceSnapshot(input.Ctx, types.NewPriceSnapshot(
		types.PriceSnapshotItems{
			{
				Denom: "usei",
				OracleExchangeRate: types.OracleExchangeRate{
					ExchangeRate: sdk.NewDec(15),
					LastUpdate:   sdk.NewInt(3700),
				},
			},
			{
				Denom: "uatom",
				OracleExchangeRate: types.OracleExchangeRate{
					ExchangeRate: sdk.NewDec(13),
					LastUpdate:   sdk.NewInt(3700),
				},
			},
		},
		int64(3700),
	))
	genesis := oracle.ExportGenesis(input.Ctx, input.OracleKeeper)

	newInput := keeper.CreateTestInput(t)
	oracle.InitGenesis(newInput.Ctx, newInput.OracleKeeper, genesis)
	newGenesis := oracle.ExportGenesis(newInput.Ctx, newInput.OracleKeeper)

	require.Equal(t, genesis, newGenesis)
}
