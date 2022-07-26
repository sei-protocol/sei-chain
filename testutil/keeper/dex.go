package keeper

import (
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/store"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	typesparams "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/x/dex/keeper"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmdb "github.com/tendermint/tm-db"
)

const (
	TestAccount    = "accnt"
	TestContract   = "tc"
	TestPriceDenom = "usdc"
	TestAssetDenom = "atom"
)

var (
	TestTicksize = sdk.OneDec()
	TestPair     = types.Pair{
		PriceDenom: TestPriceDenom,
		AssetDenom: TestAssetDenom,
		Ticksize:   &TestTicksize,
	}
)

func TestApp() *app.App {
	return app.Setup(false)
}

func DexKeeper(t testing.TB) (*keeper.Keeper, sdk.Context) {
	storeKey := sdk.NewKVStoreKey(types.StoreKey)
	memStoreKey := storetypes.NewMemoryStoreKey(types.MemStoreKey)

	db := tmdb.NewMemDB()
	stateStore := store.NewCommitMultiStore(db)
	stateStore.MountStoreWithDB(storeKey, sdk.StoreTypeIAVL, db)
	stateStore.MountStoreWithDB(memStoreKey, sdk.StoreTypeMemory, nil)
	require.NoError(t, stateStore.LoadLatestVersion())

	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	paramsSubspace := typesparams.NewSubspace(cdc,
		types.Amino,
		storeKey,
		memStoreKey,
		"DexParams",
	)
	k := keeper.NewPlainKeeper(
		cdc,
		storeKey,
		memStoreKey,
		paramsSubspace,
	)

	ctx := sdk.NewContext(stateStore, tmproto.Header{}, false, log.NewNopLogger())

	// Initialize params
	k.SetParams(ctx, types.DefaultParams())

	return k, ctx
}

func CreateAssetMetadata(keeper *keeper.Keeper, ctx sdk.Context) types.AssetMetadata {
	ibcInfo := types.AssetIBCInfo{
		SourceChannel: "channel-1",
		DstChannel:    "channel-2",
		SourceDenom:   "uusdc",
		SourceChainID: "axelar",
	}

	denomUnit := banktypes.DenomUnit{
		Denom:    "ibc/D189335C6E4A68B513C10AB227BF1C1D38C746766278BA3EEB4FB14124F1D858",
		Exponent: 0,
		Aliases:  []string{"axlusdc", "usdc"},
	}

	var denomUnits []*banktypes.DenomUnit
	denomUnits = append(denomUnits, &denomUnit)

	metadata := banktypes.Metadata{
		Description: "Circle's stablecoin on Axelar",
		DenomUnits:  denomUnits,
		Base:        "ibc/D189335C6E4A68B513C10AB227BF1C1D38C746766278BA3EEB4FB14124F1D858",
		Name:        "USD Coin",
		Display:     "axlusdc",
		Symbol:      "USDC",
	}

	item := types.AssetMetadata{
		IbcInfo:   &ibcInfo,
		TypeAsset: "erc20",
		Metadata:  metadata,
	}

	keeper.SetAssetMetadata(ctx, item)

	return item
}

func SeedPriceSnapshot(ctx sdk.Context, k *keeper.Keeper, price string, timestamp uint64) {
	priceSnapshot := types.Price{
		SnapshotTimestampInSeconds: timestamp,
		Price:                      sdk.MustNewDecFromStr(price),
		Pair:                       &TestPair,
	}
	k.SetPriceState(ctx, priceSnapshot, TestContract)
}

func CreateNLongBook(keeper *keeper.Keeper, ctx sdk.Context, n int) []types.LongBook {
	items := make([]types.LongBook, n)
	for i := range items {
		items[i].Entry = &types.OrderEntry{
			Price:      sdk.NewDec(int64(i)),
			Quantity:   sdk.NewDec(int64(i)),
			PriceDenom: TestPriceDenom,
			AssetDenom: TestAssetDenom,
		}
		items[i].Price = sdk.NewDec(int64(i))
		keeper.SetLongBook(ctx, TestContract, items[i])
	}
	return items
}

func CreateNShortBook(keeper *keeper.Keeper, ctx sdk.Context, n int) []types.ShortBook {
	items := make([]types.ShortBook, n)
	for i := range items {
		items[i].Entry = &types.OrderEntry{
			Price:      sdk.NewDec(int64(i)),
			Quantity:   sdk.NewDec(int64(i)),
			PriceDenom: TestPriceDenom,
			AssetDenom: TestAssetDenom,
		}
		items[i].Price = sdk.NewDec(int64(i))
		keeper.SetShortBook(ctx, TestContract, items[i])
	}
	return items
}

func CreateNSettlements(keeper *keeper.Keeper, ctx sdk.Context, n int) []types.Settlements {
	items := make([]types.Settlements, n)
	for i := range items {
		acct := "test_account" + strconv.Itoa(i)
		entry := types.SettlementEntry{
			Account:    acct,
			PriceDenom: "usdc" + strconv.Itoa(i),
			AssetDenom: "sei" + strconv.Itoa(i),
			OrderId:    uint64(i),
		}
		entries := []*types.SettlementEntry{&entry}
		items[i].Entries = entries
		keeper.SetSettlements(ctx, TestContract, "usdc"+strconv.Itoa(i), "sei"+strconv.Itoa(i), items[i])
	}
	return items
}
