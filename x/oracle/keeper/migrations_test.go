package keeper

import (
	"testing"

	gogotypes "github.com/gogo/protobuf/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"

	sdk "github.com/cosmos/cosmos-sdk/types"
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

func TestMigrate3to4(t *testing.T) {
	input := CreateTestInput(t)

	addr := ValAddrs[0]

	missCounter := uint64(12)

	// store the value with legacy proto
	store := input.Ctx.KVStore(input.OracleKeeper.storeKey)
	bz := input.OracleKeeper.cdc.MustMarshal(&gogotypes.UInt64Value{Value: missCounter})
	store.Set(types.GetVotePenaltyCounterKey(addr), bz)

	// set for second validator
	store.Set(types.GetVotePenaltyCounterKey(ValAddrs[1]), bz)

	// confirm legacy store value
	b := store.Get(types.GetVotePenaltyCounterKey(addr))
	mc := gogotypes.UInt64Value{}
	input.OracleKeeper.cdc.MustUnmarshal(b, &mc)
	require.Equal(t, missCounter, mc.Value)

	// Migrate store
	m := NewMigrator(input.OracleKeeper)
	m.Migrate3to4(input.Ctx)

	// Get rate
	votePenaltyCounter := input.OracleKeeper.GetVotePenaltyCounter(input.Ctx, addr)
	require.Equal(t, types.VotePenaltyCounter{MissCount: missCounter, AbstainCount: 0}, votePenaltyCounter)

	input.OracleKeeper.DeleteVotePenaltyCounter(input.Ctx, addr)
	votePenaltyCounter = input.OracleKeeper.GetVotePenaltyCounter(input.Ctx, addr) //nolint:staticcheck // no need to use this.

	numPenaltyCounters := 0
	handler := func(operators sdk.ValAddress, votePenaltyCounter types.VotePenaltyCounter) (stop bool) {
		numPenaltyCounters++
		return false
	}
	input.OracleKeeper.IterateVotePenaltyCounters(input.Ctx, handler)

	require.True(t, numPenaltyCounters == 1)
}
