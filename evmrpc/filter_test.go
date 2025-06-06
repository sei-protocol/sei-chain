package evmrpc_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
)

func TestFilterNew(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		fromBlock string
		toBlock   string
		blockHash common.Hash
		addrs     []common.Address
		topics    [][]common.Hash
		wantErr   bool
	}{
		{
			name:      "happy path",
			fromBlock: "0x1",
			toBlock:   "0x2",
			addrs:     []common.Address{common.HexToAddress("0x123")},
			topics:    [][]common.Hash{{common.HexToHash("0x456")}},
			wantErr:   false,
		},
		{
			name:      "error: block hash and block range both given",
			fromBlock: "0x1",
			toBlock:   "0x2",
			blockHash: common.HexToHash("0xabc"),
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filterCriteria := map[string]interface{}{
				"fromBlock": tt.fromBlock,
				"toBlock":   tt.toBlock,
				"address":   tt.addrs,
				"topics":    tt.topics,
			}
			if tt.blockHash != (common.Hash{}) {
				filterCriteria["blockHash"] = tt.blockHash.Hex()
			}
			if len(tt.fromBlock) > 0 || len(tt.toBlock) > 0 {
				filterCriteria["fromBlock"] = tt.fromBlock
				filterCriteria["toBlock"] = tt.toBlock
			}
			resObj := sendRequestGood(t, "newFilter", filterCriteria)
			_, errExists := resObj["error"]

			if tt.wantErr {
				require.True(t, errExists)
			} else {
				require.False(t, errExists, "error should not exist")
				got := resObj["result"].(string)
				// make sure next filter id is not equal to this one
				resObj := sendRequestGood(t, "newFilter", filterCriteria)
				got2 := resObj["result"].(string)
				require.NotEqual(t, got, got2)
			}
		})
	}
}

func TestFilterUninstall(t *testing.T) {
	t.Parallel()
	// uninstall existing filter
	filterCriteria := map[string]interface{}{
		"fromBlock": "0x1",
		"toBlock":   "0xa",
	}
	resObj := sendRequestGood(t, "newFilter", filterCriteria)
	filterId := resObj["result"].(string)
	require.NotEmpty(t, filterId)

	resObj = sendRequest(t, TestPort, "uninstallFilter", filterId)
	uninstallSuccess := resObj["result"].(bool)
	require.True(t, uninstallSuccess)

	// uninstall non-existing filter
	nonExistingFilterId := "100"
	resObj = sendRequest(t, TestPort, "uninstallFilter", nonExistingFilterId)
	uninstallSuccess = resObj["result"].(bool)
	require.False(t, uninstallSuccess)
}

type GetFilterLogTests struct {
	name      string
	blockHash *common.Hash
	fromBlock string
	toBlock   string
	addrs     []common.Address
	topics    [][]common.Hash
	wantErr   bool
	wantLen   int
	check     func(t *testing.T, log map[string]interface{})
}

func getCommonFilterLogTests() []GetFilterLogTests {
	tests := []GetFilterLogTests{
		{
			name:      "filter by single address",
			fromBlock: "0x2",
			toBlock:   "0x2",
			addrs:     []common.Address{common.HexToAddress("0x1111111111111111111111111111111111111112")},
			wantErr:   false,
			check: func(t *testing.T, log map[string]interface{}) {
				require.Equal(t, "0x1111111111111111111111111111111111111112", log["address"].(string))
			},
			wantLen: 2,
		},
		{
			name:      "filter by single topic",
			fromBlock: "0x2",
			toBlock:   "0x2",
			topics:    [][]common.Hash{{common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000123")}},
			wantErr:   false,
			check: func(t *testing.T, log map[string]interface{}) {
				require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000123", log["topics"].([]interface{})[0].(string))
			},
			wantLen: 4,
		},
		{
			name:      "filter by single topic with block range",
			fromBlock: "0x8",
			toBlock:   "0x8",
			topics:    [][]common.Hash{{common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")}},
			wantErr:   false,
			check: func(t *testing.T, log map[string]interface{}) {
				require.Equal(t, "0x1111111111111111111111111111111111111111111111111111111111111111", log["topics"].([]interface{})[0].(string))
			},
			wantLen: 1,
		},
		{
			name:      "error with from block ahead of to block",
			fromBlock: "0x3",
			toBlock:   "0x2",
			topics:    [][]common.Hash{{common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000123")}},
			wantErr:   true,
		},
		{
			name:      "multiple addresses, multiple topics",
			fromBlock: "0x2",
			toBlock:   "0x2",
			addrs: []common.Address{
				common.HexToAddress("0x1111111111111111111111111111111111111112"),
				common.HexToAddress("0x1111111111111111111111111111111111111113"),
			},
			topics: [][]common.Hash{
				{common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000123")},
				{common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000456")},
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
			fromBlock: "0x2",
			toBlock:   "0x2",
			topics: [][]common.Hash{
				{},
				{common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000456")},
			},
			wantErr: false,
			check: func(t *testing.T, log map[string]interface{}) {
				secondTopic := log["topics"].([]interface{})[1].(string)
				require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000456", secondTopic)
			},
			wantLen: 3,
		},
	}
	return tests
}

func TestFilterGetLogs(t *testing.T) {
	testFilterGetLogs(t, "eth", getCommonFilterLogTests())
}

func TestFilterSeiGetLogs(t *testing.T) {
	// make sure we pass all the eth_ namespace tests
	testFilterGetLogs(t, "sei", getCommonFilterLogTests())

	// test where we get a synthetic log
	testFilterGetLogs(t, "sei", []GetFilterLogTests{
		{
			name:      "filter by single synthetic address",
			fromBlock: "0x64",
			toBlock:   "0x64",
			addrs:     []common.Address{common.HexToAddress("0x1111111111111111111111111111111111111116")},
			wantErr:   false,
			check: func(t *testing.T, log map[string]interface{}) {
				require.Equal(t, "0x1111111111111111111111111111111111111116", log["address"].(string))
			},
			wantLen: 1,
		},
		{
			name:      "filter by single topic, include synethetic logs",
			topics:    [][]common.Hash{{common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000234")}},
			wantErr:   false,
			fromBlock: "0x64",
			toBlock:   "0x64",
			check: func(t *testing.T, log map[string]interface{}) {
				require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000234", log["topics"].([]interface{})[0].(string))
			},
			wantLen: 1,
		},
		{
			name:    "filter by single topic with default range, include synethetic logs",
			topics:  [][]common.Hash{{common.HexToHash("0x0000000000000000000000000000000000000000000000000000000000000234")}},
			wantErr: false,
			check: func(t *testing.T, log map[string]interface{}) {
				require.Equal(t, "0x0000000000000000000000000000000000000000000000000000000000000234", log["topics"].([]interface{})[0].(string))
			},
			wantLen: 1,
		},
	})
}

func TestFilterEthEndpointReturnsNormalEvmLogEvenIfSyntheticLogIsInSameBlock(t *testing.T) {
	testFilterGetLogs(t, "eth", []GetFilterLogTests{
		{
			name:      "normal evm log is returned even if synthetic log is in the same block",
			fromBlock: "0x64", // 100
			toBlock:   "0x64",
			wantErr:   false,
			check: func(t *testing.T, log map[string]interface{}) {
				// check that none of the events have the synthetic hash
				syntheticHash := multiTxBlockSynthTx.Hash()
				require.NotEqual(t, syntheticHash.Hex(), log["transactionHash"].(string))
			},
			wantLen: 2,
		},
	})
}

func testFilterGetLogs(t *testing.T, namespace string, tests []GetFilterLogTests) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filterCriteria := map[string]interface{}{
				"address": tt.addrs,
				"topics":  tt.topics,
			}
			if tt.blockHash != nil {
				filterCriteria["blockHash"] = tt.blockHash.Hex()
			}
			if len(tt.fromBlock) > 0 || len(tt.toBlock) > 0 {
				filterCriteria["fromBlock"] = tt.fromBlock
				filterCriteria["toBlock"] = tt.toBlock
			}
			var resObj map[string]interface{}
			if namespace == "eth" {
				resObj = sendRequestGood(t, "getLogs", filterCriteria)
			} else if namespace == "sei" {
				resObj = sendSeiRequestGood(t, "getLogs", filterCriteria)
			} else {
				panic("unknown namespace")
			}
			if tt.wantErr {
				_, ok := resObj["error"]
				require.True(t, ok)
			} else {
				got := resObj["result"].([]interface{})
				for _, log := range got {
					logObj := log.(map[string]interface{})
					tt.check(t, logObj)
				}
				require.Equal(t, tt.wantLen, len(got))
			}
		})
	}
}

func TestFilterGetFilterLogs(t *testing.T) {
	filterCriteria := map[string]interface{}{
		"fromBlock": "0x2",
		"toBlock":   "0x2",
	}
	resObj := sendRequestGood(t, "newFilter", filterCriteria)
	filterId := resObj["result"].(string)

	resObj = sendRequest(t, TestPort, "getFilterLogs", filterId)
	logs := resObj["result"].([]interface{})
	require.Equal(t, 4, len(logs))
	for _, log := range logs {
		logObj := log.(map[string]interface{})
		require.Equal(t, "0x2", logObj["blockNumber"].(string))
	}

	// error: filter id does not exist
	nonexistentFilterId := 1000
	resObj = sendRequest(t, TestPort, "getFilterLogs", nonexistentFilterId)
	_, ok := resObj["error"]
	require.True(t, ok)
}

func TestFilterGetFilterChanges(t *testing.T) {
	filterCriteria := map[string]interface{}{
		"fromBlock": "0x2",
	}
	resObj := sendRequest(t, TestPort, "newFilter", filterCriteria)
	filterId := resObj["result"].(string)

	resObj = sendRequest(t, TestPort, "getFilterChanges", filterId)
	logs := resObj["result"].([]interface{})
	require.Equal(t, 10, len(logs)) // limited by MaxLogNoBlock config to 4
	logObj := logs[0].(map[string]interface{})
	require.Equal(t, "0x2", logObj["blockNumber"].(string))

	// error: filter id does not exist
	nonExistingFilterId := 1000
	resObj = sendRequest(t, TestPort, "getFilterChanges", nonExistingFilterId)
	_, ok := resObj["error"]
	require.True(t, ok)
}

func TestFilterBlockFilter(t *testing.T) {
	t.Parallel()
	resObj := sendRequestGood(t, "newBlockFilter")
	blockFilterId := resObj["result"].(string)
	resObj = sendRequestGood(t, "getFilterChanges", blockFilterId)
	hashesInterface := resObj["result"].([]interface{})
	for _, hashInterface := range hashesInterface {
		hash := hashInterface.(string)
		require.Equal(t, 66, len(hash))
		require.Equal(t, "0x", hash[:2])
	}
	// query again to make sure cursor is updated
	resObj = sendRequestGood(t, "getFilterChanges", blockFilterId)
	hashesInterface = resObj["result"].([]interface{})
	for _, hashInterface := range hashesInterface {
		hash := hashInterface.(string)
		require.Equal(t, 66, len(hash))
		require.Equal(t, "0x", hash[:2])
	}
}

func TestFilterExpiration(t *testing.T) {
	t.Parallel()
	filterCriteria := map[string]interface{}{
		"fromBlock": "0x1",
		"toBlock":   "0xa",
	}
	resObj := sendRequestGood(t, "newFilter", filterCriteria)
	filterId := resObj["result"].(string)

	// wait for filter to expire
	time.Sleep(2 * filterTimeoutDuration)

	resObj = sendRequest(t, TestPort, "getFilterLogs", filterId)
	_, ok := resObj["error"]
	require.True(t, ok)
}

func TestFilterGetFilterLogsKeepsFilterAlive(t *testing.T) {
	t.Parallel()
	filterCriteria := map[string]interface{}{
		"fromBlock": "0x1",
		"toBlock":   "0xa",
	}
	resObj := sendRequestGood(t, "newFilter", filterCriteria)
	filterId := resObj["result"].(string)

	for i := 0; i < 5; i++ {
		// should keep filter alive
		resObj = sendRequestGood(t, "getFilterLogs", filterId)
		_, ok := resObj["error"]
		require.False(t, ok)
		time.Sleep(filterTimeoutDuration / 2)
	}
}

func TestFilterGetFilterChangesKeepsFilterAlive(t *testing.T) {
	t.Parallel()
	filterCriteria := map[string]interface{}{
		"fromBlock": "0x1",
		"toBlock":   "0xa",
	}
	resObj := sendRequestGood(t, "newFilter", filterCriteria)
	filterId := resObj["result"].(string)

	for i := 0; i < 5; i++ {
		// should keep filter alive
		resObj = sendRequestGood(t, "getFilterChanges", filterId)
		_, ok := resObj["error"]
		require.False(t, ok)
		time.Sleep(filterTimeoutDuration / 2)
	}
}
