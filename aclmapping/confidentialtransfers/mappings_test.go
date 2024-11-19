package confidentialtransfers_test

import (
	"crypto/ecdsa"
	"fmt"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/crypto"
	ctacl "github.com/sei-protocol/sei-chain/aclmapping/confidentialtransfers"
	aclutils "github.com/sei-protocol/sei-chain/aclmapping/utils"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	tokenfactorytypes "github.com/sei-protocol/sei-chain/x/tokenfactory/types"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption"
	"github.com/sei-protocol/sei-cryptography/pkg/encryption/elgamal"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/keeper"
	"github.com/sei-protocol/sei-chain/x/confidentialtransfers/types"
	"github.com/stretchr/testify/suite"
	"testing"
)

const (
	DefaultTestDenom = "mappingtest"
)

type KeeperTestSuite struct {
	apptesting.KeeperTestHelper

	queryClient types.QueryClient
	msgServer   types.MsgServer
	tfMsgServer tokenfactorytypes.MsgServer
	// defaultDenom is on the suite, as it depends on the creator test address.
	initalBalance sdk.Coins
	PrivKeys      []*ecdsa.PrivateKey
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
	suite.SetupConfidentialTransfers()
	suite.queryClient = types.NewQueryClient(suite.QueryHelper)
	suite.msgServer = keeper.NewMsgServerImpl(suite.App.ConfidentialTransfersKeeper)

	msgValidator := sdkacltypes.NewMsgValidator(aclutils.StoreKeyToResourceTypePrefixMap)
	suite.Ctx = suite.Ctx.WithMsgValidator(msgValidator)
	suite.PrivKeys = apptesting.CreateRandomAccountKeys(3)
}

func cacheTxContext(ctx sdk.Context) (sdk.Context, sdk.CacheMultiStore) {
	ms := ctx.MultiStore()
	msCache := ms.CacheMultiStore()
	return ctx.WithMultiStore(msCache), msCache
}

func (suite *KeeperTestSuite) SetupAccountState(privateKey *ecdsa.PrivateKey, denom string, pendingBalanceCreditCounter uint16, initialAvailableBalance, initialPendingBalance, bankAmount uint64) (types.Account, error) {
	aesKey, err := encryption.GetAESKey(*privateKey, denom)
	if err != nil {
		return types.Account{}, err
	}

	teg := elgamal.NewTwistedElgamal()
	keypair, err := teg.KeyGen(*privateKey, denom)
	if err != nil {
		return types.Account{}, err
	}

	availableBalance := initialAvailableBalance
	pendingBalance := initialPendingBalance

	// Extract the bottom 16 bits (rightmost 16 bits)
	pendingBalanceLo := uint16(pendingBalance & 0xFFFF)

	// Extract the next 32 bits (from bit 16 to bit 47)
	pendingBalanceHi := uint32((pendingBalance >> 16) & 0xFFFFFFFF)

	availableBalanceCipherText, _, err := teg.Encrypt(keypair.PublicKey, availableBalance)
	if err != nil {
		return types.Account{}, err
	}

	pendingBalanceLoCipherText, _, err := teg.Encrypt(keypair.PublicKey, uint64(pendingBalanceLo))
	if err != nil {
		return types.Account{}, err
	}

	pendingBalanceHiCipherText, _, err := teg.Encrypt(keypair.PublicKey, uint64(pendingBalanceHi))
	if err != nil {
		return types.Account{}, err
	}

	decryptableAvailableBalance, err := encryption.EncryptAESGCM(availableBalance, aesKey)
	if err != nil {
		return types.Account{}, err
	}

	initialAccountState := types.Account{
		PublicKey:                   keypair.PublicKey,
		PendingBalanceLo:            pendingBalanceLoCipherText,
		PendingBalanceHi:            pendingBalanceHiCipherText,
		PendingBalanceCreditCounter: pendingBalanceCreditCounter,
		AvailableBalance:            availableBalanceCipherText,
		DecryptableAvailableBalance: decryptableAvailableBalance,
	}

	addr := privkeyToAddress(privateKey)
	_ = suite.App.ConfidentialTransfersKeeper.SetAccount(suite.Ctx, addr.String(), denom, initialAccountState)

	bankModuleTokens := sdk.NewCoins(sdk.Coin{Amount: sdk.NewInt(int64(bankAmount)), Denom: denom})

	suite.FundAcc(addr, bankModuleTokens)

	return initialAccountState, nil
}

func privkeyToAddress(privateKey *ecdsa.PrivateKey) sdk.AccAddress {
	publicKeyBytes := crypto.FromECDSAPub(&privateKey.PublicKey)
	testAddr := sdk.AccAddress(crypto.Keccak256(publicKeyBytes[1:])[12:])
	return testAddr
}

func (suite *KeeperTestSuite) TestMsgTransferDependencies() {
	suite.PrepareTest()

	senderPk := suite.PrivKeys[0]
	senderAddr := privkeyToAddress(senderPk)
	recipientPk := suite.PrivKeys[1]
	recipientAddr := privkeyToAddress(recipientPk)
	auditorPk := suite.PrivKeys[2]
	auditorAddr := privkeyToAddress(auditorPk)

	// Initialize an account
	initialSenderState, _ := suite.SetupAccountState(senderPk, DefaultTestDenom, 10, 2000, 3000, 1000)
	initialRecipientState, _ := suite.SetupAccountState(recipientPk, DefaultTestDenom, 12, 5000, 21000, 201000)
	initialAuditorState, _ := suite.SetupAccountState(auditorPk, DefaultTestDenom, 12, 5000, 21000, 201000)

	transferAmount := uint64(500)

	// Happy Path
	auditorsInput := []types.AuditorInput{{auditorAddr.String(), &initialAuditorState.PublicKey}}
	transferStruct, err := types.NewTransfer(
		senderPk,
		senderAddr.String(),
		recipientAddr.String(),
		DefaultTestDenom,
		initialSenderState.DecryptableAvailableBalance,
		initialSenderState.AvailableBalance,
		transferAmount,
		&initialRecipientState.PublicKey,
		auditorsInput)
	suite.Require().NoError(err, "Should not have error creating valid transfer struct")

	tests := []struct {
		name          string
		expectedError error
		msg           *types.MsgTransfer
		dynamicDep    bool
	}{
		{
			name:          "transfer in dynamic dep mode",
			msg:           types.NewMsgTransferProto(transferStruct),
			expectedError: nil,
			dynamicDep:    true,
		},
		{
			name:          "transfer in sync dep mode",
			msg:           types.NewMsgTransferProto(transferStruct),
			expectedError: nil,
			dynamicDep:    false,
		},
	}
	for _, tc := range tests {
		suite.Run(fmt.Sprintf("Test Case: %s", tc.name), func() {
			handlerCtx, cms := cacheTxContext(suite.Ctx)
			_, err = suite.msgServer.Transfer(
				sdk.WrapSDKContext(handlerCtx),
				tc.msg,
			)
			suite.Require().NoError(err)

			dependencies, _ := ctacl.MsgTransferDependencyGenerator(
				suite.App.AccessControlKeeper,
				handlerCtx,
				tc.msg,
			)

			if !tc.dynamicDep {
				dependencies = sdkacltypes.SynchronousAccessOps()
			}

			if tc.expectedError != nil {
				suite.Require().EqualError(err, tc.expectedError.Error())
			} else {
				suite.Require().NoError(err)
			}

			missing := handlerCtx.MsgValidator().ValidateAccessOperations(dependencies, cms.GetEvents())
			suite.Require().Empty(missing)
		})
	}
}

func TestGeneratorInvalidTransferMessageTypes(t *testing.T) {
	app, ctx, oracleVote := setUp()

	_, err := ctacl.MsgTransferDependencyGenerator(app.AccessControlKeeper, ctx, &oracleVote)
	require.Error(t, err)
	require.Equal(t, "invalid message received for confidential transfers module", err.Error())
}

func TestGeneratorInvalidDepositMessageTypes(t *testing.T) {
	app, ctx, oracleVote := setUp()

	_, err := ctacl.MsgDepositDependencyGenerator(app.AccessControlKeeper, ctx, &oracleVote)
	require.Error(t, err)
	require.Equal(t, "invalid message received for confidential transfers module", err.Error())
}

func TestGeneratorInvalidWithdrawMessageTypes(t *testing.T) {
	app, ctx, oracleVote := setUp()

	_, err := ctacl.MsgWithdrawDependencyGenerator(app.AccessControlKeeper, ctx, &oracleVote)
	require.Error(t, err)
	require.Equal(t, "invalid message received for confidential transfers module", err.Error())
}

func setUp() (*simapp.SimApp, sdk.Context, oracletypes.MsgAggregateExchangeRateVote) {
	// setup
	accs := authtypes.GenesisAccounts{}
	var balances []banktypes.Balance

	app := simapp.SetupWithGenesisAccounts(accs, balances...)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{})

	oracleVote := oracletypes.MsgAggregateExchangeRateVote{
		ExchangeRates: "1usei",
		Feeder:        "test",
		Validator:     "validator",
	}
	return app, ctx, oracleVote
}

func (suite *KeeperTestSuite) TestMsgDepositDependencies() {
	suite.PrepareTest()

	senderPk := suite.PrivKeys[0]
	senderAddr := privkeyToAddress(senderPk)

	// Initialize an account
	_, _ = suite.SetupAccountState(senderPk, DefaultTestDenom, 10, 2000, 3000, 1000)

	depositAmount := uint64(500)

	depositStruct := &types.MsgDeposit{
		FromAddress: senderAddr.String(),
		Denom:       DefaultTestDenom,
		Amount:      depositAmount,
	}

	tests := []struct {
		name          string
		expectedError error
		msg           *types.MsgDeposit
		dynamicDep    bool
	}{
		{
			name:          "deposit in dynamic dep mode",
			msg:           depositStruct,
			expectedError: nil,
			dynamicDep:    true,
		},
		{
			name:          "deposit in sync dep mode",
			msg:           depositStruct,
			expectedError: nil,
			dynamicDep:    false,
		},
	}
	for _, tc := range tests {
		suite.Run(fmt.Sprintf("Test Case: %s", tc.name), func() {
			handlerCtx, cms := cacheTxContext(suite.Ctx)
			_, err := suite.msgServer.Deposit(
				sdk.WrapSDKContext(handlerCtx),
				tc.msg,
			)
			suite.Require().NoError(err)

			dependencies, _ := ctacl.MsgDepositDependencyGenerator(
				suite.App.AccessControlKeeper,
				handlerCtx,
				tc.msg,
			)

			if !tc.dynamicDep {
				dependencies = sdkacltypes.SynchronousAccessOps()
			}

			if tc.expectedError != nil {
				suite.Require().EqualError(err, tc.expectedError.Error())
			} else {
				suite.Require().NoError(err)
			}

			missing := handlerCtx.MsgValidator().ValidateAccessOperations(dependencies, cms.GetEvents())
			suite.Require().Empty(missing)
		})
	}
}

func (suite *KeeperTestSuite) TestMsgWithdrawDependencies() {
	suite.PrepareTest()

	senderPk := suite.PrivKeys[0]
	senderAddr := privkeyToAddress(senderPk)

	// Initialize an account
	initialState, _ := suite.SetupAccountState(senderPk, DefaultTestDenom, 10, 2000, 3000, 1000)

	withdrawAmount := uint64(500)
	withdraw, _ := types.NewWithdraw(*senderPk,
		initialState.AvailableBalance,
		DefaultTestDenom,
		senderAddr.String(),
		initialState.DecryptableAvailableBalance,
		withdrawAmount)

	withdrawStruct := types.NewMsgWithdrawProto(withdraw)

	tests := []struct {
		name          string
		expectedError error
		msg           *types.MsgWithdraw
		dynamicDep    bool
	}{
		{
			name:          "withdraw in dynamic dep mode",
			msg:           withdrawStruct,
			expectedError: nil,
			dynamicDep:    true,
		},
		{
			name:          "withdraw in sync dep mode",
			msg:           withdrawStruct,
			expectedError: nil,
			dynamicDep:    false,
		},
	}
	for _, tc := range tests {
		suite.Run(fmt.Sprintf("Test Case: %s", tc.name), func() {
			handlerCtx, cms := cacheTxContext(suite.Ctx)

			suite.FundAcc(senderAddr, sdk.NewCoins(sdk.Coin{Denom: DefaultTestDenom, Amount: sdk.NewInt(1000)}))
			err := suite.App.BankKeeper.SendCoinsFromAccountToModule(
				handlerCtx, senderAddr,
				types.ModuleName,
				sdk.NewCoins(sdk.Coin{Denom: DefaultTestDenom,
					Amount: sdk.NewInt(1000)}))
			suite.Require().NoError(err)

			_, err = suite.msgServer.Withdraw(
				sdk.WrapSDKContext(handlerCtx),
				tc.msg,
			)
			suite.Require().NoError(err)

			dependencies, _ := ctacl.MsgWithdrawDependencyGenerator(
				suite.App.AccessControlKeeper,
				handlerCtx,
				tc.msg,
			)

			if !tc.dynamicDep {
				dependencies = sdkacltypes.SynchronousAccessOps()
			}

			if tc.expectedError != nil {
				suite.Require().EqualError(err, tc.expectedError.Error())
			} else {
				suite.Require().NoError(err)
			}

			missing := handlerCtx.MsgValidator().ValidateAccessOperations(dependencies, cms.GetEvents())
			suite.Require().Empty(missing)
		})
	}
}

func TestGeneratorInvalidInitializeAccountMessageTypes(t *testing.T) {
	app, ctx, oracleVote := setUp()

	_, err := ctacl.MsgInitializeAccountDependencyGenerator(app.AccessControlKeeper, ctx, &oracleVote)
	require.Error(t, err)
	require.Equal(t, "invalid message received for confidential transfers module", err.Error())
}

func (suite *KeeperTestSuite) TestMsgInitializeAccountDependencies() {
	suite.PrepareTest()

	senderPk := suite.PrivKeys[0]
	senderAddr := privkeyToAddress(senderPk)

	initAccount, _ := types.NewInitializeAccount(senderAddr.String(), DefaultTestDenom, *senderPk)
	initializeAccountStruct := types.NewMsgInitializeAccountProto(initAccount)

	tests := []struct {
		name          string
		expectedError error
		msg           *types.MsgInitializeAccount
		dynamicDep    bool
	}{
		{
			name:          "initialize account in dynamic dep mode",
			msg:           initializeAccountStruct,
			expectedError: nil,
			dynamicDep:    true,
		},
		{
			name:          "initialize account in sync dep mode",
			msg:           initializeAccountStruct,
			expectedError: nil,
			dynamicDep:    false,
		},
	}
	for _, tc := range tests {
		suite.Run(fmt.Sprintf("Test Case: %s", tc.name), func() {
			handlerCtx, cms := cacheTxContext(suite.Ctx)
			_, err := suite.msgServer.InitializeAccount(
				sdk.WrapSDKContext(handlerCtx),
				tc.msg,
			)
			suite.Require().NoError(err)

			dependencies, _ := ctacl.MsgInitializeAccountDependencyGenerator(
				suite.App.AccessControlKeeper,
				handlerCtx,
				tc.msg,
			)

			if !tc.dynamicDep {
				dependencies = sdkacltypes.SynchronousAccessOps()
			}

			if tc.expectedError != nil {
				suite.Require().EqualError(err, tc.expectedError.Error())
			} else {
				suite.Require().NoError(err)
			}

			missing := handlerCtx.MsgValidator().ValidateAccessOperations(dependencies, cms.GetEvents())
			suite.Require().Empty(missing)
		})
	}
}
