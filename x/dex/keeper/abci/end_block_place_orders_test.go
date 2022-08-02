package abci_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dex "github.com/sei-protocol/sei-chain/x/dex/cache"
	"github.com/sei-protocol/sei-chain/x/dex/keeper/abci"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	typesutils "github.com/sei-protocol/sei-chain/x/dex/types/utils"
	minttypes "github.com/sei-protocol/sei-chain/x/mint/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func TestGetPlaceSudoMsg(t *testing.T) {
	pair := types.Pair{PriceDenom: keepertest.TestPriceDenom, AssetDenom: keepertest.TestAssetDenom}
	keeper, ctx := keepertest.DexKeeper(t)
	keeper.MemState.GetBlockOrders(keepertest.TestContract, typesutils.GetPairString(&pair)).Add(
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
	keeper.MemState.GetDepositInfo(keepertest.TestContract).Add(
		&dex.DepositInfoEntry{
			Creator: testAccount.String(),
			Denom:   amounts[0].Denom,
			Amount:  sdk.NewDec(amounts[0].Amount.Int64()),
		},
	)
	wrapper := abci.KeeperWrapper{Keeper: &keeper}
	msgs := wrapper.GetDepositSudoMsg(ctx, keepertest.TestContract)
	require.Equal(t, 1, len(msgs.OrderPlacements.Deposits))
}
