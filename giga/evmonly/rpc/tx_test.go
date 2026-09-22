package rpc

import (
	"context"
	"errors"
	"fmt"
	"math"
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

func TestGetTransactionCountCurrentState(t *testing.T) {
	address := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend := &testBackend{transactionCount: func(common.Address) uint64 { return 7 }}
	api := &txAPI{backend: backend}

	for _, tag := range []ethrpc.BlockNumberOrHash{
		ethrpc.BlockNumberOrHashWithNumber(ethrpc.LatestBlockNumber),
		ethrpc.BlockNumberOrHashWithNumber(ethrpc.SafeBlockNumber),
		ethrpc.BlockNumberOrHashWithNumber(ethrpc.FinalizedBlockNumber),
		ethrpc.BlockNumberOrHashWithNumber(ethrpc.PendingBlockNumber),
	} {
		got, err := api.GetTransactionCount(t.Context(), address, tag)
		require.NoError(t, err)
		require.Equal(t, hexutil.Uint64(7), *got)
	}
}

func TestGetTransactionCountRejectsHistoricalState(t *testing.T) {
	address := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend := &testBackend{transactionCount: func(common.Address) uint64 {
		t.Fatal("historical lookup reached the backend")
		return 0
	}}
	api := &txAPI{backend: backend}

	for _, tag := range []ethrpc.BlockNumberOrHash{
		ethrpc.BlockNumberOrHashWithNumber(ethrpc.EarliestBlockNumber),
		ethrpc.BlockNumberOrHashWithNumber(8),
		ethrpc.BlockNumberOrHashWithHash(common.Hash{0x01}, false),
		{},
	} {
		_, err := api.GetTransactionCount(t.Context(), address, tag)
		require.ErrorIs(t, err, errHistoricalStateUnsupported)
	}
}

func TestGetTransactionCountEndToEnd(t *testing.T) {
	address := common.HexToAddress("0x1000000000000000000000000000000000000001")
	backend := &testBackend{transactionCount: func(common.Address) uint64 { return 3 }}
	handler, err := newHandler(backend, evmonly.NewMemoryReceiptStore())
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got hexutil.Uint64
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_getTransactionCount", address, "latest"))
	require.Equal(t, hexutil.Uint64(3), got)
}

func TestGetTransactionReceipt(t *testing.T) {
	txHash := common.HexToHash("0x1234")
	blockHash := common.HexToHash("0xabcd")
	sender := common.HexToAddress("0x1000000000000000000000000000000000000001")
	recipient := common.HexToAddress("0x2000000000000000000000000000000000000002")
	logAddress := common.HexToAddress("0x3000000000000000000000000000000000000003")
	topic := common.HexToHash("0x4567")
	bloom := ethtypes.Bloom{9}
	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash: txHash,
		Receipt: &evmtypes.Receipt{
			TxType:            uint32(ethtypes.DynamicFeeTxType),
			CumulativeGasUsed: 43_000,
			TxHashHex:         txHash.Hex(),
			GasUsed:           22_000,
			EffectiveGasPrice: 17,
			BlockNumber:       7,
			TransactionIndex:  2,
			Status:            uint32(ethtypes.ReceiptStatusSuccessful),
			From:              sender.Hex(),
			To:                recipient.Hex(),
			Logs: []*evmtypes.Log{{
				Address: logAddress.Hex(),
				Topics:  []string{topic.Hex()},
				Data:    []byte{4, 5, 6},
				Index:   3,
			}},
			LogsBloom: bloom.Bytes(),
		},
	}}))

	backend := &testBackend{
		block: func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			require.NotNil(t, req.Height)
			require.Equal(t, coretypes.Int64(7), *req.Height)
			return &coretypes.ResultBlock{
				BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
				Block:   &tmtypes.Block{},
			}, nil
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

	var got struct {
		BlockHash         common.Hash     `json:"blockHash"`
		BlockNumber       hexutil.Uint64  `json:"blockNumber"`
		ContractAddress   *common.Address `json:"contractAddress"`
		CumulativeGasUsed hexutil.Uint64  `json:"cumulativeGasUsed"`
		EffectiveGasPrice *hexutil.Big    `json:"effectiveGasPrice"`
		From              common.Address  `json:"from"`
		GasUsed           hexutil.Uint64  `json:"gasUsed"`
		Logs              []*ethtypes.Log `json:"logs"`
		LogsBloom         ethtypes.Bloom  `json:"logsBloom"`
		Status            hexutil.Uint64  `json:"status"`
		To                *common.Address `json:"to"`
		TransactionHash   common.Hash     `json:"transactionHash"`
		TransactionIndex  hexutil.Uint64  `json:"transactionIndex"`
		Type              hexutil.Uint64  `json:"type"`
	}
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_getTransactionReceipt", txHash))
	require.Equal(t, blockHash, got.BlockHash)
	require.Equal(t, hexutil.Uint64(7), got.BlockNumber)
	require.Nil(t, got.ContractAddress)
	require.Equal(t, hexutil.Uint64(43_000), got.CumulativeGasUsed)
	require.Equal(t, big.NewInt(17), got.EffectiveGasPrice.ToInt())
	require.Equal(t, sender, got.From)
	require.Equal(t, hexutil.Uint64(22_000), got.GasUsed)
	require.Equal(t, bloom, got.LogsBloom)
	require.Equal(t, hexutil.Uint64(ethtypes.ReceiptStatusSuccessful), got.Status)
	require.NotNil(t, got.To)
	require.Equal(t, recipient, *got.To)
	require.Equal(t, txHash, got.TransactionHash)
	require.Equal(t, hexutil.Uint64(2), got.TransactionIndex)
	require.Equal(t, hexutil.Uint64(ethtypes.DynamicFeeTxType), got.Type)
	require.Len(t, got.Logs, 1)
	require.Equal(t, logAddress, got.Logs[0].Address)
	require.Equal(t, []common.Hash{topic}, got.Logs[0].Topics)
	require.Equal(t, []byte{4, 5, 6}, got.Logs[0].Data)
	require.Equal(t, uint64(7), got.Logs[0].BlockNumber)
	require.Equal(t, blockHash, got.Logs[0].BlockHash)
	require.Equal(t, txHash, got.Logs[0].TxHash)
	require.Equal(t, uint(2), got.Logs[0].TxIndex)
	require.Equal(t, uint(3), got.Logs[0].Index)

	var missing map[string]any
	require.NoError(t, client.CallContext(t.Context(), &missing, "eth_getTransactionReceipt", common.Hash{9}))
	require.Nil(t, missing)
}

func TestGetTransactionReceiptReturnsNullBeforeBlockCommit(t *testing.T) {
	txHash := common.Hash{1}
	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash: txHash,
		Receipt: &evmtypes.Receipt{
			TxHashHex:   txHash.Hex(),
			BlockNumber: 7,
		},
	}}))
	backend := &testBackend{
		block: func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			return nil, fmt.Errorf("%w: 7", coretypes.ErrHeightExceedsChainHead)
		},
	}

	got, err := (&txAPI{backend: backend, store: store}).GetTransactionReceipt(t.Context(), txHash)

	require.NoError(t, err)
	require.Nil(t, got)
}

// testChainConfig returns the chain configuration evmOnlyApplication builds:
// every fork active from genesis, chain ID set to chainID.
func testChainConfig(chainID *big.Int) *params.ChainConfig {
	cfg := *params.AllDevChainProtocolChanges
	cfg.ChainID = chainID
	return &cfg
}

func TestGetTransactionByHash(t *testing.T) {
	key, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	to := common.HexToAddress("0x2000000000000000000000000000000000000002")
	chainID := big.NewInt(713715)
	unsigned := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     5,
		GasTipCap: big.NewInt(1_500_000_000),
		GasFeeCap: big.NewInt(2_000_000_000),
		Gas:       50_000,
		To:        &to,
		Value:     big.NewInt(7),
		Data:      []byte{0xde, 0xad, 0xbe, 0xef},
	})
	tx, err := ethtypes.SignTx(unsigned, ethtypes.LatestSignerForChainID(chainID), key)
	require.NoError(t, err)
	raw, err := tx.MarshalBinary()
	require.NoError(t, err)

	blockHash := common.HexToHash("0xabcd")
	blockTime := time.Unix(1_700_000_000, 0)
	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash: tx.Hash(),
		Receipt: &evmtypes.Receipt{
			TxHashHex:        tx.Hash().Hex(),
			BlockNumber:      9,
			TransactionIndex: 1,
			From:             sender.Hex(),
		},
	}}))

	chainConfig := testChainConfig(chainID)
	backend := &testBackend{
		block: func(_ context.Context, req *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			require.NotNil(t, req.Height)
			require.Equal(t, coretypes.Int64(9), *req.Height)
			return &coretypes.ResultBlock{
				BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
				Block: &tmtypes.Block{
					Header: tmtypes.Header{Time: blockTime},
					Data:   tmtypes.Data{Txs: tmtypes.Txs{[]byte("some other transaction"), raw}},
				},
			}, nil
		},
		chainConfig: func() (*params.ChainConfig, error) { return chainConfig, nil },
		baseFee:     func() (*big.Int, error) { return new(big.Int), nil },
	}

	got, err := (&txAPI{backend: backend, store: store}).GetTransactionByHash(t.Context(), tx.Hash())

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, tx.Hash(), got.Hash)
	require.Equal(t, hexutil.Uint64(5), got.Nonce)
	require.Equal(t, hexutil.Uint64(50_000), got.Gas)
	require.Equal(t, sender, got.From)
	require.Equal(t, &to, got.To)
	require.Equal(t, big.NewInt(7), got.Value.ToInt())
	require.Equal(t, hexutil.Bytes{0xde, 0xad, 0xbe, 0xef}, got.Input)
	require.Equal(t, big.NewInt(2_000_000_000), got.GasFeeCap.ToInt())
	require.Equal(t, big.NewInt(1_500_000_000), got.GasTipCap.ToInt())
	require.NotNil(t, got.BlockHash)
	require.Equal(t, blockHash, *got.BlockHash)
	require.NotNil(t, got.BlockNumber)
	require.Equal(t, big.NewInt(9), got.BlockNumber.ToInt())
	require.NotNil(t, got.TransactionIndex)
	require.Equal(t, hexutil.Uint64(1), *got.TransactionIndex)
}

func TestGetTransactionByHashPatchesFromWhenSenderDoesNotRecover(t *testing.T) {
	sender := common.HexToAddress("0x1000000000000000000000000000000000000001")
	to := common.HexToAddress("0x2000000000000000000000000000000000000002")
	// A legacy transaction protected (EIP-155) for a different chain ID than
	// the backend's chain config fails sender recovery against that config's
	// signer, exercising the same edge case evmrpc's transaction API patches
	// from the receipt.
	unsigned := ethtypes.NewTx(&ethtypes.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1_000_000_000),
		Gas:      21_000,
		To:       &to,
		Value:    big.NewInt(1),
	})
	key, err := crypto.HexToECDSA("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	require.NoError(t, err)
	tx, err := ethtypes.SignTx(unsigned, ethtypes.NewEIP155Signer(big.NewInt(999)), key)
	require.NoError(t, err)
	raw, err := tx.MarshalBinary()
	require.NoError(t, err)

	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash: tx.Hash(),
		Receipt: &evmtypes.Receipt{
			TxHashHex:        tx.Hash().Hex(),
			BlockNumber:      3,
			TransactionIndex: 0,
			From:             sender.Hex(),
		},
	}}))
	chainConfig := testChainConfig(big.NewInt(713715))
	backend := &testBackend{
		block: func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			return &coretypes.ResultBlock{
				BlockID: tmtypes.BlockID{Hash: common.HexToHash("0xabcd").Bytes()},
				Block: &tmtypes.Block{
					Header: tmtypes.Header{Time: time.Unix(1_700_000_000, 0)},
					Data:   tmtypes.Data{Txs: tmtypes.Txs{raw}},
				},
			}, nil
		},
		chainConfig: func() (*params.ChainConfig, error) { return chainConfig, nil },
		baseFee:     func() (*big.Int, error) { return new(big.Int), nil },
	}

	got, err := (&txAPI{backend: backend, store: store}).GetTransactionByHash(t.Context(), tx.Hash())

	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, sender, got.From)
}

func TestGetTransactionByHashReturnsNullForUnknownHash(t *testing.T) {
	backend := &testBackend{
		block: func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			t.Fatal("block lookup reached for an unknown hash")
			return nil, nil
		},
	}
	got, err := (&txAPI{backend: backend, store: evmonly.NewMemoryReceiptStore()}).GetTransactionByHash(t.Context(), common.Hash{9})
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestGetTransactionByHashReturnsNullBeforeBlockCommit(t *testing.T) {
	txHash := common.Hash{1}
	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash: txHash,
		Receipt: &evmtypes.Receipt{
			TxHashHex:   txHash.Hex(),
			BlockNumber: 7,
		},
	}}))
	backend := &testBackend{
		block: func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			return nil, fmt.Errorf("%w: 7", coretypes.ErrHeightExceedsChainHead)
		},
	}

	got, err := (&txAPI{backend: backend, store: store}).GetTransactionByHash(t.Context(), txHash)

	require.NoError(t, err)
	require.Nil(t, got)
}

func TestGetTransactionByHashEndToEnd(t *testing.T) {
	tx, raw := testSignedTransaction(t)
	sender, err := ethtypes.Sender(ethtypes.LatestSignerForChainID(big.NewInt(713715)), tx)
	require.NoError(t, err)
	blockHash := common.HexToHash("0xabcd")
	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash: tx.Hash(),
		Receipt: &evmtypes.Receipt{
			TxHashHex:        tx.Hash().Hex(),
			BlockNumber:      4,
			TransactionIndex: 0,
			From:             sender.Hex(),
		},
	}}))
	chainConfig := testChainConfig(big.NewInt(713715))
	backend := &testBackend{
		block: func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
			return &coretypes.ResultBlock{
				BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
				Block: &tmtypes.Block{
					Header: tmtypes.Header{Time: time.Unix(1_700_000_000, 0)},
					Data:   tmtypes.Data{Txs: tmtypes.Txs{raw}},
				},
			}, nil
		},
		chainConfig: func() (*params.ChainConfig, error) { return chainConfig, nil },
		baseFee:     func() (*big.Int, error) { return new(big.Int), nil },
	}
	handler, err := newHandler(backend, store)
	require.NoError(t, err)
	t.Cleanup(handler.Stop)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client, err := ethrpc.DialHTTP(server.URL)
	require.NoError(t, err)
	t.Cleanup(client.Close)

	var got export.RPCTransaction
	require.NoError(t, client.CallContext(t.Context(), &got, "eth_getTransactionByHash", tx.Hash()))
	require.Equal(t, tx.Hash(), got.Hash)
	require.Equal(t, sender, got.From)
	require.Equal(t, hexutil.Uint64(0), got.Nonce)
	require.Equal(t, hexutil.Uint64(21_000), got.Gas)
	require.Equal(t, big.NewInt(1_000_000_000), got.GasPrice.ToInt())
	require.Equal(t, big.NewInt(1), got.Value.ToInt())
	require.NotNil(t, got.BlockHash)
	require.Equal(t, blockHash, *got.BlockHash)

	var missing *export.RPCTransaction
	require.NoError(t, client.CallContext(t.Context(), &missing, "eth_getTransactionByHash", common.Hash{9}))
	require.Nil(t, missing)
}

func TestHandlerRequiresReceiptStore(t *testing.T) {
	_, err := newHandler(&testBackend{}, nil)
	require.EqualError(t, err, "EVM-only RPC requires a receipt store")
}

func TestLookupFinalizedTxSurfacesStoreAndBlockErrors(t *testing.T) {
	txHash := common.Hash{1}
	// Setup: one real receipt so overflow/lookup cases have something to find.
	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash: txHash,
		Receipt: &evmtypes.Receipt{
			TxHashHex:   txHash.Hex(),
			BlockNumber: 7,
		},
	}}))

	t.Run("store error", func(t *testing.T) {
		want := errors.New("receipt db closed")
		api := &txAPI{
			backend: &testBackend{},
			store: stubReceiptStore{
				ReceiptStore: store,
				get: func(sdk.Context, common.Hash) (*evmtypes.Receipt, error) {
					return nil, want
				},
			},
		}

		// Test: lookup hits a store failure that is not ErrNotFound.
		got, err := api.GetTransactionReceipt(t.Context(), txHash)

		// Verify: wrapped store error, no receipt.
		require.Nil(t, got)
		require.ErrorIs(t, err, want)
		require.ErrorContains(t, err, "read transaction receipt")
	})

	t.Run("nil receipt without ErrNotFound", func(t *testing.T) {
		api := &txAPI{
			backend: &testBackend{},
			store: stubReceiptStore{
				ReceiptStore: store,
				get: func(sdk.Context, common.Hash) (*evmtypes.Receipt, error) {
					return nil, nil
				},
			},
		}

		// Test: store returns a nil receipt with a nil error.
		got, err := api.GetTransactionReceipt(t.Context(), txHash)

		// Verify: that combination is treated as a store bug, not a miss.
		require.Nil(t, got)
		require.EqualError(t, err, "receipt store returned a nil receipt")
	})

	t.Run("block number exceeds int64", func(t *testing.T) {
		overflowHash := common.Hash{2}
		api := &txAPI{
			backend: &testBackend{},
			store: stubReceiptStore{
				ReceiptStore: store,
				get: func(sdk.Context, common.Hash) (*evmtypes.Receipt, error) {
					return &evmtypes.Receipt{
						TxHashHex:   overflowHash.Hex(),
						BlockNumber: math.MaxInt64 + 1,
					}, nil
				},
			},
		}

		// Test: receipt's height cannot be passed to Block().
		got, err := api.GetTransactionReceipt(t.Context(), overflowHash)

		// Verify: overflow is an error, not a null result.
		require.Nil(t, got)
		require.ErrorContains(t, err, "exceeds int64")
	})

	t.Run("block lookup error", func(t *testing.T) {
		want := errors.New("block store unavailable")
		api := &txAPI{
			backend: &testBackend{
				block: func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
					return nil, want
				},
			},
			store: store,
		}

		// Test: Block fails with an error other than height-exceeds-head.
		got, err := api.GetTransactionReceipt(t.Context(), txHash)

		// Verify: wrapped block-store error.
		require.Nil(t, got)
		require.ErrorIs(t, err, want)
		require.ErrorContains(t, err, "read receipt block 7")
	})

	t.Run("nil block", func(t *testing.T) {
		api := &txAPI{
			backend: &testBackend{
				block: func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
					return nil, nil
				},
			},
			store: store,
		}

		// Test: Block returns a nil result without error.
		got, err := api.GetTransactionReceipt(t.Context(), txHash)

		// Verify: treated as not-yet-finalized, same as a height miss.
		require.NoError(t, err)
		require.Nil(t, got)
	})
}

func TestGetTransactionByHashRejectsMismatchedIndexAndUndecodableTx(t *testing.T) {
	tx, raw := testSignedTransaction(t)
	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash: tx.Hash(),
		Receipt: &evmtypes.Receipt{
			TxHashHex:        tx.Hash().Hex(),
			BlockNumber:      4,
			TransactionIndex: 3,
			From:             common.HexToAddress("0x1000000000000000000000000000000000000001").Hex(),
		},
	}}))

	t.Run("index exceeds block txs", func(t *testing.T) {
		api := &txAPI{
			backend: &testBackend{
				block: func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
					return &coretypes.ResultBlock{
						Block: &tmtypes.Block{Data: tmtypes.Data{Txs: tmtypes.Txs{raw}}},
					}, nil
				},
			},
			store: store,
		}

		// Test: receipt index 3 into a one-transaction block.
		got, err := api.GetTransactionByHash(t.Context(), tx.Hash())

		// Verify: index mismatch is an error, not a truncated decode.
		require.Nil(t, got)
		require.ErrorContains(t, err, "exceeds block 4 transaction count 1")
	})

	t.Run("undecodable tx bytes", func(t *testing.T) {
		api := &txAPI{
			backend: &testBackend{
				block: func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
					return &coretypes.ResultBlock{
						Block: &tmtypes.Block{
							Header: tmtypes.Header{Height: 4},
							Data:   tmtypes.Data{Txs: tmtypes.Txs{[]byte("not-an-rlp-tx"), []byte("x"), []byte("y"), []byte("z")}},
						},
					}, nil
				},
			},
			store: store,
		}

		// Test: the bytes at the receipt index are not a transaction.
		got, err := api.GetTransactionByHash(t.Context(), tx.Hash())

		// Verify: decode error names the block and index.
		require.Nil(t, got)
		require.ErrorContains(t, err, "decode transaction at block 4 index 3")
	})
}

func TestGetTransactionByHashSurfacesBackendErrors(t *testing.T) {
	tx, raw := testSignedTransaction(t)
	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash: tx.Hash(),
		Receipt: &evmtypes.Receipt{
			TxHashHex:        tx.Hash().Hex(),
			BlockNumber:      4,
			TransactionIndex: 0,
		},
	}}))
	block := func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
		return &coretypes.ResultBlock{
			BlockID: tmtypes.BlockID{Hash: common.HexToHash("0xabcd").Bytes()},
			Block: &tmtypes.Block{
				Header: tmtypes.Header{Time: time.Unix(1_700_000_000, 0)},
				Data:   tmtypes.Data{Txs: tmtypes.Txs{raw}},
			},
		}, nil
	}

	t.Run("chain config", func(t *testing.T) {
		want := errors.New("no chain config")
		api := &txAPI{
			backend: &testBackend{
				block:       block,
				chainConfig: func() (*params.ChainConfig, error) { return nil, want },
			},
			store: store,
		}

		// Test: EvmChainConfig fails after a successful decode.
		got, err := api.GetTransactionByHash(t.Context(), tx.Hash())

		// Verify: that error is returned as-is.
		require.Nil(t, got)
		require.ErrorIs(t, err, want)
	})

	t.Run("negative block time", func(t *testing.T) {
		api := &txAPI{
			backend: &testBackend{
				block: func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
					return &coretypes.ResultBlock{
						Block: &tmtypes.Block{
							Header: tmtypes.Header{Time: time.Unix(-1, 0)},
							Data:   tmtypes.Data{Txs: tmtypes.Txs{raw}},
						},
					}, nil
				},
				chainConfig: func() (*params.ChainConfig, error) { return testChainConfig(big.NewInt(713715)), nil },
			},
			store: store,
		}

		// Test: block timestamp cannot be a uint64.
		got, err := api.GetTransactionByHash(t.Context(), tx.Hash())

		// Verify: negative time is rejected.
		require.Nil(t, got)
		require.ErrorContains(t, err, "time is negative")
	})

	t.Run("base fee", func(t *testing.T) {
		want := errors.New("no base fee")
		api := &txAPI{
			backend: &testBackend{
				block:       block,
				chainConfig: func() (*params.ChainConfig, error) { return testChainConfig(big.NewInt(713715)), nil },
				baseFee:     func() (*big.Int, error) { return nil, want },
			},
			store: store,
		}

		// Test: EvmBaseFee fails after a valid timestamp.
		got, err := api.GetTransactionByHash(t.Context(), tx.Hash())

		// Verify: that error is returned as-is.
		require.Nil(t, got)
		require.ErrorIs(t, err, want)
	})
}

func TestGetTransactionReceiptIncludesContractAddressAndSkipsNilLogs(t *testing.T) {
	txHash := common.Hash{1}
	blockHash := common.HexToHash("0xabcd")
	created := common.HexToAddress("0x4000000000000000000000000000000000000004")
	store := evmonly.NewMemoryReceiptStore()
	require.NoError(t, store.SetReceipts(sdk.Context{}.WithContext(t.Context()), []receipt.ReceiptRecord{{
		TxHash: txHash,
		Receipt: &evmtypes.Receipt{
			TxHashHex:        txHash.Hex(),
			BlockNumber:      7,
			ContractAddress:  created.Hex(),
			Logs:             []*evmtypes.Log{nil},
			TransactionIndex: 0,
		},
	}}))
	api := &txAPI{
		backend: &testBackend{
			block: func(context.Context, *coretypes.RequestBlockInfo) (*coretypes.ResultBlock, error) {
				return &coretypes.ResultBlock{
					BlockID: tmtypes.BlockID{Hash: blockHash.Bytes()},
					Block:   &tmtypes.Block{},
				}, nil
			},
		},
		store: store,
	}

	// Test: receipt with a contract address and a nil log slot.
	got, err := api.GetTransactionReceipt(t.Context(), txHash)

	// Verify: contract address is set and the nil log is dropped.
	require.NoError(t, err)
	require.Equal(t, &created, got["contractAddress"])
	require.Empty(t, got["logs"])
}
