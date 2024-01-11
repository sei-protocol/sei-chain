package evmrpc_test

import (
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestTxPoolContent(t *testing.T) {
	// send a transaction with too high nonce and check if it is in the txpool
	to := common.HexToAddress("010203")
	txDataWithHighNonce := ethtypes.DynamicFeeTx{
		Nonce:     1001, // too high nonce
		GasFeeCap: big.NewInt(11),
		Gas:       1001,
		To:        &to,
		Value:     big.NewInt(1001),
		Data:      []byte("abc"),
		ChainID:   EVMKeeper.ChainID(Ctx),
	}
	resObj := sendDynamicFeeTx(t, &txDataWithHighNonce)
	fmt.Println("resObj = ", resObj)
	txDataWithHighNonce2 := ethtypes.DynamicFeeTx{
		Nonce:     1002, // too high nonce
		GasFeeCap: big.NewInt(12),
		Gas:       1002,
		To:        &to,
		Value:     big.NewInt(1002),
		Data:      []byte("def"),
		ChainID:   EVMKeeper.ChainID(Ctx),
	}
	resObj = sendDynamicFeeTx(t, &txDataWithHighNonce2)
	fmt.Println("resObj2 = ", resObj)

	body := "{\"jsonrpc\": \"2.0\",\"method\": \"txpool_content\",\"params\":[],\"id\":\"test\"}"
	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("http://%s:%d", TestAddr, TestPort), strings.NewReader(body))
	require.Nil(t, err)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	require.Nil(t, err)
	resBody, err := io.ReadAll(res.Body)
	require.Nil(t, err)
	fmt.Println(string(resBody))
}

func sendDynamicFeeTx(t *testing.T, txData *ethtypes.DynamicFeeTx) map[string]interface{} {
	mnemonic := "fish mention unlock february marble dove vintage sand hub ordinary fade found inject room embark supply fabric improve spike stem give current similar glimpse"
	derivedPriv, _ := hd.Secp256k1.Derive()(mnemonic, "", "")
	privKey := hd.Secp256k1.Generate()(derivedPriv)
	testPrivHex := hex.EncodeToString(privKey.Bytes())
	key, _ := crypto.HexToECDSA(testPrivHex)
	evmParams := EVMKeeper.GetParams(Ctx)
	ethCfg := evmParams.GetChainConfig().EthereumConfig(EVMKeeper.ChainID(Ctx))
	signer := ethtypes.MakeSigner(ethCfg, big.NewInt(Ctx.BlockHeight()), uint64(Ctx.BlockTime().Unix()))
	tx := ethtypes.NewTx(txData)
	tx, err := ethtypes.SignTx(tx, signer, key)
	require.Nil(t, err)
	bz, err := tx.MarshalBinary()
	require.Nil(t, err)
	payload := "0x" + hex.EncodeToString(bz)

	resObj := sendRequestGood(t, "sendRawTransaction", payload)
	return resObj
}
