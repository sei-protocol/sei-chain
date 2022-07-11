package keeper_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dex "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestGetPlaceSudoMsg(t *testing.T) {
	pair := types.Pair{PriceDenom: TEST_PRICE_DENOM, AssetDenom: TEST_ASSET_DENOM}
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.MemState.GetBlockOrders(TEST_CONTRACT, types.GetPairString(&pair)).AddOrder(
		types.Order{
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
	msgs := keeper.GetPlaceSudoMsg(ctx, TEST_CONTRACT, []types.Pair{pair})
	require.Equal(t, 2, len(msgs))
}

func TestGetDepositSudoMsg(t *testing.T) {
	testApp := keepertest.TestApp()
	ctx := testApp.BaseApp.NewContext(false, tmproto.Header{})
	bankkeeper := testApp.BankKeeper
	testAccount, _ := sdk.AccAddressFromBech32("sei1yezq49upxhunjjhudql2fnj5dgvcwjj87pn2wx")
	amounts := sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(1000000)))
	bankkeeper.MintCoins(ctx, minttypes.ModuleName, amounts)
	bankkeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, testAccount, amounts)
	keeper := testApp.DexKeeper
	keeper.MemState.GetDepositInfo(TEST_CONTRACT).AddDeposit(
		dex.DepositInfoEntry{
			Creator: testAccount.String(),
			Denom:   amounts[0].Denom,
			Amount:  sdk.NewDec(amounts[0].Amount.Int64()),
		},
	)
	msgs := keeper.GetDepositSudoMsg(ctx, TEST_CONTRACT)
	require.Equal(t, 1, len(msgs.OrderPlacements.Deposits))
}
