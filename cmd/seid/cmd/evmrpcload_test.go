package cmd_test

import (
	"math/big"
	"net/http"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/sei-protocol/sei-chain/cmd/seid/cmd"
	"github.com/stretchr/testify/require"
)

func TestDecodeCallArgsStr(t *testing.T) {
	str := "0x0F7F6B308B5111EB4a86D44Dc90394b53A3aCe13,0xa822AfcdDff7c3236D2dC2643646C7B36BDefb3c,01,2,21000,10,20,30"
	expected := map[string]interface{}{
		"from":                 common.HexToAddress("0x0F7F6B308B5111EB4a86D44Dc90394b53A3aCe13"),
		"to":                   cmd.HexToAddressPtr("0xa822AfcdDff7c3236D2dC2643646C7B36BDefb3c"),
		"input":                hexutil.Bytes([]byte{1}),
		"value":                (*hexutil.Big)(big.NewInt(2)),
		"gas":                  hexutil.Uint64(21000),
		"gasPrice":             (*hexutil.Big)(big.NewInt(10)),
		"maxFeePerGas":         (*hexutil.Big)(big.NewInt(20)),
		"maxPriorityFeePerGas": (*hexutil.Big)(big.NewInt(30)),
	}
	require.Equal(t, expected, cmd.DecodeCallArgsStr(str))
	// empty case
	str = "0x0F7F6B308B5111EB4a86D44Dc90394b53A3aCe13,,,,,,,"
	expected = map[string]interface{}{
		"from": common.HexToAddress("0x0F7F6B308B5111EB4a86D44Dc90394b53A3aCe13"),
		"to":   (*common.Address)(nil),
	}
	require.Equal(t, expected, cmd.DecodeCallArgsStr(str))
}

func TestDecodeFilterArgsStr(t *testing.T) {
	// blockhash case
	str := "0x0F7F6B308B5111EB4a86D44Dc90394b53A3aCe13-0xa822AfcdDff7c3236D2dC2643646C7B36BDefb3c,0x1|0x2-0x3,0x4"
	expected := map[string]interface{}{
		"address": []common.Address{
			common.HexToAddress("0x0F7F6B308B5111EB4a86D44Dc90394b53A3aCe13"),
			common.HexToAddress("0xa822AfcdDff7c3236D2dC2643646C7B36BDefb3c"),
		},
		"topics": [][]common.Hash{
			{common.HexToHash("0x1"), common.HexToHash("0x2")},
			{common.HexToHash("0x3")},
		},
		"blockHash": common.HexToHash("0x4"),
	}
	require.Equal(t, expected, cmd.DecodeFilterArgsStr(str))
	// from-to case
	str = "0x0F7F6B308B5111EB4a86D44Dc90394b53A3aCe13,0x1,,earliest,latest"
	expected = map[string]interface{}{
		"address": []common.Address{
			common.HexToAddress("0x0F7F6B308B5111EB4a86D44Dc90394b53A3aCe13"),
		},
		"topics":    [][]common.Hash{{common.HexToHash("0x1")}},
		"fromBlock": "earliest",
		"toBlock":   "latest",
	}
	require.Equal(t, expected, cmd.DecodeFilterArgsStr(str))
}

func TestGetTypedParams(t *testing.T) {
	addr1, hash1 := common.HexToAddress("0x1"), common.HexToHash("0x1")
	for _, testCase := range []struct {
		method   string
		params   []string
		expected []interface{}
	}{
		{"eth_getBalance", []string{"0x1", "latest"}, []interface{}{addr1, "latest"}},
		{"eth_getBlockByHash", []string{"0x1"}, []interface{}{hash1, true}},
		{"eth_getBlockReceipts", []string{"latest"}, []interface{}{"latest"}},
		{"eth_getBlockByNumber", []string{"1"}, []interface{}{big.NewInt(1), true}},
		{"eth_getTransactionReceipt", []string{"0x1"}, []interface{}{hash1}},
		{"eth_estimateGas", []string{"0x0F7F6B308B5111EB4a86D44Dc90394b53A3aCe13,,,,,,,"}, []interface{}{map[string]interface{}{
			"from": common.HexToAddress("0x0F7F6B308B5111EB4a86D44Dc90394b53A3aCe13"),
			"to":   (*common.Address)(nil),
		}}},
		{"eth_call", []string{"0x0F7F6B308B5111EB4a86D44Dc90394b53A3aCe13,,,,,,,", "latest"}, []interface{}{map[string]interface{}{
			"from": common.HexToAddress("0x0F7F6B308B5111EB4a86D44Dc90394b53A3aCe13"),
			"to":   (*common.Address)(nil),
		}, "latest"}},
		{"eth_getLogs", []string{"0x0F7F6B308B5111EB4a86D44Dc90394b53A3aCe13,0x1,,earliest,latest"}, []interface{}{map[string]interface{}{
			"address": []common.Address{
				common.HexToAddress("0x0F7F6B308B5111EB4a86D44Dc90394b53A3aCe13"),
			},
			"topics":    [][]common.Hash{{common.HexToHash("0x1")}},
			"fromBlock": "earliest",
			"toBlock":   "latest",
		}}},
	} {
		require.Equal(t, testCase.expected, cmd.EvmRPCLoad{
			Method: testCase.method,
			Params: testCase.params,
		}.GetTypedParams())
	}
}

func TestConfig(t *testing.T) {
	config := cmd.LoadConfig("config/rpc_load.json")
	config.SetMaxIdleConnsPerHost()
	require.Equal(t, 5, http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost)
}
