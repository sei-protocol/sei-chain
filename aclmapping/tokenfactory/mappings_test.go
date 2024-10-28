package acltokenfactorymapping_test

import (
	"fmt"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	tkfactory "github.com/sei-protocol/sei-chain/aclmapping/tokenfactory"
	aclutils "github.com/sei-protocol/sei-chain/aclmapping/utils"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	tokenfactorykeeper "github.com/sei-protocol/sei-chain/x/tokenfactory/keeper"
	"github.com/sei-protocol/sei-chain/x/tokenfactory/types"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

type KeeperTestSuite struct {
	apptesting.KeeperTestHelper

	queryClient tokenfactorytypes.QueryClient
	msgServer   tokenfactorytypes.MsgServer
	// defaultDenom is on the suite, as it depends on the creator test address.
	defaultDenom  string
	testDenom     string
	initalBalance sdk.Coins
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
	suite.testDenom = "foocoins"

	suite.initalBalance = sdk.Coins{sdk.NewInt64Coin(suite.defaultDenom, 100000000000)}
	suite.FundAcc(suite.TestAccs[0], suite.initalBalance)

	suite.SetupTokenFactory()
	suite.queryClient = tokenfactorytypes.NewQueryClient(suite.QueryHelper)
	suite.msgServer = tokenfactorykeeper.NewMsgServerImpl(suite.App.TokenFactoryKeeper)

	res, err := suite.msgServer.CreateDenom(
		sdk.WrapSDKContext(suite.Ctx),
		types.NewMsgCreateDenom(suite.TestAccs[0].String(), suite.testDenom),
	)

	if err != nil {
		panic(err)
	}
	suite.testDenom = res.GetNewTokenDenom()

	_, err = suite.msgServer.Mint(
		sdk.WrapSDKContext(suite.Ctx),
		types.NewMsgMint(suite.TestAccs[0].String(), sdk.NewInt64Coin(suite.testDenom, 1000000)),
	)
	if err != nil {
		panic(err)
	}

	msgValidator := sdkacltypes.NewMsgValidator(aclutils.StoreKeyToResourceTypePrefixMap)
	suite.Ctx = suite.Ctx.WithMsgValidator(msgValidator)
}

func cacheTxContext(ctx sdk.Context) (sdk.Context, sdk.CacheMultiStore) {
	ms := ctx.MultiStore()
	msCache := ms.CacheMultiStore()
	return ctx.WithMultiStore(msCache), msCache
}

func (suite *KeeperTestSuite) TestMsgBurnDependencies() {
	suite.PrepareTest()

	burnAmount := sdk.NewInt64Coin(suite.testDenom, 10)
	addr1 := suite.TestAccs[0].String()
	tests := []struct {
		name          string
		expectedError error
		msg           *tokenfactorytypes.MsgBurn
		dynamicDep    bool
	}{
		{
			name:          "default burn",
			msg:           tokenfactorytypes.NewMsgBurn(addr1, burnAmount),
			expectedError: nil,
			dynamicDep:    true,
		},
		{
			name:          "dont check synchronous",
			msg:           tokenfactorytypes.NewMsgBurn(addr1, burnAmount),
			expectedError: nil,
			dynamicDep:    false,
		},
	}
	for _, tc := range tests {
		suite.Run(fmt.Sprintf("Test Case: %s", tc.name), func() {
			handlerCtx, cms := cacheTxContext(suite.Ctx)
			_, err := suite.msgServer.Burn(
				sdk.WrapSDKContext(handlerCtx),
				tc.msg,
			)

			depdenencies, _ := tkfactory.TokenFactoryBurnDependencyGenerator(
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

func (suite *KeeperTestSuite) TestMsgMintDependencies() {
	suite.PrepareTest()

	burnAmount := sdk.NewInt64Coin(suite.testDenom, 10)
	addr1 := suite.TestAccs[0].String()
	tests := []struct {
		name          string
		expectedError error
		msg           *tokenfactorytypes.MsgMint
		dynamicDep    bool
	}{
		{
			name:          "default mint",
			msg:           tokenfactorytypes.NewMsgMint(addr1, burnAmount),
			expectedError: nil,
			dynamicDep:    true,
		},
		{
			name:          "dont check synchronous",
			msg:           tokenfactorytypes.NewMsgMint(addr1, burnAmount),
			expectedError: nil,
			dynamicDep:    false,
		},
	}
	for _, tc := range tests {
		suite.Run(fmt.Sprintf("Test Case: %s", tc.name), func() {
			handlerCtx, cms := cacheTxContext(suite.Ctx)
			_, err := suite.msgServer.Mint(
				sdk.WrapSDKContext(handlerCtx),
				tc.msg,
			)

			depdenencies, _ := tkfactory.TokenFactoryMintDependencyGenerator(
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
	accs := authtypes.GenesisAccounts{}
	balances := []banktypes.Balance{}

	app := simapp.SetupWithGenesisAccounts(accs, balances...)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	oracleVote := oracletypes.MsgAggregateExchangeRateVote{
		ExchangeRates: "1usei",
		Feeder:        "test",
		Validator:     "validator",
	}

	_, err := tkfactory.TokenFactoryBurnDependencyGenerator(app.AccessControlKeeper, ctx, &oracleVote)
	require.Error(t, err)

	_, err = tkfactory.TokenFactoryMintDependencyGenerator(app.AccessControlKeeper, ctx, &oracleVote)
	require.Error(t, err)
}

func TestMsgBeginBurnDepedencyGenerator(t *testing.T) {
	priv1 := secp256k1.GenPrivKey()
	addr1 := sdk.AccAddress(priv1.PubKey().Address())

	accs := authtypes.GenesisAccounts{}
	balances := []banktypes.Balance{}

	app := simapp.SetupWithGenesisAccounts(accs, balances...)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	sendMsg := tokenfactorytypes.MsgBurn{
		Sender: addr1.String(),
		Amount: sdk.NewInt64Coin("usei", 10),
	}

	accessOps, err := tkfactory.TokenFactoryBurnDependencyGenerator(app.AccessControlKeeper, ctx, &sendMsg)
	require.NoError(t, err)
	err = acltypes.ValidateAccessOps(accessOps)
	require.NoError(t, err)
}
