package evmrpc

import (
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

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
	require.Equal(t, 1, filterId)

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
		// / having a bit of trouble specifying block range not given
		//
		// 	name:      "error: neither block hash nor block range given",
		// 	blockHash: "0x0000000000000000000000000000000000000000000000000000000000000000",
		// 	fromBlock: "0x0",
		// 	toBlock:   "0x0",
		// 	addrs:     []common.Address{common.HexToAddress(common.Bytes2Hex([]byte("evmAddr")))},
		// 	topics:    []common.Hash{common.HexToHash(common.Bytes2Hex([]byte("topic")))},
		// 	wantErr:   true,
		// 	wantLen:   0,
		// },
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
			name:      "filter out by single address",
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
			name:      "multiple addresses with nonoverlapping return values",
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
			name:      "filter by topic",
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
	// if first call to GetFilterChanges it needs to
	fromBlock := "0x5"
	toBlock := "latest"
	addrs := []common.Address{common.HexToAddress(common.Bytes2Hex([]byte("evmAddr")))}
	emptyArr := []string{}
	resObj := sendRequest(t, TestPort, "newFilter", fromBlock, toBlock, addrs, emptyArr)
	filterId := int(resObj["result"].(float64))
	fmt.Println("got filterId = ", filterId)

	resObj = sendRequest(t, TestPort, "getFilterChanges", filterId)
	logs := resObj["result"].([]interface{})
	require.Equal(t, 1, len(logs))
	logObj := logs[0].(map[string]interface{})
	fmt.Println("logObj = ", logObj)
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
