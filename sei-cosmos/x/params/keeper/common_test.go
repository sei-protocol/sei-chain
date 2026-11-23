package keeper_test

import (
	seiapp "github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	paramskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"
)

func testComponents() (*codec.LegacyAmino, sdk.Context, sdk.StoreKey, sdk.StoreKey, paramskeeper.Keeper) {
	encodingConfig := seiapp.MakeEncodingConfig()
	legacyAmino := createTestCodec()
	mkey := sdk.NewKVStoreKey("test")
	tkey := sdk.NewTransientStoreKey("transient_test")
	ctx := testutil.DefaultContext(mkey, tkey)
	keeper := paramskeeper.NewKeeper(encodingConfig.Marshaler, legacyAmino, mkey, tkey, encodingConfig.InterfaceRegistry)

	return legacyAmino, ctx, mkey, tkey, keeper
}

type invalid struct{}

type s struct {
	I int
}

func createTestCodec() *codec.LegacyAmino {
	cdc := codec.NewLegacyAmino()
	sdk.RegisterLegacyAminoCodec(cdc)
	cdc.RegisterConcrete(s{}, "test/s", nil)
	cdc.RegisterConcrete(invalid{}, "test/invalid", nil)
	return cdc
}
