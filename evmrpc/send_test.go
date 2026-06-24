package evmrpc_test

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	legacyabci "github.com/sei-protocol/sei-chain/app/legacyabci"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	"github.com/sei-protocol/sei-chain/sei-cosmos/crypto/hd"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

type sendProxyClient struct {
	*MockClient
	proxyURL *url.URL
}

func (c *sendProxyClient) EvmProxy(common.Address) utils.Option[*url.URL] {
	if c.proxyURL == nil {
		return utils.None[*url.URL]()
	}
	return utils.Some(c.proxyURL)
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
	require.Equal(t, ": invalid sequence", errMap["message"].(string))
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

	proxyURL, err := url.Parse(server.URL)
	require.NoError(t, err)

	sendAPI := evmrpc.NewSendAPI(
		&sendProxyClient{MockClient: &MockClient{}, proxyURL: proxyURL},
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
