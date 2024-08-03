package abci_test

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/abci"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestGetPlaceSudoMsg(t *testing.T) {
	pair := types.Pair{PriceDenom: keepertest.TestPriceDenom, AssetDenom: keepertest.TestAssetDenom}
	keeper, ctx := keepertest.DexKeeper(t)
	dexutils.GetMemState(ctx.Context()).GetBlockOrders(ctx, keepertest.TestContract, pair).Add(
		&types.Order{
			Id:                1,
			Price:             sdk.OneDec(),
			Quantity:          sdk.OneDec(),
			PriceDenom:        "USDC",
			AssetDenom:        "ATOM",
			OrderType:         types.OrderType_LIMIT,
			PositionDirection: types.PositionDirection_LONG,
			Data:              "{\"position_effect\":\"OPEN\",\"leverage\":\"1\"}",
		},
	)
	wrapper := abci.KeeperWrapper{Keeper: keeper}
	msgs := wrapper.GetPlaceSudoMsg(ctx, keepertest.TestContract, []types.Pair{pair})
	require.Equal(t, 1, len(msgs))
}

func TestGetDepositSudoMsg(t *testing.T) {
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{})
	ctx = ctx.WithContext(context.WithValue(ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(testApp.GetMemKey(types.MemStoreKey))))
	testAccount, _ := sdk.AccAddressFromBech32("sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx")
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000)))
	bankkeeper := testApp.BankKeeper
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	err := bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, testAccount, amounts)
	require.Nil(t, err)
	// this send would happen in msg server
	err = bankkeeper.SendCoinsFromAccountToModule(ctx, testAccount, dextypes.ModuleName, amounts)
	require.Nil(t, err)
	keeper := testApp.DexKeeper
	dexutils.GetMemState(ctx.Context()).GetDepositInfo(ctx, keepertest.TestContract).Add(
		&types.DepositInfoEntry{
			Creator: testAccount.String(),
			Denom:   amounts[0].Denom,
			Amount:  sdk.NewDec(amounts[0].Amount.Int64()),
		},
	)
	wrapper := abci.KeeperWrapper{Keeper: &keeper}
	msgs := wrapper.GetDepositSudoMsg(ctx, keepertest.TestContract)
	require.Equal(t, 1, len(msgs.OrderPlacements.Deposits))

	contractAddr, _ := sdk.AccAddressFromBech32(keepertest.TestContract)
	contractBalance := testApp.BankKeeper.GetBalance(ctx, contractAddr, "usei")
	require.Equal(t, contractBalance.Amount.Int64(), int64(1000000))
}
