package evmonly

import (
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
			executor := NewExecutor(Config{OCCWorkers: workers}, withTestStores(store, receipts, store.EncodeChangeSet))
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
				Context: block,
				Txs:     [][]byte{stale, first, first, next, next},
			})
			require.NoError(t, err)
			defer result.Release()
			require.Len(t, result.Txs, 5)
			require.Len(t, result.Receipts, 5)
			require.Equal(t, uint64(42_000), result.GasUsed)
			for _, i := range []int{0, 2, 4} {
				require.ErrorIs(t, result.Txs[i].Err, core.ErrNonceTooLow)
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
			staleReceipt, err := receipts.GetReceipt(newReceiptContext(t.Context(), 1), decodeTx(t, stale).Hash())
			require.NoError(t, err)
			require.Equal(t, uint32(ethtypes.ReceiptStatusFailed), staleReceipt.Status)
			require.Zero(t, staleReceipt.GasUsed)
			require.NotEmpty(t, staleReceipt.VmError)
			firstHash := decodeTx(t, first).Hash()
			original, err := receipts.GetReceipt(newReceiptContext(t.Context(), 1), firstHash)
			require.NoError(t, err)
			require.Equal(t, uint32(ethtypes.ReceiptStatusFailed), original.Status)
			require.Zero(t, original.GasUsed)
			require.NotEmpty(t, original.VmError)
			require.Equal(t, uint32(2), original.TransactionIndex)

			// A replay in a later block gets its own failed receipt. Hash lookups
			// follow the receipt store's latest-write semantics.
			block.Number = 2
			replayed, err := executor.ExecuteBlock(t.Context(), BlockRequest{Context: block, Txs: [][]byte{first}})
			require.NoError(t, err)
			defer replayed.Release()
			require.Empty(t, replayed.ChangeSet)
			require.Zero(t, replayed.GasUsed)
			latest, err := receipts.GetReceipt(newReceiptContext(t.Context(), 2), firstHash)
			require.NoError(t, err)
			require.Equal(t, uint64(2), latest.BlockNumber)
			require.Equal(t, uint32(0), latest.TransactionIndex)
			require.Equal(t, uint32(ethtypes.ReceiptStatusFailed), latest.Status)
			require.Zero(t, latest.GasUsed)
			require.Zero(t, latest.CumulativeGasUsed)
			require.NotEmpty(t, latest.VmError)
		})
	}
}

func TestReceiptRecordsIncludeStaleNonces(t *testing.T) {
	for _, count := range []int{2, occParallelReceiptThreshold} {
		t.Run(fmt.Sprintf("count=%d", count), func(t *testing.T) {
			executor := NewExecutor(Config{OCCWorkers: 4})
			t.Cleanup(executor.Close)
			result := &BlockResult{}
			for i := range count {
				txResult := TxResult{}
				if i%2 == 1 {
					txResult.Err = fmt.Errorf("replay: %w", core.ErrNonceTooLow)
				}
				result.Txs = append(result.Txs, txResult)
				result.Receipts = append(result.Receipts, &ethtypes.Receipt{TransactionIndex: uint(i)})
			}
			records, err := executor.receiptRecordsParallel(t.Context(), 1, result)
			require.NoError(t, err)
			require.Len(t, records, count)
			for i, record := range records {
				require.Equal(t, uint32(i), record.Receipt.TransactionIndex)
				if i%2 == 1 {
					require.Equal(t, "replay: "+core.ErrNonceTooLow.Error(), record.Receipt.VmError)
					require.Zero(t, record.Receipt.GasUsed)
				}
			}
		})
	}
}
