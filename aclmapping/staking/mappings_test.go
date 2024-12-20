package aclstakingmapping_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/cosmos/cosmos-sdk/x/staking/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	stakingacl "github.com/sei-protocol/sei-chain/aclmapping/staking"
	aclutils "github.com/sei-protocol/sei-chain/aclmapping/utils"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"

	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type KeeperTestSuite struct {
	apptesting.KeeperTestHelper

	queryClient stakingtypes.QueryClient
	msgServer   stakingtypes.MsgServer
	// defaultDenom is on the suite, as it depends on the creator test address.
	defaultDenom        string
	defaultExchangeRate string
	initalBalance       sdk.Coins

	validator     sdk.ValAddress
	newValidator  sdk.ValAddress
	delegateMsg   *types.MsgDelegate
	undelegateMsg *types.MsgUndelegate
	redelegateMsg *types.MsgBeginRedelegate
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
	suite.initalBalance = sdk.Coins{sdk.NewInt64Coin("usei", 100000000000)}
	suite.FundAcc(suite.TestAccs[0], suite.initalBalance)

	suite.queryClient = stakingtypes.NewQueryClient(suite.QueryHelper)
	suite.msgServer = stakingkeeper.NewMsgServerImpl(suite.App.StakingKeeper)

	msgValidator := sdkacltypes.NewMsgValidator(aclutils.StoreKeyToResourceTypePrefixMap)
	suite.Ctx = suite.Ctx.WithMsgValidator(msgValidator)

	suite.Ctx = suite.Ctx.WithBlockHeight(10)
	suite.Ctx = suite.Ctx.WithBlockTime(time.Unix(333, 0))
	suite.validator = suite.SetupValidator(stakingtypes.Bonded)
	suite.newValidator = suite.SetupValidator(stakingtypes.Unbonded)

	notBondedPool := suite.App.StakingKeeper.GetNotBondedPool(suite.Ctx)
	suite.App.AccountKeeper.SetModuleAccount(suite.Ctx, notBondedPool)

	validator, _ := suite.App.StakingKeeper.GetValidator(suite.Ctx, suite.validator)
	suite.App.StakingKeeper.SetValidatorByConsAddr(suite.Ctx, validator)

	newValidator, _ := suite.App.StakingKeeper.GetValidator(suite.Ctx, suite.newValidator)
	suite.App.StakingKeeper.SetValidatorByConsAddr(suite.Ctx, newValidator)

	valTokens := suite.App.StakingKeeper.TokensFromConsensusPower(suite.Ctx, 10)
	validator, issuedShares := validator.AddTokensFromDel(valTokens)
	validator = keeper.TestingUpdateValidator(suite.App.StakingKeeper, suite.Ctx, validator, true)

	newValTokens := suite.App.StakingKeeper.TokensFromConsensusPower(suite.Ctx, 10)
	newValidator, newIssuedShares := newValidator.AddTokensFromDel(newValTokens)
	newValidator = keeper.TestingUpdateValidator(suite.App.StakingKeeper, suite.Ctx, validator, true)

	val0AccAddr := sdk.AccAddress(suite.validator)
	val1AccAddr := sdk.AccAddress(suite.newValidator)

	val0selfDelegation := types.NewDelegation(val0AccAddr, suite.validator, issuedShares)
	val1SelfDelegation := types.NewDelegation(val1AccAddr, suite.validator, newIssuedShares)

	suite.App.StakingKeeper.SetDelegation(suite.Ctx, val0selfDelegation)
	suite.App.StakingKeeper.SetDelegation(suite.Ctx, val1SelfDelegation)

	bondedPool := suite.App.StakingKeeper.GetBondedPool(suite.Ctx)
	suite.App.AccountKeeper.SetModuleAccount(suite.Ctx, bondedPool)

	suite.delegateMsg = &stakingtypes.MsgDelegate{
		Amount:           sdk.NewInt64Coin("usei", 10),
		ValidatorAddress: suite.validator.String(),
		DelegatorAddress: suite.TestAccs[0].String(),
	}
	suite.undelegateMsg = &stakingtypes.MsgUndelegate{
		Amount:           sdk.NewInt64Coin("usei", 10),
		ValidatorAddress: suite.validator.String(),
		DelegatorAddress: suite.TestAccs[0].String(),
	}
	suite.redelegateMsg = &stakingtypes.MsgBeginRedelegate{
		Amount:              sdk.NewInt64Coin("usei", 10),
		ValidatorSrcAddress: suite.validator.String(),
		ValidatorDstAddress: suite.newValidator.String(),
		DelegatorAddress:    suite.TestAccs[0].String(),
	}

	_, err := suite.msgServer.Delegate(
		sdk.WrapSDKContext(suite.Ctx),
		suite.delegateMsg,
	)
	if err != nil {
		panic(err)
	}
}

func (suite *KeeperTestSuite) TestMsgUndelegateDependencies() {
	suite.PrepareTest()
	tests := []struct {
		name          string
		expectedError error
		msg           *stakingtypes.MsgUndelegate
		dynamicDep    bool
	}{
		{
			name:          "default vote",
			msg:           suite.undelegateMsg,
			expectedError: nil,
			dynamicDep:    true,
		},
		{
			name:          "dont check synchronous",
			msg:           suite.undelegateMsg,
			expectedError: nil,
			dynamicDep:    false,
		},
	}
	for _, tc := range tests {
		suite.Run(fmt.Sprintf("Test Case: %s", tc.name), func() {

			handlerCtx, cms := aclutils.CacheTxContext(suite.Ctx)
			_, err := suite.msgServer.Undelegate(
				sdk.WrapSDKContext(handlerCtx),
				tc.msg,
			)
			depdenencies, _ := stakingacl.MsgUndelegateDependencyGenerator(
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

func (suite *KeeperTestSuite) TestMsgRedelegateDependencies() {
	suite.PrepareTest()
	tests := []struct {
		name          string
		expectedError error
		msg           *stakingtypes.MsgBeginRedelegate
		dynamicDep    bool
	}{
		{
			name:          "default vote",
			msg:           suite.redelegateMsg,
			expectedError: nil,
			dynamicDep:    true,
		},
		{
			name:          "dont check synchronous",
			msg:           suite.redelegateMsg,
			expectedError: nil,
			dynamicDep:    false,
		},
	}
	for _, tc := range tests {
		suite.Run(fmt.Sprintf("Test Case: %s", tc.name), func() {

			handlerCtx, cms := aclutils.CacheTxContext(suite.Ctx)
			_, err := suite.msgServer.BeginRedelegate(
				sdk.WrapSDKContext(handlerCtx),
				tc.msg,
			)
			depdenencies, _ := stakingacl.MsgBeginRedelegateDependencyGenerator(
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

func (suite *KeeperTestSuite) TestMsgDelegateDependencies() {
	suite.PrepareTest()
	tests := []struct {
		name          string
		expectedError error
		msg           *stakingtypes.MsgDelegate
		dynamicDep    bool
	}{
		{
			name:          "default vote",
			msg:           suite.delegateMsg,
			expectedError: nil,
			dynamicDep:    true,
		},
		{
			name:          "dont check synchronous",
			msg:           suite.delegateMsg,
			expectedError: nil,
			dynamicDep:    false,
		},
	}
	for _, tc := range tests {
		suite.Run(fmt.Sprintf("Test Case: %s", tc.name), func() {
			handlerCtx, cms := aclutils.CacheTxContext(suite.Ctx)
			_, err := suite.msgServer.Delegate(
				sdk.WrapSDKContext(handlerCtx),
				tc.msg,
			)
			depdenencies, _ := stakingacl.MsgDelegateDependencyGenerator(
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
	testWrapper := app.NewTestWrapper(t, tm, valPub, false, false)

	stakingDelegate := stakingtypes.MsgDelegate{
		DelegatorAddress: "delegator",
		ValidatorAddress: "validator",
		Amount:           sdk.Coin{Denom: "usei", Amount: sdk.NewInt(5)},
	}
	oracleVote := oracletypes.MsgAggregateExchangeRateVote{
		ExchangeRates: "1usei",
		Feeder:        "test",
		Validator:     "validator",
	}

	_, err := stakingacl.MsgUndelegateDependencyGenerator(testWrapper.App.AccessControlKeeper, testWrapper.Ctx, &oracleVote)
	require.Error(t, err)
	_, err = stakingacl.MsgUndelegateDependencyGenerator(testWrapper.App.AccessControlKeeper, testWrapper.Ctx, &stakingDelegate)
	require.Error(t, err)
	_, err = stakingacl.MsgUndelegateDependencyGenerator(testWrapper.App.AccessControlKeeper, testWrapper.Ctx, &stakingDelegate)
	require.Error(t, err)

}

func (suite *KeeperTestSuite) TestMsgDelegateGenerator() {
	suite.PrepareTest()
	stakingDelegate := suite.delegateMsg

	accessOps, err := stakingacl.MsgDelegateDependencyGenerator(
		suite.App.AccessControlKeeper,
		suite.Ctx,
		stakingDelegate,
	)
	require.NoError(suite.T(), err)

	err = acltypes.ValidateAccessOps(accessOps)
	require.NoError(suite.T(), err)

	_, err = stakingacl.MsgDelegateDependencyGenerator(
		suite.App.AccessControlKeeper,
		suite.Ctx,
		suite.redelegateMsg,
	)
	require.Error(suite.T(), err)
}

func (suite *KeeperTestSuite) TestMsgUndelegateGenerator() {
	suite.PrepareTest()
	accessOps, err := stakingacl.MsgUndelegateDependencyGenerator(
		suite.App.AccessControlKeeper,
		suite.Ctx,
		suite.undelegateMsg,
	)
	require.NoError(suite.T(), err)
	err = acltypes.ValidateAccessOps(accessOps)
	require.NoError(suite.T(), err)

	_, err = stakingacl.MsgUndelegateDependencyGenerator(
		suite.App.AccessControlKeeper,
		suite.Ctx,
		suite.redelegateMsg,
	)
	require.Error(suite.T(), err)
}

func (suite *KeeperTestSuite) TestMsgBeginRedelegateGenerator() {
	suite.PrepareTest()
	accessOps, err := stakingacl.MsgBeginRedelegateDependencyGenerator(
		suite.App.AccessControlKeeper,
		suite.Ctx,
		suite.redelegateMsg,
	)
	require.NoError(suite.T(), err)
	err = acltypes.ValidateAccessOps(accessOps)
	require.NoError(suite.T(), err)

	_, err = stakingacl.MsgBeginRedelegateDependencyGenerator(
		suite.App.AccessControlKeeper,
		suite.Ctx,
		suite.undelegateMsg,
	)
	require.Error(suite.T(), err)
}
