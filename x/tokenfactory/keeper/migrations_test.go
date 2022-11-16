package keeper

import (
	"testing"

	gogotypes "github.com/gogo/protobuf/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/address"
)

func TestMigrate2to3(t *testing.T) {
	input := CreateTestInput(t)

	exchangeRate := sdk.NewDecWithPrec(839, int64(OracleDecPrecision)).MulInt64(utils.MicroUnit)

	// store the value with legacy proto
	store := input.Ctx.KVStore(input.OracleKeeper.storeKey)
	bz := input.OracleKeeper.cdc.MustMarshal(&sdk.DecProto{Dec: exchangeRate})
	store.Set(types.GetExchangeRateKey(utils.MicroSeiDenom), bz)

	// validate legacy store value
	b := store.Get(types.GetExchangeRateKey(utils.MicroSeiDenom))
	dp := sdk.DecProto{}
	input.OracleKeeper.cdc.MustUnmarshal(b, &dp)
	require.Equal(t, dp.Dec, exchangeRate)

	// Migrate store
	m := NewMigrator(input.OracleKeeper)
	m.Migrate2to3(input.Ctx)

	// Get rate
	rate, lastUpdate, err := input.OracleKeeper.GetBaseExchangeRate(input.Ctx, utils.MicroSeiDenom)
	require.NoError(t, err)
	require.Equal(t, exchangeRate, rate)
	require.Equal(t, sdk.ZeroInt(), lastUpdate)

	input.OracleKeeper.DeleteBaseExchangeRate(input.Ctx, utils.MicroAtomDenom)
	_, _, err = input.OracleKeeper.GetBaseExchangeRate(input.Ctx, utils.MicroAtomDenom)
	require.Error(t, err)

	numExchangeRates := 0
	handler := func(denom string, exchangeRate types.OracleExchangeRate) (stop bool) {
		numExchangeRates = numExchangeRates + 1
		return false
	}
	input.OracleKeeper.IterateBaseExchangeRates(input.Ctx, handler)

	require.True(t, numExchangeRates == 1)
}
