package rpc

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethrpc "github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/giga/evmonly"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

// floorReceiptStore overrides EarliestVersion on an otherwise real store,
// answering successive calls from floors and repeating the last one.
type floorReceiptStore struct {
	receipt.ReceiptStore
	floors []int64
}

func (s *floorReceiptStore) EarliestVersion() int64 {
	floor := s.floors[0]
	if len(s.floors) > 1 {
		s.floors = s.floors[1:]
	}
	return floor
}

// receiptTxHashes returns the transactionHash of each encoded receipt, in order.
func receiptTxHashes(receipts []map[string]any) []common.Hash {
	hashes := make([]common.Hash, len(receipts))
	for i, r := range receipts {
		hashes[i] = r["transactionHash"].(common.Hash)
	}
	return hashes
}

func TestGetBlockReceipts(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	twoTxBlock, tx1, tx2, store := multiTxBlock(t, 9, blockHash, time.Unix(1_700_000_000, 0))
	emptyBlock := &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
		Block:   &tmtypes.Block{Header: tmtypes.Header{Height: 4, Time: time.Unix(1_700_000_000, 0)}},
	}
	unindexedBlock := &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
		Block:   &tmtypes.Block{Header: tmtypes.Header{Height: 10, Time: time.Unix(1_700_000_000, 0)}},
	}
	backendErr := errors.New("block store unavailable")
	byNumber := ethrpc.BlockNumberOrHashWithNumber
	byHash := ethrpc.BlockNumberOrHashWithHash(blockHash, false)
	heightOf := func(h int64) *coretypes.Int64 {
		height := coretypes.Int64(h)
		return &height
	}
	both := []common.Hash{tx1.Hash(), tx2.Hash()}

	for _, tc := range []struct {
		name       string
		arg        ethrpc.BlockNumberOrHash
		head       uint64
		floors     []int64
		wantHeight *coretypes.Int64
		block      *coretypes.ResultBlock
		blockErr   error
		want       []common.Hash
		wantErr    error
	}{
		// Current-state tags all resolve to the latest block whose receipts are indexed.
		{name: "latest", arg: byNumber(ethrpc.LatestBlockNumber), wantHeight: heightOf(9), block: twoTxBlock, want: both},
		{name: "safe", arg: byNumber(ethrpc.SafeBlockNumber), wantHeight: heightOf(9), block: twoTxBlock, want: both},
		{name: "finalized", arg: byNumber(ethrpc.FinalizedBlockNumber), wantHeight: heightOf(9), block: twoTxBlock, want: both},
		{name: "pending", arg: byNumber(ethrpc.PendingBlockNumber), wantHeight: heightOf(9), block: twoTxBlock, want: both},
		{name: "tag behind a committed head the receipts have not reached", arg: byNumber(ethrpc.LatestBlockNumber), head: 12, wantHeight: heightOf(9), block: twoTxBlock, want: both},
		{name: "explicit height", arg: byNumber(9), wantHeight: heightOf(9), block: twoTxBlock, want: both},
		{name: "known hash", arg: byHash, block: twoTxBlock, want: both},
		{name: "empty block is an empty list, not null", arg: byNumber(4), wantHeight: heightOf(4), block: emptyBlock, want: []common.Hash{}},
		// Requests with no block, or with receipts not yet indexed, answer null, not an error.
		{name: "height not yet indexed", arg: byNumber(10), wantHeight: heightOf(10), block: unindexedBlock},
		{name: "hash not yet indexed", arg: byHash, block: unindexedBlock},
		{name: "future height", arg: byNumber(100), wantHeight: heightOf(100), blockErr: fmt.Errorf("%w: 100", coretypes.ErrHeightExceedsChainHead)},
		{name: "earliest", arg: byNumber(ethrpc.EarliestBlockNumber), wantHeight: heightOf(0), blockErr: fmt.Errorf("%w: 0", coretypes.ErrZeroOrNegativeHeight)},
		{name: "nil block", arg: byNumber(9), wantHeight: heightOf(9)},
		{name: "unknown hash, empty result", arg: byHash, block: &coretypes.ResultBlock{}},
		{name: "unknown hash, nil result", arg: byHash},
		// A pruned height or receipts, a missing block selector, and any other backend failure are errors.
		{name: "pruned height", arg: byNumber(1), wantHeight: heightOf(1), blockErr: coretypes.WrapErrHeightNotAvailable(1, utils.None[int64]()), wantErr: coretypes.ErrHeightNotAvailable},
		{name: "pruned receipts", arg: byNumber(9), floors: []int64{10}, wantHeight: heightOf(9), block: twoTxBlock, wantErr: errBlockReceiptsPruned},
		{name: "neither number nor hash", wantErr: errBlockReceiptsNoBlock},
		{name: "unexpected backend error by number", arg: byNumber(9), wantHeight: heightOf(9), blockErr: backendErr, wantErr: backendErr},
		{name: "unexpected backend error by hash", arg: byHash, blockErr: backendErr, wantErr: backendErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			head := tc.head
			if head == 0 {
				head = 9
			}
			backend := &testBackend{
				block: func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
					require.Equal(t, tc.wantHeight, req.Height)
					return tc.block, tc.blockErr
				},
				blockByHash: func(_ context.Context, req *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
					require.Equal(t, blockHash.Bytes(), []byte(req.Hash))
					return tc.block, tc.blockErr
				},
				blockNumber: func() uint64 { return head },
			}
			receipts := store
			if tc.floors != nil {
				receipts = &floorReceiptStore{ReceiptStore: store, floors: tc.floors}
			}

			got, err := (&blockAPI{backend: backend, store: receipts}).GetBlockReceipts(t.Context(), tc.arg)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			if tc.want == nil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tc.want, receiptTxHashes(got))
		})
	}
}

// TestGetBlockReceiptsMatchesGetTransactionReceipt pins that each block receipt
// equals eth_getTransactionReceipt's answer for its hash, and that the block
// lists only receipts stored at that block: a replayed transaction keeps its
// earlier receipt and a rejected one has none. Both the iterating store and
// the by-hash fallback must agree.
func TestGetBlockReceiptsMatchesGetTransactionReceipt(t *testing.T) {
	tx1, raw1 := testSignedTransaction(t)
	tx2, raw2 := secondSignedTransaction(t)
	receiptAt := func(hash common.Hash, blockNumber uint64, index uint32) receipt.ReceiptRecord {
		return receipt.ReceiptRecord{TxHash: hash, Receipt: &evmtypes.Receipt{
			TxHashHex: hash.Hex(), BlockNumber: blockNumber, TransactionIndex: index, GasUsed: 21_000, CumulativeGasUsed: 21_000,
		}}
	}
	blockHash := common.HexToHash("0x9")
	block := &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
		Block: &tmtypes.Block{
			Header: tmtypes.Header{Height: 9, Time: time.Unix(1_700_000_000, 0)},
			Data:   tmtypes.Data{Txs: tmtypes.Txs{raw1, raw2}},
		},
	}

	for _, tc := range []struct {
		name        string
		records     []receipt.ReceiptRecord
		want        []common.Hash
		wantIndexes []hexutil.Uint64
	}{
		{
			name:        "every transaction has a receipt",
			records:     []receipt.ReceiptRecord{receiptAt(tx1.Hash(), 9, 0), receiptAt(tx2.Hash(), 9, 1)},
			want:        []common.Hash{tx1.Hash(), tx2.Hash()},
			wantIndexes: []hexutil.Uint64{0, 1},
		},
		{
			name:        "replay keeps its receipt at the earlier block",
			records:     []receipt.ReceiptRecord{receiptAt(tx1.Hash(), 9, 0), receiptAt(tx2.Hash(), 3, 7)},
			want:        []common.Hash{tx1.Hash()},
			wantIndexes: []hexutil.Uint64{0},
		},
		{
			name:        "rejected transaction has no receipt",
			records:     []receipt.ReceiptRecord{receiptAt(tx2.Hash(), 9, 1)},
			want:        []common.Hash{tx2.Hash()},
			wantIndexes: []hexutil.Uint64{1},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			memory := evmonly.NewMemoryReceiptStore()
			require.NoError(t, memory.SetReceipts(sdk.Context{}.WithContext(t.Context()).WithBlockHeight(9), tc.records))
			backend := &testBackend{
				block: func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
					return block, nil
				},
				blockNumber: func() uint64 { return 9 },
			}
			for _, store := range []receipt.ReceiptStore{memory, hashOnlyReceiptStore{ReceiptStore: memory}} {
				got, err := (&blockAPI{backend: backend, store: store}).GetBlockReceipts(t.Context(), ethrpc.BlockNumberOrHashWithNumber(9))
				require.NoError(t, err)
				require.Equal(t, tc.want, receiptTxHashes(got))
				for i, r := range got {
					require.Equal(t, tc.wantIndexes[i], r["transactionIndex"])
					byHash, err := (&txAPI{backend: backend, store: store}).GetTransactionReceipt(t.Context(), tc.want[i])
					require.NoError(t, err)
					require.Equal(t, byHash, r)
				}
			}
		})
	}
}

func TestGetBlockReceiptsSurfacesStoreErrors(t *testing.T) {
	block, tx1, _, memory := multiTxBlock(t, 9, common.HexToHash("0xabcd"), time.Unix(1_700_000_000, 0))
	backend := &testBackend{
		block: func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			return block, nil
		},
		blockNumber: func() uint64 { return 9 },
	}
	iterateErr := errors.New("receipt iterator unavailable")
	readErr := errors.New("receipt read failed")

	for _, tc := range []struct {
		name    string
		store   receipt.ReceiptStore
		wantErr error
	}{
		{
			name: "iterator error",
			store: stubIteratingReceiptStore{ReceiptStore: memory, iterate: func(uint64) (receipt.ReceiptIterator, error) {
				return nil, iterateErr
			}},
			wantErr: iterateErr,
		},
		{
			name:    "by-hash fallback read error",
			store:   failingGasReceiptStore{ReceiptStore: hashOnlyReceiptStore{ReceiptStore: memory}, hash: tx1.Hash(), err: readErr},
			wantErr: readErr,
		},
		{
			name:    "receipts pruned during the read",
			store:   &floorReceiptStore{ReceiptStore: memory, floors: []int64{0, 10}},
			wantErr: errBlockReceiptsPruned,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := (&blockAPI{backend: backend, store: tc.store}).GetBlockReceipts(t.Context(), ethrpc.BlockNumberOrHashWithNumber(9))
			require.ErrorIs(t, err, tc.wantErr)
			require.Nil(t, got)
		})
	}
}

func TestGetBlockReceiptsEndToEnd(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	block, tx1, tx2, store := multiTxBlock(t, 9, blockHash, time.Unix(1_700_000_000, 0))
	backend := &testBackend{
		block: func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			return block, nil
		},
		blockByHash: func(_ context.Context, req *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
			if common.BytesToHash(req.Hash) != blockHash {
				return &coretypes.ResultBlock{}, nil
			}
			return block, nil
		},
		blockNumber: func() uint64 { return 9 },
		proxy:       utils.None[*ethrpc.Client](),
	}
	handler, err := newHandler(backend, store)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	type encodedReceipt struct {
		BlockHash        common.Hash    `json:"blockHash"`
		TransactionHash  common.Hash    `json:"transactionHash"`
		TransactionIndex hexutil.Uint64 `json:"transactionIndex"`
	}
	want := []encodedReceipt{
		{BlockHash: blockHash, TransactionHash: tx1.Hash(), TransactionIndex: 0},
		{BlockHash: blockHash, TransactionHash: tx2.Hash(), TransactionIndex: 1},
	}
	for _, arg := range []any{"latest", "0x9", blockHash} {
		var got []encodedReceipt
		require.NoError(t, client.CallContext(t.Context(), &got, "eth_getBlockReceipts", arg))
		require.Equal(t, want, got)
	}

	var missing []encodedReceipt
	require.NoError(t, client.CallContext(t.Context(), &missing, "eth_getBlockReceipts", common.Hash{9}))
	require.Nil(t, missing)

	err = client.CallContext(t.Context(), &missing, "eth_getBlockReceipts", map[string]any{})
	var rpcErr ethrpc.Error
	require.ErrorAs(t, err, &rpcErr)
	require.Equal(t, -32602, rpcErr.ErrorCode())
}
