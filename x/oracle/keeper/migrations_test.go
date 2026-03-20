package keeper_test

import (
	"testing"

	gogotypes "github.com/gogo/protobuf/types"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/x/oracle/keeper"
	"github.com/sei-protocol/sei-chain/x/oracle/keeper/testutils"
	"github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/sei-protocol/sei-chain/x/oracle/utils"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/address"
)

func TestMigrate2to3(t *testing.T) {
	input := testutils.CreateTestInput(t)

	exchangeRate := sdk.NewDecWithPrec(839, int64(types.OracleDecPrecision)).MulInt64(utils.MicroUnit)

	// store the value with legacy proto
	store := input.Ctx.KVStore(input.OracleKeeper.GetStoreKey())
	bz := input.OracleKeeper.GetCdc().MustMarshal(&sdk.DecProto{Dec: exchangeRate})
	store.Set(types.GetExchangeRateKey(utils.MicroSeiDenom), bz)

	// validate legacy store value
	b := store.Get(types.GetExchangeRateKey(utils.MicroSeiDenom))
	dp := sdk.DecProto{}
	input.OracleKeeper.GetCdc().MustUnmarshal(b, &dp)
	require.Equal(t, dp.Dec, exchangeRate)

	// Migrate store
	m := keeper.NewMigrator(input.OracleKeeper)
	m.Migrate2to3(input.Ctx)

	// Get rate
	rate, lastUpdate, lastUpdateTimestamp, err := input.OracleKeeper.GetBaseExchangeRate(input.Ctx, utils.MicroSeiDenom)
	require.NoError(t, err)
	require.Equal(t, exchangeRate, rate)
	require.Equal(t, sdk.ZeroInt(), lastUpdate)
	require.Equal(t, int64(0), lastUpdateTimestamp)

	input.OracleKeeper.DeleteBaseExchangeRate(input.Ctx, utils.MicroAtomDenom)
	_, _, _, err = input.OracleKeeper.GetBaseExchangeRate(input.Ctx, utils.MicroAtomDenom)
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
	input := testutils.CreateTestInput(t)

	addr := testutils.ValAddrs[0]

	missCounter := uint64(12)

	// store the value with legacy proto
	store := input.Ctx.KVStore(input.OracleKeeper.GetStoreKey())
	bz := input.OracleKeeper.GetCdc().MustMarshal(&gogotypes.UInt64Value{Value: missCounter})
	store.Set(types.GetVotePenaltyCounterKey(addr), bz)

	// set for second validator
	store.Set(types.GetVotePenaltyCounterKey(testutils.ValAddrs[1]), bz)

	// confirm legacy store value
	b := store.Get(types.GetVotePenaltyCounterKey(addr))
	mc := gogotypes.UInt64Value{}
	input.OracleKeeper.GetCdc().MustUnmarshal(b, &mc)
	require.Equal(t, missCounter, mc.Value)

	// Migrate store
	m := keeper.NewMigrator(input.OracleKeeper)
	m.Migrate3to4(input.Ctx)

	// Get rate
	votePenaltyCounter := input.OracleKeeper.GetVotePenaltyCounter(input.Ctx, addr)
	require.Equal(t, types.VotePenaltyCounter{MissCount: missCounter, AbstainCount: 0}, votePenaltyCounter)

	input.OracleKeeper.DeleteVotePenaltyCounter(input.Ctx, addr)

	numPenaltyCounters := 0
	handler := func(operators sdk.ValAddress, votePenaltyCounter types.VotePenaltyCounter) (stop bool) {
		numPenaltyCounters++
		return false
	}
	input.OracleKeeper.IterateVotePenaltyCounters(input.Ctx, handler)

	require.True(t, numPenaltyCounters == 1)
}

func TestMigrate4to5(t *testing.T) {
	input := testutils.CreateTestInput(t)

	addr := testutils.ValAddrs[0]

	missCounter := uint64(12)
	oldPrevoteKey := []byte{0x04}
	genPrevoteKey := func(v sdk.ValAddress) []byte {
		return append(oldPrevoteKey, address.MustLengthPrefix(v)...)
	}

	// store the value with legacy proto
	store := input.Ctx.KVStore(input.OracleKeeper.GetStoreKey())
	// doesn't really matter what we set as the bytes so we're just using data from a previous test case
	bz := input.OracleKeeper.GetCdc().MustMarshal(&gogotypes.UInt64Value{Value: missCounter})
	store.Set(genPrevoteKey(addr), bz)

	// set for second validator
	store.Set(genPrevoteKey(testutils.ValAddrs[1]), bz)

	require.Equal(t, store.Has(genPrevoteKey(addr)), true)
	require.Equal(t, store.Has(genPrevoteKey(testutils.ValAddrs[1])), true)

	// Migrate store
	m := keeper.NewMigrator(input.OracleKeeper)
	m.Migrate4to5(input.Ctx)
	// should have been deleted from store
	require.Equal(t, store.Has(genPrevoteKey(addr)), false)
	require.Equal(t, store.Has(genPrevoteKey(testutils.ValAddrs[1])), false)
}

func TestMigrate5to6(t *testing.T) {
	input := testutils.CreateTestInput(t)

	addr := testutils.ValAddrs[0]
	input.Ctx.KVStore(input.OracleKeeper.GetStoreKey())
	input.OracleKeeper.SetVotePenaltyCounter(
		input.Ctx,
		addr,
		12,
		13,
		0,
	)

	// Migrate store
	m := keeper.NewMigrator(input.OracleKeeper)
	input.Ctx = input.Ctx.WithBlockHeight(int64(input.OracleKeeper.GetParams(input.Ctx).SlashWindow) + 10000)
	m.Migrate5To6(input.Ctx)

	// Get rate
	votePenaltyCounter := input.OracleKeeper.GetVotePenaltyCounter(input.Ctx, addr)
	require.Equal(t, types.VotePenaltyCounter{
		MissCount:    12,
		AbstainCount: 13,
		SuccessCount: 9975,
	}, votePenaltyCounter)
}
