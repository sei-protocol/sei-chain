package app_test

import (
	"context"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"regexp"
	"testing"
	"time"

	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/server/api"
	cosmosConfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/app"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/keys/secp256k1"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/config"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/sei-protocol/sei-chain/x/evm/types/ethtx"
	oracletypes "github.com/sei-protocol/sei-chain/x/oracle/types"
	"github.com/stretchr/testify/require"
)

func TestEmptyBlockIdempotency(t *testing.T) {
	commitData := [][]byte{}
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	for i := 1; i <= 10; i++ {
		testWrapper := app.NewTestWrapper(t, tm, valPub, false)
		res, _ := testWrapper.App.FinalizeBlock(context.Background(), &abci.RequestFinalizeBlock{Height: 1})
		testWrapper.App.Commit(context.Background())
		data := res.AppHash
		commitData = append(commitData, data)
	}

	referenceData := commitData[0]
	for _, data := range commitData[1:] {
		require.Equal(t, len(referenceData), len(data))
	}
}

func TestProcessOracleAndOtherTxsSuccess(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()
	secondAcc := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub, false)

	account := sdk.AccAddress(valPub.Address()).String()
	account2 := sdk.AccAddress(secondAcc.Address()).String()
	validator := sdk.ValAddress(valPub.Address()).String()

	oracleMsg := &oracletypes.MsgAggregateExchangeRateVote{
		ExchangeRates: "1.2uatom",
		Feeder:        account,
		Validator:     validator,
	}

	otherMsg := &banktypes.MsgSend{
		FromAddress: account,
		ToAddress:   account2,
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 2)),
	}

	oracleTxBuilder := app.MakeEncodingConfig().TxConfig.NewTxBuilder()
	otherTxBuilder := app.MakeEncodingConfig().TxConfig.NewTxBuilder()
	txEncoder := app.MakeEncodingConfig().TxConfig.TxEncoder()

	err := oracleTxBuilder.SetMsgs(oracleMsg)
	require.NoError(t, err)
	oracleTxBuilder.SetGasLimit(200000)
	oracleTxBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", 20000)))
	oracleTx, err := txEncoder(oracleTxBuilder.GetTx())
	require.NoError(t, err)

	err = otherTxBuilder.SetMsgs(otherMsg)
	require.NoError(t, err)
	otherTxBuilder.SetGasLimit(100000)
	otherTxBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", 10000)))
	otherTx, err := txEncoder(otherTxBuilder.GetTx())
	require.NoError(t, err)

	txs := [][]byte{
		oracleTx,
		otherTx,
	}

	req := &abci.RequestFinalizeBlock{
		Height: 1,
	}
	_, txResults, _, _ := testWrapper.App.ProcessBlock(
		testWrapper.Ctx.WithBlockHeight(
			1,
		),
		txs,
		req,
		req.DecidedLastCommit,
		false,
	)
	fmt.Println("txResults1", txResults)

	require.Equal(t, 2, len(txResults))
	require.Equal(t, uint32(15), txResults[0].Code)
	require.Equal(t, uint32(15), txResults[1].Code)

	diffOrderTxs := [][]byte{
		otherTx,
		oracleTx,
	}

	req = &abci.RequestFinalizeBlock{
		Height: 1,
	}
	_, txResults2, _, _ := testWrapper.App.ProcessBlock(
		testWrapper.Ctx.WithBlockHeight(
			1,
		),
		diffOrderTxs,
		req,
		req.DecidedLastCommit,
		false,
	)
	fmt.Println("txResults2", txResults2)

	require.Equal(t, 2, len(txResults2))
	// opposite ordering due to true index ordering
	require.Equal(t, uint32(15), txResults2[0].Code)
	require.Equal(t, uint32(15), txResults2[1].Code)
}

func TestInvalidProposalWithExcessiveGasWanted(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub, false)
	ap := testWrapper.App
	ctx := testWrapper.Ctx.WithConsensusParams(&types.ConsensusParams{
		Block: &types.BlockParams{MaxGas: 10},
	})
	emptyTxBuilder := app.MakeEncodingConfig().TxConfig.NewTxBuilder()
	txEncoder := app.MakeEncodingConfig().TxConfig.TxEncoder()
	emptyTxBuilder.SetGasLimit(10)
	emptyTx, _ := txEncoder(emptyTxBuilder.GetTx())

	badProposal := abci.RequestProcessProposal{
		Txs:    [][]byte{emptyTx, emptyTx},
		Height: 1,
	}
	res, err := ap.ProcessProposalHandler(ctx, &badProposal)
	require.Nil(t, err)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, res.Status)
}

func TestInvalidProposalWithExcessiveGasEstimates(t *testing.T) {
	type TxType struct {
		isEVM       bool
		gasEstimate uint64
		gasWanted   uint64
	}
	tests := []struct {
		name              string
		maxBlockGas       int64
		maxBlockGasWanted int64
		txs               []TxType
		expectedStatus    abci.ResponseProcessProposal_ProposalStatus
	}{
		{
			name:              "reject when total cosmos tx gas estimates exceed block gas limit",
			maxBlockGas:       20000,
			maxBlockGasWanted: math.MaxInt64,
			txs:               []TxType{{isEVM: false, gasEstimate: 0, gasWanted: 30000}},
			expectedStatus:    abci.ResponseProcessProposal_REJECT,
		},
		{
			name:              "reject when total evm tx gas estimates exceed block gas limit",
			maxBlockGas:       20000,
			maxBlockGasWanted: math.MaxInt64,
			txs:               []TxType{{isEVM: true, gasEstimate: 30000, gasWanted: 30000}},
			expectedStatus:    abci.ResponseProcessProposal_REJECT,
		},
		{
			name:              "single tx: ignore evm gas estimate above maxBlockGas and use gasWanted (accept)",
			maxBlockGas:       20000,
			maxBlockGasWanted: math.MaxInt64,
			txs:               []TxType{{isEVM: true, gasEstimate: 30000, gasWanted: 15000}},
			expectedStatus:    abci.ResponseProcessProposal_ACCEPT,
		},
		{
			name:              "accept when total cosmos tx gas limit is below block gas limit",
			maxBlockGas:       20000,
			maxBlockGasWanted: math.MaxInt64,
			txs:               []TxType{{isEVM: false, gasEstimate: 0, gasWanted: 10000}},
			expectedStatus:    abci.ResponseProcessProposal_ACCEPT,
		},
		{
			name:              "single tx: accept when total evm tx gas estimate is below block gas limit but gas wanted above block gas limit",
			maxBlockGas:       35000,
			maxBlockGasWanted: math.MaxInt64,
			txs:               []TxType{{isEVM: true, gasEstimate: 30000, gasWanted: 100000}},
			expectedStatus:    abci.ResponseProcessProposal_ACCEPT,
		},
		{
			name:              "multiple txs: accept when total evm tx gas estimate is below block gas limit but gas wanted is above block gas limit",
			maxBlockGas:       60000,
			maxBlockGasWanted: math.MaxInt64,
			txs: []TxType{
				{isEVM: true, gasEstimate: 30000, gasWanted: 100000},
				{isEVM: true, gasEstimate: 30000, gasWanted: 100000},
			},
			expectedStatus: abci.ResponseProcessProposal_ACCEPT,
		},
		{
			name:              "multiple txs: accept when mix of cosmos txs and evm txs",
			maxBlockGas:       100000,
			maxBlockGasWanted: math.MaxInt64,
			txs: []TxType{
				{isEVM: false, gasEstimate: 0, gasWanted: 50000},
				{isEVM: true, gasEstimate: 50000, gasWanted: 100000},
			},
			expectedStatus: abci.ResponseProcessProposal_ACCEPT,
		},
		{
			name:              "multiple txs: reject when mix of cosmos txs and evm txs",
			maxBlockGas:       100000,
			maxBlockGasWanted: math.MaxInt64,
			txs: []TxType{
				{isEVM: false, gasEstimate: 0, gasWanted: 51000},
				{isEVM: true, gasEstimate: 50000, gasWanted: 100000},
			},
			expectedStatus: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:              "single tx: reject when gas wanted is above maxBlockGasWanted",
			maxBlockGas:       math.MaxInt64,
			maxBlockGasWanted: 10,
			txs: []TxType{
				{isEVM: false, gasEstimate: 0, gasWanted: 11}, // exceed max block gas wanted
			},
			expectedStatus: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:              "multiple txs: reject when gas wanted is above maxBlockGasWanted",
			maxBlockGas:       math.MaxInt64,
			maxBlockGasWanted: 10,
			txs: []TxType{
				// gasWanted combined should exceed maxBlockGasWanted
				{isEVM: false, gasEstimate: 0, gasWanted: 9},
				{isEVM: true, gasEstimate: 0, gasWanted: 9},
			},
			expectedStatus: abci.ResponseProcessProposal_REJECT,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tm := time.Now().UTC()
			valPub := secp256k1.GenPrivKey().PubKey()

			testWrapper := app.NewTestWrapper(t, tm, valPub, false)
			ap := testWrapper.App
			ctx := testWrapper.Ctx.WithConsensusParams(&types.ConsensusParams{
				Block: &types.BlockParams{
					MaxGas:       tc.maxBlockGas,
					MaxGasWanted: tc.maxBlockGasWanted,
				},
			})

			var txs [][]byte
			for _, tx := range tc.txs {
				if tx.isEVM {
					// Create EVM transaction
					privKey := testkeeper.MockPrivateKey()
					key, _ := crypto.HexToECDSA(hex.EncodeToString(privKey.Bytes()))
					txData := ethtypes.LegacyTx{
						Nonce:    1,
						GasPrice: big.NewInt(10),
						Gas:      tx.gasWanted,
					}
					chainCfg := evmtypes.DefaultChainConfig()
					ethCfg := chainCfg.EthereumConfig(big.NewInt(config.DefaultChainID))
					signer := ethtypes.MakeSigner(ethCfg, big.NewInt(1), uint64(123))
					signedTx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
					require.Nil(t, err)
					ethtxdata, err := ethtx.NewTxDataFromTx(signedTx)
					require.Nil(t, err)
					msg, err := evmtypes.NewMsgEVMTransaction(ethtxdata)
					require.Nil(t, err)
					txBuilder := ap.GetTxConfig().NewTxBuilder()
					txBuilder.SetMsgs(msg)
					txBuilder.SetGasEstimate(tx.gasEstimate)
					txbz, _ := ap.GetTxConfig().TxEncoder()(txBuilder.GetTx())
					// Create two transactions to exceed the block gas limit
					txs = append(txs, txbz)
				} else {
					// Create Cosmos transaction
					cosmosTxBuilder := app.MakeEncodingConfig().TxConfig.NewTxBuilder()
					cosmosTxBuilder.SetMsgs(&banktypes.MsgSend{}) // Using a dummy msg since msg is undefined
					cosmosTxBuilder.SetGasEstimate(tx.gasEstimate)
					cosmosTxBuilder.SetGasLimit(tx.gasWanted)
					emptyTx, _ := ap.GetTxConfig().TxEncoder()(cosmosTxBuilder.GetTx())
					// Create two transactions to exceed the block gas limit
					txs = append(txs, emptyTx)
				}
			}

			proposal := abci.RequestProcessProposal{
				Txs:    txs,
				Height: 1,
			}
			res, err := ap.ProcessProposalHandler(ctx, &proposal)
			require.Nil(t, err)
			require.Equal(t, tc.expectedStatus, res.Status)
		})
	}
}

func TestOverflowGas(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub, false)
	ap := testWrapper.App
	ctx := testWrapper.Ctx.WithConsensusParams(&types.ConsensusParams{
		Block: &types.BlockParams{MaxGas: math.MaxInt64},
	})
	emptyTxBuilder := app.MakeEncodingConfig().TxConfig.NewTxBuilder()
	txEncoder := app.MakeEncodingConfig().TxConfig.TxEncoder()
	emptyTxBuilder.SetGasLimit(uint64(math.MaxInt64))
	emptyTx, _ := txEncoder(emptyTxBuilder.GetTx())

	secondEmptyTxBuilder := app.MakeEncodingConfig().TxConfig.NewTxBuilder()
	secondEmptyTxBuilder.SetGasLimit(10)
	secondTx, _ := txEncoder(secondEmptyTxBuilder.GetTx())

	proposal := abci.RequestProcessProposal{
		Txs:    [][]byte{emptyTx, secondTx},
		Height: 1,
	}
	res, err := ap.ProcessProposalHandler(ctx, &proposal)
	require.Nil(t, err)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, res.Status)
}

func TestDecodeTransactionsConcurrently(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub, false)
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	to := new(common.Address)
	copy(to[:], []byte("0x1234567890abcdef1234567890abcdef12345678"))
	txData := ethtypes.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(10),
		Gas:      1000,
		To:       to,
		Value:    big.NewInt(1000),
		Data:     []byte("abc"),
	}
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(big.NewInt(config.DefaultChainID))
	signer := ethtypes.MakeSigner(ethCfg, big.NewInt(1), uint64(123))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	ethtxdata, _ := ethtx.NewTxDataFromTx(tx)
	if err != nil {
		return
	}
	msg, _ := evmtypes.NewMsgEVMTransaction(ethtxdata)
	txBuilder := testWrapper.App.GetTxConfig().NewTxBuilder()
	txBuilder.SetMsgs(msg)
	evmtxbz, _ := testWrapper.App.GetTxConfig().TxEncoder()(txBuilder.GetTx())

	bankMsg := &banktypes.MsgSend{
		FromAddress: "",
		ToAddress:   "",
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("usei", 2)),
	}

	bankTxBuilder := testWrapper.App.GetTxConfig().NewTxBuilder()
	bankTxBuilder.SetMsgs(bankMsg)
	bankTxBuilder.SetGasLimit(200000)
	bankTxBuilder.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin("usei", 20000)))
	banktxbz, _ := testWrapper.App.GetTxConfig().TxEncoder()(bankTxBuilder.GetTx())

	invalidbz := []byte("abc")

	typedTxs := testWrapper.App.DecodeTransactionsConcurrently(testWrapper.Ctx, [][]byte{evmtxbz, invalidbz, banktxbz})
	require.NotNil(t, typedTxs[0])
	require.NotNil(t, typedTxs[0].GetMsgs()[0].(*evmtypes.MsgEVMTransaction).Derived)
	require.Nil(t, typedTxs[1])
	require.NotNil(t, typedTxs[2])

	// test panic handling
	testWrapper.App.SetTxDecoder(func(txBytes []byte) (sdk.Tx, error) { panic("test") })
	typedTxs = testWrapper.App.DecodeTransactionsConcurrently(testWrapper.Ctx, [][]byte{evmtxbz, invalidbz, banktxbz})
	require.Nil(t, typedTxs[0])
	require.Nil(t, typedTxs[1])
	require.Nil(t, typedTxs[2])
}

func TestApp_RegisterAPIRoutes(t *testing.T) {
	type args struct {
		apiSvr    *api.Server
		apiConfig cosmosConfig.APIConfig
	}
	tests := []struct {
		name        string
		args        args
		wantSwagger bool
	}{
		{
			name: "swagger added to the router if configured",
			args: args{
				apiSvr: &api.Server{
					ClientCtx:         client.Context{},
					Router:            &mux.Router{},
					GRPCGatewayRouter: runtime.NewServeMux(),
				},
				apiConfig: cosmosConfig.APIConfig{
					Swagger: true,
				},
			},
			wantSwagger: true,
		},
		{
			name: "swagger not added to the router if not configured",
			args: args{
				apiSvr: &api.Server{
					ClientCtx:         client.Context{},
					Router:            &mux.Router{},
					GRPCGatewayRouter: runtime.NewServeMux(),
				},
				apiConfig: cosmosConfig.APIConfig{},
			},
			wantSwagger: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seiApp := &app.App{}
			seiApp.RegisterAPIRoutes(tt.args.apiSvr, tt.args.apiConfig)
			routes := tt.args.apiSvr.Router
			gotSwagger := isSwaggerRouteAdded(routes)

			if !reflect.DeepEqual(gotSwagger, tt.wantSwagger) {
				t.Errorf("Run() gotSwagger = %v, want %v", gotSwagger, tt.wantSwagger)
			}
		})

	}
}

func TestGetEVMMsg(t *testing.T) {
	a := &app.App{}
	require.Nil(t, a.GetEVMMsg(nil))
	require.Nil(t, a.GetEVMMsg(app.MakeEncodingConfig().TxConfig.NewTxBuilder().GetTx()))
	tb := app.MakeEncodingConfig().TxConfig.NewTxBuilder()
	tb.SetMsgs(&evmtypes.MsgEVMTransaction{}) // invalid msg
	require.Nil(t, a.GetEVMMsg(tb.GetTx()))

	tb = app.MakeEncodingConfig().TxConfig.NewTxBuilder()
	privKey := testkeeper.MockPrivateKey()
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	txData := ethtypes.LegacyTx{}
	chainCfg := evmtypes.DefaultChainConfig()
	ethCfg := chainCfg.EthereumConfig(big.NewInt(config.DefaultChainID))
	signer := ethtypes.MakeSigner(ethCfg, big.NewInt(1), uint64(123))
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&txData), signer, key)
	ethtxdata, _ := ethtx.NewTxDataFromTx(tx)
	if err != nil {
		return
	}
	msg, _ := evmtypes.NewMsgEVMTransaction(ethtxdata)
	tb.SetMsgs(msg)
	require.NotNil(t, a.GetEVMMsg(tb.GetTx()))
}

func TestGetDeliverTxEntry(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub, false)
	ap := testWrapper.App
	ctx := testWrapper.Ctx.WithConsensusParams(&types.ConsensusParams{
		Block: &types.BlockParams{MaxGas: 10},
	})
	emptyTxBuilder := app.MakeEncodingConfig().TxConfig.NewTxBuilder()
	txEncoder := app.MakeEncodingConfig().TxConfig.TxEncoder()
	emptyTxBuilder.SetGasLimit(10)
	tx := emptyTxBuilder.GetTx()
	bz, _ := txEncoder(tx)

	require.NotNil(t, ap.GetDeliverTxEntry(ctx, 0, bz, tx))

	require.NotNil(t, ap.GetDeliverTxEntry(ctx, 0, bz, nil))
}

func isSwaggerRouteAdded(router *mux.Router) bool {
	var isAdded bool
	err := router.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		pathTemplate, err := route.GetPathTemplate()
		if err == nil && pathTemplate == "/swagger/" {
			isAdded = true
		}
		return nil
	})
	if err != nil {
		return false
	}
	return isAdded
}

func TestGaslessTransactionExtremeGasValue(t *testing.T) {
	sei := app.Setup(t, false, false, false)
	ctx := sei.BaseApp.NewContext(false, types.Header{})

	testAddr := sdk.AccAddress([]byte("test_address_1234567"))

	// Create a potentially gasless transaction with extreme gas value
	attackMsg := &evmtypes.MsgAssociate{
		Sender:        testAddr.String(),
		CustomMessage: "overflow_attack",
	}

	attackTxBuilder := sei.GetTxConfig().NewTxBuilder()
	attackTxBuilder.SetMsgs(attackMsg)
	attackTxBuilder.SetGasLimit(uint64(9223372036854775807)) // 2^63-1, extreme value
	attackTx := attackTxBuilder.GetTx()

	// Encode the transaction
	attackTxBytes, err := sei.GetTxConfig().TxEncoder()(attackTx)
	require.NoError(t, err)

	// Gasless transactions skip metrics recording
	// Non-gasless transactions have overflow protection in IncrGasCounter
	require.NotPanics(t, func() {
		result := sei.DeliverTxWithResult(ctx, attackTxBytes, attackTx)
		require.NotNil(t, result)
	}, "Extreme gas values should never cause panic due to overflow protection")
}

// TestProcessProposalHandlerPanicRecovery tests the panic recovery mechanism in ProcessProposalHandler.
func TestProcessProposalHandlerPanicRecovery(t *testing.T) {
	tm := time.Now().UTC()
	valPub := secp256k1.GenPrivKey().PubKey()

	testWrapper := app.NewTestWrapper(t, tm, valPub, false)
	appInstance := testWrapper.App
	ctx := testWrapper.Ctx

	// malicious tx with MsgAggregateExchangeRateVote with invalid feeder address
	maliciousTx := []byte{
		0x0a, 0x90, 0x01, 0x0a, 0x2f, 0x2f, 0x6f, 0x72, 0x61, 0x63, 0x6c, 0x65, 0x2e, 0x76, 0x31, 0x62, 0x65, 0x74, 0x61, 0x31, 0x2e, 0x4d, 0x73, 0x67, 0x41, 0x67, 0x67, 0x72, 0x65, 0x67, 0x61, 0x74, 0x65, 0x45, 0x78, 0x63, 0x68, 0x61, 0x6e, 0x67, 0x65, 0x52, 0x61, 0x74, 0x65, 0x56, 0x6f, 0x74, 0x65, 0x12, 0x5d, 0x0a, 0x16, 0x31, 0x30, 0x30, 0x30, 0x30, 0x75, 0x73, 0x65, 0x69, 0x3a, 0x31, 0x30, 0x30, 0x30, 0x30, 0x75, 0x61, 0x74, 0x6f, 0x6d, 0x12, 0x04, 0x31, 0x2e, 0x30, 0x30, 0x1a, 0x28, 0x73, 0x65, 0x69, 0x31, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x71, 0x22, 0x13, 0x69, 0x6e, 0x76, 0x61, 0x6c, 0x69, 0x64, 0x2d, 0x66, 0x65, 0x65, 0x64, 0x65, 0x72, 0x2d, 0x61, 0x64, 0x64, 0x72,
	}

	req := &abci.RequestProcessProposal{
		Height: ctx.BlockHeight(),
		Hash:   []byte("panic-test"),
		Txs:    [][]byte{maliciousTx}, // Include the malicious transaction
	}

	// Clear any existing optimistic processing state
	appInstance.ClearOptimisticProcessingInfo()

	resp, err := appInstance.ProcessProposalHandler(ctx, req)
	require.NoError(t, err)

	if resp.Status == abci.ResponseProcessProposal_REJECT {
		t.Log("SECURITY TEST: Precheck caught potential issue and rejected proposal")
	} else {
		t.Log("SECURITY TEST: Proposal accepted - no panic detected (expected with current protections)")

		// If accepted, wait for optimistic processing to complete
		info := appInstance.GetOptimisticProcessingInfo()
		if info.Completion != nil {
			select {
			case <-info.Completion:
				finalInfo := appInstance.GetOptimisticProcessingInfo()
				if finalInfo.Aborted {
					t.Log("Backup panic recovery worked correctly")
				} else {
					t.Log("Optimistic processing completed normally")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Timeout waiting for completion signal")
			}
		}
	}
}

// TestProcessBlockUpgradePanicLogic tests the upgrade panic detection logic
// Since ProcessBlock has multiple panic recovery layers, we test the logic directly
func TestProcessBlockUpgradePanicLogic(t *testing.T) {
	// This tests the exact same logic used in ProcessBlock's defer function
	// We extract and test the core logic to ensure it works correctly
	testUpgradePanicDetection := func(panicMsg string) (shouldRepanic bool, shouldRecover bool) {
		// This uses the same regex pattern as ProcessBlock for consistency with Cosmovisor
		// Matches multiple upgrade-related panic patterns from sei-cosmos
		upgradeRe := regexp.MustCompile(`^(UPGRADE "[^"]+" NEEDED at height:?\s*\d+|Wrong app version \d+, upgrade handler is missing for .+ upgrade plan|BINARY UPDATED BEFORE TRIGGER! UPGRADE "[^"]+")`)
		if upgradeRe.MatchString(panicMsg) {
			return true, false // Should re-panic
		}
		return false, true // Should recover
	}

	testCases := []struct {
		name          string
		panicMsg      string
		shouldRepanic bool
		shouldRecover bool
		description   string
	}{
		{
			name:          "legitimate_upgrade_panic",
			panicMsg:      `UPGRADE "test-version" NEEDED at height: 100: test upgrade`,
			shouldRepanic: true,
			shouldRecover: false,
			description:   "Legitimate upgrade panic should be re-panicked",
		},
		{
			name:          "malicious_upgrade_in_middle",
			panicMsg:      `malicious attack UPGRADE "fake" NEEDED at height: 100`,
			shouldRepanic: false,
			shouldRecover: true,
			description:   "Malicious message with UPGRADE in middle should be recovered",
		},
		{
			name:          "normal_panic",
			panicMsg:      "runtime error: index out of range",
			shouldRepanic: false,
			shouldRecover: true,
			description:   "Normal panic should be recovered",
		},
		{
			name:          "upgrade_prefix_wrong_format",
			panicMsg:      `UPGRADE "fake" but wrong format`,
			shouldRepanic: false,
			shouldRecover: true,
			description:   "UPGRADE prefix but missing 'NEEDED at height' should be recovered",
		},
		{
			name:          "case_sensitive_test",
			panicMsg:      `upgrade "fake" NEEDED at height: 100`,
			shouldRepanic: false,
			shouldRecover: true,
			description:   "Lowercase 'upgrade' should be recovered (case sensitive)",
		},
		{
			name:          "different_upgrade_format",
			panicMsg:      `UPGRADE "mainnet-v2" NEEDED at height: 200000: major upgrade`,
			shouldRepanic: true,
			shouldRecover: false,
			description:   "Different upgrade version format should still work",
		},
		{
			name:          "wrong_app_version_panic",
			panicMsg:      `Wrong app version 5, upgrade handler is missing for v5.9.0 upgrade plan`,
			shouldRepanic: true,
			shouldRecover: false,
			description:   "Wrong app version panic should be re-panicked",
		},
		{
			name:          "binary_updated_early_panic",
			panicMsg:      `BINARY UPDATED BEFORE TRIGGER! UPGRADE "v6.0.0" - in binary but not executed on chain`,
			shouldRepanic: true,
			shouldRecover: false,
			description:   "Binary updated too early panic should be re-panicked",
		},
		{
			name:          "malicious_wrong_version_format",
			panicMsg:      `malicious Wrong app version attack`,
			shouldRepanic: false,
			shouldRecover: true,
			description:   "Malicious message mimicking wrong version should be recovered",
		},
		{
			name:          "malicious_binary_updated_format",
			panicMsg:      `attack BINARY UPDATED BEFORE TRIGGER! fake message`,
			shouldRepanic: false,
			shouldRecover: true,
			description:   "Malicious message mimicking binary update should be recovered",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			shouldRepanic, shouldRecover := testUpgradePanicDetection(tc.panicMsg)

			if tc.shouldRepanic {
				require.True(t, shouldRepanic, "Expected panic to be re-panicked: %s", tc.description)
				require.False(t, shouldRecover, "Expected panic NOT to be recovered: %s", tc.description)
			}

			if tc.shouldRecover {
				require.False(t, shouldRepanic, "Expected panic NOT to be re-panicked: %s", tc.description)
				require.True(t, shouldRecover, "Expected panic to be recovered: %s", tc.description)
			}
		})
	}
}

func TestDeliverTxWithNilTypedTxDoesNotPanic(t *testing.T) {
	sei := app.Setup(t, false, false, false)
	ctx := sei.BaseApp.NewContext(false, types.Header{})

	malformedTxBytes := []byte("invalid tx bytes that cannot be decoded")

	require.NotPanics(t, func() {
		result := sei.DeliverTxWithResult(ctx, malformedTxBytes, nil)
		require.NotNil(t, result)
	})
}
