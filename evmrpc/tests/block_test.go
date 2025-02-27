package tests

import (
	"fmt"
	"math/big"
	"testing"

	wasmtypes "github.com/CosmWasm/wasmd/x/wasm/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/sei-protocol/sei-chain/precompiles"
	"github.com/sei-protocol/sei-chain/precompiles/pointer"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/stretchr/testify/require"
)

func TestGetBlockByHash(t *testing.T) {
	port := 7777
	txBz := signAndEncodeTx(send(0), mnemonic1)
	RunWithServer(
		SetupTestServer(port, [][][]byte{{txBz}}, mnemonicInitializer(mnemonic1)),
		func() {
			res := sendRequestWithNamespace("eth", port, "getBlockByHash", common.HexToHash("0x1").Hex(), true)
			blockHash := res["result"].(map[string]interface{})["hash"]
			require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000001", blockHash.(string))
		},
	)
}

func TestGetSeiBlockByHash(t *testing.T) {
	port := 7779
	pInfo := precompiles.GetPrecompileInfo(pointer.PrecompileName)
	input, err := pInfo.ABI.Pack("addCW20Pointer", "sei18cszlvm6pze0x9sz32qnjq4vtd45xehqs8dq7cwy8yhq35wfnn3quh5sau")
	require.Nil(t, err)
	pointer := common.HexToAddress(pointer.PointerAddress)
	txData := &ethtypes.DynamicFeeTx{
		Nonce:     0,
		GasFeeCap: big.NewInt(1000000000),
		Gas:       4000000,
		To:        &pointer,
		Value:     big.NewInt(0),
		Data:      input,
		ChainID:   chainId,
	}
	tx1 := signAndEncodeTx(txData, mnemonic1)
	recipient, _ := testkeeper.MockAddressPair()
	msg := &wasmtypes.MsgExecuteContract{
		Sender:   getSeiAddrWithMnemonic(mnemonic1).String(),
		Contract: "sei18cszlvm6pze0x9sz32qnjq4vtd45xehqs8dq7cwy8yhq35wfnn3quh5sau",
		Msg:      []byte(fmt.Sprintf("{\"transfer\":{\"recipient\":\"%s\",\"amount\":\"100\"}}", recipient.String())),
	}
	tx := signCosmosTxWithMnemonic(msg, mnemonic1, 7, 0)
	txBz, _ := testkeeper.EVMTestApp.GetTxConfig().TxEncoder()(tx)
	RunWithServer(
		SetupTestServer(port, [][][]byte{{tx1}, {txBz}}, mnemonicInitializer(mnemonic1), cw20Initializer(mnemonic1)),
		func() {
			res := sendRequestWithNamespace("sei", port, "getBlockByHash", common.HexToHash("0x2").Hex(), true)
			txs := res["result"].(map[string]interface{})["transactions"]
			require.Len(t, txs.([]interface{}), 1)
		},
	)
}

func TestGetBlockByNumber(t *testing.T) {
	port := 7778
	txBz1 := signAndEncodeTx(send(0), mnemonic1)
	txBz2 := signAndEncodeTx(send(1), mnemonic1)
	txBz3 := signAndEncodeTx(send(2), mnemonic1)
	RunWithServer(
		SetupTestServer(port, [][][]byte{{txBz1}, {txBz2}, {txBz3}}, mnemonicInitializer(mnemonic1)),
		func() {
			res := sendRequestWithNamespace("eth", port, "getBlockByNumber", "earliest", true)
			blockHash := res["result"].(map[string]interface{})["hash"]
			require.Equal(t, "0xF9D3845DF25B43B1C6926F3CEDA6845C17F5624E12212FD8847D0BA01DA1AB9E", blockHash.(string))
			res = sendRequestWithNamespace("eth", port, "getBlockByNumber", "safe", true)
			blockHash = res["result"].(map[string]interface{})["hash"]
			require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000003", blockHash.(string))
			res = sendRequestWithNamespace("eth", port, "getBlockByNumber", "latest", true)
			blockHash = res["result"].(map[string]interface{})["hash"]
			require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000003", blockHash.(string))
			res = sendRequestWithNamespace("eth", port, "getBlockByNumber", "finalized", true)
			blockHash = res["result"].(map[string]interface{})["hash"]
			require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000003", blockHash.(string))
			res = sendRequestWithNamespace("eth", port, "getBlockByNumber", "pending", true)
			blockHash = res["result"].(map[string]interface{})["hash"]
			require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000003", blockHash.(string))
			res = sendRequestWithNamespace("eth", port, "getBlockByNumber", "0x2", true)
			blockHash = res["result"].(map[string]interface{})["hash"]
			require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000002", blockHash.(string))
		},
	)
}
