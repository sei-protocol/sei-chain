package rpc

import (
	"context"
	"fmt"
	"math/big"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/export"
	"github.com/ethereum/go-ethereum/params"
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

// secondSignedTransaction returns a transaction distinct from
// testSignedTransaction's: a different sender and a legacy tx with a
// different nonce/recipient, so multi-tx block tests can tell the two apart.
func secondSignedTransaction(t *testing.T) (*ethtypes.Transaction, []byte) {
	t.Helper()
	key, err := crypto.HexToECDSA("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	require.NoError(t, err)
	to := common.HexToAddress("0x3000000000000000000000000000000000000003")
	tx := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    2,
		GasPrice: big.NewInt(1_000_000_000),
		Gas:      21_000,
		To:       &to,
		Value:    big.NewInt(2),
	})
	tx, err = ethtypes.SignTx(tx, ethtypes.LatestSignerForChainID(big.NewInt(713715)), key)
	require.NoError(t, err)
	raw, err := tx.MarshalBinary()
	require.NoError(t, err)
	return tx, raw
}

// fixedGasLimitBackend returns a testBackend with a fixed EvmGasLimit and
// EvmChainConfig, wired with block as its Block lookup.
func fixedGasLimitBackend(t *testing.T, gasLimit uint64, block func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error)) *testBackend {
	t.Helper()
	return &testBackend{
		block:       block,
		gasLimit:    func() (uint64, error) { return gasLimit, nil },
		chainConfig: func() (*params.ChainConfig, error) { return testChainConfig(big.NewInt(713715)), nil },
		baseFee:     func() (*big.Int, error) { return new(big.Int), nil },
	}
}

func TestGetBlockByNumberCurrentStateTagsPassNilHeight(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	blockTime := time.Unix(1_700_000_000, 0)
	for _, tag := range []ethrpc.BlockNumber{
		ethrpc.LatestBlockNumber, ethrpc.SafeBlockNumber, ethrpc.FinalizedBlockNumber, ethrpc.PendingBlockNumber,
	} {
		backend := fixedGasLimitBackend(t, 35_000_000, func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			require.Nil(t, req.Height)
			return &coretypes.ResultBlock{
				BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
				Block:   &tmtypes.Block{Header: tmtypes.Header{Height: 9, Time: blockTime}},
			}, nil
		})
		got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByNumber(t.Context(), tag, false)
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, (*hexutil.Big)(big.NewInt(9)), got["number"])
	}
}

func TestGetBlockByNumberAcceptsAnExplicitHistoricalHeight(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	backend := fixedGasLimitBackend(t, 35_000_000, func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		require.NotNil(t, req.Height)
		require.Equal(t, coretypes.Int64(3), *req.Height)
		return &coretypes.ResultBlock{
			BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
			Block:   &tmtypes.Block{Header: tmtypes.Header{Height: 3, Time: time.Unix(1_700_000_000, 0)}},
		}, nil
	})

	got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByNumber(t.Context(), ethrpc.BlockNumber(3), false)

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, (*hexutil.Big)(big.NewInt(3)), got["number"])
}

func TestGetBlockByNumberReturnsNullForAFutureHeight(t *testing.T) {
	backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return nil, fmt.Errorf("%w: 100", coretypes.ErrHeightExceedsChainHead)
	})

	got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByNumber(t.Context(), ethrpc.BlockNumber(100), false)

	require.NoError(t, err)
	require.Nil(t, got)
}

func TestGetBlockByNumberEarliestReturnsNullBeforeAnyCommittedBlock(t *testing.T) {
	backend := fixedGasLimitBackend(t, 35_000_000, func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		require.NotNil(t, req.Height)
		require.Equal(t, coretypes.Int64(0), *req.Height)
		return nil, fmt.Errorf("%w: 0", coretypes.ErrZeroOrNegativeHeight)
	})

	got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByNumber(t.Context(), ethrpc.EarliestBlockNumber, false)

	require.NoError(t, err)
	require.Nil(t, got)
}

func TestGetBlockByNumberReturnsNullForAPrunedHeight(t *testing.T) {
	backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return nil, coretypes.WrapErrHeightNotAvailable(1, utils.None[int64]())
	})

	got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByNumber(t.Context(), ethrpc.BlockNumber(1), false)

	require.NoError(t, err)
	require.Nil(t, got)
}

func TestGetBlockByHashReturnsNullForUnknownHash(t *testing.T) {
	backend := &testBackend{
		blockByHash: func(context.Context, *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
			return &coretypes.ResultBlock{}, nil
		},
	}

	got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByHash(t.Context(), common.Hash{9}, false)

	require.NoError(t, err)
	require.Nil(t, got)
}

func TestGetBlockByHashReturnsTheMatchingBlock(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	var gotHash []byte
	backend := fixedGasLimitBackend(t, 35_000_000, nil)
	backend.blockByHash = func(_ context.Context, req *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
		gotHash = req.Hash
		return &coretypes.ResultBlock{
			BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
			Block:   &tmtypes.Block{Header: tmtypes.Header{Height: 5, Time: time.Unix(1_700_000_000, 0)}},
		}, nil
	}

	got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByHash(t.Context(), blockHash, false)

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, blockHash.Bytes(), gotHash)
	require.Equal(t, blockHash, got["hash"])
	require.Equal(t, (*hexutil.Big)(big.NewInt(5)), got["number"])
}

func TestEncodeBlockEmptyBlockHasZeroGasUsedAndNoTransactions(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return &coretypes.ResultBlock{
			BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
			Block:   &tmtypes.Block{Header: tmtypes.Header{Height: 4, Time: time.Unix(1_700_000_000, 0)}},
		}, nil
	})

	got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByNumber(t.Context(), ethrpc.LatestBlockNumber, false)

	require.NoError(t, err)
	require.Equal(t, hexutil.Uint64(0), got["gasUsed"])
	require.Equal(t, []any{}, got["transactions"])
	require.Equal(t, hexutil.Uint64(35_000_000), got["gasLimit"])
}

// multiTxBlock builds a two-transaction ResultBlock and the matching receipt
// store entries, returning the block, its two decoded transactions in order,
// and the store.
func multiTxBlock(t *testing.T, height int64, blockHash common.Hash, blockTime time.Time) (*coretypes.ResultBlock, *ethtypes.Transaction, *ethtypes.Transaction, receipt.ReceiptStore) {
	t.Helper()
	tx1, raw1 := testSignedTransaction(t)
	sender1, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(big.NewInt(713715)), tx1)
	require.NoError(t, err)
	tx2, raw2 := secondSignedTransaction(t)
	sender2, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(big.NewInt(713715)), tx2)
	require.NoError(t, err)

	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{
		{
			TxHash: tx1.Hash(),
			Receipt: &evmtypes.Receipt{
				TxHashHex:         tx1.Hash().Hex(),
				BlockNumber:       uint64(height), //nolint:gosec // G115: test height is positive.
				TransactionIndex:  0,
				From:              sender1.Hex(),
				CumulativeGasUsed: 21_000,
			},
		},
		{
			TxHash: tx2.Hash(),
			Receipt: &evmtypes.Receipt{
				TxHashHex:         tx2.Hash().Hex(),
				BlockNumber:       uint64(height), //nolint:gosec // G115: test height is positive.
				TransactionIndex:  1,
				From:              sender2.Hex(),
				CumulativeGasUsed: 43_500,
			},
		},
	}))

	block := &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
		Block: &tmtypes.Block{
			Header: tmtypes.Header{Height: height, Time: blockTime},
			Data:   tmtypes.Data{Txs: tmtypes.Txs{raw1, raw2}},
		},
	}
	return block, tx1, tx2, store
}

func TestEncodeBlockHashOnlyListMatchesGetTransactionByHash(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	blockTime := time.Unix(1_700_000_000, 0)
	block, tx1, tx2, store := multiTxBlock(t, 9, blockHash, blockTime)
	backend := fixedGasLimitBackend(t, 35_000_000, func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		require.NotNil(t, req.Height)
		require.Equal(t, coretypes.Int64(9), *req.Height)
		return block, nil
	})

	got, err := (&blockAPI{backend: backend, store: store}).GetBlockByNumber(t.Context(), ethrpc.BlockNumber(9), false)
	require.NoError(t, err)
	gotTxs, ok := got["transactions"].([]any)
	require.True(t, ok)
	require.Equal(t, []any{tx1.Hash(), tx2.Hash()}, gotTxs)
	// The last transaction's receipt already carries the block's total gas used.
	require.Equal(t, hexutil.Uint64(43_500), got["gasUsed"])

	txAPI := &txAPI{backend: backend, store: store}
	byHash1, err := txAPI.GetTransactionByHash(t.Context(), tx1.Hash())
	require.NoError(t, err)
	require.Equal(t, tx1.Hash(), byHash1.Hash)
	byHash2, err := txAPI.GetTransactionByHash(t.Context(), tx2.Hash())
	require.NoError(t, err)
	require.Equal(t, tx2.Hash(), byHash2.Hash)
}

func TestEncodeBlockFullTxIncludesDecodedTransactionsInOrder(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	blockTime := time.Unix(1_700_000_000, 0)
	block, tx1, tx2, store := multiTxBlock(t, 9, blockHash, blockTime)
	backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return block, nil
	})

	got, err := (&blockAPI{backend: backend, store: store}).GetBlockByNumber(t.Context(), ethrpc.LatestBlockNumber, true)

	require.NoError(t, err)
	gotTxs, ok := got["transactions"].([]any)
	require.True(t, ok)
	require.Len(t, gotTxs, 2)
	first, ok := gotTxs[0].(*export.RPCTransaction)
	require.True(t, ok)
	require.Equal(t, tx1.Hash(), first.Hash)
	require.Equal(t, hexutil.Uint64(0), *first.TransactionIndex)
	second, ok := gotTxs[1].(*export.RPCTransaction)
	require.True(t, ok)
	require.Equal(t, tx2.Hash(), second.Hash)
	require.Equal(t, hexutil.Uint64(1), *second.TransactionIndex)
	require.Equal(t, hexutil.Uint64(43_500), got["gasUsed"])
	require.Equal(t, (*hexutil.Big)(big.NewInt(0)), got["totalDifficulty"])
}

// TestEncodeBlockDocumentedHeaderGaps pins the header fields
// gigaRouterCommon.translateGlobalBlock never populates, confirmed by reading
// that translation rather than assumed.
func TestEncodeBlockDocumentedHeaderGaps(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return &coretypes.ResultBlock{
			BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
			Block: &tmtypes.Block{
				Header:     tmtypes.Header{ChainID: "evmonly-test", Height: 4, Time: time.Unix(1_700_000_000, 0)},
				LastCommit: &tmtypes.Commit{},
			},
		}, nil
	})

	got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByNumber(t.Context(), ethrpc.LatestBlockNumber, false)

	require.NoError(t, err)
	require.Equal(t, common.Hash{}, got["parentHash"])
	require.Equal(t, common.Hash{}, got["stateRoot"])
	require.Equal(t, common.Hash{}, got["transactionsRoot"])
	require.Equal(t, common.Hash{}, got["receiptsRoot"])
	require.Equal(t, common.Address{}, got["miner"])
	require.Equal(t, ethtypes.Bloom{}, got["logsBloom"])
}

func TestGetBlockByNumberEndToEnd(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	blockTime := time.Unix(1_700_000_000, 0)
	block, tx1, _, store := multiTxBlock(t, 9, blockHash, blockTime)
	backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return block, nil
	})
	backend.blockByHash = func(context.Context, *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
		return block, nil
	}
	backend.proxy = utils.None[*ethrpc.Client]()
	handler, err := newHandler(backend, store)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var byNumber map[string]any
	require.NoError(t, client.CallContext(t.Context(), &byNumber, "eth_getBlockByNumber", "latest", false))
	require.Equal(t, "0x9", byNumber["number"])
	txHashes, ok := byNumber["transactions"].([]any)
	require.True(t, ok)
	require.Equal(t, tx1.Hash().Hex(), txHashes[0])

	var byHash map[string]any
	require.NoError(t, client.CallContext(t.Context(), &byHash, "eth_getBlockByHash", blockHash, false))
	require.Equal(t, "0x9", byHash["number"])

	var missing map[string]any
	backend.blockByHash = func(context.Context, *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
		return &coretypes.ResultBlock{}, nil
	}
	require.NoError(t, client.CallContext(t.Context(), &missing, "eth_getBlockByHash", common.Hash{9}, false))
	require.Nil(t, missing)
}
