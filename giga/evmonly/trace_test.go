package evmonly

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

// TestReplayStateAppliesEachPrefixAsTheBlockDid replays every prefix of a block holding a rejected
// transaction and checks the state each leaves, and that the whole block replays to the state its
// execution committed.
func TestReplayStateAppliesEachPrefixAsTheBlockDid(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := testAddress(0xb1)
	txs := [][]byte{
		signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(5), nil),
		// A spent nonce: the block rejects it, so it changes nothing.
		signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(9), nil),
		signLegacyTx(t, key, chainID, 1, &recipient, big.NewInt(6), nil),
	}
	newState := func() *MemoryState {
		state := NewMemoryState()
		state.SetBalance(sender, big.NewInt(testFundedBalanceWei))
		return state
	}
	cfg := Config{RejectUnappliableTxs: true}

	for _, tc := range []struct {
		name          string
		prefix        int
		wantRecipient uint64
		wantNonce     uint64
	}{
		{name: "no transactions", prefix: 0, wantRecipient: 0, wantNonce: 0},
		{name: "first transaction", prefix: 1, wantRecipient: 5, wantNonce: 1},
		{name: "rejected transaction changes nothing", prefix: 2, wantRecipient: 5, wantNonce: 1},
		{name: "whole block", prefix: 3, wantRecipient: 11, wantNonce: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := NewMemoryStore(newState()).OpenView()
			defer view.Close()

			replayed, err := NewExecutor(cfg).ReplayState(t.Context(), view, BlockRequest{
				Context: blockContext(chainID),
				Txs:     txs[:tc.prefix],
			})

			require.NoError(t, err)
			require.Equal(t, tc.wantRecipient, replayed.GetBalance(recipient).Uint64())
			require.Equal(t, tc.wantNonce, replayed.GetNonce(sender))
			// The replay stays in memory: the view still holds the state before the block.
			require.Zero(t, view.GetNonce(sender))
		})
	}

	// Executing the block commits what replaying all of it produces.
	state := newState()
	result, err := NewExecutor(cfg, withTestState(state)).ExecuteBlock(t.Context(), BlockRequest{
		Context: blockContext(chainID),
		Txs:     txs,
	})
	require.NoError(t, err)
	state.ApplyChangeSet(result.ChangeSet)
	require.Equal(t, big.NewInt(11), state.GetBalance(recipient))
	require.Equal(t, uint64(2), state.GetNonce(sender))
}

// TestReplayStateRefusesAnInvalidBlockContext refuses to replay transactions under a block context
// no block could execute at.
func TestReplayStateRefusesAnInvalidBlockContext(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	recipient := testAddress(0xb2)
	state := NewMemoryState()
	state.SetBalance(crypto.PubkeyToAddress(key.PublicKey), big.NewInt(testFundedBalanceWei))
	view := NewMemoryStore(state).OpenView()
	defer view.Close()
	blockCtx := blockContext(chainID)
	blockCtx.GasLimit = 0

	_, err = NewExecutor(Config{RejectUnappliableTxs: true}).ReplayState(t.Context(), view, BlockRequest{
		Context: blockCtx,
		Txs:     [][]byte{signLegacyTx(t, key, chainID, 0, &recipient, big.NewInt(1), nil)},
	})

	require.ErrorIs(t, err, errInvalidBlockGasLimit)
}

// TestReplayStateStopsATransactionWhenCanceled cancels a replay while a transaction whose gas alone
// would never stop it is running, and checks the replay ends with the cancellation.
func TestReplayStateStopsATransactionWhenCanceled(t *testing.T) {
	chainID := big.NewInt(testChainID)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	loop := testAddress(0xb3)
	const gas = 1_000_000_000_000
	state := NewMemoryState()
	state.SetBalance(crypto.PubkeyToAddress(key.PublicKey), new(big.Int).Mul(big.NewInt(gas), big.NewInt(testGasPriceWei)))
	state.SetCode(loop, infiniteLoopCode())
	view := NewMemoryStore(state).OpenView()
	defer view.Close()
	blockCtx := blockContext(chainID)
	blockCtx.GasLimit = gas
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	_, err = NewExecutor(Config{RejectUnappliableTxs: true}).ReplayState(ctx, view, BlockRequest{
		Context: blockCtx,
		Txs:     [][]byte{signLegacyTxWithGas(t, key, chainID, 0, &loop, big.NewInt(0), nil, gas)},
	})

	require.ErrorIs(t, err, context.Canceled)
}
