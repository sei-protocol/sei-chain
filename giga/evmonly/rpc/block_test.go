package rpc

import (
	"context"
	"errors"
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

func TestGetBlockByNumberReturnsErrorForAPrunedHeight(t *testing.T) {
	backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return nil, coretypes.WrapErrHeightNotAvailable(1, utils.None[int64]())
	})

	got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByNumber(t.Context(), ethrpc.BlockNumber(1), false)

	require.ErrorIs(t, err, coretypes.ErrHeightNotAvailable)
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
				GasUsed:           21_000,
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
				GasUsed:           22_500,
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

func TestGetBlockByNumberPropagatesUnexpectedBackendError(t *testing.T) {
	want := errors.New("block store unavailable")
	backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return nil, want
	})

	// Test: Block fails with an error other than the zero/negative or future sentinels.
	got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByNumber(t.Context(), ethrpc.BlockNumber(3), false)

	// Verify: that error is returned as-is, not mapped to null.
	require.Nil(t, got)
	require.ErrorIs(t, err, want)
}

func TestGetBlockByNumberReturnsNullForANilBlock(t *testing.T) {
	backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return nil, nil
	})

	// Test: Block returns a nil result without error.
	got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByNumber(t.Context(), ethrpc.LatestBlockNumber, false)

	// Verify: treated as a miss.
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestGetBlockByHashPropagatesBackendError(t *testing.T) {
	want := errors.New("hash index unavailable")
	backend := &testBackend{
		blockByHash: func(context.Context, *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
			return nil, want
		},
	}

	// Test: BlockByHash fails.
	got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByHash(t.Context(), common.Hash{9}, false)

	// Verify: that error is returned as-is.
	require.Nil(t, got)
	require.ErrorIs(t, err, want)
}

func TestEncodeBlockRejectsNegativeTime(t *testing.T) {
	backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return &coretypes.ResultBlock{
			Block: &tmtypes.Block{Header: tmtypes.Header{Height: 4, Time: time.Unix(-1, 0)}},
		}, nil
	})

	// Test: block timestamp cannot be a uint64.
	got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByNumber(t.Context(), ethrpc.LatestBlockNumber, false)

	// Verify: negative time is rejected.
	require.Nil(t, got)
	require.ErrorContains(t, err, "time is negative")
}

func TestEncodeBlockSurfacesGasLimitAndBaseFeeErrors(t *testing.T) {
	block := func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return &coretypes.ResultBlock{
			BlockID: tmtypes.BlockID{Hash: common.HexToHash("0xabcd").Bytes()},
			Block:   &tmtypes.Block{Header: tmtypes.Header{Height: 4, Time: time.Unix(1_700_000_000, 0)}},
		}, nil
	}

	t.Run("gas limit", func(t *testing.T) {
		want := errors.New("no gas limit")
		backend := fixedGasLimitBackend(t, 35_000_000, block)
		backend.gasLimit = func() (uint64, error) { return 0, want }

		// Test: EvmGasLimit fails.
		got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByNumber(t.Context(), ethrpc.LatestBlockNumber, false)

		// Verify: that error is returned as-is.
		require.Nil(t, got)
		require.ErrorIs(t, err, want)
	})

	t.Run("base fee", func(t *testing.T) {
		want := errors.New("no base fee")
		backend := fixedGasLimitBackend(t, 35_000_000, block)
		backend.baseFee = func() (*big.Int, error) { return nil, want }

		// Test: EvmBaseFee fails after a valid gas limit.
		got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByNumber(t.Context(), ethrpc.LatestBlockNumber, false)

		// Verify: that error is returned as-is.
		require.Nil(t, got)
		require.ErrorIs(t, err, want)
	})
}

func TestEncodeBlockFullTxChainConfigError(t *testing.T) {
	want := errors.New("no chain config")
	backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return &coretypes.ResultBlock{
			Block: &tmtypes.Block{
				Header: tmtypes.Header{Height: 4, Time: time.Unix(1_700_000_000, 0)},
				Data:   tmtypes.Data{Txs: tmtypes.Txs{[]byte("tx")}},
			},
		}, nil
	})
	backend.chainConfig = func() (*params.ChainConfig, error) { return nil, want }

	// Test: full-tx encoding needs a chain config.
	got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByNumber(t.Context(), ethrpc.LatestBlockNumber, true)

	// Verify: that error is returned as-is.
	require.Nil(t, got)
	require.ErrorIs(t, err, want)
}

func TestEncodeBlockRejectsUndecodableTransactions(t *testing.T) {
	garbage := []byte("not-an-rlp-tx")
	backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return &coretypes.ResultBlock{
			Block: &tmtypes.Block{
				Header: tmtypes.Header{Height: 4, Time: time.Unix(1_700_000_000, 0)},
				Data:   tmtypes.Data{Txs: tmtypes.Txs{garbage}},
			},
		}, nil
	})
	api := &blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}

	t.Run("hash only", func(t *testing.T) {
		// Test: hash-only encoding still has to decode each tx to hash it.
		got, err := api.GetBlockByNumber(t.Context(), ethrpc.LatestBlockNumber, false)

		// Verify: decode error names the block and index.
		require.Nil(t, got)
		require.ErrorContains(t, err, "decode transaction at block 4 index 0")
	})

	t.Run("full tx", func(t *testing.T) {
		// Test: full-tx encoding hits the same bad bytes.
		got, err := api.GetBlockByNumber(t.Context(), ethrpc.LatestBlockNumber, true)

		// Verify: same decode error.
		require.Nil(t, got)
		require.ErrorContains(t, err, "decode transaction at block 4 index 0")
	})
}

func TestEncodeBlockReceiptReadError(t *testing.T) {
	_, raw := testSignedTransaction(t)
	want := errors.New("receipt db closed")
	memory := evmonly.NewMemoryReceiptStore()
	require.NoError(t, memory.SetLatestVersion(4))
	store := stubReceiptStore{
		ReceiptStore: hashOnlyReceiptStore{ReceiptStore: memory},
		get: func(sdk.Context, common.Hash) (*evmtypes.Receipt, error) {
			return nil, want
		},
	}
	backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return &coretypes.ResultBlock{
			Block: &tmtypes.Block{
				Header: tmtypes.Header{Height: 4, Time: time.Unix(1_700_000_000, 0)},
				Data:   tmtypes.Data{Txs: tmtypes.Txs{raw}},
			},
		}, nil
	})
	api := &blockAPI{backend: backend, store: store}

	t.Run("hash only", func(t *testing.T) {
		// Test: hash-only encoding recomputes gasUsed from the block's receipts.
		got, err := api.GetBlockByNumber(t.Context(), ethrpc.LatestBlockNumber, false)

		// Verify: wrapped receipt error.
		require.Nil(t, got)
		require.ErrorIs(t, err, want)
		require.ErrorContains(t, err, "for block 4")
	})

	t.Run("full tx", func(t *testing.T) {
		// Test: full-tx encoding reads a receipt per transaction.
		got, err := api.GetBlockByNumber(t.Context(), ethrpc.LatestBlockNumber, true)

		// Verify: wrapped receipt error names the index.
		require.Nil(t, got)
		require.ErrorIs(t, err, want)
		require.ErrorContains(t, err, "read transaction receipt at block 4 index 0")
	})
}

func TestEncodeBlockMissingReceiptLeavesGasUsedZero(t *testing.T) {
	_, raw := testSignedTransaction(t)
	backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return &coretypes.ResultBlock{
			Block: &tmtypes.Block{
				Header: tmtypes.Header{Height: 4, Time: time.Unix(1_700_000_000, 0)},
				Data:   tmtypes.Data{Txs: tmtypes.Txs{raw}},
			},
		}, nil
	})

	// Test: the last tx has no stored receipt.
	got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockByNumber(t.Context(), ethrpc.LatestBlockNumber, false)

	// Verify: block is still returned, with gasUsed left at zero.
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, hexutil.Uint64(0), got["gasUsed"])
}

func TestGetBlockTransactionCountByNumber(t *testing.T) {
	twoTxBlock, _, _, _ := multiTxBlock(t, 9, common.HexToHash("0xabcd"), time.Unix(1_700_000_000, 0))
	emptyBlock := &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{Hash: common.HexToHash("0xabcd").Bytes()},
		Block:   &tmtypes.Block{Header: tmtypes.Header{Height: 4, Time: time.Unix(1_700_000_000, 0)}},
	}
	backendErr := errors.New("block store unavailable")
	heightOf := func(h int64) *coretypes.Int64 {
		height := coretypes.Int64(h)
		return &height
	}
	countOf := func(n uint) *hexutil.Uint {
		count := hexutil.Uint(n)
		return &count
	}

	for _, tc := range []struct {
		name       string
		number     ethrpc.BlockNumber
		wantHeight *coretypes.Int64
		block      *coretypes.ResultBlock
		blockErr   error
		want       *hexutil.Uint
		wantErr    error
	}{
		// Current-state tags all resolve to the committed block, requested with a nil height.
		{name: "latest", number: ethrpc.LatestBlockNumber, block: twoTxBlock, want: countOf(2)},
		{name: "safe", number: ethrpc.SafeBlockNumber, block: twoTxBlock, want: countOf(2)},
		{name: "finalized", number: ethrpc.FinalizedBlockNumber, block: twoTxBlock, want: countOf(2)},
		{name: "pending", number: ethrpc.PendingBlockNumber, block: twoTxBlock, want: countOf(2)},
		{name: "explicit historical height", number: ethrpc.BlockNumber(3), wantHeight: heightOf(3), block: twoTxBlock, want: countOf(2)},
		{name: "empty block is zero, not null", number: ethrpc.LatestBlockNumber, block: emptyBlock, want: countOf(0)},
		// Heights with no block answer null, not an error.
		{name: "future height", number: ethrpc.BlockNumber(100), wantHeight: heightOf(100), blockErr: fmt.Errorf("%w: 100", coretypes.ErrHeightExceedsChainHead)},
		{name: "earliest", number: ethrpc.EarliestBlockNumber, wantHeight: heightOf(0), blockErr: fmt.Errorf("%w: 0", coretypes.ErrZeroOrNegativeHeight)},
		{name: "nil block", number: ethrpc.LatestBlockNumber},
		// A pruned height and any other backend failure are errors.
		{name: "pruned height", number: ethrpc.BlockNumber(1), wantHeight: heightOf(1), blockErr: coretypes.WrapErrHeightNotAvailable(1, utils.None[int64]()), wantErr: coretypes.ErrHeightNotAvailable},
		{name: "unexpected backend error", number: ethrpc.BlockNumber(3), wantHeight: heightOf(3), blockErr: backendErr, wantErr: backendErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &testBackend{
				block: func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
					require.Equal(t, tc.wantHeight, req.Height)
					return tc.block, tc.blockErr
				},
			}

			got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockTransactionCountByNumber(t.Context(), tc.number)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.want, got)
		})
	}
}

func TestGetBlockTransactionCountByHash(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	twoTxBlock, _, _, _ := multiTxBlock(t, 5, blockHash, time.Unix(1_700_000_000, 0))
	backendErr := errors.New("hash index unavailable")
	two := hexutil.Uint(2)

	for _, tc := range []struct {
		name     string
		block    *coretypes.ResultBlock
		blockErr error
		want     *hexutil.Uint
		wantErr  error
	}{
		{name: "known hash", block: twoTxBlock, want: &two},
		// An unknown hash answers null, whether the backend returns an empty or a nil result.
		{name: "unknown hash, empty result", block: &coretypes.ResultBlock{}},
		{name: "unknown hash, nil result"},
		{name: "backend error", blockErr: backendErr, wantErr: backendErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := &testBackend{
				blockByHash: func(_ context.Context, req *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
					require.Equal(t, blockHash.Bytes(), []byte(req.Hash))
					return tc.block, tc.blockErr
				},
			}

			got, err := (&blockAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetBlockTransactionCountByHash(t.Context(), blockHash)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.want, got)
		})
	}
}

// TestGetBlockTransactionCountMatchesGetBlockTransactions pins that both count
// methods equal the length of eth_getBlockByNumber's transactions list, which
// covers every transaction in the block body, including one whose receipt is
// not stored in this block.
func TestGetBlockTransactionCountMatchesGetBlockTransactions(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	twoTxBlock, _, _, twoTxStore := multiTxBlock(t, 9, blockHash, time.Unix(1_700_000_000, 0))
	_, raw := testSignedTransaction(t)
	noReceiptBlock := &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
		Block: &tmtypes.Block{
			Header: tmtypes.Header{Height: 4, Time: time.Unix(1_700_000_000, 0)},
			Data:   tmtypes.Data{Txs: tmtypes.Txs{raw}},
		},
	}

	for _, tc := range []struct {
		name  string
		block *coretypes.ResultBlock
		store receipt.ReceiptStore
		want  hexutil.Uint
	}{
		{name: "transactions with receipts", block: twoTxBlock, store: twoTxStore, want: 2},
		{name: "transaction without a receipt", block: noReceiptBlock, store: evmonly.NewMemoryReceiptStore(), want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
				return tc.block, nil
			})
			backend.blockByHash = func(context.Context, *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
				return tc.block, nil
			}
			api := &blockAPI{backend: backend, store: tc.store}

			byNumber, err := api.GetBlockTransactionCountByNumber(t.Context(), ethrpc.LatestBlockNumber)
			require.NoError(t, err)
			byHash, err := api.GetBlockTransactionCountByHash(t.Context(), blockHash)
			require.NoError(t, err)
			encoded, err := api.GetBlockByNumber(t.Context(), ethrpc.LatestBlockNumber, false)
			require.NoError(t, err)

			require.NotNil(t, byNumber)
			require.NotNil(t, byHash)
			require.Equal(t, tc.want, *byNumber)
			require.Equal(t, tc.want, *byHash)
			require.Len(t, encoded["transactions"], int(tc.want))
		})
	}
}

func TestGetBlockTransactionCountEndToEnd(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	block, _, _, store := multiTxBlock(t, 9, blockHash, time.Unix(1_700_000_000, 0))
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
		proxy: utils.None[*ethrpc.Client](),
	}
	handler, err := newHandler(backend, store)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var byNumber string
	require.NoError(t, client.CallContext(t.Context(), &byNumber, "eth_getBlockTransactionCountByNumber", "latest"))
	require.Equal(t, "0x2", byNumber)

	var byHash string
	require.NoError(t, client.CallContext(t.Context(), &byHash, "eth_getBlockTransactionCountByHash", blockHash))
	require.Equal(t, "0x2", byHash)

	var missing *hexutil.Uint
	require.NoError(t, client.CallContext(t.Context(), &missing, "eth_getBlockTransactionCountByHash", common.Hash{9}))
	require.Nil(t, missing)
}

func TestGetTransactionByBlockNumberAndIndex(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	twoTxBlock, tx1, tx2, store := multiTxBlock(t, 9, blockHash, time.Unix(1_700_000_000, 0))
	emptyBlock := &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
		Block:   &tmtypes.Block{Header: tmtypes.Header{Height: 4, Time: time.Unix(1_700_000_000, 0)}},
	}
	backendErr := errors.New("block store unavailable")
	heightOf := func(h int64) *coretypes.Int64 {
		height := coretypes.Int64(h)
		return &height
	}

	for _, tc := range []struct {
		name       string
		number     ethrpc.BlockNumber
		index      hexutil.Uint
		wantHeight *coretypes.Int64
		block      *coretypes.ResultBlock
		blockErr   error
		want       *ethtypes.Transaction
		wantErr    error
	}{
		// Current-state tags all resolve to the committed block, requested with a nil height.
		{name: "latest", number: ethrpc.LatestBlockNumber, index: 0, block: twoTxBlock, want: tx1},
		{name: "safe", number: ethrpc.SafeBlockNumber, index: 1, block: twoTxBlock, want: tx2},
		{name: "finalized", number: ethrpc.FinalizedBlockNumber, index: 0, block: twoTxBlock, want: tx1},
		{name: "pending", number: ethrpc.PendingBlockNumber, index: 1, block: twoTxBlock, want: tx2},
		{name: "explicit historical height", number: ethrpc.BlockNumber(9), index: 1, wantHeight: heightOf(9), block: twoTxBlock, want: tx2},
		// An index with no transaction answers null.
		{name: "index past the end", number: ethrpc.LatestBlockNumber, index: 2, block: twoTxBlock},
		{name: "index beyond int range", number: ethrpc.LatestBlockNumber, index: hexutil.Uint(^uint(0)), block: twoTxBlock},
		{name: "empty block", number: ethrpc.LatestBlockNumber, index: 0, block: emptyBlock},
		// Heights with no block answer null, not an error.
		{name: "future height", number: ethrpc.BlockNumber(100), wantHeight: heightOf(100), blockErr: fmt.Errorf("%w: 100", coretypes.ErrHeightExceedsChainHead)},
		{name: "earliest", number: ethrpc.EarliestBlockNumber, wantHeight: heightOf(0), blockErr: fmt.Errorf("%w: 0", coretypes.ErrZeroOrNegativeHeight)},
		{name: "nil block", number: ethrpc.LatestBlockNumber},
		// A pruned height and any other backend failure are errors.
		{name: "pruned height", number: ethrpc.BlockNumber(1), wantHeight: heightOf(1), blockErr: coretypes.WrapErrHeightNotAvailable(1, utils.None[int64]()), wantErr: coretypes.ErrHeightNotAvailable},
		{name: "unexpected backend error", number: ethrpc.BlockNumber(3), wantHeight: heightOf(3), blockErr: backendErr, wantErr: backendErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := fixedGasLimitBackend(t, 35_000_000, func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
				require.Equal(t, tc.wantHeight, req.Height)
				return tc.block, tc.blockErr
			})

			got, err := (&blockAPI{backend: backend, store: store}).GetTransactionByBlockNumberAndIndex(t.Context(), tc.number, tc.index)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			if tc.want == nil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tc.want.Hash(), got.Hash)
			require.Equal(t, hexutil.Uint64(tc.index), *got.TransactionIndex)
			require.Equal(t, blockHash, *got.BlockHash)
			require.Equal(t, (*hexutil.Big)(big.NewInt(9)), got.BlockNumber)
		})
	}
}

func TestGetTransactionByBlockHashAndIndex(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	twoTxBlock, tx1, tx2, store := multiTxBlock(t, 5, blockHash, time.Unix(1_700_000_000, 0))
	backendErr := errors.New("hash index unavailable")

	for _, tc := range []struct {
		name     string
		index    hexutil.Uint
		block    *coretypes.ResultBlock
		blockErr error
		want     *ethtypes.Transaction
		wantErr  error
	}{
		{name: "first transaction", index: 0, block: twoTxBlock, want: tx1},
		{name: "second transaction", index: 1, block: twoTxBlock, want: tx2},
		{name: "index past the end", index: 2, block: twoTxBlock},
		// An unknown hash answers null, whether the backend returns an empty or a nil result.
		{name: "unknown hash, empty result", block: &coretypes.ResultBlock{}},
		{name: "unknown hash, nil result"},
		{name: "backend error", blockErr: backendErr, wantErr: backendErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := fixedGasLimitBackend(t, 35_000_000, nil)
			backend.blockByHash = func(_ context.Context, req *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
				require.Equal(t, blockHash.Bytes(), []byte(req.Hash))
				return tc.block, tc.blockErr
			}

			got, err := (&blockAPI{backend: backend, store: store}).GetTransactionByBlockHashAndIndex(t.Context(), blockHash, tc.index)

			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			if tc.want == nil {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tc.want.Hash(), got.Hash)
			require.Equal(t, hexutil.Uint64(tc.index), *got.TransactionIndex)
			require.Equal(t, blockHash, *got.BlockHash)
			require.Equal(t, (*hexutil.Big)(big.NewInt(5)), got.BlockNumber)
		})
	}
}

// TestGetTransactionByBlockAndIndexMatchesGetBlockFullTransactions pins that
// both index lookups return exactly the entry eth_getBlockByNumber with full
// transactions lists at that index, including for a replayed transaction whose
// stored receipt belongs to an earlier block and for one with no stored receipt.
func TestGetTransactionByBlockAndIndexMatchesGetBlockFullTransactions(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	blockTime := time.Unix(1_700_000_000, 0)
	twoTxBlock, _, _, twoTxStore := multiTxBlock(t, 9, blockHash, blockTime)
	tx, raw := testSignedTransaction(t)
	sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(big.NewInt(713715)), tx)
	require.NoError(t, err)
	oneTxBlock := &coretypes.ResultBlock{
		BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
		Block: &tmtypes.Block{
			Header: tmtypes.Header{Height: 9, Time: blockTime},
			Data:   tmtypes.Data{Txs: tmtypes.Txs{raw}},
		},
	}
	replayStore := evmonly.NewMemoryReceiptStore()
	require.NoError(t, replayStore.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash:  tx.Hash(),
		Receipt: &evmtypes.Receipt{TxHashHex: tx.Hash().Hex(), BlockNumber: 3, TransactionIndex: 7, From: sender.Hex()},
	}}))

	for _, tc := range []struct {
		name  string
		block *coretypes.ResultBlock
		store receipt.ReceiptStore
	}{
		{name: "transactions with receipts", block: twoTxBlock, store: twoTxStore},
		{name: "replayed transaction with an earlier block's receipt", block: oneTxBlock, store: replayStore},
		{name: "transaction without a receipt", block: oneTxBlock, store: evmonly.NewMemoryReceiptStore()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
				return tc.block, nil
			})
			backend.blockByHash = func(context.Context, *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
				return tc.block, nil
			}
			api := &blockAPI{backend: backend, store: tc.store}
			encoded, err := api.GetBlockByNumber(t.Context(), ethrpc.LatestBlockNumber, true)
			require.NoError(t, err)
			fullTxs, ok := encoded["transactions"].([]any)
			require.True(t, ok)
			require.Len(t, fullTxs, len(tc.block.Block.Txs))

			for i, want := range fullTxs {
				byNumber, err := api.GetTransactionByBlockNumberAndIndex(t.Context(), ethrpc.LatestBlockNumber, hexutil.Uint(i))
				require.NoError(t, err)
				byHash, err := api.GetTransactionByBlockHashAndIndex(t.Context(), blockHash, hexutil.Uint(i))
				require.NoError(t, err)

				require.Equal(t, want, byNumber)
				require.Equal(t, want, byHash)
				// The block's own position wins over a stored receipt from another block.
				require.Equal(t, (*hexutil.Big)(big.NewInt(9)), byNumber.BlockNumber)
				require.Equal(t, hexutil.Uint64(i), *byNumber.TransactionIndex)
			}
		})
	}
}

func TestGetTransactionByBlockAndIndexSurfacesRenderErrors(t *testing.T) {
	_, raw := testSignedTransaction(t)
	blockWith := func(txs tmtypes.Txs, blockTime time.Time) *coretypes.ResultBlock {
		return &coretypes.ResultBlock{
			BlockID: tmtypes.BlockID{Hash: common.HexToHash("0xabcd").Bytes()},
			Block: &tmtypes.Block{
				Header: tmtypes.Header{Height: 4, Time: blockTime},
				Data:   tmtypes.Data{Txs: txs},
			},
		}
	}
	validTime := time.Unix(1_700_000_000, 0)
	baseFeeErr := errors.New("no base fee")
	chainConfigErr := errors.New("no chain config")
	receiptErr := errors.New("receipt db closed")
	failingReceipts := stubReceiptStore{
		ReceiptStore: evmonly.NewMemoryReceiptStore(),
		get: func(sdk.Context, common.Hash) (*evmtypes.Receipt, error) {
			return nil, receiptErr
		},
	}

	for _, tc := range []struct {
		name            string
		block           *coretypes.ResultBlock
		configure       func(*testBackend)
		store           receipt.ReceiptStore
		wantErr         error
		wantErrContains string
	}{
		{name: "negative block time", block: blockWith(tmtypes.Txs{raw}, time.Unix(-1, 0)), wantErrContains: "block 4 time is negative"},
		{
			name:      "base fee error",
			block:     blockWith(tmtypes.Txs{raw}, validTime),
			configure: func(b *testBackend) { b.baseFee = func() (*big.Int, error) { return nil, baseFeeErr } },
			wantErr:   baseFeeErr,
		},
		{
			name:  "chain config error",
			block: blockWith(tmtypes.Txs{raw}, validTime),
			configure: func(b *testBackend) {
				b.chainConfig = func() (*params.ChainConfig, error) { return nil, chainConfigErr }
			},
			wantErr: chainConfigErr,
		},
		{name: "undecodable transaction", block: blockWith(tmtypes.Txs{[]byte("not-an-rlp-tx")}, validTime), wantErrContains: "decode transaction at block 4 index 0"},
		{name: "receipt read error", block: blockWith(tmtypes.Txs{raw}, validTime), store: failingReceipts, wantErr: receiptErr, wantErrContains: "read transaction receipt at block 4 index 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
				return tc.block, nil
			})
			if tc.configure != nil {
				tc.configure(backend)
			}
			store := tc.store
			if store == nil {
				store = evmonly.NewMemoryReceiptStore()
			}

			got, err := (&blockAPI{backend: backend, store: store}).GetTransactionByBlockNumberAndIndex(t.Context(), ethrpc.LatestBlockNumber, 0)

			require.Nil(t, got)
			require.Error(t, err)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			}
			if tc.wantErrContains != "" {
				require.ErrorContains(t, err, tc.wantErrContains)
			}
		})
	}
}

func TestGetTransactionByBlockAndIndexEndToEnd(t *testing.T) {
	blockHash := common.HexToHash("0xabcd")
	block, tx1, tx2, store := multiTxBlock(t, 9, blockHash, time.Unix(1_700_000_000, 0))
	backend := fixedGasLimitBackend(t, 35_000_000, func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return block, nil
	})
	backend.blockByHash = func(_ context.Context, req *coretypes.RequestBlockByHash) (*coretypes.ResultBlock, error) {
		if common.BytesToHash(req.Hash) != blockHash {
			return &coretypes.ResultBlock{}, nil
		}
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
	require.NoError(t, client.CallContext(t.Context(), &byNumber, "eth_getTransactionByBlockNumberAndIndex", "latest", "0x1"))
	require.Equal(t, tx2.Hash().Hex(), byNumber["hash"])
	require.Equal(t, "0x1", byNumber["transactionIndex"])
	require.Equal(t, "0x9", byNumber["blockNumber"])
	require.Equal(t, blockHash.Hex(), byNumber["blockHash"])

	var byHash map[string]any
	require.NoError(t, client.CallContext(t.Context(), &byHash, "eth_getTransactionByBlockHashAndIndex", blockHash, "0x0"))
	require.Equal(t, tx1.Hash().Hex(), byHash["hash"])
	require.Equal(t, "0x0", byHash["transactionIndex"])

	var pastEnd map[string]any
	require.NoError(t, client.CallContext(t.Context(), &pastEnd, "eth_getTransactionByBlockNumberAndIndex", "latest", "0x2"))
	require.Nil(t, pastEnd)

	var unknownHash map[string]any
	require.NoError(t, client.CallContext(t.Context(), &unknownHash, "eth_getTransactionByBlockHashAndIndex", common.Hash{9}, "0x0"))
	require.Nil(t, unknownHash)
}
