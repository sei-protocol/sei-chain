package keeper_test

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/codec"
	codectypes "github.com/sei-protocol/sei-chain/sei-cosmos/codec/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"

	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	crisiskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/crisis/keeper"
	crisistypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/crisis/types"
	paramskeeper "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/keeper"
	paramstypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/params/types"
)

type noopSupplyKeeper struct{}

func (noopSupplyKeeper) SendCoinsFromAccountToModule(sdk.Context, sdk.AccAddress, string, sdk.Coins) error {
	return nil
}

func createTestKeeper() crisiskeeper.Keeper {
	legacyAmino := codec.NewLegacyAmino()
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	key := sdk.NewKVStoreKey(paramstypes.StoreKey)
	tkey := sdk.NewTransientStoreKey(paramstypes.TStoreKey)
	paramsKeeper := paramskeeper.NewKeeper(cdc, legacyAmino, key, tkey)

	return crisiskeeper.NewKeeper(paramsKeeper.Subspace(crisistypes.ModuleName), 1, noopSupplyKeeper{}, "fee_collector")
}

func TestInvariants(t *testing.T) {
	keeper := createTestKeeper()

	require.Equal(t, uint(1), keeper.InvCheckPeriod())

	orgInvRoutes := keeper.Routes()
	keeper.RegisterRoute("testModule", "testRoute", func(sdk.Context) (string, bool) { return "", false })
	require.Equal(t, len(orgInvRoutes)+1, len(keeper.Routes()))
}

func TestAssertInvariants(t *testing.T) {
	keeper := createTestKeeper()
	ctx := sdk.NewContext(nil, tmproto.Header{}, true)

	keeper.RegisterRoute("testModule", "testRoute1", func(sdk.Context) (string, bool) { return "", false })
	require.NotPanics(t, func() { keeper.AssertInvariants(ctx) })

	keeper.RegisterRoute("testModule", "testRoute2", func(sdk.Context) (string, bool) { return "", true })
	require.Panics(t, func() { keeper.AssertInvariants(ctx) })
}
