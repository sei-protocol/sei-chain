package evmonly

import (
	receiptdb "github.com/sei-protocol/sei-chain/sei-db/receipt_db"
	abevm "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/evm"

	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestExecutorStaleNoncesDoNotAbortBlock(t *testing.T) {
	for _, workers := range []int{1, 4} {
		t.Run(fmt.Sprintf("workers=%d", workers), func(t *testing.T) {
			chainID := big.NewInt(testChainID)
			key, err := crypto.GenerateKey()
			require.NoError(t, err)
			sender := crypto.PubkeyToAddress(key.PublicKey)
			recipient := testAddress(0xa7)
			state := NewMemoryState()
			initialBalance := big.NewInt(1_000_000_000_000_000)
			state.SetBalance(sender, initialBalance)
			state.SetNonce(sender, 127)
			store := NewMemoryStore(state)
			receipts := NewMemoryReceiptStore()
			executor := NewExecutor(
				Config{OCCWorkers: workers},
				withTestStores(store, receipts),
			)
			t.Cleanup(executor.Close)

			// The stale creation also exceeds the block gas limit. Nonce rejection
			// must happen before gas reservation and must not create a contract.
			stale := signLegacyTx(t, key, chainID, 126, nil, big.NewInt(1), []byte{0x00})
			first := signLegacyTxWithGas(t, key, chainID, 127, &recipient, big.NewInt(1), nil, 21_000)
			next := signLegacyTxWithGas(t, key, chainID, 128, &recipient, big.NewInt(1), nil, 21_000)
			block := blockContext(chainID)
			block.Number = 1
			block.GasLimit = 42_000
			result, err := executor.ExecuteBlock(t.Context(), BlockRequest{
				Context:    block,
				Txs:        []abevm.RawTx{stale, first, first, next, next},
				TxsOrdered: true,
			})
			require.NoError(t, err)
			defer result.Release()
			require.Len(t, result.Txs, 5)
			require.Len(t, result.Receipts, 5)
			require.Equal(t, uint64(42_000), result.GasUsed)
			for _, i := range []int{0, 2, 4} {
				require.ErrorIs(t, result.Txs[i].Err, core.ErrNonceTooLow)
				require.True(t, result.Txs[i].Rejected)
				require.Zero(t, result.Txs[i].GasUsed)
				require.Equal(t, ethtypes.ReceiptStatusFailed, result.Receipts[i].Status)
				require.Empty(t, result.Receipts[i].Logs)
				require.Equal(t, common.Address{}, result.Receipts[i].ContractAddress)
			}
			for i, cumulative := range []uint64{0, 21_000, 21_000, 42_000, 42_000} {
				require.Equal(t, cumulative, result.Receipts[i].CumulativeGasUsed)
				require.Equal(t, uint(i), result.Receipts[i].TransactionIndex)
			}
			view := store.OpenView()
			require.Equal(t, uint64(129), view.GetNonce(sender))
			require.Equal(t, big.NewInt(2), new(big.Int).SetBytes(view.GetBalance(recipient).Bytes()))
			wantBalance := new(big.Int).Sub(initialBalance, big.NewInt(42_000*testGasPriceWei+2))
			require.Equal(t, wantBalance, new(big.Int).SetBytes(view.GetBalance(sender).Bytes()))
			view.Close()
			_, err = receipts.GetReceipt(newReceiptContext(t.Context(), 1), decodeTx(t, stale).Hash())
			require.ErrorIs(t, err, receiptdb.ErrNotFound)
			firstHash := decodeTx(t, first).Hash()
			original, err := receipts.GetReceipt(newReceiptContext(t.Context(), 1), firstHash)
			require.NoError(t, err)
			require.Equal(t, uint32(ethtypes.ReceiptStatusSuccessful), original.Status)
			require.Equal(t, uint64(21_000), original.GasUsed)
			require.Empty(t, original.VmError)
			require.Equal(t, uint32(0), original.TransactionIndex)

			// A replay must preserve the receipt of the original execution.
			block.Number = 2
			replayed, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: block, Txs: []abevm.RawTx{first}})
			require.NoError(t, err)
			defer replayed.Release()
			require.True(t, replayed.ChangeSet.IsEmpty())
			require.Zero(t, replayed.GasUsed)
			latest, err := receipts.GetReceipt(newReceiptContext(t.Context(), 2), firstHash)
			require.NoError(t, err)
			require.Equal(t, original, latest)
		})
	}
}

func TestReceiptRecordsOmitRejectedAttempts(t *testing.T) {
	result := &BlockResult{
		Txs: []TxResult{
			{},
			{Rejected: true, Err: core.ErrNonceTooHigh},
			{Rejected: true, Err: core.ErrInsufficientFunds},
			{Rejected: true, Err: fmt.Errorf("replay: %w", core.ErrNonceTooLow)},
			{},
		},
	}
	for i := range result.Txs {
		result.Receipts = append(result.Receipts, &ethtypes.Receipt{TransactionIndex: uint(i)})
	}
	records, err := receiptRecords(finalizeReceiptTestBlock(t, 1, result))
	require.NoError(t, err)
	require.Len(t, records, 2)
	for i, record := range records {
		require.Equal(t, uint32(i), record.Receipt.TransactionIndex)
		require.Empty(t, record.Receipt.VmError)
	}
}
