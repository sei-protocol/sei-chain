package acldexmapping_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	"github.com/k0kubun/pp/v3"
	dexacl "github.com/sei-protocol/sei-chain/aclmapping/dex"
	aclutils "github.com/sei-protocol/sei-chain/aclmapping/utils"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	keepertest "github.com/sei-protocol/sei-chain/testutil/keeper"
	dexcache "github.com/sei-protocol/sei-chain/x/dex/cache"
	dexmsgserver "github.com/sei-protocol/sei-chain/x/dex/keeper/msgserver"
	"github.com/sei-protocol/sei-chain/x/dex/types"
	dextypes "github.com/sei-protocol/sei-chain/x/dex/types"
	dexutils "github.com/sei-protocol/sei-chain/x/dex/utils"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type KeeperTestSuite struct {
	apptesting.KeeperTestHelper

	queryClient         dextypes.QueryClient
	msgServer           dextypes.MsgServer
	defaultDenom        string
	defaultExchangeRate string
	initialBalance      sdk.Coins
	creator             string
	contract            string

	msgPlaceOrders  *dextypes.MsgPlaceOrders
	msgCancelOrders *dextypes.MsgCancelOrders
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(KeeperTestSuite))
}

// Runs before each test case
func (suite *KeeperTestSuite) SetupTest() {
	suite.Setup()
}

// Explicitly only run once during setup
func (suite *KeeperTestSuite) PrepareTest() {
	suite.defaultDenom = "usei"
	suite.defaultExchangeRate = fmt.Sprintf("%dusei", sdk.NewDec(1700))

	suite.initialBalance = sdk.Coins{sdk.NewInt64Coin(suite.defaultDenom, 100000000000)}
	suite.initialBalance = sdk.Coins{sdk.NewInt64Coin("usei", 100000000000)}
	suite.FundAcc(suite.TestAccs[0], suite.initialBalance)

	suite.queryClient = dextypes.NewQueryClient(suite.QueryHelper)
	suite.msgServer = dexmsgserver.NewMsgServerImpl(suite.App.DexKeeper)

	msgValidator := sdkacltypes.NewMsgValidator(aclutils.StoreKeyToResourceTypePrefixMap)
	suite.Ctx = suite.Ctx.WithMsgValidator(msgValidator)

	suite.Ctx = suite.Ctx.WithBlockHeight(10)
	suite.Ctx = suite.Ctx.WithBlockTime(time.Unix(333, 0))

	suite.creator = suite.TestAccs[0].String()
	suite.contract = "sei14hj2tavq8fpesdwxxcu44rty3hh90vhujrvcmstl4zr3txmfvw9sh9m79m"

	suite.App.DexKeeper.AddRegisteredPair(suite.Ctx, suite.contract, keepertest.TestPair)
	suite.App.DexKeeper.SetPriceTickSizeForPair(suite.Ctx, suite.contract, keepertest.TestPair, *keepertest.TestPair.PriceTicksize)
	suite.App.DexKeeper.SetQuantityTickSizeForPair(suite.Ctx, suite.contract, keepertest.TestPair, *keepertest.TestPair.PriceTicksize)

	suite.msgPlaceOrders = &types.MsgPlaceOrders{
		Creator:      suite.creator,
		ContractAddr: suite.contract,
		Orders: []*types.Order{
			{
				Price:             sdk.MustNewDecFromStr("10"),
				Quantity:          sdk.MustNewDecFromStr("10"),
				Data:              "",
				PositionDirection: types.PositionDirection_LONG,
				OrderType:         types.OrderType_LIMIT,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
			},
			{
				Price:             sdk.MustNewDecFromStr("20"),
				Quantity:          sdk.MustNewDecFromStr("5"),
				Data:              "",
				PositionDirection: types.PositionDirection_SHORT,
				OrderType:         types.OrderType_MARKET,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
			},
		},
	}

	suite.msgCancelOrders = &types.MsgCancelOrders{
		Creator:      suite.creator,
		ContractAddr: suite.contract,
		Cancellations: []*types.Cancellation{
			{
				Id:                1,
				Price:             sdk.MustNewDecFromStr("10"),
				Creator:           suite.creator,
				PositionDirection: types.PositionDirection_LONG,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
			},
			{
				Id:                2,
				Creator:           suite.creator,
				Price:             sdk.MustNewDecFromStr("20"),
				PositionDirection: types.PositionDirection_SHORT,
				PriceDenom:        keepertest.TestPriceDenom,
				AssetDenom:        keepertest.TestAssetDenom,
			},
		},
	}
}

func (suite *KeeperTestSuite) TestMsgPlaceOrder() {
	suite.PrepareTest()
	tests := []struct {
		name          string
		expectedError error
		msg           *dextypes.MsgPlaceOrders
		dynamicDep    bool
	}{
		{
			name:          "default place order",
			msg:           suite.msgPlaceOrders,
			expectedError: nil,
			dynamicDep:    true,
		},
		{
			name:          "dont check synchronous",
			msg:           suite.msgPlaceOrders,
			expectedError: nil,
			dynamicDep:    false,
		},
	}
	for _, tc := range tests {
		suite.Run(fmt.Sprintf("Test Case: %s", tc.name), func() {
			goCtx := context.WithValue(suite.Ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(suite.App.GetMemKey(dextypes.MemStoreKey)))
			suite.Ctx = suite.Ctx.WithContext(goCtx)

			handlerCtx, cms := aclutils.CacheTxContext(suite.Ctx)
			_, err := suite.msgServer.PlaceOrders(
				sdk.WrapSDKContext(handlerCtx),
				tc.msg,
			)

			depdenencies, _ := dexacl.DexPlaceOrdersDependencyGenerator(
				suite.App.AccessControlKeeper,
				handlerCtx,
				tc.msg,
			)

			if !tc.dynamicDep {
				depdenencies = sdkacltypes.SynchronousAccessOps()
			}

			if tc.expectedError != nil {
				suite.Require().EqualError(err, tc.expectedError.Error())
			} else {
				suite.Require().NoError(err)
			}

			missing := handlerCtx.MsgValidator().ValidateAccessOperations(depdenencies, cms.GetEvents())
			pp.Default.SetColoringEnabled(false)

			suite.Require().Empty(missing)
		})
	}
}

func (suite *KeeperTestSuite) TestMsgCancelOrder() {
	suite.PrepareTest()
	tests := []struct {
		name          string
		expectedError error
		msg           *dextypes.MsgCancelOrders
		dynamicDep    bool
	}{
		{
			name:          "default cancel order",
			msg:           suite.msgCancelOrders,
			expectedError: nil,
			dynamicDep:    true,
		},
		{
			name:          "dont check synchronous",
			msg:           suite.msgCancelOrders,
			expectedError: nil,
			dynamicDep:    false,
		},
	}
	for _, tc := range tests {
		suite.Run(fmt.Sprintf("Test Case: %s", tc.name), func() {
			goCtx := context.WithValue(suite.Ctx.Context(), dexutils.DexMemStateContextKey, dexcache.NewMemState(suite.App.GetMemKey(dextypes.MemStoreKey)))
			suite.Ctx = suite.Ctx.WithContext(goCtx)

			_, err := suite.msgServer.PlaceOrders(
				sdk.WrapSDKContext(suite.Ctx),
				suite.msgPlaceOrders,
			)

			handlerCtx, cms := aclutils.CacheTxContext(suite.Ctx)
			_, err = suite.msgServer.CancelOrders(
				sdk.WrapSDKContext(handlerCtx),
				tc.msg,
			)

			depdenencies, _ := dexacl.DexCancelOrdersDependencyGenerator(
				suite.App.AccessControlKeeper,
				handlerCtx,
				tc.msg,
			)

			if !tc.dynamicDep {
				depdenencies = sdkacltypes.SynchronousAccessOps()
			}

			if tc.expectedError != nil {
				suite.Require().EqualError(err, tc.expectedError.Error())
			} else {
				suite.Require().NoError(err)
			}

			missing := handlerCtx.MsgValidator().ValidateAccessOperations(depdenencies, cms.GetEvents())
			suite.Require().Empty(missing)
		})
	}
}

func TestGeneratorInvalidMessageTypes(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	testWrapper := app.NewTestWrapper(t, tm, valPub, false)

	oracleVote := oracletypes.MsgAggregateExchangeRateVote{
		ExchangeRates: "1usei",
		Feeder:        "test",
		Validator:     "validator",
	}

	_, err := dexacl.DexPlaceOrdersDependencyGenerator(
		testWrapper.App.AccessControlKeeper,
		testWrapper.Ctx,
		&oracleVote,
	)
	require.Error(t, err)

	_, err = dexacl.DexCancelOrdersDependencyGenerator(
		testWrapper.App.AccessControlKeeper,
		testWrapper.Ctx,
		&oracleVote,
	)
	require.Error(t, err)
}

func (suite *KeeperTestSuite) TestMsgPlaceOrderGenerator() {
	suite.PrepareTest()

	accessOps, err := dexacl.DexPlaceOrdersDependencyGenerator(
		suite.App.AccessControlKeeper,
		suite.Ctx,
		suite.msgPlaceOrders,
	)
	require.NoError(suite.T(), err)
	err = acltypes.ValidateAccessOps(accessOps)
	require.NoError(suite.T(), err)
}

func (suite *KeeperTestSuite) TestMsgCancelOrderGenerator() {
	suite.PrepareTest()
	accessOps, err := dexacl.DexCancelOrdersDependencyGenerator(
		suite.App.AccessControlKeeper,
		suite.Ctx,
		suite.msgCancelOrders,
	)
	require.NoError(suite.T(), err)
	err = acltypes.ValidateAccessOps(accessOps)
	require.NoError(suite.T(), err)
}
