package evmrpc_test

import (
	"crypto/sha256"
	"math/big"
	"sync"
	"testing"
	"time"

	wasmtypes "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm/types"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/export"
	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/sei-protocol/sei-chain/sei-cosmos/client"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	banktypes "github.com/sei-protocol/sei-chain/sei-cosmos/x/bank/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/rpc/coretypes"
	tmtypes "github.com/sei-protocol/sei-chain/sei-tendermint/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestEncodeTmBlock_EmptyTransactions(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	block := &coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(MockHeight8),
			Data:   tmtypes.Data{},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight8 - 1,
			},
		},
	}

	// Call EncodeTmBlock with empty transactions
	result, err := evmrpc.EncodeTmBlock(func(i int64) sdk.Context { return ctx }, func(i int64) client.TxConfig { return TxConfig }, block, k, true, false, evmrpc.NewBlockCache(3000), &sync.Mutex{})
	require.Nil(t, err)

	// Assert txHash is equal to ethtypes.EmptyTxsHash
	require.Equal(t, ethtypes.EmptyTxsHash, result["transactionsRoot"])
}

// Sei commits blocks more often than once a second, so the seconds-only
// Ethereum "timestamp" repeats across consecutive blocks. milliTimestamp carries
// the sub-second part the Tendermint header already holds.
func TestEncodeTmBlockMilliTimestamp(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	header := mockBlockHeader(MockHeight8)
	header.Time = time.Unix(1696941649, 125_000_000).UTC()
	block := &coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: header,
			Data:   tmtypes.Data{},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight8 - 1,
			},
		},
	}

	result, err := evmrpc.EncodeTmBlock(func(i int64) sdk.Context { return ctx }, func(i int64) client.TxConfig { return TxConfig }, block, k, true, false, evmrpc.NewBlockCache(3000), &sync.Mutex{})
	require.Nil(t, err)

	require.Equal(t, hexutil.Uint64(1696941649), result["timestamp"])
	require.Equal(t, hexutil.Uint64(1696941649125), result["milliTimestamp"])
}

func TestEncodeBankMsg(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{}).WithBlockTime(time.Now())
	fromSeiAddr, _ := testkeeper.MockAddressPair()
	toSeiAddr, _ := testkeeper.MockAddressPair()
	b := TxConfig.NewTxBuilder()
	b.SetMsgs(banktypes.NewMsgSend(fromSeiAddr, toSeiAddr, sdk.NewCoins(sdk.NewCoin("usei", sdk.NewInt(10)))))
	tx := b.GetTx()
	resBlock := coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(MockHeight8),
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{func() []byte {
					bz, _ := Encoder(tx)
					return bz
				}()},
			},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight8 - 1,
			},
		},
	}
	res, err := evmrpc.EncodeTmBlock(func(i int64) sdk.Context { return ctx }, func(i int64) client.TxConfig { return TxConfig }, &resBlock, k, true, false, evmrpc.NewBlockCache(3000), &sync.Mutex{})
	require.Nil(t, err)
	txs := res["transactions"].([]any)
	require.Equal(t, 0, len(txs))
}

func TestEncodeWasmExecuteMsg(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).WithBlockHeight(MockHeight8)
	fromSeiAddr, fromEvmAddr := testkeeper.MockAddressPair()
	toSeiAddr, _ := testkeeper.MockAddressPair()
	b := TxConfig.NewTxBuilder()
	b.SetMsgs(&wasmtypes.MsgExecuteContract{
		Sender:   fromSeiAddr.String(),
		Contract: toSeiAddr.String(),
		Msg:      []byte{1, 2, 3},
	})
	tx := b.GetTx()
	bz, _ := Encoder(tx)
	txHash := sha256.Sum256(bz)
	hash := common.BytesToHash(txHash[:])
	testkeeper.MustMockReceipt(t, k, ctx, hash, &types.Receipt{
		TransactionIndex: 1,
		From:             fromEvmAddr.Hex(),
		TxHashHex:        hash.Hex(),
	})
	receipt := testkeeper.WaitForReceipt(t, k, ctx, hash)
	require.Equal(t, hash.Hex(), receipt.TxHashHex)
	resBlock := coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(MockHeight8),
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{bz},
			},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight8 - 1,
			},
		},
	}
	res, err := evmrpc.EncodeTmBlock(func(i int64) sdk.Context { return ctx }, func(i int64) client.TxConfig { return TxConfig }, &resBlock, k, true, true, evmrpc.NewBlockCache(3000), &sync.Mutex{})
	require.Nil(t, err)
	txs := res["transactions"].([]any)
	require.Equal(t, 1, len(txs))
	ti := uint64(0)
	bh := common.HexToHash(MockBlockID.Hash.String())
	to := common.Address(toSeiAddr)
	require.Equal(t, &export.RPCTransaction{
		BlockHash:        &bh,
		BlockNumber:      (*hexutil.Big)(big.NewInt(MockHeight8)),
		From:             fromEvmAddr,
		To:               &to,
		Input:            []byte{1, 2, 3},
		Hash:             common.Hash(sha256.Sum256(bz)),
		TransactionIndex: (*hexutil.Uint64)(&ti),
		V:                nil,
		R:                nil,
		S:                nil,
	}, txs[0].(*export.RPCTransaction))
}

// Wasm-execute txs contribute receipt.GasUsed to the block's gasUsed total.
func TestEncodeWasmExecuteMsg_GasUsedFromReceipt(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx(nil).WithBlockHeight(MockHeight8)
	fromSeiAddr, fromEvmAddr := testkeeper.MockAddressPair()
	toSeiAddr, _ := testkeeper.MockAddressPair()
	b := TxConfig.NewTxBuilder()
	b.SetMsgs(&wasmtypes.MsgExecuteContract{
		Sender:   fromSeiAddr.String(),
		Contract: toSeiAddr.String(),
		Msg:      []byte{1, 2, 3},
	})
	tx := b.GetTx()
	bz, _ := Encoder(tx)
	txHash := sha256.Sum256(bz)
	hash := common.BytesToHash(txHash[:])
	testkeeper.MustMockReceipt(t, k, ctx, hash, &types.Receipt{
		TransactionIndex: 1,
		From:             fromEvmAddr.Hex(),
		TxHashHex:        hash.Hex(),
		GasUsed:          54321,
	})
	resBlock := coretypes.ResultBlock{
		BlockID: MockBlockID,
		Block: &tmtypes.Block{
			Header: mockBlockHeader(MockHeight8),
			Data: tmtypes.Data{
				Txs: []tmtypes.Tx{bz},
			},
			LastCommit: &tmtypes.Commit{
				Height: MockHeight8 - 1,
			},
		},
	}
	res, err := evmrpc.EncodeTmBlock(func(i int64) sdk.Context { return ctx }, func(i int64) client.TxConfig { return TxConfig }, &resBlock, k, true, true, evmrpc.NewBlockCache(3000), &sync.Mutex{})
	require.Nil(t, err)
	require.Equal(t, hexutil.Uint64(54321), res["gasUsed"])
	txs := res["transactions"].([]any)
	require.Equal(t, 1, len(txs))
}

func TestEVMLaunchHeightValidation(t *testing.T) {
	// Test ValidateEVMBlockHeight function
	// Should pass for pacific-1 with valid height
	err := evmrpc.ValidateEVMBlockHeight("pacific-1", 79123881)
	require.NoError(t, err)

	err = evmrpc.ValidateEVMBlockHeight("pacific-1", 79123882)
	require.NoError(t, err)

	// Should fail for pacific-1 with invalid height
	err = evmrpc.ValidateEVMBlockHeight("pacific-1", 79123880)
	require.Error(t, err)
	require.Contains(t, err.Error(), "EVM is only supported from block 79123881 onwards")

	// Should pass for other chains with any height
	err = evmrpc.ValidateEVMBlockHeight("atlantic-2", 1)
	require.NoError(t, err)

	err = evmrpc.ValidateEVMBlockHeight("test-chain", 1)
	require.NoError(t, err)
}

func TestEVMBlockValidationEdgeCases(t *testing.T) {
	// Test edge cases for EVM block validation

	// Test exactly at launch height
	err := evmrpc.ValidateEVMBlockHeight("pacific-1", 79123881)
	require.NoError(t, err)

	// Test one block before launch height
	err = evmrpc.ValidateEVMBlockHeight("pacific-1", 79123880)
	require.Error(t, err)
	require.Equal(t, "EVM is only supported from block 79123881 onwards", err.Error())

	// Test way before launch height
	err = evmrpc.ValidateEVMBlockHeight("pacific-1", 1000000)
	require.Error(t, err)
	require.Equal(t, "EVM is only supported from block 79123881 onwards", err.Error())

	// Test block 0
	err = evmrpc.ValidateEVMBlockHeight("pacific-1", 0)
	require.Error(t, err)
	require.Equal(t, "EVM is only supported from block 79123881 onwards", err.Error())
}

func TestEVMBlockValidationDifferentChains(t *testing.T) {
	// Test that validation only applies to pacific-1
	chains := []string{"atlantic-2", "arctic-1", "test-chain", "unknown-chain", ""}

	for _, chainID := range chains {
		// All non-pacific-1 chains should pass validation for any block height
		err := evmrpc.ValidateEVMBlockHeight(chainID, 1)
		require.NoError(t, err, "Chain %s should not validate block heights", chainID)

		err = evmrpc.ValidateEVMBlockHeight(chainID, 0)
		require.NoError(t, err, "Chain %s should not validate block heights", chainID)

		err = evmrpc.ValidateEVMBlockHeight(chainID, 79123880)
		require.NoError(t, err, "Chain %s should not validate block heights", chainID)
	}
}
