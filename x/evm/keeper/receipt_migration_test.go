package keeper_test

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/sei-protocol/sei-chain/sei-cosmos/store/prefix"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	testkeeper "github.com/sei-protocol/sei-chain/testutil/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/keeper"
	"github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestMigrateLegacyReceiptsBatch(t *testing.T) {
	k := &testkeeper.EVMTestApp.EvmKeeper
	ctx := testkeeper.EVMTestApp.GetContextForDeliverTx([]byte{})

	// Seed 101 legacy receipts
	var txs []common.Hash
	for i := byte(1); i <= 101; i++ {
		tx := common.BytesToHash([]byte{i})
		receipt := &types.Receipt{TxHashHex: tx.Hex()}
		setLegacyReceipt(ctx, k, tx, receipt)
		txs = append(txs, tx)
	}

	require.Equal(t, 101, getLegacyReceiptCount(ctx, k))

	// Verify some sample legacy receipts exist before migration
	checkLegacyReceipt(t, ctx, k, txs[0], true)
	checkLegacyReceipt(t, ctx, k, txs[len(txs)-1], true)

	// Verify some sample legacy receipts are not retrievable from receipt.db before migration
	for _, tx := range txs {
		_, err := k.GetReceiptFromReceiptStore(ctx, tx)
		require.Error(t, err)
	}

	migrated, err := k.MigrateLegacyReceiptsBatch(ctx, keeper.LegacyReceiptMigrationBatchSize)
	require.NoError(t, err)
	require.Equal(t, 100, migrated)
	// Flush transient receipts so they are available in receipt.db for assertions below
	require.NoError(t, k.FlushTransientReceipts(ctx))
	require.Equal(t, 1, getLegacyReceiptCount(ctx, k))

	// The first tx should have been removed from legacy store; the last should still exist
	for i := 0; i < 100; i++ {
		checkLegacyReceipt(t, ctx, k, txs[i], false)
	}
	checkLegacyReceipt(t, ctx, k, txs[len(txs)-1], true)

	// Verify first 100 legacy receipts are retrievable from receipt.db after migration
	for i := 0; i < 100; i++ {
		tx := txs[i]
		r := testkeeper.WaitForReceiptFromStore(t, k, ctx, tx)
		require.Equal(t, tx.Hex(), r.TxHashHex)
	}
	// Verify last receipt is not retrievable from receipt.db after migration
	_, err = k.GetReceiptFromReceiptStore(ctx, txs[len(txs)-1])
	require.Error(t, err)

	// Check GetReceipt works for migrated receipts
	for _, tx := range txs[:100] {
		r := testkeeper.WaitForReceipt(t, k, ctx, tx)
		require.Equal(t, tx.Hex(), r.TxHashHex)
	}

	// Advance height to ensure subsequent ApplyChangesetSync uses a new version
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	migrated, err = k.MigrateLegacyReceiptsBatch(ctx, keeper.LegacyReceiptMigrationBatchSize)
	require.NoError(t, err)
	require.Equal(t, 1, migrated)
	// Flush remaining transient receipt
	require.NoError(t, k.FlushTransientReceipts(ctx))
	require.Equal(t, 0, getLegacyReceiptCount(ctx, k))

	// Verify all receipts are retrievable from receipt.db after migration
	for _, tx := range txs {
		r := testkeeper.WaitForReceiptFromStore(t, k, ctx, tx)
		require.Equal(t, tx.Hex(), r.TxHashHex)
	}

	// Check all receipts not retrievable from legacy store
	for i := 0; i < 100; i++ {
		checkLegacyReceipt(t, ctx, k, txs[i], false)
	}

	// Check GetReceipt works for all receipts
	for _, tx := range txs {
		r := testkeeper.WaitForReceipt(t, k, ctx, tx)
		require.Equal(t, tx.Hex(), r.TxHashHex)
	}
}

func setLegacyReceipt(ctx sdk.Context, k *keeper.Keeper, txHash common.Hash, receipt *types.Receipt) {
	store := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), types.ReceiptKeyPrefix)
	bz, err := receipt.Marshal()
	if err != nil {
		panic(err)
	}
	store.Set(txHash[:], bz)
}

func getLegacyReceiptCount(ctx sdk.Context, k *keeper.Keeper) (cnt int) {
	store := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), types.ReceiptKeyPrefix)
	iter := store.Iterator(nil, nil)
	defer func() { _ = iter.Close() }()
	for ; iter.Valid(); iter.Next() {
		cnt++
	}
	return
}

// checkLegacyReceipt asserts the presence of a legacy receipt (stored in KV under ReceiptKeyPrefix)
// prior to migration, and its absence after migration.
func checkLegacyReceipt(t *testing.T, ctx sdk.Context, k *keeper.Keeper, txHash common.Hash, shouldExist bool) {
	store := prefix.NewStore(ctx.KVStore(k.GetStoreKey()), types.ReceiptKeyPrefix)
	exists := store.Get(txHash[:]) != nil
	if shouldExist {
		require.Truef(t, exists, "expected legacy receipt to exist for %s", txHash.Hex())
	} else {
		require.Falsef(t, exists, "expected legacy receipt to be migrated for %s", txHash.Hex())
	}
}
