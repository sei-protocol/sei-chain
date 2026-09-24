package rpc

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/eth/filters"
	"github.com/stretchr/testify/require"

	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/testutil"
	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// TestGetLogsLittIdxLogIndexMatchesReceipt writes Giga-shaped receipts, whose
// stored log indexes are already block-wide, to the production littidx
// backend and checks that eth_getLogs reports the same logIndex as
// eth_getTransactionReceipt does for the same log.
func TestGetLogsLittIdxLogIndexMatchesReceipt(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey("evm")
	tkey := storetypes.NewTransientStoreKey("evm_transient")
	ctx := testutil.DefaultContext(storeKey, tkey)
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "littidx"
	cfg.DBDirectory = t.TempDir()
	cfg.KeepRecent = 0
	cfg.AsyncWriteBuffer = 0
	store, err := receipt.NewReceiptStore(cfg, storeKey)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	const height = 5
	log := func(address common.Address, index uint32, topics ...common.Hash) *evmtypes.Log {
		hexTopics := make([]string, len(topics))
		for i, topic := range topics {
			hexTopics[i] = topic.Hex()
		}
		return &evmtypes.Log{Address: address.Hex(), Topics: hexTopics, Data: []byte{byte(index)}, Index: index}
	}
	stored := func(txIndex uint32, logs ...*evmtypes.Log) receipt.ReceiptRecord {
		hash := filterTxHash(height, txIndex)
		return receipt.ReceiptRecord{TxHash: hash, Receipt: &evmtypes.Receipt{
			TxHashHex:        hash.Hex(),
			BlockNumber:      height,
			TransactionIndex: txIndex,
			Logs:             logs,
		}}
	}
	// tx0 emits one log, tx1 two and tx2 one, numbered 0..3 across the block.
	records := []receipt.ReceiptRecord{
		stored(0, log(filterContractB, 0, filterTopicY)),
		stored(1, log(filterContractA, 1, filterTopicX), log(filterContractA, 2, filterTopicY)),
		stored(2, log(filterContractA, 3, filterTopicX)),
	}
	for h := int64(1); h < height; h++ {
		require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(h), nil))
	}
	require.NoError(t, store.SetReceipts(ctx.WithBlockHeight(height), records))
	require.Equal(t, int64(height), store.LatestVersion())

	api := &filterAPI{backend: filterFixtureBackend(t, height), store: store}
	logs, err := api.GetLogs(t.Context(), filters.FilterCriteria{
		FromBlock: big.NewInt(height),
		ToBlock:   big.NewInt(height),
	})
	require.NoError(t, err)
	require.Len(t, logs, 4)
	for i, lg := range logs {
		require.Equal(t, uint(i), lg.Index, "log %d", i) //nolint:gosec // small test indices
		require.Equal(t, uint64(height), lg.BlockNumber)
		require.Equal(t, filterBlockHash(height), lg.BlockHash)
		require.Equal(t, filterTxHash(height, uint32(lg.TxIndex)), lg.TxHash) //nolint:gosec // small test indices

		// eth_getTransactionReceipt reports the stored index verbatim.
		rcpt, err := store.GetReceipt(ctx, lg.TxHash)
		require.NoError(t, err)
		require.Equal(t, uint(rcpt.Logs[lg.Index-uint(rcpt.Logs[0].Index)].Index), lg.Index)
	}

	logs, err = api.GetLogs(t.Context(), filters.FilterCriteria{
		FromBlock: big.NewInt(height),
		ToBlock:   big.NewInt(height),
		Addresses: []common.Address{filterContractA},
		Topics:    [][]common.Hash{{filterTopicX}},
	})
	require.NoError(t, err)
	require.Len(t, logs, 2)
	require.Equal(t, uint(1), logs[0].Index)
	require.Equal(t, uint(3), logs[1].Index)
}
