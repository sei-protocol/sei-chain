package evmonly

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/core"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// TestExecutorRejectsSpentNonceWithoutFailingBlock reproduces the fault that halted
// giga-testnet-2 at block 296796: one sender's nonce 178 appeared in a block after
// nonce 178 had already been spent, and the executor answered by failing the block,
// which panicked every validator at the same height.
//
// Autobahn orders transactions without checking nonces, so the chain cannot avoid
// producing such a block. The transaction has to become a receipt.
func TestExecutorRejectsSpentNonceWithoutFailingBlock(t *testing.T) {
	for _, occ := range []bool{false, true} {
		name := "sequential"
		if occ {
			name = "occ"
		}
		t.Run(name, func(t *testing.T) {
			chainID := big.NewInt(testChainID)
			key, err := crypto.GenerateKey()
			require.NoError(t, err)
			sender := crypto.PubkeyToAddress(key.PublicKey)
			recipient := testAddress(0xa1)

			state := NewMemoryState()
			state.SetBalance(sender, big.NewInt(testFundedBalanceWei))

			// The block carries nonce 0, then 0 again, then 1. The middle transaction is
			// unappliable by the time it runs; the third must still execute.
			txs := [][]byte{
				signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(5), nil),
				signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(9), nil),
				signLegacyTx(t, key, chainID, 1, &recipient, big.NewInt(6), nil),
			}
			cfg := Config{RejectUnappliableTxs: true}
			if occ {
				cfg.ParseWorkers = 4
			}
			executor := NewExecutor(cfg, withTestState(state))

			result, err := executor.ExecuteBlock(t.Context(), BlockRequest{
				Context: blockContext(chainID),
				Txs:     txs,
			})

			// The block executes. Before the fix this returned an error, which
			// FinalizeBlock turned into a node panic.
			require.NoError(t, err)
			require.Len(t, result.Txs, 3, "every transaction in the block needs a result")
			require.Len(t, result.Receipts, 3, "every transaction in the block needs a receipt")

			require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[0].Status)
			require.False(t, result.Txs[0].Rejected)

			rejected := result.Txs[1]
			require.True(t, rejected.Rejected, "the spent-nonce transaction is rejected")
			require.Equal(t, ethtypes.ReceiptStatusFailed, rejected.Status)
			require.Zero(t, rejected.GasUsed, "a rejected transaction consumes no gas")
			require.ErrorContains(t, rejected.Err, "nonce too low")
			require.Equal(t, ethtypes.ReceiptStatusFailed, result.Receipts[1].Status)
			require.Zero(t, result.Receipts[1].GasUsed)
			require.Empty(t, result.Receipts[1].Logs)

			require.Equal(t, ethtypes.ReceiptStatusSuccessful, result.Txs[2].Status,
				"a rejected transaction must not stop the rest of the block")
			require.False(t, result.Txs[2].Rejected)

			// Gas accounts for the two that ran and nothing for the one that did not.
			require.Equal(t, uint64(2*21_000), result.GasUsed)

			// The rejected transaction moved no value and consumed no nonce. Its 9 wei
			// never leaves the sender; only the 5 and the 6 do.
			state.ApplyChangeSet(result.ChangeSet)
			require.Equal(t, big.NewInt(11), state.GetBalance(recipient))
			require.Equal(t, uint64(2), state.GetNonce(sender))
		})
	}
}

// TestExecutorRejectsUnderfundedTxWithoutFailingBlock covers the other pre-check
// that reaches a block on a chain whose consensus does not price transactions.
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
	require.ErrorContains(t, result.Txs[0].Err, "insufficient funds")
	require.False(t, result.Txs[1].Rejected)
	require.Equal(t, uint64(21_000), result.GasUsed)

	// The sender's one wei is still there: BuyGas debits before the block's gas is
	// claimed, so the revert is what keeps the changeset honest.
	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, big.NewInt(1), state.GetBalance(poor))
	require.Equal(t, uint64(0), state.GetNonce(poor))
	require.Equal(t, big.NewInt(4), state.GetBalance(recipient))
}

// TestRejectedTxCountsAsItsOwnStatus keeps a rejected transaction out of the
// reverted bucket, so a chain quietly rejecting traffic is visible rather than
// looking like ordinary contract failure.
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

// TestExecutorDefaultStillAbortsOnUnappliableTx keeps geth parity for callers that
// have it: under Ethereum's rules a block holding a spent nonce is an invalid block,
// and the executor is also used as a geth-parity execution boundary. Only a caller
// whose ordering layer skips these checks opts out.
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
