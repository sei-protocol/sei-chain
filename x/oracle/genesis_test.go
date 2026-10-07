package oracle_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/x/oracle"
	"github.com/sei-protocol/sei-chain/x/oracle/keeper/testutils"
	"github.com/sei-protocol/sei-chain/x/oracle/types"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
)

func TestInitGenesis(t *testing.T) {
	input := testutils.CreateTestInput(t)
	params := types.DefaultParams()
	params.VotePeriod = 1
	vote := types.NewAggregateExchangeRateVote(types.ExchangeRateTuples{{Denom: "foo", ExchangeRate: sdk.NewDec(123)}}, testutils.ValAddrs[0])
	penalty := types.VotePenaltyCounter{MissCount: 2, AbstainCount: 3, SuccessCount: 4}
	snapshot := types.NewPriceSnapshot(types.PriceSnapshotItems{{
		Denom: "usei",
		OracleExchangeRate: types.OracleExchangeRate{
			ExchangeRate: sdk.NewDec(12),
			LastUpdate:   sdk.NewInt(3600),
		},
	}}, 3600)
	genesis := types.NewGenesisState(
		params,
		[]types.ExchangeRateTuple{{Denom: "denom", ExchangeRate: sdk.NewDec(123)}},
		[]types.FeederDelegation{{FeederAddress: testutils.Addrs[1].String(), ValidatorAddress: testutils.ValAddrs[0].String()}},
		[]types.PenaltyCounter{{ValidatorAddress: testutils.ValAddrs[0].String(), VotePenaltyCounter: &penalty}},
		[]types.AggregateExchangeRateVote{vote},
		types.PriceSnapshots{snapshot},
	)

	oracle.InitGenesis(input.Ctx, input.OracleKeeper, genesis)

	require.Equal(t, params, input.OracleKeeper.GetParams(input.Ctx))
	rate, _, _, err := input.OracleKeeper.GetBaseExchangeRate(input.Ctx, "denom")
	require.NoError(t, err)
	require.Equal(t, sdk.NewDec(123), rate)
	require.Equal(t, testutils.Addrs[1], input.OracleKeeper.GetFeederDelegation(input.Ctx, testutils.ValAddrs[0]))
	require.Equal(t, penalty, input.OracleKeeper.GetVotePenaltyCounter(input.Ctx, testutils.ValAddrs[0]))
	storedVote, err := input.OracleKeeper.GetAggregateExchangeRateVote(input.Ctx, testutils.ValAddrs[0])
	require.NoError(t, err)
	require.Equal(t, vote, storedVote)
	require.Equal(t, snapshot, input.OracleKeeper.GetPriceSnapshot(input.Ctx, 3600))
}
