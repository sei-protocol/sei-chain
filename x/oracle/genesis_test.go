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
	input.OracleKeeper.SetAggregateExchangeRatePrevote(input.Ctx, keeper.ValAddrs[0], types.NewAggregateExchangeRatePrevote(types.AggregateVoteHash{123}, keeper.ValAddrs[0], uint64(2)))
	input.OracleKeeper.SetAggregateExchangeRateVote(input.Ctx, keeper.ValAddrs[0], types.NewAggregateExchangeRateVote(types.ExchangeRateTuples{{Denom: "foo", ExchangeRate: sdk.NewDec(123)}}, keeper.ValAddrs[0]))
	input.OracleKeeper.SetTobinTax(input.Ctx, "denom", sdk.NewDecWithPrec(123, 3))
	input.OracleKeeper.SetTobinTax(input.Ctx, "denom2", sdk.NewDecWithPrec(123, 3))
	genesis := oracle.ExportGenesis(input.Ctx, input.OracleKeeper)

	newInput := keeper.CreateTestInput(t)
	oracle.InitGenesis(newInput.Ctx, newInput.OracleKeeper, genesis)
	newGenesis := oracle.ExportGenesis(newInput.Ctx, newInput.OracleKeeper)

	require.Equal(t, genesis, newGenesis)
}
