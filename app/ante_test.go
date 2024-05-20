package app_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"math/big"
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

	"github.com/cosmos/cosmos-sdk/utils/tracing"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	xauthsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	aclutils "github.com/sei-protocol/sei-chain/aclmapping/utils"
	app "github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
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
			AccessControlKeeper: &suite.App.AccessControlKeeper,
			TracingInfo:         tracingInfo,
			EVMKeeper:           &suite.App.EvmKeeper,
			LatestCtxGetter:     func() sdk.Context { return suite.Ctx },
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
	depdenencies, _ := suite.anteDepGenerator([]sdkacltypes.AccessOperation{}, validTx, 0)
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
	depdenencies, _ = suite.anteDepGenerator([]sdkacltypes.AccessOperation{}, invalidTx, 0)
	_, err = suite.anteHandler(handlerCtx, invalidTx, false)
	missing = handlerCtx.MsgValidator().ValidateAccessOperations(depdenencies, cms.GetEvents())

	err = suite.AnteHandlerValidateAccessOp(depdenencies)
	require.NoError(suite.T(), err)

	suite.Require().Empty(missing)

	suite.Require().Nil(err, "ValidateBasicDecorator ran on ReCheck")
}

func TestEvmAnteErrorHandler(t *testing.T) {
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	txData := ethtypes.LegacyTx{
		GasPrice: big.NewInt(1000000000000),
		Gas:      200000,
		To:       nil,
		Value:    big.NewInt(0),
		Data:     []byte{},
		Nonce:    1, // will cause ante error
	}
	chainID := testkeeper.EVMTestApp.EvmKeeper.ChainID(ctx)
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(chainID)
	blockNum := big.NewInt(ctx.BlockHeight())
	signer := ethtypes.MakeSigner(ethCfg, blockNum, uint64(ctx.BlockTime().Unix()))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	require.Nil(t, err)
	txwrapper, err := ethtx.NewLegacyTx(tx)
	require.Nil(t, err)
	req, err := types.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)
	builder := testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	builder.SetMsgs(req)
	txToSend := builder.GetTx()
	encodedTx, err := testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(txToSend)
	require.Nil(t, err)

	addr, _ := testkeeper.PrivateKeyToAddresses(privKey)
	testkeeper.EVMTestApp.BankKeeper.AddCoins(ctx, addr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000000000))), true)
	res := testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTx{Tx: encodedTx}, txToSend, sha256.Sum256(encodedTx))
	require.NotEqual(t, 0, res.Code)
	testkeeper.EVMTestApp.EvmKeeper.SetTxResults([]*abci.ExecTxResult{{
		Code: res.Code,
	}})
	deferredInfo := testkeeper.EVMTestApp.EvmKeeper.GetEVMTxDeferredInfo(ctx)
	require.Equal(t, 1, len(deferredInfo))
	require.Contains(t, deferredInfo[0].Error, "nonce too high")
}
