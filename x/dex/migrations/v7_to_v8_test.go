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

func TestMigrate7to8(t *testing.T) {
	storeKey := sdk.NewKVStoreKey(types.StoreKey)
	memStoreKey := storetypes.NewMemoryStoreKey(types.MemStoreKey)

	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(storeKey, sdk.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memStoreKey, sdk.StoreTypeMemory, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	ctx := sdk.NewContext(stateStore, tmproto.Header{}, false, log.NewNopLogger())

	// write old settlements
	store := prefix.NewStore(
		ctx.KVStore(storeKey),
		migrations.SettlementEntryPrefix(keepertest.TestContract, keepertest.TestPriceDenom, keepertest.TestAssetDenom),
	)
	settlements := types.Settlements{
		Entries: []*types.SettlementEntry{
			{
				Account:  keepertest.TestAccount,
				OrderId:  1,
				Quantity: sdk.MustNewDecFromStr("0.3"),
			},
			{
				Account:  keepertest.TestAccount,
				OrderId:  1,
				Quantity: sdk.MustNewDecFromStr("0.7"),
			},
		},
	}
	bz, _ := settlements.Marshal()
	store.Set(types.GetSettlementOrderIDPrefix(1, keepertest.TestAccount), bz)

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

	err := migrations.V7ToV8(ctx, storeKey)
	require.Nil(t, err)

	store = prefix.NewStore(
		ctx.KVStore(storeKey),
		migrations.SettlementEntryPrefix(keepertest.TestContract, keepertest.TestPriceDenom, keepertest.TestAssetDenom),
	)
	settlementEntry1Key := types.GetSettlementKey(1, keepertest.TestAccount, 0)
	settlementEntry1 := types.SettlementEntry{}
	settlementEntry1Bytes := store.Get(settlementEntry1Key)
	_ = settlementEntry1.Unmarshal(settlementEntry1Bytes)
	require.Equal(t, sdk.MustNewDecFromStr("0.3"), settlementEntry1.Quantity)
	settlementEntry2Key := types.GetSettlementKey(1, keepertest.TestAccount, 1)
	settlementEntry2 := types.SettlementEntry{}
	settlementEntry2Bytes := store.Get(settlementEntry2Key)
	_ = settlementEntry2.Unmarshal(settlementEntry2Bytes)
	require.Equal(t, sdk.MustNewDecFromStr("0.7"), settlementEntry2.Quantity)

	require.False(t, store.Has(types.GetSettlementOrderIDPrefix(1, keepertest.TestAccount)))

	store = prefix.NewStore(ctx.KVStore(storeKey), migrations.NextSettlementIDPrefix(keepertest.TestContract, keepertest.TestPriceDenom, keepertest.TestAssetDenom))
	key := make([]byte, 8)
	binary.BigEndian.PutUint64(key, uint64(1))
	require.Equal(t, uint64(2), binary.BigEndian.Uint64(store.Get(key)))
}
