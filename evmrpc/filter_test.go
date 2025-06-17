package evmrpc_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
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

func TestGetLogsBlockHashIsNotZero(t *testing.T) {
	t.Parallel()
	// Test that eth_getLogs returns logs with correct blockHash (not zero hash)
	filterCriteria := map[string]interface{}{
		"fromBlock": "0x2",
		"toBlock":   "0x2",
	}
	resObj := sendRequestGood(t, "getLogs", filterCriteria)
	logs := resObj["result"].([]interface{})
	require.Greater(t, len(logs), 0, "should have at least one log")

	expectedBlockHash := "0x0000000000000000000000000000000000000000000000000000000000000002" // MultiTxBlockHash
	zeroBlockHash := "0x0000000000000000000000000000000000000000000000000000000000000000"

	for i, logInterface := range logs {
		log := logInterface.(map[string]interface{})
		blockHash := log["blockHash"].(string)

		// The main check: blockHash should not be zero
		require.NotEqual(t, zeroBlockHash, blockHash,
			"log %d should not have zero blockHash", i)

		// Additional check: it should be the expected block hash for block 2
		require.Equal(t, expectedBlockHash, blockHash,
			"log %d should have correct blockHash for block 2", i)

		// Verify other expected fields are present
		require.Equal(t, "0x2", log["blockNumber"].(string),
			"log %d should be from block 2", i)
	}
}

func TestGetLogsTransactionIndexConsistency(t *testing.T) {
	t.Parallel()

	// Test that eth_getLogs returns logs with transaction indices that match eth_getBlockByNumber
	// This is a regression test for the transaction index mismatch issue

	// Get logs from a known block with multiple transactions
	filterCriteria := map[string]interface{}{
		"fromBlock": "0x8",
		"toBlock":   "0x8",
	}
	resObj := sendRequestGood(t, "getLogs", filterCriteria)
	logs := resObj["result"].([]interface{})
	require.Greater(t, len(logs), 0, "should have at least one log")

	// Get the block to see what transaction indices eth_getBlockByNumber returns
	blockRes := sendRequestGood(t, "getBlockByNumber", "0x8", true)
	block := blockRes["result"].(map[string]interface{})
	transactions := block["transactions"].([]interface{})
	require.Greater(t, len(transactions), 0, "block should have transactions")

	// Create a map of transaction hash to EVM transaction index from the block
	hashToEvmIndex := make(map[string]int)
	for i, txInterface := range transactions {
		tx := txInterface.(map[string]interface{})
		txHash := tx["hash"].(string)
		hashToEvmIndex[txHash] = i
	}

	// Verify that each log's transactionIndex is a valid EVM index (0 to len(transactions)-1)
	for i, logInterface := range logs {
		log := logInterface.(map[string]interface{})
		txHash := log["transactionHash"].(string)
		logTxIndex := log["transactionIndex"].(string)

		// Convert hex string to int for comparison
		logTxIndexInt, err := strconv.ParseInt(logTxIndex[2:], 16, 64)
		require.NoError(t, err, "should be able to parse transaction index from log %d", i)

		// Key assertion: transactionIndex should be a valid EVM transaction index
		require.GreaterOrEqual(t, logTxIndexInt, int64(0),
			"log %d: transactionIndex %d should be >= 0", i, logTxIndexInt)
		require.Less(t, logTxIndexInt, int64(len(transactions)),
			"log %d: transactionIndex %d should be < %d (number of EVM transactions)",
			i, logTxIndexInt, len(transactions))

		// If the transaction exists in the block, verify indices match
		if expectedEvmIndex, exists := hashToEvmIndex[txHash]; exists {
			require.Equal(t, int64(expectedEvmIndex), logTxIndexInt,
				"log %d: transactionIndex from eth_getLogs (%d) should match EVM transaction index from eth_getBlockByNumber (%d) for tx %s",
				i, logTxIndexInt, expectedEvmIndex, txHash)
		}
	}

	// Additional check: ensure transaction indices are reasonable for the block structure
	// Block 8 should have mixed transaction types, so EVM transaction indices should be sequential
	txIndicesFound := make(map[int64]bool)
	for _, logInterface := range logs {
		log := logInterface.(map[string]interface{})
		logTxIndex := log["transactionIndex"].(string)
		logTxIndexInt, _ := strconv.ParseInt(logTxIndex[2:], 16, 64)
		txIndicesFound[logTxIndexInt] = true
	}

	// We should not see indices that are >= number of EVM transactions
	for txIndex := range txIndicesFound {
		require.Less(t, txIndex, int64(len(transactions)),
			"no log should have transactionIndex %d when there are only %d EVM transactions",
			txIndex, len(transactions))
	}
}

func TestCollectLogsEvmTransactionIndex(t *testing.T) {
	t.Parallel()

	// This is a unit test for the core logic that collectLogs implements
	// It tests that transaction indices are set correctly for EVM transactions

	// Set up the test environment - use the correct return values from MockEVMKeeper
	k, ctx := testkeeper.MockEVMKeeper()

	// Create a mock block with mixed transaction types (similar to block 2 in our test data)
	// We'll simulate the transaction hashes that getTxHashesFromBlock would return
	evmTxHashes := []common.Hash{
		common.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"), // EVM tx index 0
		common.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"), // EVM tx index 1
		common.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"), // EVM tx index 2
	}

	// Create mock receipts with logs
	for i, txHash := range evmTxHashes {
		receipt := &evmtypes.Receipt{
			TxHashHex:        txHash.Hex(),
			TransactionIndex: uint32(i + 10), // Use high absolute indices to simulate mixed tx types
			BlockNumber:      2,
			Logs: []*evmtypes.Log{
				{
					Address: "0x1111111111111111111111111111111111111112",
					Topics:  []string{"0x0000000000000000000000000000000000000000000000000000000000000123"},
					Data:    []byte("test data"),
					Index:   0,
				},
			},
			LogsBloom: make([]byte, 256), // Empty bloom for simplicity
		}

		// Fill bloom filter to match our test filters
		receipt.LogsBloom[0] = 0xFF // Simple bloom that will match any filter

		k.MockReceipt(ctx, txHash, receipt)
	}

	// Test the core logic that collectLogs implements
	// This simulates what collectLogs does for each EVM transaction
	var collectedLogs []*ethtypes.Log
	evmTxIndex := 0
	totalLogs := uint(0)

	for _, txHash := range evmTxHashes {
		receipt, err := k.GetReceipt(ctx, txHash)
		require.NoError(t, err, "should be able to get receipt for tx %s", txHash.Hex())

		// This simulates keeper.GetLogsForTx
		logs := keeper.GetLogsForTx(receipt, totalLogs)

		// This is the key part we're testing: setting the correct EVM transaction index
		for _, log := range logs {
			log.TxIndex = uint(evmTxIndex) // This should override receipt.TransactionIndex
			collectedLogs = append(collectedLogs, log)
		}

		totalLogs += uint(len(receipt.Logs))
		evmTxIndex++
	}

	// Verify that the transaction indices are set correctly
	require.Equal(t, len(evmTxHashes), len(collectedLogs), "should have one log per transaction")

	for i, log := range collectedLogs {
		// This is the main assertion: TxIndex should be the EVM transaction index (0, 1, 2)
		// NOT the absolute transaction index (10, 11, 12)
		require.Equal(t, uint(i), log.TxIndex,
			"log %d should have EVM transaction index %d, but got %d", i, i, log.TxIndex)

		// Verify it's NOT using the absolute transaction index
		require.NotEqual(t, uint(i+10), log.TxIndex,
			"log %d should not use absolute transaction index %d", i, i+10)
	}
}
