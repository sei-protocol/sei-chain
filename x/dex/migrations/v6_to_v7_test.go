package migrations_test

import (
	"encoding/binary"
	"testing"

	"github.com/cosmos/cosmos-sdk/store"
	"github.com/cosmos/cosmos-sdk/store/prefix"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/migrations"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmdb "github.com/tendermint/tm-db"
)

func TestMigrate6to7(t *testing.T) {
	storeKey := sdk.NewKVStoreKey(types.StoreKey)
	memStoreKey := storetypes.NewMemoryStoreKey(types.MemStoreKey)

	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(storeKey, sdk.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memStoreKey, sdk.StoreTypeMemory, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	ctx := sdk.NewContext(stateStore, tmproto.Header{}, false, log.NewNopLogger())

	// write old order ID
	store := prefix.NewStore(ctx.KVStore(storeKey), []byte{})
	oldID := make([]byte, 8)
	binary.BigEndian.PutUint64(oldID, 10)
	store.Set(types.KeyPrefix(types.NextOrderIDKey), oldID)

	// write old price state
	store = prefix.NewStore(ctx.KVStore(storeKey), append(
		append(
			append(types.KeyPrefix(types.PriceKey), types.KeyPrefix(keepertest.TestContract)...),
			types.KeyPrefix(keepertest.TestPriceDenom)...,
		),
		types.KeyPrefix(keepertest.TestAssetDenom)...),
	)

	price := types.Price{
		SnapshotTimestampInSeconds: 5,
		Price:                      sdk.MustNewDecFromStr("123.4"),
		Pair:                       &keepertest.TestPair,
	}
	priceBytes, _ := price.Marshal()
	store.Set(keeper.GetKeyForTs(price.SnapshotTimestampInSeconds), priceBytes)

	// register contract / pair
	store = prefix.NewStore(
		ctx.KVStore(storeKey),
		[]byte(keeper.ContractPrefixKey),
	)
	contract := types.ContractInfo{
		CodeId:            1,
		ContractAddr:      keepertest.TestContract,
		NeedOrderMatching: true,
	}
	contractBytes, _ := contract.Marshal()
	store.Set([]byte(contract.ContractAddr), contractBytes)

	store = prefix.NewStore(ctx.KVStore(storeKey), types.RegisteredPairPrefix(keepertest.TestContract))
	keyBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(keyBytes, 0)
	pairBytes, _ := keepertest.TestPair.Marshal()
	store.Set(keyBytes, pairBytes)

	err := migrations.V6ToV7(ctx, storeKey)
	require.Nil(t, err)

	store = prefix.NewStore(ctx.KVStore(storeKey), types.NextOrderIDPrefix(keepertest.TestContract))
	byteKey := types.KeyPrefix(types.NextOrderIDKey)
	bz := store.Get(byteKey)
	require.Equal(t, uint64(10), binary.BigEndian.Uint64(bz))

	store = prefix.NewStore(ctx.KVStore(storeKey), types.PricePrefix(keepertest.TestContract, keepertest.TestPriceDenom, keepertest.TestAssetDenom))
	key := keeper.GetKeyForTs(5)
	priceRes := types.Price{}
	b := store.Get(key)
	_ = priceRes.Unmarshal(b)
	require.Equal(t, price, priceRes)
}
