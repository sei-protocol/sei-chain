package evmrpc

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/stretchr/testify/require"
)

// TODO: delete this, this is just for experimenting
func TestFilterCriteria(t *testing.T) {
	filterCriteria := filters.FilterCriteria{
		FromBlock: big.NewInt(1),
		ToBlock:   big.NewInt(2),
		Topics:    [][]common.Hash{},
	}
	fmt.Println("filterCriteria: ", filterCriteria)
}

// TODO: delete this, this is just for experimenting
func TestFilterCriteriaMarshal(t *testing.T) {
	filterCriteria := map[string]interface{}{
		// NOTE: if you specify blockhash, you cannot specify
		"blockHash": "0xa241003d969ada282a4fe554392ef921bbeeb427810cb5e976f9225495a10888",
		"addresses": []common.Address{
			common.HexToAddress("0x95222290DD7278Aa3Ddd389Cc1E1d165CC4BAfe5"),
			common.HexToAddress("0x1f9090aaE28b8a3dCeaDf281B0F12828e676c326"),
		},
		"topics": [][]common.Hash{
			{
				common.BigToHash(big.NewInt(1)),
				common.BigToHash(big.NewInt(2)),
			},
			{
				common.BigToHash(big.NewInt(3)),
				common.BigToHash(big.NewInt(4)),
			},
		},
	}
	resObj := sendRequestGood(t, "pocFilterCriteria", filterCriteria)
	fmt.Println("resObj: ", resObj)
}

func TestNewFilter(t *testing.T) {
	tests := []struct {
		name      string
		fromBlock string
		toBlock   string
		addrs     []common.Address
		topics    []common.Hash
		wantErr   bool
		wantId    float64
	}{
		{
			name:      "happy path",
			fromBlock: "0x1",
			toBlock:   "0x2",
			addrs:     []common.Address{common.HexToAddress(common.Bytes2Hex([]byte("evmAddr")))},
			topics:    []common.Hash{common.HexToHash(common.Bytes2Hex([]byte("topic")))},
			wantErr:   false,
			wantId:    1,
		},
		{
			name:      "from block after to block",
			fromBlock: "0x2",
			toBlock:   "0x1",
			addrs:     []common.Address{common.HexToAddress(common.Bytes2Hex([]byte("evmAddr")))},
			topics:    []common.Hash{common.HexToHash(common.Bytes2Hex([]byte("topic")))},
			wantErr:   true,
			wantId:    0,
		},
		{
			name:      "from block is latest but to block is not",
			fromBlock: "latest",
			toBlock:   "0x1",
			addrs:     []common.Address{common.HexToAddress(common.Bytes2Hex([]byte("evmAddr")))},
			topics:    []common.Hash{common.HexToHash(common.Bytes2Hex([]byte("topic")))},
			wantErr:   true,
			wantId:    0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resObj := sendRequest(t, TestPort, "newFilter", tt.fromBlock, tt.toBlock, tt.addrs, tt.topics)
			if tt.wantErr {
				_, ok := resObj["error"]
				require.True(t, ok)
			} else {
				got := resObj["result"].(float64)
				require.Equal(t, tt.wantId, got)
				resObj := sendRequest(t, TestPort, "newFilter", tt.fromBlock, tt.toBlock, tt.addrs, tt.topics)
				got2 := resObj["result"].(float64)
				require.Equal(t, tt.wantId+1, got2)
			}
		})
	}
}

func TestUninstallFilter(t *testing.T) {
	// uninstall existing filter
	emptyArr := []string{}
	resObj := sendRequest(t, TestPort, "newFilter", "0x1", "0xa", []common.Address{}, emptyArr)
	filterId := int(resObj["result"].(float64))
	require.GreaterOrEqual(t, filterId, 1)

	resObj = sendRequest(t, TestPort, "uninstallFilter", filterId)
	uninstallSuccess := resObj["result"].(bool)
	require.True(t, uninstallSuccess)

	// uninstall non-existing filter
	nonExistingFilterId := 100
	resObj = sendRequest(t, TestPort, "uninstallFilter", nonExistingFilterId)
	uninstallSuccess = resObj["result"].(bool)
	require.False(t, uninstallSuccess)
}

func TestGetLogs(t *testing.T) {
	tests := []struct {
		name      string
		blockHash string
		fromBlock string
		toBlock   string
		addrs     []common.Address
		topics    []common.Hash
		wantErr   bool
		wantLen   int
		check     func(t *testing.T, log map[string]interface{})
	}{
		{
			name:      "error: block hash and block range both given",
			blockHash: "0x1111111111111111111111111111111111111111111111111111111111111111",
			fromBlock: "0x1",
			toBlock:   "0x2",
			wantErr:   true,
		},
		{
			name:      "error: from block after to block",
			blockHash: "0x0000000000000000000000000000000000000000000000000000000000000000",
			fromBlock: "0x2",
			toBlock:   "0x1",
			wantErr:   true,
		},
		{
			name:      "only block hash given 1",
			blockHash: "0x1111111111111111111111111111111111111111111111111111111111111111",
			fromBlock: "0x0",
			toBlock:   "0x0",
			wantErr:   false,
			check: func(t *testing.T, log map[string]interface{}) {
				require.Equal(t, "0x1111111111111111111111111111111111111111111111111111111111111111", log["blockHash"])
			},
			wantLen: 1,
		},
		{
			name:      "only block hash given 2",
			blockHash: "0x1111111111111111111111111111111111111111111111111111111111111112",
			fromBlock: "0x0",
			toBlock:   "0x0",
			wantErr:   false,
			check: func(t *testing.T, log map[string]interface{}) {
				require.Equal(t, "0x1111111111111111111111111111111111111111111111111111111111111112", log["blockHash"])
			},
			wantLen: 1,
		},
		{
			name:      "filter out blocks out of range",
			blockHash: "0x0000000000000000000000000000000000000000000000000000000000000000",
			fromBlock: "0x1",
			toBlock:   "0x1",
			wantErr:   false,
			check: func(t *testing.T, log map[string]interface{}) {
				require.Equal(t, "0x1", log["blockNumber"].(string))
			},
			wantLen: 1,
		},
		{
			name:      "filter by single address",
			blockHash: "0x0000000000000000000000000000000000000000000000000000000000000000",
			fromBlock: "0x2",
			toBlock:   "0x2",
			addrs:     []common.Address{common.HexToAddress("0x1111111111111111111111111111111111111112")},
			wantErr:   false,
			check: func(t *testing.T, log map[string]interface{}) {
				require.Equal(t, "0x1111111111111111111111111111111111111112", log["address"].(string))
			},
			wantLen: 1,
		},
		{
			name:      "filter by single topic",
			blockHash: "0x0000000000000000000000000000000000000000000000000000000000000000",
			fromBlock: "0x3",
			toBlock:   "0x3",
			topics:    []common.Hash{common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000123")},
			wantErr:   false,
			check: func(t *testing.T, log map[string]interface{}) {
				require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000123", log["topics"].([]interface{})[0].(string))
			},
			wantLen: 1,
		},
		{
			name:      "multiple addresses, multiple topics",
			blockHash: "0x0000000000000000000000000000000000000000000000000000000000000000",
			fromBlock: "0x2",
			toBlock:   "0x2",
			addrs: []common.Address{
				common.HexToAddress("0x1111111111111111111111111111111111111112"),
				common.HexToAddress("0x1111111111111111111111111111111111111113"),
			},
			topics: []common.Hash{
				common.Hash(common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000123")),
				common.Hash(common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000456")),
			},
			wantErr: false,
			check: func(t *testing.T, log map[string]interface{}) {
				if log["address"].(string) != "0x1111111111111111111111111111111111111112" && log["address"].(string) != "0x1111111111111111111111111111111111111113" {
					t.Fatalf("address %s not in expected list", log["address"].(string))
				}
				firstTopic := log["topics"].([]interface{})[0].(string)
				secondTopic := log["topics"].([]interface{})[1].(string)
				require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000123", firstTopic)
				require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000456", secondTopic)
			},
			wantLen: 2,
		},
		{
			name:      "wildcard first topic",
			blockHash: "0x0000000000000000000000000000000000000000000000000000000000000000",
			fromBlock: "0x2",
			toBlock:   "0x2",
			topics: []common.Hash{
				common.Hash{}, // wildcard
				common.Hash(common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000456")),
			},
			wantErr: false,
			check: func(t *testing.T, log map[string]interface{}) {
				secondTopic := log["topics"].([]interface{})[1].(string)
				require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000456", secondTopic)
			},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resObj := sendRequest(t, TestPort, "getLogs", tt.blockHash, tt.addrs, tt.fromBlock, tt.toBlock, tt.topics)
			if tt.wantErr {
				_, ok := resObj["error"]
				require.True(t, ok)
			} else {
				got := resObj["result"].([]interface{})
				for _, log := range got {
					logObj := log.(map[string]interface{})
					tt.check(t, logObj)
				}
				require.Equal(t, len(got), tt.wantLen)
			}
		})
	}
}

func TestGetFilterLogs(t *testing.T) {
	fromBlock := "0x4"
	toBlock := "0x4"
	addrs := []common.Address{common.HexToAddress(common.Bytes2Hex([]byte("evmAddr")))}
	emptyArr := []string{}
	resObj := sendRequest(t, TestPort, "newFilter", fromBlock, toBlock, addrs, emptyArr)
	filterId := int(resObj["result"].(float64))

	resObj = sendRequest(t, TestPort, "getFilterLogs", filterId)
	logs := resObj["result"].([]interface{})
	require.Equal(t, 1, len(logs))
	for _, log := range logs {
		logObj := log.(map[string]interface{})
		require.Equal(t, "0x4", logObj["blockNumber"].(string))
	}

	// error: filter id does not exist
	nonexistentFilterId := 1000
	resObj = sendRequest(t, TestPort, "getFilterLogs", nonexistentFilterId)
	_, ok := resObj["error"]
	require.True(t, ok)
}

func TestGetFilterChanges(t *testing.T) {
	fromBlock := "0x5"
	toBlock := "latest"
	addrs := []common.Address{common.HexToAddress(common.Bytes2Hex([]byte("evmAddr")))}
	emptyArr := []string{}
	resObj := sendRequest(t, TestPort, "newFilter", fromBlock, toBlock, addrs, emptyArr)
	filterId := int(resObj["result"].(float64))

	resObj = sendRequest(t, TestPort, "getFilterChanges", filterId)
	logs := resObj["result"].([]interface{})
	require.Equal(t, 1, len(logs))
	logObj := logs[0].(map[string]interface{})
	require.Equal(t, "0x5", logObj["blockNumber"].(string))

	// another query
	resObj = sendRequest(t, TestPort, "getFilterChanges", filterId)
	logs = resObj["result"].([]interface{})
	require.Equal(t, 1, len(logs))
	logObj = logs[0].(map[string]interface{})
	require.Equal(t, "0x6", logObj["blockNumber"].(string))

	// error: filter id does not exist
	nonExistingFilterId := 1000
	resObj = sendRequest(t, TestPort, "getFilterChanges", nonExistingFilterId)
	_, ok := resObj["error"]
	require.True(t, ok)
}
