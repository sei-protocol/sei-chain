package rpc

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http/httptest"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/filters"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

var (
	filterContractA = common.HexToAddress("0x3000000000000000000000000000000000000003")
	filterContractB = common.HexToAddress("0x4000000000000000000000000000000000000004")
	filterTopicX    = common.HexToHash("0xaaaa")
	filterTopicY    = common.HexToHash("0xbbbb")
)

func filterBlockHash(height uint64) common.Hash {
	return common.BytesToHash(fmt.Appendf(nil, "block-%d", height))
}

func filterTxHash(height uint64, txIndex uint32) common.Hash {
	return common.BytesToHash(fmt.Appendf(nil, "tx-%d-%d", height, txIndex))
}

// filterFixtureStore seeds blocks 5..9. Block 5 holds two transactions, the
// first emitting one log for contract B and the second emitting two logs for
// contract A (block-wide indexes 1 and 2), so the second transaction's logs
// exercise the first-log-index rebasing. Block 8 holds one contract A log.
func filterFixtureStore(t *testing.T) *evmonly.MemoryReceiptStore {
	t.Helper()
	store := evmonly.NewMemoryReceiptStore()
	log := func(address common.Address, index uint32, topics ...common.Hash) *evmtypes.Log {
		hexTopics := make([]string, len(topics))
		for i, topic := range topics {
			hexTopics[i] = topic.Hex()
		}
		return &evmtypes.Log{Address: address.Hex(), Topics: hexTopics, Data: []byte{byte(index)}, Index: index}
	}
	stored := func(height uint64, txIndex uint32, logs ...*evmtypes.Log) receipt.ReceiptRecord {
		hash := filterTxHash(height, txIndex)
		return receipt.ReceiptRecord{TxHash: hash, Receipt: &evmtypes.Receipt{
			TxHashHex:        hash.Hex(),
			BlockNumber:      height,
			TransactionIndex: txIndex,
			Logs:             logs,
		}}
	}
	ctx := sdk.Context{}.WithContext(t.Context())
	require.NoError(t, store.SetReceipts(ctx, []receipt.ReceiptRecord{
		stored(5, 0, log(filterContractB, 0, filterTopicY)),
		stored(5, 1, log(filterContractA, 1, filterTopicX), log(filterContractA, 2, filterTopicY)),
	}))
	require.NoError(t, store.SetReceipts(ctx, []receipt.ReceiptRecord{stored(6, 0)}))
	require.NoError(t, store.SetReceipts(ctx, []receipt.ReceiptRecord{stored(8, 0, log(filterContractA, 0, filterTopicX))}))
	require.NoError(t, store.SetLatestVersion(9))
	return store
}

func filterFixtureBackend(t *testing.T, head uint64) *testBackend {
	t.Helper()
	return &testBackend{
		blockNumber: func() uint64 { return head },
		block: func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			require.NotNil(t, req.Height)
			height := uint64(*req.Height)
			return &coretypes.ResultBlock{
				BlockID: tmtypes.BlockID{Hash: filterBlockHash(height).Bytes()},
				Block:   &tmtypes.Block{Header: tmtypes.Header{Height: int64(height)}},
			}, nil
		},
		blockByHash: func(_ context.Context, req *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
			for height := uint64(1); height <= head; height++ {
				if common.BytesToHash(req.Hash) == filterBlockHash(height) {
					return &coretypes.ResultBlock{
						BlockID: tmtypes.BlockID{Hash: req.Hash},
						Block:   &tmtypes.Block{Header: tmtypes.Header{Height: int64(height)}},
					}, nil
				}
			}
			return nil, nil
		},
	}
}

func TestGetLogsAddressRangeQuery(t *testing.T) {
	api := &filterAPI{backend: filterFixtureBackend(t, 9), store: filterFixtureStore(t)}
	logs, err := api.GetLogs(t.Context(), filters.FilterCriteria{
		FromBlock: big.NewInt(1),
		ToBlock:   big.NewInt(9),
		Addresses: []common.Address{filterContractA},
	})
	require.NoError(t, err)
	require.Len(t, logs, 3)

	require.Equal(t, uint64(5), logs[0].BlockNumber)
	require.Equal(t, filterBlockHash(5), logs[0].BlockHash)
	require.Equal(t, filterTxHash(5, 1), logs[0].TxHash)
	require.Equal(t, uint(1), logs[0].TxIndex)
	require.Equal(t, uint(1), logs[0].Index)
	require.Equal(t, []common.Hash{filterTopicX}, logs[0].Topics)
	require.Equal(t, []byte{1}, logs[0].Data)

	require.Equal(t, uint(2), logs[1].Index)
	require.Equal(t, filterTxHash(5, 1), logs[1].TxHash)

	require.Equal(t, uint64(8), logs[2].BlockNumber)
	require.Equal(t, filterBlockHash(8), logs[2].BlockHash)
	require.Equal(t, filterTxHash(8, 0), logs[2].TxHash)
	require.Equal(t, uint(0), logs[2].TxIndex)
	require.Equal(t, uint(0), logs[2].Index)
}

func TestGetLogsTopicFilter(t *testing.T) {
	api := &filterAPI{backend: filterFixtureBackend(t, 9), store: filterFixtureStore(t)}
	logs, err := api.GetLogs(t.Context(), filters.FilterCriteria{
		FromBlock: big.NewInt(5),
		ToBlock:   big.NewInt(5),
		Topics:    [][]common.Hash{{filterTopicY}},
	})
	require.NoError(t, err)
	require.Len(t, logs, 2)
	require.Equal(t, filterContractB, logs[0].Address)
	require.Equal(t, uint(0), logs[0].Index)
	require.Equal(t, filterContractA, logs[1].Address)
	require.Equal(t, uint(2), logs[1].Index)

	logs, err = api.GetLogs(t.Context(), filters.FilterCriteria{
		Addresses: []common.Address{filterContractA},
		Topics:    [][]common.Hash{{filterTopicY}},
	})
	require.NoError(t, err)
	require.Empty(t, logs)
}

func TestGetLogsBlockHashForm(t *testing.T) {
	api := &filterAPI{backend: filterFixtureBackend(t, 9), store: filterFixtureStore(t)}
	blockHash := filterBlockHash(5)
	logs, err := api.GetLogs(t.Context(), filters.FilterCriteria{BlockHash: &blockHash})
	require.NoError(t, err)
	require.Len(t, logs, 3)
	for _, lg := range logs {
		require.Equal(t, uint64(5), lg.BlockNumber)
		require.Equal(t, blockHash, lg.BlockHash)
	}

	unknown := common.HexToHash("0xdead")
	_, err = api.GetLogs(t.Context(), filters.FilterCriteria{BlockHash: &unknown})
	require.ErrorContains(t, err, "not found")
}

func TestGetLogsResolvesTagsAgainstIndexedRange(t *testing.T) {
	store := filterFixtureStore(t)
	// The committed head (20) is ahead of the store's indexed head (9).
	api := &filterAPI{backend: filterFixtureBackend(t, 20), store: store}

	// Neither bound set: latest indexed..latest indexed.
	logs, err := api.GetLogs(t.Context(), filters.FilterCriteria{})
	require.NoError(t, err)
	require.Empty(t, logs)

	// Head tags resolve to the indexed head, not the committed one.
	logs, err = api.GetLogs(t.Context(), filters.FilterCriteria{
		FromBlock: big.NewInt(8), ToBlock: big.NewInt(ethrpc.LatestBlockNumber.Int64()),
	})
	require.NoError(t, err)
	require.Len(t, logs, 1)

	// Earliest tag follows the retention floor as history is pruned.
	logs, err = api.GetLogs(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(ethrpc.EarliestBlockNumber.Int64())})
	require.NoError(t, err)
	require.Len(t, logs, 4)
	require.NoError(t, store.PruneHistory(6))
	logs, err = api.GetLogs(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(ethrpc.EarliestBlockNumber.Int64())})
	require.NoError(t, err)
	require.Len(t, logs, 1)
}

func TestGetLogsRejectsBoundsOutsideIndexedRange(t *testing.T) {
	store := filterFixtureStore(t)
	api := &filterAPI{backend: filterFixtureBackend(t, 20), store: store}

	// An explicit toBlock between the indexed head and the committed head must
	// not be answered partially: the caller would advance past unseen logs.
	_, err := api.GetLogs(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(8), ToBlock: big.NewInt(10)})
	require.ErrorIs(t, err, errLogRangeNotIndexed)
	_, err = api.GetLogs(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(50)})
	require.ErrorIs(t, err, errLogRangeInverted)
	_, err = api.GetLogs(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(50), ToBlock: big.NewInt(60)})
	require.ErrorIs(t, err, errLogRangeNotIndexed)
	blockHash := filterBlockHash(12)
	_, err = api.GetLogs(t.Context(), filters.FilterCriteria{BlockHash: &blockHash})
	require.ErrorIs(t, err, errLogRangeNotIndexed)

	// A pruned fromBlock is reported rather than silently skipped.
	require.NoError(t, store.PruneHistory(6))
	_, err = api.GetLogs(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(5), ToBlock: big.NewInt(8)})
	require.ErrorIs(t, err, errLogRangePruned)
	blockHash = filterBlockHash(5)
	_, err = api.GetLogs(t.Context(), filters.FilterCriteria{BlockHash: &blockHash})
	require.ErrorIs(t, err, errLogRangePruned)
	logs, err := api.GetLogs(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(6), ToBlock: big.NewInt(9)})
	require.NoError(t, err)
	require.Len(t, logs, 1)
}

func TestGetLogsRejectsBadRanges(t *testing.T) {
	api := &filterAPI{backend: filterFixtureBackend(t, 9), store: filterFixtureStore(t)}
	_, err := api.GetLogs(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(8), ToBlock: big.NewInt(7)})
	require.ErrorIs(t, err, errLogRangeInverted)

	store := filterFixtureStore(t)
	require.NoError(t, store.SetLatestVersion(5000))
	api = &filterAPI{backend: filterFixtureBackend(t, 5000), store: store}
	_, err = api.GetLogs(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(1), ToBlock: big.NewInt(2001)})
	require.ErrorIs(t, err, errLogRangeTooWide)
	_, err = api.GetLogs(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(1), ToBlock: big.NewInt(2000)})
	require.NoError(t, err)

	tooBig := new(big.Int).Lsh(big.NewInt(1), 70)
	_, err = api.GetLogs(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(1), ToBlock: tooBig})
	require.ErrorContains(t, err, "exceeds int64")
}

func TestGetLogsEnforcesLogBudget(t *testing.T) {
	store := evmonly.NewMemoryReceiptStore()
	logs := make([]*evmtypes.Log, maxLogsPerQuery+1)
	for i := range logs {
		logs[i] = &evmtypes.Log{Address: filterContractA.Hex(), Index: uint32(i)} //nolint:gosec // small test count
	}
	hash := filterTxHash(1, 0)
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash:  hash,
		Receipt: &evmtypes.Receipt{TxHashHex: hash.Hex(), BlockNumber: 1, Logs: logs},
	}}))
	api := &filterAPI{backend: filterFixtureBackend(t, 1), store: store}
	_, err := api.GetLogs(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(1), ToBlock: big.NewInt(1)})
	require.Error(t, err)
	require.ErrorContains(t, err, "filter logs")
}

func TestGetLogsSurfacesStoreAndBlockErrors(t *testing.T) {
	boom := errors.New("boom")
	backend := filterFixtureBackend(t, 9)
	backend.block = func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return nil, boom
	}
	api := &filterAPI{backend: backend, store: filterFixtureStore(t)}
	_, err := api.GetLogs(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(5), ToBlock: big.NewInt(5)})
	require.ErrorIs(t, err, boom)

	api = &filterAPI{backend: filterFixtureBackend(t, 9), store: stubReceiptStore{
		ReceiptStore: filterFixtureStore(t),
		get:          func(sdk.Context, common.Hash) (*evmtypes.Receipt, error) { return nil, boom },
	}}
	_, err = api.GetLogs(t.Context(), filters.FilterCriteria{FromBlock: big.NewInt(5), ToBlock: big.NewInt(5)})
	require.ErrorIs(t, err, boom)
}

func TestGetLogsEndToEnd(t *testing.T) {
	handler, err := newHandler(filterFixtureBackend(t, 9), filterFixtureStore(t))
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got []*ethtypes.Log
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_getLogs", map[string]any{
		"fromBlock": "0x1",
		"toBlock":   "0x9",
		"address":   filterContractA,
	}))
	require.Len(t, got, 3)
	require.Equal(t, filterBlockHash(5), got[0].BlockHash)
	require.Equal(t, uint(1), got[0].Index)
	require.Equal(t, filterBlockHash(8), got[2].BlockHash)

	got = nil
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_getLogs", map[string]any{
		"blockHash": filterBlockHash(8),
		"topics":    []any{[]common.Hash{filterTopicX, filterTopicY}},
	}))
	require.Len(t, got, 1)
	require.Equal(t, filterTxHash(8, 0), got[0].TxHash)

	got = nil
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_getLogs", map[string]any{
		"fromBlock": "0x6",
		"toBlock":   "0x7",
	}))
	require.NotNil(t, got)
	require.Empty(t, got)

	err = client.CallContext(t.Context(), &got, "eth_getLogs", map[string]any{
		"fromBlock": "0x8",
		"toBlock":   "0x7",
	})
	var rpcErr ethrpc.Error
	require.ErrorAs(t, err, &rpcErr)
	require.Equal(t, -32602, rpcErr.ErrorCode())
}
