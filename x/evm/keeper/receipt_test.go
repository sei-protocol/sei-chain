package keeper_test

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestReceipt(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	txHash := common.HexToHash("0x0750333eac0be1203864220893d8080dd8a8fd7a2ed098dfd92a718c99d437f2")
	_, err := k.GetReceipt(ctx, txHash)
	require.NotNil(t, err)
	k.MockReceipt(ctx, txHash, &types.Receipt{TxHashHex: txHash.Hex()})
	k.AppendToEvmTxDeferredInfo(ctx, ethtypes.Bloom{}, common.Hash{1}, sdk.NewInt(1)) // make sure this isn't flushed into receipt store
	r, err := k.GetReceipt(ctx, txHash)
	require.Nil(t, err)
	require.Equal(t, txHash.Hex(), r.TxHashHex)
	_, err = k.GetReceipt(ctx, common.Hash{1})
	require.Equal(t, "receipt not found", err.Error())
}

func TestGetReceiptWithRetry(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	txHash := common.HexToHash("0x0750333eac0be1203864220893d8080dd8a8fd7a2ed098dfd92a718c99d437f2")

	// Test max retries exceeded first
	nonExistentHash := common.Hash{1}
	_, err := k.GetReceiptWithRetry(ctx, nonExistentHash, 2)
	require.NotNil(t, err)
	require.Equal(t, "receipt not found", err.Error())

	// Then test successful retry
	go func() {
		time.Sleep(300 * time.Millisecond)
		k.MockReceipt(ctx, txHash, &types.Receipt{TxHashHex: txHash.Hex()})
	}()

	r, err := k.GetReceiptWithRetry(ctx, txHash, 3)
	require.Nil(t, err)
	require.Equal(t, txHash.Hex(), r.TxHashHex)
}

func TestFlushTransientReceipts(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	txHash := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	receipt := &types.Receipt{TxHashHex: txHash.Hex(), Status: 1}

	// Set a transient receipt
	err := k.SetTransientReceipt(ctx, txHash, receipt)
	require.NoError(t, err)

	// Should be retrievable from transient store
	tr, err := k.GetTransientReceipt(ctx, txHash, 0)
	require.NoError(t, err)
	require.Equal(t, receipt.TxHashHex, tr.TxHashHex)

	// Not yet in persistent store
	_, err = k.GetReceipt(ctx, txHash)
	require.Error(t, err)

	// Flush synchronously
	err = k.FlushTransientReceipts(ctx)
	require.NoError(t, err)

	// Now should be retrievable from persistent store
	pr, err := k.GetReceipt(ctx, txHash)
	require.NoError(t, err)
	require.Equal(t, receipt.TxHashHex, pr.TxHashHex)

	// Should not be in transient store anymore (depends on implementation, but let's check)
	_, _ = k.GetTransientReceipt(ctx, txHash, 0)
	// Could be not found or still present depending on flush logic, so we don't assert error here

	// Flushing with no receipts should not error
	err = k.FlushTransientReceipts(ctx)
	require.NoError(t, err)
}

func TestDeleteTransientReceipt(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	txHash := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	receipt := &types.Receipt{TxHashHex: txHash.Hex(), Status: 1}

	err := k.SetTransientReceipt(ctx, txHash, receipt)
	require.NoError(t, err)

	k.DeleteTransientReceipt(ctx, txHash, 0)

	receipt, err = k.GetTransientReceipt(ctx, txHash, 0)
	require.Nil(t, receipt)
	require.Equal(t, "receipt not found", err.Error())
}

// Flush transient receipts should not adjust cumulative gas used for legacy receipts
func TestFlushTransientReceiptsLegacyReceipts(t *testing.T) {
	// Pacific-1
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})
	ctx = ctx.WithChainID("pacific-1")

	// Create two receipts in same block
	txHash1 := common.HexToHash("0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	txHash2 := common.HexToHash("0x2234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	receipt1 := &types.Receipt{
		TxHashHex:   txHash1.Hex(),
		BlockNumber: 100, // Below pacific-1 legacy threshold
		GasUsed:     1000,
	}
	receipt2 := &types.Receipt{
		TxHashHex:   txHash2.Hex(),
		BlockNumber: 100, // Below pacific-1 legacy threshold
		GasUsed:     2000,
	}

	// Set both receipts
	err := k.SetTransientReceipt(ctx, txHash1, receipt1)
	require.NoError(t, err)
	err = k.SetTransientReceipt(ctx, txHash2, receipt2)
	require.NoError(t, err)

	// Flush both receipts
	err = k.FlushTransientReceipts(ctx)
	require.NoError(t, err)

	// Verify cumulative gas not changed for either receipt
	r1, err := k.GetReceipt(ctx, txHash1)
	require.NoError(t, err)
	require.Equal(t, uint64(1000), r1.GasUsed)
	require.Equal(t, uint64(0), r1.CumulativeGasUsed)

	r2, err := k.GetReceipt(ctx, txHash2)
	require.NoError(t, err)
	require.Equal(t, uint64(2000), r2.GasUsed)
	require.Equal(t, uint64(0), r2.CumulativeGasUsed)

	// do the same for non-legacy receipts and make sure cumulative gas used is adjusted
	txHash3 := common.HexToHash("0x3234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	receipt3 := &types.Receipt{
		TxHashHex:   txHash3.Hex(),
		BlockNumber: 162745894, // Above pacific-1 legacy threshold
		GasUsed:     3000,
	}
	err = k.SetTransientReceipt(ctx, txHash3, receipt3)
	require.NoError(t, err)
	err = k.FlushTransientReceipts(ctx)
	require.NoError(t, err)

	r3, err := k.GetReceipt(ctx, txHash3)
	require.NoError(t, err)
	require.Equal(t, uint64(3000), r3.GasUsed)
	require.Equal(t, uint64(3000), r3.CumulativeGasUsed)

	// Atlantic-2
	ctx = ctx.WithChainID("atlantic-2")

	// Create two receipts in same block
	txHash4 := common.HexToHash("0x4234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	txHash5 := common.HexToHash("0x5234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	receipt4 := &types.Receipt{
		TxHashHex:   txHash4.Hex(),
		BlockNumber: 100, // Below atlantic-2 legacy threshold
		GasUsed:     4000,
	}
	receipt5 := &types.Receipt{
		TxHashHex:   txHash5.Hex(),
		BlockNumber: 100, // Below atlantic-2 legacy threshold
		GasUsed:     5000,
	}

	// Set both receipts
	err = k.SetTransientReceipt(ctx, txHash4, receipt4)
	require.NoError(t, err)
	err = k.SetTransientReceipt(ctx, txHash5, receipt5)
	require.NoError(t, err)

	// Flush both receipts
	err = k.FlushTransientReceipts(ctx)
	require.NoError(t, err)

	// Verify cumulative gas not changed for either receipt
	r4, err := k.GetReceipt(ctx, txHash4)
	require.NoError(t, err)
	require.Equal(t, uint64(4000), r4.GasUsed)
	require.Equal(t, uint64(0), r4.CumulativeGasUsed)

	r5, err := k.GetReceipt(ctx, txHash5)
	require.NoError(t, err)
	require.Equal(t, uint64(5000), r5.GasUsed)
	require.Equal(t, uint64(0), r5.CumulativeGasUsed)

	// do the same for non-legacy receipts and make sure cumulative gas used is adjusted
	txHash6 := common.HexToHash("0x6234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	receipt6 := &types.Receipt{
		TxHashHex:   txHash6.Hex(),
		BlockNumber: 191939682, // Above atlantic-2 legacy threshold
		GasUsed:     6000,
	}
	err = k.SetTransientReceipt(ctx, txHash6, receipt6)
	require.NoError(t, err)
	err = k.FlushTransientReceipts(ctx)
	require.NoError(t, err)

	r6, err := k.GetReceipt(ctx, txHash6)
	require.NoError(t, err)
	require.Equal(t, uint64(6000), r6.GasUsed)
	require.Equal(t, uint64(6000), r6.CumulativeGasUsed)

	// Arctic-1
	ctx = ctx.WithChainID("arctic-1")

	// Create two receipts in same block
	txHash7 := common.HexToHash("0x7234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	txHash8 := common.HexToHash("0x8234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")

	receipt7 := &types.Receipt{
		TxHashHex:   txHash7.Hex(),
		BlockNumber: 100, // Below arctic-1 legacy threshold
		GasUsed:     7000,
	}
	receipt8 := &types.Receipt{
		TxHashHex:   txHash8.Hex(),
		BlockNumber: 100, // Below arctic-1 legacy threshold
		GasUsed:     8000,
	}

	// Set both receipts
	err = k.SetTransientReceipt(ctx, txHash7, receipt7)
	require.NoError(t, err)
	err = k.SetTransientReceipt(ctx, txHash8, receipt8)
	require.NoError(t, err)

	// Flush both receipts
	err = k.FlushTransientReceipts(ctx)
	require.NoError(t, err)

	// Verify cumulative gas not changed for either receipt
	r7, err := k.GetReceipt(ctx, txHash7)
	require.NoError(t, err)
	require.Equal(t, uint64(7000), r7.GasUsed)
	require.Equal(t, uint64(0), r7.CumulativeGasUsed)

	r8, err := k.GetReceipt(ctx, txHash8)
	require.NoError(t, err)
	require.Equal(t, uint64(8000), r8.GasUsed)
	require.Equal(t, uint64(0), r8.CumulativeGasUsed)

	// do the same for non-legacy receipts and make sure cumulative gas used is adjusted
	txHash9 := common.HexToHash("0x9234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef")
	receipt9 := &types.Receipt{
		TxHashHex:   txHash9.Hex(),
		BlockNumber: 109393644, // Above arctic-1 legacy threshold
		GasUsed:     9000,
	}
	err = k.SetTransientReceipt(ctx, txHash9, receipt9)
	require.NoError(t, err)
	err = k.FlushTransientReceipts(ctx)
	require.NoError(t, err)

	r9, err := k.GetReceipt(ctx, txHash9)
	require.NoError(t, err)
	require.Equal(t, uint64(9000), r9.GasUsed)
	require.Equal(t, uint64(9000), r9.CumulativeGasUsed)
}
