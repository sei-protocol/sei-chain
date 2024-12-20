package acloraclemapping_test

import (
	"fmt"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	oracleacl "github.com/sei-protocol/sei-chain/aclmapping/oracle"
	aclutils "github.com/sei-protocol/sei-chain/aclmapping/utils"
	utils "github.com/sei-protocol/sei-chain/aclmapping/utils"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	oraclekeeper "github.com/sei-protocol/sei-chain/x/oracle/keeper"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type KeeperTestSuite struct {
	apptesting.KeeperTestHelper

	queryClient oracletypes.QueryClient
	msgServer   oracletypes.MsgServer
	// defaultDenom is on the suite, as it depends on the creator test address.
	defaultDenom        string
	defaultExchangeRate string
	initalBalance       sdk.Coins

	validator sdk.ValAddress
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

	suite.initalBalance = sdk.Coins{sdk.NewInt64Coin(suite.defaultDenom, 100000000000)}
	suite.FundAcc(suite.TestAccs[0], suite.initalBalance)

	suite.queryClient = oracletypes.NewQueryClient(suite.QueryHelper)
	suite.msgServer = oraclekeeper.NewMsgServerImpl(suite.App.OracleKeeper)

	// testInput := oraclekeeper.CreateTestInput(suite.T())
	// suite.Ctx = testInput.Ctx

	msgValidator := sdkacltypes.NewMsgValidator(aclutils.StoreKeyToResourceTypePrefixMap)
	suite.Ctx = suite.Ctx.WithMsgValidator(msgValidator)
	suite.Ctx = suite.Ctx.WithBlockHeight(1)
	suite.validator = suite.SetupValidator(stakingtypes.Bonded)

	suite.App.OracleKeeper.SetFeederDelegation(suite.Ctx, suite.validator, suite.TestAccs[0])
	suite.App.OracleKeeper.SetVoteTarget(suite.Ctx, suite.defaultDenom)
}

func (suite *KeeperTestSuite) TestMsgBurnDependencies() {
	suite.PrepareTest()
	tests := []struct {
		name          string
		expectedError error
		msg           *oracletypes.MsgAggregateExchangeRateVote
		dynamicDep    bool
	}{
		{
			name: "default vote",
			msg: &oracletypes.MsgAggregateExchangeRateVote{
				ExchangeRates: suite.defaultExchangeRate,
				Feeder:        suite.TestAccs[0].String(),
				Validator:     suite.validator.String(),
			},
			expectedError: nil,
			dynamicDep:    true,
		},
		{
			name: "dont check synchronous",
			msg: &oracletypes.MsgAggregateExchangeRateVote{
				ExchangeRates: suite.defaultExchangeRate,
				Feeder:        suite.TestAccs[0].String(),
				Validator:     suite.validator.String(),
			},
			expectedError: nil,
			dynamicDep:    false,
		},
	}
	for _, tc := range tests {
		suite.Run(fmt.Sprintf("Test Case: %s", tc.name), func() {
			handlerCtx, cms := utils.CacheTxContext(suite.Ctx)
			_, err := suite.msgServer.AggregateExchangeRateVote(
				sdk.WrapSDKContext(handlerCtx),
				tc.msg,
			)
			depdenencies, _ := oracleacl.MsgVoteDependencyGenerator(
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

	testWrapper := app.NewTestWrapper(t, tm, valPub, false, false)

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

func TestMsgVoteDependencyGeneratorInvalidMsgType(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub, false, false)
	_, err := oracleacl.MsgVoteDependencyGenerator(testWrapper.App.AccessControlKeeper, testWrapper.Ctx, &banktypes.MsgSend{})
	require.Error(t, err)
}

func TestOracleDependencyGenerator(t *testing.T) {
	oracleDependencyGenerator := oracleacl.GetOracleDependencyGenerator()
	// verify that there's one entry, for oracle aggregate vote
	require.Equal(t, 1, len(oracleDependencyGenerator))
	// check that oracle vote dep generator is in the map
	_, ok := oracleDependencyGenerator[acltypes.GenerateMessageKey(&oracletypes.MsgAggregateExchangeRateVote{})]
	require.True(t, ok)
}
