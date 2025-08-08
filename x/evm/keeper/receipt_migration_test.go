package keeper_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/store/prefix"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
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

	migrated, err := k.MigrateLegacyReceiptsBatch(ctx, keeper.LegacyReceiptMigrationBatchSize)
	require.NoError(t, err)
	require.Equal(t, 100, migrated)
	require.Equal(t, 1, getLegacyReceiptCount(ctx, k))

	// A migrated receipt should now be retrievable via persistent store
	r, err := k.GetReceipt(ctx, txs[0])
	require.NoError(t, err)
	require.Equal(t, txs[0].Hex(), r.TxHashHex)

	// Advance height to ensure subsequent ApplyChangeset uses a new version
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	migrated, err = k.MigrateLegacyReceiptsBatch(ctx, keeper.LegacyReceiptMigrationBatchSize)
	require.NoError(t, err)
	require.Equal(t, 1, migrated)
	require.Equal(t, 0, getLegacyReceiptCount(ctx, k))

	// The last receipt should also be retrievable
	r, err = k.GetReceipt(ctx, txs[len(txs)-1])
	require.NoError(t, err)
	require.Equal(t, txs[len(txs)-1].Hex(), r.TxHashHex)
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
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		cnt++
	}
	return
}
