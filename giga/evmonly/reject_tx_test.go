package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// TestExecutorRejectsSpentNonceWithoutFailingBlock turns a spent nonce into a
// failed receipt while the rest of the block still executes, on both paths.
func TestExecutorRejectsSpentNonceWithoutFailingBlock(t *testing.T) {
	forEachExecutionPath(t, func(t *testing.T, cfg Config) {
		chainID := big.NewInt(testChainID)
		key, err := crypto.GenerateKey()
		require.NoError(t, err)
		sender := crypto.PubkeyToAddress(key.PublicKey)
		recipient := testAddress(0xa1)

		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(testFundedBalanceWei))

		txs := [][]byte{
			signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(5), nil),
			signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(9), nil),
			signLegacyTx(t, key, chainID, 1, &recipient, big.NewInt(6), nil),
		}
		executor := NewExecutor(cfg, withTestState(state))

		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{
			Context: blockContext(chainID),
			Txs:     txs,
		})

		require.NoError(t, err)
		require.Len(t, result.Txs, 3)
		require.Len(t, result.Receipts, 3)
		requireOCCRan(t, cfg, result)

		require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)
		require.False(t, result.Txs[0].Rejected)

		rejected := result.Txs[1]
		require.True(t, rejected.Rejected)
		require.Equal(t, ethtypes.ReceiptStatusFailed, rejected.Status)
		require.Zero(t, rejected.GasUsed)
		require.ErrorIs(t, rejected.Err, core.ErrNonceTooLow)
		require.Equal(t, ethtypes.ReceiptStatusFailed, result.Receipts[1].Status)
		require.Zero(t, result.Receipts[1].GasUsed)
		require.Empty(t, result.Receipts[1].Logs)

		require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[2].Status)
		require.False(t, result.Txs[2].Rejected)
		require.Equal(t, uint64(2*21_000), result.GasUsed)

		state.ApplyChangeSet(result.ChangeSet)
		require.Equal(t, big.NewInt(11), state.GetBalance(recipient))
		require.Equal(t, uint64(2), state.GetNonce(sender))
	})
}

// TestExecutorRejectsTxOverBlockGasWithoutFailingBlock records a transaction
// that exceeds the remaining block gas while preserving earlier execution.
func TestExecutorRejectsTxOverBlockGasWithoutFailingBlock(t *testing.T) {
	forEachExecutionPath(t, func(t *testing.T, cfg Config) {
		chainID := big.NewInt(testChainID)
		recipient := testAddress(0xa4)
		key0, err := crypto.GenerateKey()
		require.NoError(t, err)
		key1, err := crypto.GenerateKey()
		require.NoError(t, err)

		state := NewMemoryState()
		state.SetBalance(crypto.PubkeyToAddress(key0.PublicKey), big.NewInt(testFundedBalanceWei))
		state.SetBalance(crypto.PubkeyToAddress(key1.PublicKey), big.NewInt(testFundedBalanceWei))

		blockCtx := blockContext(chainID)
		blockCtx.GasLimit = 30_000
		executor := NewExecutor(cfg, withTestState(state))
		result, err := executor.ExecuteBlock(t.Context(), BlockRequest{
			Context: blockCtx,
			Txs: [][]byte{
				signLegacyTxWithGas(t, key0, chainID, 0, &recipient, big.NewInt(1), nil, 21_000),
				signLegacyTxWithGas(t, key1, chainID, 0, &recipient, big.NewInt(1), nil, 21_000),
			},
		})

		require.NoError(t, err)
		requireOCCRan(t, cfg, result)
		require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)
		require.False(t, result.Txs[0].Rejected)
		require.True(t, result.Txs[1].Rejected)
		require.ErrorIs(t, result.Txs[1].Err, core.ErrGasLimitReached)
		require.Equal(t, uint64(21_000), result.GasUsed)
	})
}

// TestExecutorRejectionRestoresBlockGasPool ensures a failed pre-check does not
// consume block gas needed by later transactions.
func TestExecutorRejectionRestoresBlockGasPool(t *testing.T) {
	chainID := big.NewInt(testChainID)
	recipient := testAddress(0xa5)
	key0, err := crypto.GenerateKey()
	require.NoError(t, err)
	key1, err := crypto.GenerateKey()
	require.NoError(t, err)
	key2, err := crypto.GenerateKey()
	require.NoError(t, err)

	state := NewMemoryState()
	state.SetBalance(crypto.PubkeyToAddress(key0.PublicKey), big.NewInt(testFundedBalanceWei))
	state.SetBalance(crypto.PubkeyToAddress(key1.PublicKey), big.NewInt(testFundedBalanceWei))
	state.SetBalance(crypto.PubkeyToAddress(key2.PublicKey), big.NewInt(testFundedBalanceWei))

	data := make([]byte, 100)
	for i := range data {
		data[i] = 1
	}
	blockCtx := blockContext(chainID)
	blockCtx.GasLimit = 42_000
	executor := NewExecutor(Config{RejectUnappliableTxs: true}, withTestState(state))
	result, err := executor.ExecuteBlock(t.Context(), BlockRequest{
		Context: blockCtx,
		Txs: [][]byte{
			signLegacyTxWithGas(t, key0, chainID, 0, &recipient, big.NewInt(1), data, 21_000),
			signLegacyTxWithGas(t, key1, chainID, 0, &recipient, big.NewInt(1), nil, 21_000),
			signLegacyTxWithGas(t, key2, chainID, 0, &recipient, big.NewInt(1), nil, 21_000),
		},
	})

	require.NoError(t, err)
	require.True(t, result.Txs[0].Rejected)
	require.ErrorIs(t, result.Txs[0].Err, core.ErrIntrinsicGas)
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[1].Status)
	require.False(t, result.Txs[1].Rejected)
	require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[2].Status)
	require.False(t, result.Txs[2].Rejected)
	require.Equal(t, uint64(42_000), result.GasUsed)
}

// TestExecutorRejectsUnderfundedTxWithoutFailingBlock turns insufficient funds
// into a failed receipt while the rest of the block still executes.
func TestExecutorRejectsUnderfundedTxWithoutFailingBlock(t *testing.T) {
	chainID := big.NewInt(testChainID)
	poorKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	richKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	poor := crypto.PubkeyToAddress(poorKey.PublicKey)
	rich := crypto.PubkeyToAddress(richKey.PublicKey)
	recipient := testAddress(0xa2)

	state := NewMemoryState()
	state.SetBalance(rich, big.NewInt(testFundedBalanceWei))
	state.SetBalance(poor, big.NewInt(1))

	executor := NewExecutor(Config{RejectUnappliableTxs: true}, withTestState(state))
	result, err := executor.ExecuteBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs: [][]byte{
			signLegacyTx(t, poorKey, chainID, 0, &recipient, big.NewInt(1), nil),
			signLegacyTx(t, richKey, chainID, 0, &recipient, big.NewInt(4), nil),
		},
	})

	require.NoError(t, err)
	require.True(t, result.Txs[0].Rejected)
	require.ErrorIs(t, result.Txs[0].Err, core.ErrInsufficientFunds)
	require.False(t, result.Txs[1].Rejected)
	require.Equal(t, uint64(21_000), result.GasUsed)

	// The sender's one wei is still there: BuyGas debits before the block's gas is
	// claimed, so the revert is what keeps the changeset honest.
	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, big.NewInt(1), state.GetBalance(poor))
	require.Equal(t, uint64(0), state.GetNonce(poor))
	require.Equal(t, big.NewInt(4), state.GetBalance(recipient))
}

// TestRejectedTxCountsAsItsOwnStatus keeps rejected transactions distinct from
// ordinary reverted transactions in execution metrics.
func TestRejectedTxCountsAsItsOwnStatus(t *testing.T) {
	require.Equal(t, txExecutionStatusRejected,
		txExecutionStatus(TxResult{Status: ethtypes.ReceiptStatusFailed, Rejected: true}))
	require.Equal(t, txExecutionStatusReverted,
		txExecutionStatus(TxResult{Status: ethtypes.ReceiptStatusFailed}))
	require.Equal(t, txExecutionStatusSuccess,
		txExecutionStatus(TxResult{Status: ethtypes.ReceiptStatusSuccessful}))
	require.Contains(t, txExecutionStatuses, txExecutionStatusRejected,
		"the status must be in the vocabulary or it is never reported as zero")
}

// TestExecutorDefaultStillAbortsOnUnappliableTx preserves geth's invalid-block
// behavior when rejected transactions are disabled.
func TestExecutorDefaultStillAbortsOnUnappliableTx(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xa3)

	state := NewMemoryState()
	state.SetBalance(sender, big.NewInt(testFundedBalanceWei))
	state.SetNonce(sender, 1)

	executor := NewExecutor(Config{}, withTestState(state))
	result, err := executor.ExecuteBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     [][]byte{signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)},
	})

	require.Error(t, err)
	require.ErrorIs(t, err, core.ErrNonceTooLow)
	require.Nil(t, result)
}

func forEachExecutionPath(t *testing.T, run func(t *testing.T, cfg Config)) {
	t.Helper()
	for _, occ := range []bool{false, true} {
		name := "sequential"
		if occ {
			name = "occ"
		}
		t.Run(name, func(t *testing.T) {
			cfg := Config{RejectUnappliableTxs: true}
			if occ {
				cfg.OCCWorkers = 4
			}
			run(t, cfg)
		})
	}
}

func requireOCCRan(t *testing.T, cfg Config, result *BlockResult) {
	t.Helper()
	if cfg.OCCWorkers == 0 {
		return
	}
	require.True(t, result.OCCStats.Attempted)
	require.False(t, result.OCCStats.Fallback)
	require.Empty(t, result.OCCStats.FallbackReason)
}
