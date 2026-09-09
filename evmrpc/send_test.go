package evmrpc_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	legacyabci "github.com/sei-protocol/sei-chain/app/legacyabci"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	"github.com/sei-protocol/sei-chain/x/evm/types"
)

type sendProxyClient struct {
	*MockClient
	proxyClient *rpc.Client
}

type sendCaptureClient struct {
	*MockClient
	tx          tmtypes.Tx
	syncCount   int
	commitCount int
}

func (c *sendProxyClient) EvmProxy(common.Address) utils.Option[*rpc.Client] {
	if c.proxyClient == nil {
		return utils.None[*rpc.Client]()
	}
	return utils.Some(c.proxyClient)
}

func (c *sendCaptureClient) BroadcastTx(_ context.Context, tx tmtypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	c.syncCount++
	c.tx = tx
	return &coretypes.ResultBroadcastTx{Code: 0}, nil
}

func (c *sendCaptureClient) BroadcastTxCommit(_ context.Context, tx tmtypes.Tx) (*coretypes.ResultBroadcastTxCommit, error) {
	c.commitCount++
	c.tx = tx
	return &coretypes.ResultBroadcastTxCommit{}, nil
}

type sendRejectClient struct {
	*MockClient
	res *coretypes.ResultBroadcastTx
	err error
}

func (c *sendRejectClient) BroadcastTx(context.Context, tmtypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	return c.res, c.err
}

type sendCommitRejectClient struct {
	*MockClient
	res *coretypes.ResultBroadcastTxCommit
	err error
}

func (c *sendCommitRejectClient) BroadcastTxCommit(context.Context, tmtypes.Tx) (*coretypes.ResultBroadcastTxCommit, error) {
	return c.res, c.err
}

func requireRPCError(t *testing.T, err error, code int, message string) {
	t.Helper()
	rpcErr, ok := err.(rpc.Error)
	require.True(t, ok, "err should implement rpc.Error, got %T: %v", err, err)
	require.Equal(t, code, rpcErr.ErrorCode())
	require.Equal(t, message, rpcErr.Error())
}

func newTestSendAPI(tmClient client.LocalClient, sendConfig *evmrpc.SendConfig) *evmrpc.SendAPI {
	return evmrpc.NewSendAPI(
		tmClient,
		func(int64) client.TxConfig { return TxConfig },
		sendConfig,
		EVMKeeper,
		legacyabci.BeginBlockKeepers{},
		func(int64) sdk.Context { return Ctx },
		"",
		nil,
		nil,
		nil,
		evmrpc.ConnectionTypeHTTP,
		utils.None[time.Duration](),
		evmrpc.NewBlockCache(1),
		nil,
		nil,
	)
}

func TestMnemonicToPrivateKey(t *testing.T) {
	mnemonic := "mushroom lamp kingdom obscure sun advice puzzle ancient crystal service beef have zone true chimney act situate laundry guess vacuum razor virus wink enforce"
	hdp := hd.CreateHDPath(sdk.GetConfig().GetCoinType(), 0, 0).String()
	derivedPriv, _ := hd.Secp256k1.Derive()(mnemonic, "", hdp)
	privKey := hd.Secp256k1.Generate()(derivedPriv)
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	require.Equal(t, "fcf3a38c4c63a29f60ec962f4b87ac67a182a3d546fa6e46fef3606e089072d2", testPrivHex)
}

func TestSendRawTransaction(t *testing.T) {
	// build tx
	to := common.HexToAddress("010203")
	txData := ethtypes.DynamicFeeTx{
		Nonce:     1,
		GasFeeCap: big.NewInt(10),
		Gas:       1000,
		To:        &to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   EVMKeeper.ChainID(Ctx),
	}
	mnemonic := "fish mention unlock february marble dove vintage sand hub ordinary fade found inject room embark supply fabric improve spike stem give current similar glimpse"
	derivedPriv, _ := hd.Secp256k1.Derive()(mnemonic, "", "")
	privKey := hd.Secp256k1.Generate()(derivedPriv)
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	ethCfg := types.DefaultChainConfig().EthereumConfig(EVMKeeper.ChainID(Ctx))
	signer := ethtypes.MakeSigner(ethCfg, big.NewInt(Ctx.BlockHeight()), uint64(Ctx.BlockTime().Unix()))
	tx := ethtypes.NewTx(&txData)
	tx, err := ethtypes.SignTx(tx, signer, key)
	require.Nil(t, err)
	bz, err := tx.MarshalBinary()
	require.Nil(t, err)
	payload := "0x" + hex.EncodeToString(bz)

	resObj := sendRequestGood(t, "sendRawTransaction", payload)
	result := resObj["result"].(string)
	require.Equal(t, tx.Hash().Hex(), result)

	// bad payload
	resObj = sendRequestGood(t, "sendRawTransaction", "0x1234")
	errMap := resObj["error"].(map[string]interface{})
	require.Equal(t, "transaction type not supported", errMap["message"].(string))

	// bad server
	resObj = sendRequestBad(t, "sendRawTransaction", payload)
	errMap = resObj["error"].(map[string]interface{})
	require.Equal(t, "internal error", errMap["message"].(string))
	require.Equal(t, float64(-32603), errMap["code"].(float64))
}

func TestSendRawTransactionUsesProxy(t *testing.T) {
	to := common.HexToAddress("010203")
	_, tx := buildTx(ethtypes.DynamicFeeTx{
		Nonce:     1,
		GasFeeCap: big.NewInt(10),
		Gas:       1000,
		To:        &to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   EVMKeeper.ChainID(Ctx),
	})
	ethTxBytes, err := tx.MarshalBinary()
	require.NoError(t, err)

	var gotMethod string
	var gotPayload string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Len(t, req.Params, 1)
		gotMethod = req.Method
		require.NoError(t, json.Unmarshal(req.Params[0], &gotPayload))

		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  tx.Hash().Hex(),
		}))
	}))
	defer server.Close()

	proxyClient, err := rpc.DialContext(t.Context(), server.URL)
	require.NoError(t, err)
	t.Cleanup(proxyClient.Close)

	sendAPI := evmrpc.NewSendAPI(
		&sendProxyClient{MockClient: &MockClient{}, proxyClient: proxyClient},
		func(int64) client.TxConfig { return TxConfig },
		&evmrpc.SendConfig{},
		EVMKeeper,
		legacyabci.BeginBlockKeepers{},
		func(int64) sdk.Context { return Ctx },
		"",
		nil,
		nil,
		nil,
		evmrpc.ConnectionTypeHTTP,
		utils.None[time.Duration](),
		evmrpc.NewBlockCache(1),
		nil,
		nil,
	)

	hash, err := sendAPI.SendRawTransaction(context.Background(), hexutil.Bytes(ethTxBytes))
	require.NoError(t, err)
	require.Equal(t, tx.Hash(), hash)
	require.Equal(t, "eth_sendRawTransaction", gotMethod)
	require.Equal(t, hexutil.Encode(ethTxBytes), gotPayload)
}

func TestSendRawTransactionUsesGasLimitWhenSimulationDisabled(t *testing.T) {
	to := common.HexToAddress("010203")
	gasLimit := uint64(123456)
	_, tx := buildTx(ethtypes.DynamicFeeTx{
		Nonce:     1,
		GasFeeCap: big.NewInt(10),
		Gas:       gasLimit,
		To:        &to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   EVMKeeper.ChainID(Ctx),
	})
	ethTxBytes, err := tx.MarshalBinary()
	require.NoError(t, err)

	tmClient := &sendCaptureClient{MockClient: &MockClient{}}
	sendAPI := newTestSendAPI(tmClient, evmrpc.NewSendConfig(false, false, false))

	hash, err := sendAPI.SendRawTransaction(context.Background(), hexutil.Bytes(ethTxBytes))
	require.NoError(t, err)
	require.Equal(t, tx.Hash(), hash)
	require.NotNil(t, tmClient.tx)
	require.Equal(t, 1, tmClient.syncCount)
	require.Equal(t, 0, tmClient.commitCount)

	decodedTx, err := TxConfig.TxDecoder()(tmClient.tx)
	require.NoError(t, err)
	require.Equal(t, gasLimit, decodedTx.GetGasEstimate())
}

func TestSendRawTransactionSlowOnAutobahnUsesBroadcastTx(t *testing.T) {
	ethTxBytes, tx := mustSignTestTx(t)
	tmClient := &sendCaptureClient{MockClient: &MockClient{}}
	sendAPI := newTestSendAPI(tmClient, evmrpc.NewSendConfig(true, false, true))

	hash, err := sendAPI.SendRawTransaction(context.Background(), hexutil.Bytes(ethTxBytes))
	require.NoError(t, err)
	require.Equal(t, tx.Hash(), hash)
	require.Equal(t, 1, tmClient.syncCount)
	require.Equal(t, 0, tmClient.commitCount)
}

func TestSendRawTransactionSlowOnCometUsesBroadcastTxCommit(t *testing.T) {
	ethTxBytes, tx := mustSignTestTx(t)
	tmClient := &sendCaptureClient{MockClient: &MockClient{}}
	sendAPI := newTestSendAPI(tmClient, evmrpc.NewSendConfig(true, false, false))

	hash, err := sendAPI.SendRawTransaction(context.Background(), hexutil.Bytes(ethTxBytes))
	require.NoError(t, err)
	require.Equal(t, tx.Hash(), hash)
	require.Equal(t, 0, tmClient.syncCount)
	require.Equal(t, 1, tmClient.commitCount)
}

func TestSendRawTransactionTranslatesBroadcastErrors(t *testing.T) {
	ethTxBytes, _ := mustSignTestTx(t)
	tests := []struct {
		name      string
		res       *coretypes.ResultBroadcastTx
		broadcast error
		code      int
		message   string
		sentinel  error
	}{
		{
			name:     "nonce too low",
			res:      &coretypes.ResultBroadcastTx{Codespace: "sdk", Code: 32, Log: "next nonce 5, tx nonce 3: incorrect account sequence"},
			code:     -32000,
			message:  "nonce too low: next nonce 5, tx nonce 3",
			sentinel: core.ErrNonceTooLow,
		},
		{
			name:    "insufficient fee",
			res:     &coretypes.ResultBroadcastTx{Codespace: "sdk", Code: 13, Log: "insufficient fee"},
			code:    -32000,
			message: "max fee per gas less than block base fee",
		},
		{
			name:      "already known",
			broadcast: errors.New("tx already exists in cache"),
			code:      -32000,
			message:   "already known",
		},
		{
			name:      "unmapped broadcast error",
			broadcast: errors.New("some unexpected internal thing"),
			code:      -32603,
			message:   "internal error",
		},
		{
			name:    "missing broadcast response",
			code:    -32603,
			message: "internal error",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sendAPI := newTestSendAPI(
				&sendRejectClient{MockClient: &MockClient{}, res: tc.res, err: tc.broadcast},
				evmrpc.NewSendConfig(false, false, false),
			)
			_, err := sendAPI.SendRawTransaction(context.Background(), hexutil.Bytes(ethTxBytes))
			requireRPCError(t, err, tc.code, tc.message)
			if tc.sentinel != nil {
				require.True(t, errors.Is(err, tc.sentinel))
			}
		})
	}
}

func TestSendRawTransactionSlowCommitTranslatesNonceTooLow(t *testing.T) {
	ethTxBytes, _ := mustSignTestTx(t)
	sendAPI := newTestSendAPI(
		&sendCommitRejectClient{
			MockClient: &MockClient{},
			res: &coretypes.ResultBroadcastTxCommit{
				CheckTx: abci.ResponseCheckTx{
					Codespace: "sdk",
					Code:      32,
					Log:       "next nonce 5, tx nonce 3: incorrect account sequence",
				},
			},
		},
		evmrpc.NewSendConfig(true, false, false),
	)
	_, err := sendAPI.SendRawTransaction(context.Background(), hexutil.Bytes(ethTxBytes))
	requireRPCError(t, err, -32000, "nonce too low: next nonce 5, tx nonce 3")
}

func TestSendRawTransactionProxyPassesThroughRemoteError(t *testing.T) {
	ethTxBytes, _ := mustSignTestTx(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]any{
				"code":    -32000,
				"message": "nonce too low: next nonce 5, tx nonce 3",
			},
		}))
	}))
	defer server.Close()

	proxyClient, err := rpc.DialContext(t.Context(), server.URL)
	require.NoError(t, err)
	t.Cleanup(proxyClient.Close)

	sendAPI := newTestSendAPI(
		&sendProxyClient{MockClient: &MockClient{}, proxyClient: proxyClient},
		&evmrpc.SendConfig{},
	)
	_, err = sendAPI.SendRawTransaction(context.Background(), hexutil.Bytes(ethTxBytes))
	requireRPCError(t, err, -32000, "nonce too low: next nonce 5, tx nonce 3")
}

func TestSendRawTransactionProxyHidesTransportURL(t *testing.T) {
	ethTxBytes, _ := mustSignTestTx(t)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	proxyClient, err := rpc.DialContext(t.Context(), server.URL)
	require.NoError(t, err)
	t.Cleanup(proxyClient.Close)
	server.Close()

	sendAPI := newTestSendAPI(
		&sendProxyClient{MockClient: &MockClient{}, proxyClient: proxyClient},
		&evmrpc.SendConfig{},
	)
	_, err = sendAPI.SendRawTransaction(context.Background(), hexutil.Bytes(ethTxBytes))
	requireRPCError(t, err, -32603, "internal error")
	require.False(t, strings.Contains(err.Error(), server.URL))
}

func TestSendRawTransactionMapsEmptySetCodeAuthorizationList(t *testing.T) {
	chainID := EVMKeeper.ChainID(Ctx)
	key, err := crypto.HexToECDSA(strings.Repeat("46", 32))
	require.NoError(t, err)
	tx, err := ethtypes.SignTx(ethtypes.NewTx(&ethtypes.SetCodeTx{
		ChainID:   uint256.MustFromBig(chainID),
		GasTipCap: uint256.NewInt(1),
		GasFeeCap: uint256.NewInt(2),
		Gas:       21000,
		To:        common.Address{1},
		Value:     uint256.NewInt(0),
		AuthList:  nil,
	}), ethtypes.NewPragueSigner(chainID), key)
	require.NoError(t, err)
	raw, err := tx.MarshalBinary()
	require.NoError(t, err)

	sendAPI := newTestSendAPI(&MockClient{}, evmrpc.NewSendConfig(false, false, false))
	_, err = sendAPI.SendRawTransaction(t.Context(), raw)
	requireRPCError(t, err, -32000, "set code tx must have at least one authorization tuple")
}

func TestSendRawTransactionMapsOversizedSignature(t *testing.T) {
	chainID := EVMKeeper.ChainID(Ctx)
	v := new(big.Int).Add(new(big.Int).Mul(chainID, big.NewInt(2)), big.NewInt(35))
	to := common.Address{1}
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &to,
		Value:    new(big.Int),
		V:        v,
		R:        new(big.Int).Lsh(big.NewInt(1), 256),
		S:        big.NewInt(1),
	})
	raw, err := tx.MarshalBinary()
	require.NoError(t, err)

	sendAPI := newTestSendAPI(&MockClient{}, evmrpc.NewSendConfig(false, false, false))
	_, err = sendAPI.SendRawTransaction(t.Context(), raw)
	requireRPCError(t, err, -32000, "invalid sender: invalid transaction v, r, s values")
}

func mustSignTestTx(t *testing.T) ([]byte, *ethtypes.Transaction) {
	t.Helper()
	to := common.HexToAddress("010203")
	_, tx := buildTx(ethtypes.DynamicFeeTx{
		Nonce:     1,
		GasFeeCap: big.NewInt(10),
		Gas:       1000,
		To:        &to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   EVMKeeper.ChainID(Ctx),
	})
	ethTxBytes, err := tx.MarshalBinary()
	require.NoError(t, err)
	return ethTxBytes, tx
}
