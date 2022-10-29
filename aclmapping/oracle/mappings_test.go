package acloraclemapping_test

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	oracleacl "github.com/sei-protocol/sei-chain/aclmapping/oracle"
	aclutils "github.com/sei-protocol/sei-chain/aclmapping/utils"
	utils "github.com/sei-protocol/sei-chain/aclmapping/utils"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type KeeperTestSuite struct {
	apptesting.KeeperTestHelper

	queryClient oracletypes.QueryClient
	msgServer   oracletypes.MsgServer
	// defaultDenom is on the suite, as it depends on the creator test address.
	defaultDenom string

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
	suite.initalBalance = sdk.Coins{sdk.NewInt64Coin(suite.defaultDenom, 100000000000)}
	suite.FundAcc(suite.TestAccs[0], suite.initalBalance)

	suite.SetupTokenFactory()
	suite.queryClient = oracletypes.NewQueryClient(suite.QueryHelper)
	suite.msgServer = oraclekeeper.NewMsgServerImpl(suite.App.OracleKeeper)

	msgValidator := sdkacltypes.NewMsgValidator(aclutils.StoreKeyToResourceTypePrefixMap)
	suite.Ctx = suite.Ctx.WithMsgValidator(msgValidator)
}

func (suite *KeeperTestSuite) TestMsgBurnDependencies() {
	suite.PrepareTest()
	tests := []struct {
		name          string
		expectedError error
		msg           *oracletypes.MsgAggregateExchangeRateVote
		dynamicDep 	  bool
	}{
		{
			name:          "default vote",
			msg:           &oracletypes.MsgAggregateExchangeRateVote{
				ExchangeRates: "1usei",
				Feeder:        "test",
				Validator:     "validator",
			},
			expectedError: nil,
			dynamicDep: true,
		},
		{
			name:          "dont check synchronous",
			msg:           &oracletypes.MsgAggregateExchangeRateVote{
				ExchangeRates: "1usei",
				Feeder:        "test",
				Validator:     "validator",
			},
			expectedError: nil,
			dynamicDep: false,
		},
	}
	for _, tc := range tests {
		suite.Run(fmt.Sprintf("Test Case: %s", tc.name), func() {
			handlerCtx, cms := utils.CacheTxContext(suite.Ctx)
			_, err := suite.msgServer.AggregateExchangeRateVote(
				sdk.WrapSDKContext(handlerCtx),
				tc.msg,
			)
			suite.App.BankKeeper.WriteDeferredOperations(suite.Ctx)

			depdenencies , _ := oracleacl.MsgVoteDependencyGenerator(
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
func TestMsgVoteDependencyGenerator(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub)

	oracleVote := oracletypes.MsgAggregateExchangeRateVote{
		ExchangeRates: "1usei",
		Feeder:        "test",
		Validator:     "validator",
	}

	accessOps, err := oracleacl.MsgVoteDependencyGenerator(testWrapper.App.AccessControlKeeper, testWrapper.Ctx, &oracleVote)
	require.NoError(t, err)
	err = acltypes.ValidateAccessOps(accessOps)
	require.NoError(t, err)
}
