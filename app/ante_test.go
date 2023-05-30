package app_test

import (
	"context"
	"testing"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/simapp"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkacltypes "github.com/cosmos/cosmos-sdk/types/accesscontrol"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	acltypes "github.com/cosmos/cosmos-sdk/x/accesscontrol/types"
	"github.com/cosmos/cosmos-sdk/x/feegrant"

	"github.com/cosmos/cosmos-sdk/utils/tracing"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	xauthsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	aclutils "github.com/sei-protocol/sei-chain/aclmapping/utils"
	app "github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.opentelemetry.io/otel"
)

// AnteTestSuite is a test suite to be used with ante handler tests.
type AnteTestSuite struct {
	apptesting.KeeperTestHelper

	anteHandler      sdk.AnteHandler
	anteDepGenerator sdk.AnteDepGenerator
	clientCtx        client.Context
	txBuilder        client.TxBuilder
	testAcc          sdk.AccAddress
	testAccPriv      cryptotypes.PrivKey
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(AnteTestSuite))
}

// SetupTest setups a new test, with new app, context, and anteHandler.
func (suite *AnteTestSuite) SetupTest(isCheckTx bool) {
	suite.Setup()

	// keys and addresses
	suite.testAccPriv, _, suite.testAcc = testdata.KeyTestPubAddr()
	initalBalance := sdk.Coins{sdk.NewInt64Coin("atom", 100000000000)}
	suite.FundAcc(suite.testAcc, initalBalance)

	suite.Ctx = suite.Ctx.WithBlockHeight(1)

	msgValidator := sdkacltypes.NewMsgValidator(aclutils.StoreKeyToResourceTypePrefixMap)
	suite.Ctx = suite.Ctx.WithMsgValidator(msgValidator)

	// Set up TxConfig.
	encodingConfig := simapp.MakeTestEncodingConfig()
	// We're using TestMsg encoding in some tests, so register it here.
	encodingConfig.Amino.RegisterConcrete(&testdata.TestMsg{}, "testdata.TestMsg", nil)
	testdata.RegisterInterfaces(encodingConfig.InterfaceRegistry)

	suite.clientCtx = client.Context{}.
		WithTxConfig(encodingConfig.TxConfig)

	wasmConfig := wasmtypes.DefaultWasmConfig()
	defaultTracer, _ := tracing.DefaultTracerProvider()
	otel.SetTracerProvider(defaultTracer)
	tr := defaultTracer.Tracer("component-main")

	tracingInfo := &tracing.Info{
		Tracer: &tr,
	}
	tracingInfo.SetContext(context.Background())
	antehandler, anteDepGenerator, err := app.NewAnteHandlerAndDepGenerator(
		app.HandlerOptions{
			HandlerOptions: ante.HandlerOptions{
				AccountKeeper:   suite.App.AccountKeeper,
				BankKeeper:      suite.App.BankKeeper,
				FeegrantKeeper:  suite.App.FeeGrantKeeper,
				ParamsKeeper:    suite.App.ParamsKeeper,
				SignModeHandler: suite.clientCtx.TxConfig.SignModeHandler(),
				SigGasConsumer:  ante.DefaultSigVerificationGasConsumer,
				// BatchVerifier:   app.batchVerifier,
			},
			IBCKeeper:           suite.App.IBCKeeper,
			WasmConfig:          &wasmConfig,
			WasmKeeper:          &suite.App.WasmKeeper,
			OracleKeeper:        &suite.App.OracleKeeper,
			DexKeeper:           &suite.App.DexKeeper,
			AccessControlKeeper: &suite.App.AccessControlKeeper,
			TracingInfo:         tracingInfo,
			CheckTxMemState:     suite.App.CheckTxMemState,
		},
	)

	suite.Require().NoError(err)
	suite.anteHandler = antehandler
	suite.anteDepGenerator = anteDepGenerator
}

func (suite *AnteTestSuite) AnteHandlerValidateAccessOp(acessOps []sdkacltypes.AccessOperation) error {
	for _, accessOp := range acessOps {
		err := acltypes.ValidateAccessOp(accessOp)
		if err != nil {
			return err
		}
	}
	return nil
}

// CreateTestTx is a helper function to create a tx given multiple inputs.
func (suite *AnteTestSuite) CreateTestTx(privs []cryptotypes.PrivKey, accNums []uint64, accSeqs []uint64, chainID string) (xauthsigning.Tx, error) {
	// First round: we gather all the signer infos. We use the "set empty
	// signature" hack to do that.
	var sigsV2 []signing.SignatureV2
	for i, priv := range privs {
		sigV2 := signing.SignatureV2{
			PubKey: priv.PubKey(),
			Data: &signing.SingleSignatureData{
				SignMode:  suite.clientCtx.TxConfig.SignModeHandler().DefaultMode(),
				Signature: nil,
			},
			Sequence: accSeqs[i],
		}

		sigsV2 = append(sigsV2, sigV2)
	}
	err := suite.txBuilder.SetSignatures(sigsV2...)
	if err != nil {
		return nil, err
	}

	// Second round: all signer infos are set, so each signer can sign.
	sigsV2 = []signing.SignatureV2{}
	for i, priv := range privs {
		signerData := xauthsigning.SignerData{
			ChainID:       chainID,
			AccountNumber: accNums[i],
			Sequence:      accSeqs[i],
		}
		sigV2, err := tx.SignWithPrivKey(
			suite.clientCtx.TxConfig.SignModeHandler().DefaultMode(), signerData,
			suite.txBuilder, priv, suite.clientCtx.TxConfig, accSeqs[i])
		if err != nil {
			return nil, err
		}

		sigsV2 = append(sigsV2, sigV2)
	}
	err = suite.txBuilder.SetSignatures(sigsV2...)
	if err != nil {
		return nil, err
	}

	return suite.txBuilder.GetTx(), nil
}

func (suite *AnteTestSuite) TestValidateDepedencies() {
	suite.SetupTest(true) // setup
	suite.txBuilder = suite.clientCtx.TxConfig.NewTxBuilder()

	// msg and signatures
	msg := testdata.NewTestMsg(suite.testAcc)
	feeAmount := testdata.NewTestFeeAmount()
	gasLimit := testdata.NewTestGasLimit()
	suite.Require().NoError(suite.txBuilder.SetMsgs(msg))
	suite.txBuilder.SetFeeAmount(feeAmount)
	suite.txBuilder.SetGasLimit(gasLimit)

	privs, accNums, accSeqs := []cryptotypes.PrivKey{}, []uint64{}, []uint64{}
	invalidTx, err := suite.CreateTestTx(privs, accNums, accSeqs, suite.Ctx.ChainID())
	suite.Require().NoError(err)

	_, err = suite.anteHandler(suite.Ctx, invalidTx, false)

	suite.Require().NotNil(err, "Did not error on invalid tx")

	privs, accNums, accSeqs = []cryptotypes.PrivKey{suite.testAccPriv}, []uint64{8}, []uint64{0}

	handlerCtx, cms := aclutils.CacheTxContext(suite.Ctx)
	validTx, err := suite.CreateTestTx(privs, accNums, accSeqs, suite.Ctx.ChainID())

	suite.Require().NoError(err)
	depdenencies, _ := suite.anteDepGenerator([]sdkacltypes.AccessOperation{}, validTx)
	_, err = suite.anteHandler(handlerCtx, validTx, false)
	suite.Require().Nil(err, "ValidateBasicDecorator returned error on valid tx. err: %v", err)
	err = suite.AnteHandlerValidateAccessOp(depdenencies)

	require.NoError(suite.T(), err)

	missing := handlerCtx.MsgValidator().ValidateAccessOperations(depdenencies, cms.GetEvents())
	suite.Require().Empty(missing)

	// test decorator skips on recheck
	suite.Ctx = suite.Ctx.WithIsReCheckTx(true)

	// decorator should skip processing invalidTx on recheck and thus return nil-error
	handlerCtx, cms = aclutils.CacheTxContext(suite.Ctx)
	depdenencies, _ = suite.anteDepGenerator([]sdkacltypes.AccessOperation{}, invalidTx)
	_, err = suite.anteHandler(handlerCtx, invalidTx, false)
	missing = handlerCtx.MsgValidator().ValidateAccessOperations(depdenencies, cms.GetEvents())

	err = suite.AnteHandlerValidateAccessOp(depdenencies)
	require.NoError(suite.T(), err)

	suite.Require().Empty(missing)

	suite.Require().Nil(err, "ValidateBasicDecorator ran on ReCheck")
}

func (suite *AnteTestSuite) TestDeductFeeDependency() {
	suite.SetupTest(false) // setup
	suite.txBuilder = suite.clientCtx.TxConfig.NewTxBuilder()

	// keys and addresses
	priv1, _, addr1 := testdata.KeyTestPubAddr()
	_, _, addr2 := testdata.KeyTestPubAddr()

	// msg and signatures
	msg := testdata.NewTestMsg(addr1)
	feeAmount := testdata.NewTestFeeAmount()
	gasLimit := testdata.NewTestGasLimit()
	suite.Require().NoError(suite.txBuilder.SetMsgs(msg))
	suite.txBuilder.SetFeeGranter(addr2)
	suite.txBuilder.SetFeeAmount(feeAmount)
	suite.txBuilder.SetGasLimit(gasLimit)

	privs, accNums, accSeqs := []cryptotypes.PrivKey{priv1}, []uint64{0}, []uint64{0}
	tx, err := suite.CreateTestTx(privs, accNums, accSeqs, suite.Ctx.ChainID())
	suite.Require().NoError(err)

	dfd := ante.NewDeductFeeDecorator(suite.App.AccountKeeper, suite.App.BankKeeper, suite.App.FeeGrantKeeper, suite.App.ParamsKeeper, nil)

	antehandler, decorator := sdk.ChainAnteDecorators(dfd)

	// Set account with sufficient funds
	acc := suite.App.AccountKeeper.NewAccountWithAddress(suite.Ctx, addr1)
	suite.App.AccountKeeper.SetAccount(suite.Ctx, acc)
	err = simapp.FundAccount(suite.App.BankKeeper, suite.Ctx, addr1, sdk.NewCoins(sdk.NewCoin("atom", sdk.NewInt(200))))
	suite.Require().NoError(err)

	acc2 := suite.App.AccountKeeper.NewAccountWithAddress(suite.Ctx, addr2)
	suite.App.AccountKeeper.SetAccount(suite.Ctx, acc2)
	err = simapp.FundAccount(suite.App.BankKeeper, suite.Ctx, addr2, sdk.NewCoins(sdk.NewCoin("atom", sdk.NewInt(200))))
	suite.Require().NoError(err)
	// create fee grant
	err = suite.App.FeeGrantKeeper.GrantAllowance(suite.Ctx, addr2, addr1, &feegrant.BasicAllowance{})
	suite.Require().NoError(err)

	msgValidator := sdkacltypes.NewMsgValidator(aclutils.StoreKeyToResourceTypePrefixMap)
	suite.Ctx = suite.Ctx.WithMsgValidator(msgValidator)
	ms := suite.Ctx.MultiStore()
	msCache := ms.CacheMultiStore()
	suite.Ctx = suite.Ctx.WithMultiStore(msCache)

	_, err = antehandler(suite.Ctx, tx, false)
	suite.Require().Nil(err, "Tx errored after account has been set with sufficient funds")

	newDeps, err := decorator([]sdkacltypes.AccessOperation{}, tx)
	suite.Require().NoError(err)

	storeAccessOpEvents := msCache.GetEvents()

	missingAccessOps := suite.Ctx.MsgValidator().ValidateAccessOperations(newDeps, storeAccessOpEvents)
	suite.Require().Equal(0, len(missingAccessOps))
}
