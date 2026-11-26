package evmonly

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/archive"
	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"
)

type finalizedArchive struct {
	hashes   []common.Hash
	indexes  []uint
	logs     []ethtypes.Log
	rejected bool
}

func (a *finalizedArchive) ArchiveBlock(_ context.Context, block *archive.ExecutedBlock) error {
	for _, tx := range block.Txs {
		a.hashes = append(a.hashes, tx.Tx.Hash())
		a.indexes = append(a.indexes, tx.Receipt.TransactionIndex)
		a.rejected = a.rejected || tx.Rejected
		for _, log := range tx.Receipt.Logs {
			a.logs = append(a.logs, *log)
		}
	}
	return nil
}

func (*finalizedArchive) LatestBlock() uint64 { return 0 }
func (*finalizedArchive) Close() error        { return nil }

func TestFinalizedBlockOmitsRejectedAttempts(t *testing.T) {
	forEachExecutionPath(t, func(t *testing.T, cfg Config) {
		chainID := big.NewInt(testChainID)
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		unfunded, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)
		logger, reverter, recipient := testAddress(0xd1), testAddress(0xd2), testAddress(0xd3)
		newState := func() *MemoryState {
			state := NewMemoryState()
			state.SetBalance(sender, big.NewInt(testFundedBalanceWei))
			state.SetCode(logger, []byte{0x60, 0x00, 0x60, 0x00, 0xa0, 0x00})
			state.SetCode(reverter, []byte{0x60, 0x00, 0x60, 0x00, 0xfd})
			return state
		}
		logTx := abevm.RawTx(signLegacyTxWithGas(t, key, chainID, 0, &logger, new(big.Int), nil, 100_000))
		revertTx := abevm.RawTx(signLegacyTxWithGas(t, key, chainID, 1, &reverter, new(big.Int), nil, 100_000))
		transfer := abevm.RawTx(signLegacyTxWithGas(t, key, chainID, 2, &recipient, big.NewInt(7), nil, 21_000))
		txs := []abevm.RawTx{
			signLegacyTx(t, key, chainID, 9, &recipient, big.NewInt(1), nil),
			signLegacyTx(t, unfunded, chainID, 0, &recipient, big.NewInt(1), nil),
			signLegacyTxWithGas(t, key, chainID, 0, &recipient, big.NewInt(1), []byte{1}, 21_000),
			logTx, logTx, revertTx, transfer,
			signLegacyTxWithGas(t, key, chainID, 3, &recipient, big.NewInt(1), nil, 400_000),
		}
		store := NewMemoryStore(newState())
		receipts := NewMemoryReceiptStore()
		history := &finalizedArchive{}
		executor := NewExecutor(cfg, withTestStores(store, receipts))
		defer executor.Close()
		executor.archive = history
		block := blockContext(chainID)
		block.Number, block.GasLimit = 1, 400_000
		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: block, Txs: txs, TxsOrdered: true})
		require.NoError(t, err)
		defer result.Release()
		requireOCCRan(t, cfg, result)
		for i, cause := range map[int]error{0: core.ErrNonceTooHigh, 1: core.ErrInsufficientFunds, 2: core.ErrIntrinsicGas, 4: core.ErrNonceTooLow, 7: core.ErrGasLimitReached} {
			require.True(t, result.Txs[i].Rejected)
			require.ErrorIs(t, result.Txs[i].Err, cause)
			require.Zero(t, result.Txs[i].GasUsed)
		}
		require.False(t, result.Txs[5].Rejected)
		require.ErrorIs(t, result.Txs[5].Err, vm.ErrExecutionReverted)
		kept := []abevm.RawTx{logTx, revertTx, transfer}
		for i, raw := range kept {
			receipt, err := receipts.GetReceipt(newReceiptContext(t.Context(), 1), raw.Hash())
			require.NoError(t, err)
			require.Equal(t, uint32(i), receipt.TransactionIndex)
		}
		for _, i := range []int{0, 1, 2, 7} {
			_, err := receipts.GetReceipt(newReceiptContext(t.Context(), 1), txs[i].Hash())
			require.ErrorIs(t, err, receiptdb.ErrNotFound)
		}
		require.Equal(t, []common.Hash{logTx.Hash(), revertTx.Hash(), transfer.Hash()}, history.hashes)
		require.Equal(t, []uint{0, 1, 2}, history.indexes)
		require.False(t, history.rejected)
		require.Len(t, history.logs, 1)
		require.Zero(t, history.logs[0].TxIndex)
		require.Zero(t, history.logs[0].Index)
		// Diagnostics still describe attempts in their input positions.
		require.Equal(t, uint(3), result.Receipts[3].TransactionIndex)
		require.Equal(t, uint(3), result.Receipts[3].Logs[0].TxIndex)
		cleanStore := NewMemoryStore(newState())
		clean := NewExecutor(cfg, withTestStores(cleanStore, NewMemoryReceiptStore()))
		defer clean.Close()
		expected, err := clean.ExecuteBlock(t.Context(), BlockRequest{Context: block, Txs: kept, TxsOrdered: true})
		require.NoError(t, err)
		defer expected.Release()
		require.Equal(t, expected.ChangeSet, result.ChangeSet)
		require.Equal(t, expected.GasUsed, result.GasUsed)
	})
}

func TestFinalizeBlockRejectsMalformedResults(t *testing.T) {
	block := OrderedBlock{Txs: make([]abevm.EvmTx, 1)}
	_, err := finalizeBlock(block, &BlockResult{})
	require.ErrorContains(t, err, "1 transactions but 0 results and 0 receipts")
	_, err = finalizeBlock(block, &BlockResult{Txs: []TxResult{{}}, Receipts: ethtypes.Receipts{nil}})
	require.ErrorContains(t, err, "receipt 0 is nil")
}
