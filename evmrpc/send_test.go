package evmrpc_test

import (
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

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
	tx, payload := signTxData(t, txData)
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

func TestSendRawTransactionHighGasWanted(t *testing.T) {
	to := common.HexToAddress("010203")
	txData := ethtypes.DynamicFeeTx{
		Nonce:     1,
		GasFeeCap: big.NewInt(10),
		Gas:       1200000,
		To:        &to,
		Value:     big.NewInt(1000),
		Data:      []byte("abc"),
		ChainID:   EVMKeeper.ChainID(Ctx),
	}
	_, payload := signTxData(t, txData)
	resObj := sendRequestGood(t, "sendRawTransaction", payload)
	fmt.Println(resObj)
	require.NotNil(t, resObj["error"])
	// expect an error
	errMap := resObj["error"].(map[string]interface{})
	require.Equal(t, "Estimated gas used (1200000) differs too much from gas limit (1000000). Please try again with a more accurate gas limit.", errMap["message"].(string))
}

func signTxData(t *testing.T, txData ethtypes.DynamicFeeTx) (*ethtypes.Transaction, string) {
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
	return tx, payload
}
