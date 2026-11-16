package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/stretchr/testify/require"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

func TestArchiveBlock(t *testing.T) {
	block := PreparedBlock{
		Context: BlockContext{Number: 7, Time: 70, GasLimit: 1_000, BaseFee: big.NewInt(3),
			BlockHash: common.Hash{0xb7}, ParentHash: common.Hash{0xb6}},
	}
	result := &BlockResult{GasUsed: 42, ChangeSet: gigatypes.StateChangeSet{
		Storage: []gigatypes.StorageChange{{Address: common.Address{0xc0}, Key: common.Hash{1}}},
	}}
	// An executed transaction, a stale-nonce rejection and a rejection the sender can still cure.
	for i, err := range []error{nil, core.ErrNonceTooLow, core.ErrNonceTooHigh} {
		tx := ethtypes.NewTx(&ethtypes.LegacyTx{Nonce: uint64(i)})
		block.Txs = append(block.Txs, PreparedTx{Tx: tx, Sender: common.Address{byte(i)}})
		result.Txs = append(result.Txs, TxResult{Hash: tx.Hash(), Rejected: err != nil, Err: err})
		result.Receipts = append(result.Receipts, &ethtypes.Receipt{TxHash: tx.Hash()})
	}

	got, err := archiveBlock(block, result)
	require.NoError(t, err)
	require.Equal(t, uint64(7), got.Number)
	require.Equal(t, common.Hash{0xb7}, got.Hash)
	require.Equal(t, common.Hash{0xb6}, got.ParentHash)
	require.Equal(t, uint64(70), got.Time)
	require.Equal(t, uint64(1_000), got.GasLimit)
	require.Equal(t, uint64(42), got.GasUsed)
	require.Equal(t, big.NewInt(3), got.BaseFee)
	require.Equal(t, result.ChangeSet, got.Changes)
	require.Len(t, got.Txs, 3)
	for i, tx := range got.Txs {
		require.Same(t, block.Txs[i].Tx, tx.Tx)
		require.Equal(t, block.Txs[i].Sender, tx.Sender)
		require.Same(t, result.Receipts[i], tx.Receipt)
		require.Equal(t, result.Txs[i].Rejected, tx.Rejected)
	}
	// Found by hash exactly when the receipt store keeps a receipt for it.
	require.True(t, got.Txs[0].Locatable)
	require.True(t, got.Txs[1].Locatable)
	require.False(t, got.Txs[2].Locatable)

	result.Receipts = result.Receipts[:2]
	_, err = archiveBlock(block, result)
	require.Error(t, err)
}
