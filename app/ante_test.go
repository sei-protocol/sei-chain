package app_test

import (
	"crypto/sha256"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client/tx"
	cryptotypes "github.com/sei-protocol/sei-chain/sei-cosmos/crypto/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil/testdata"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/types/tx/signing"
	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	app "github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/app/apptesting"
	"github.com/sei-protocol/sei-chain/sei-cosmos/utils/tracing"
	"github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/ante"
	xauthsigning "github.com/sei-protocol/sei-chain/sei-cosmos/x/auth/signing"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.opentelemetry.io/otel"
)

// AnteTestSuite is a test suite to be used with ante handler tests.
type AnteTestSuite struct {
	apptesting.KeeperTestHelper

	anteHandler sdk.AnteHandler
	clientCtx   client.Context
	txBuilder   client.TxBuilder
	testAcc     sdk.AccAddress
	testAccPriv cryptotypes.PrivKey
}

func TestKeeperTestSuite(t *testing.T) {
	suite.Run(t, new(AnteTestSuite))
}

// SetupTest setups a new test, with new app, context, and anteHandler.
func (suite *AnteTestSuite) SetupTest(isCheckTx bool) {
	suite.Setup()

	// keys and addresses
	suite.testAccPriv, _, suite.testAcc = testdata.KeyTestPubAddr()
	initalBalance := sdk.Coins{sdk.NewInt64Coin("usei", 100000000000)}
	suite.FundAcc(suite.testAcc, initalBalance)

	suite.Ctx = suite.Ctx.WithBlockHeight(1)

	// Set up TxConfig.
	encodingConfig := app.MakeEncodingConfig()
	// We're using TestMsg encoding in some tests, so register it here.
	encodingConfig.Amino.RegisterConcrete(&testdata.TestMsg{}, "testdata.TestMsg", nil)
	testdata.RegisterInterfaces(encodingConfig.InterfaceRegistry)

	suite.clientCtx = client.Context{}.
		WithTxConfig(encodingConfig.TxConfig)

	wasmConfig := wasmtypes.DefaultWasmConfig()
	defaultTracer, _ := tracing.DefaultTracerProvider()
	otel.SetTracerProvider(defaultTracer)
	tr := defaultTracer.Tracer("component-main")

	tracingInfo := tracing.NewTracingInfo(tr, true)
	antehandler, _, err := app.NewAnteHandler(
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
			IBCKeeper:       suite.App.IBCKeeper,
			WasmConfig:      &wasmConfig,
			WasmKeeper:      &suite.App.WasmKeeper,
			OracleKeeper:    &suite.App.OracleKeeper,
			TracingInfo:     tracingInfo,
			EVMKeeper:       &suite.App.EvmKeeper,
			LatestCtxGetter: func() sdk.Context { return suite.Ctx },
		},
	)

	suite.Require().NoError(err)
	suite.anteHandler = antehandler
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
	req, err := evmtypes.NewMsgEVMTransaction(txwrapper)
	require.Nil(t, err)
	builder := testkeeper.EVMTestApp.GetTxConfig().NewTxBuilder()
	builder.SetMsgs(req)
	txToSend := builder.GetTx()
	encodedTx, err := testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(txToSend)
	require.Nil(t, err)

	addr, _ := testkeeper.PrivateKeyToAddresses(privKey)
	testkeeper.EVMTestApp.BankKeeper.AddCoins(ctx, addr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(100000000000))), true)
	res := testkeeper.EVMTestApp.DeliverTx(ctx, abci.RequestDeliverTxV2{Tx: encodedTx}, txToSend, sha256.Sum256(encodedTx))
	require.NotEqual(t, 0, res.Code)
	testkeeper.EVMTestApp.EvmKeeper.SetTxResults([]*abci.ExecTxResult{{
		Code: res.Code,
		Log:  "nonce too high",
	}})
	testkeeper.EVMTestApp.EvmKeeper.SetMsgs([]*evmtypes.MsgEVMTransaction{req})
	deferredInfo := testkeeper.EVMTestApp.EvmKeeper.GetAllEVMTxDeferredInfo(ctx)
	require.Equal(t, 1, len(deferredInfo))
	require.Contains(t, deferredInfo[0].Error, "nonce too high")
}
