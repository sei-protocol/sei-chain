package baseapp_test

import (
	"context"
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/simapp"
	"github.com/cosmos/cosmos-sdk/testutil"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
)

func TestRegisterMsgService(t *testing.T) {
	db := dbm.NewMemDB()

	// Create an encoding config that doesn't register testdata Msg services.
	encCfg := simapp.MakeTestEncodingConfig()
	app := baseapp.NewBaseApp("test", log.NewTestingLogger(t), db, encCfg.TxConfig.TxDecoder(), nil, &testutil.TestAppOpts{})
	app.SetInterfaceRegistry(encCfg.InterfaceRegistry)
	require.Panics(t, func() {
		testdata.RegisterMsgServer(
			app.MsgServiceRouter(),
			testdata.MsgServerImpl{},
		)
	})

	// Register testdata Msg services, and rerun `RegisterService`.
	testdata.RegisterInterfaces(encCfg.InterfaceRegistry)
	require.NotPanics(t, func() {
		testdata.RegisterMsgServer(
			app.MsgServiceRouter(),
			testdata.MsgServerImpl{},
		)
	})
}

func TestRegisterMsgServiceTwice(t *testing.T) {
	// Setup baseapp.
	db := dbm.NewMemDB()
	encCfg := simapp.MakeTestEncodingConfig()
	app := baseapp.NewBaseApp("test", log.NewTestingLogger(t), db, encCfg.TxConfig.TxDecoder(), nil, &testutil.TestAppOpts{})
	app.SetInterfaceRegistry(encCfg.InterfaceRegistry)
	testdata.RegisterInterfaces(encCfg.InterfaceRegistry)

	// First time registering service shouldn't panic.
	require.NotPanics(t, func() {
		testdata.RegisterMsgServer(
			app.MsgServiceRouter(),
			testdata.MsgServerImpl{},
		)
	})

	// Second time should panic.
	require.Panics(t, func() {
		testdata.RegisterMsgServer(
			app.MsgServiceRouter(),
			testdata.MsgServerImpl{},
		)
	})
}

func TestMsgService(t *testing.T) {
	priv, _, _ := testdata.KeyTestPubAddr()
	encCfg := simapp.MakeTestEncodingConfig()
	testdata.RegisterInterfaces(encCfg.InterfaceRegistry)
	db := dbm.NewMemDB()
	decoder := encCfg.TxConfig.TxDecoder()
	app := baseapp.NewBaseApp("test", log.NewTestingLogger(t), db, decoder, nil, &testutil.TestAppOpts{})
	app.SetInterfaceRegistry(encCfg.InterfaceRegistry)
	testdata.RegisterMsgServer(
		app.MsgServiceRouter(),
		testdata.MsgServerImpl{},
	)
	app.SetFinalizeBlocker(func(ctx sdk.Context, req *abci.RequestFinalizeBlock) (*abci.ResponseFinalizeBlock, error) {
		txResults := []*abci.ExecTxResult{}
		for _, txbz := range req.Txs {
			tx, err := decoder(txbz)
			if err != nil {
				txResults = append(txResults, &abci.ExecTxResult{})
				continue
			}
			deliverTxResp := app.DeliverTx(ctx, abci.RequestDeliverTx{
				Tx: txbz,
			}, tx, sha256.Sum256(txbz))
			txResults = append(txResults, &abci.ExecTxResult{
				Code:      deliverTxResp.Code,
				Data:      deliverTxResp.Data,
				Log:       deliverTxResp.Log,
				Info:      deliverTxResp.Info,
				GasWanted: deliverTxResp.GasWanted,
				GasUsed:   deliverTxResp.GasUsed,
				Events:    deliverTxResp.Events,
				Codespace: deliverTxResp.Codespace,
			})
		}
		return &abci.ResponseFinalizeBlock{
			TxResults: txResults,
		}, nil
	})
	app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Height: 1,
	})

	msg := testdata.MsgCreateDog{Dog: &testdata.Dog{Name: "Spot"}}
	txBuilder := encCfg.TxConfig.NewTxBuilder()
	txBuilder.SetFeeAmount(testdata.NewTestFeeAmount())
	txBuilder.SetGasLimit(testdata.NewTestGasLimit())
	err := txBuilder.SetMsgs(&msg)
	require.NoError(t, err)

	// First round: we gather all the signer infos. We use the "set empty
	// signature" hack to do that.
	sigV2 := signing.SignatureV2{
		PubKey: priv.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode:  encCfg.TxConfig.SignModeHandler().DefaultMode(),
			Signature: nil,
		},
		Sequence: 0,
	}

	err = txBuilder.SetSignatures(sigV2)
	require.NoError(t, err)

	// Second round: all signer infos are set, so each signer can sign.
	signerData := authsigning.SignerData{
		ChainID:       "test",
		AccountNumber: 0,
		Sequence:      0,
	}
	sigV2, err = tx.SignWithPrivKey(
		encCfg.TxConfig.SignModeHandler().DefaultMode(), signerData,
		txBuilder, priv, encCfg.TxConfig, 0)
	require.NoError(t, err)
	err = txBuilder.SetSignatures(sigV2)
	require.NoError(t, err)

	// Send the tx to the app
	txBytes, err := encCfg.TxConfig.TxEncoder()(txBuilder.GetTx())
	require.NoError(t, err)
	res, err := app.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{
		Height: 2,
		Txs:    [][]byte{txBytes},
	})
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, res.TxResults[0].Code, "res=%+v", res)
}
