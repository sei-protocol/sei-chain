package migrations_test

import (
	"encoding/binary"
	"github.com/sei-protocol/sei-chain/x/dex/migrations"
	"testing"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
)

func TestMigrate9to10(t *testing.T) {
	dexkeeper, ctx := keepertest.DexKeeper(t)
	// write old contract
	store := prefix.NewStore(
		ctx.KVStore(dexkeeper.GetStoreKey()),
		[]byte(keeper.ContractPrefixKey),
	)
	contract := types.ContractInfo{
		CodeId:            1,
		ContractAddr:      keepertest.TestContract,
		NeedOrderMatching: true,
	}
	contractBytes, _ := contract.Marshal()
	store.Set([]byte(contract.ContractAddr), contractBytes)

	// write old match results format
	store = prefix.NewStore(
		ctx.KVStore(dexkeeper.GetStoreKey()),
		types.MatchResultPrefix(keepertest.TestContract),
	)
	order1 := []*types.Order{{
		Id: 1,
	}}
	order2 := []*types.Order{{
		Id: 2,
	}}
	matchResult1 := types.MatchResult{
		Height:       int64(1),
		ContractAddr: keepertest.TestContract,
		Orders:       order1,
	}
	matchResult2 := types.MatchResult{
		Height:       int64(2),
		ContractAddr: keepertest.TestContract,
		Orders:       order2,
	}
	// Insert old match results format to blockHeights 1, 2
	// That is: <matchResultsKey>-<Height>
	height := 1
	bz, err := matchResult1.Marshal()
	if err != nil {
		panic(err)
	}
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, uint64(height))
	store.Set(key, bz)

	height = 2
	bz, err = matchResult2.Marshal()
	if err != nil {
		panic(err)
	}
	key = make([]byte, 8)
	binary.BigEndian.PutUint64(key, uint64(height))
	store.Set(key, bz)

	ctx = ctx.WithBlockHeight(3)

	// Perform migration
	err = migrations.V9ToV10(ctx, *dexkeeper)
	require.NoError(t, err)

	// We expect that everything is under the new format: <matchResultsKey>
	matchResult, found := dexkeeper.GetMatchResultState(ctx, keepertest.TestContract)
	require.True(t, found)
	require.Equal(t, types.MatchResult{Height: 3, ContractAddr: keepertest.TestContract, Orders: order2}, *matchResult)
	// All previous <matchResultKey>-<height> should be purged
	for i := 0; i < 2; i++ {
		store := prefix.NewStore(
			ctx.KVStore(dexkeeper.GetStoreKey()),
			types.MatchResultPrefix(keepertest.TestContract),
		)
		key := make([]byte, 8)
		binary.BigEndian.PutUint64(key, uint64(i))
		require.False(t, store.Has(key))
	}
}
