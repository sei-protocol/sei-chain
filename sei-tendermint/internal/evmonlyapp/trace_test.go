package evmonlyapp

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// traceTestHeader returns the header a test block at height carries, with parentHash the hash of
// the block before it.
func traceTestHeader(height int64, parentHash []byte) *ethtypes.Header {
	req := evmOnlyTestBlock(height)
	return &ethtypes.Header{
		Number:     big.NewInt(height),
		Time:       uint64(req.Header.Time.Unix()), //nolint:gosec // G115: test times are positive.
		ParentHash: common.BytesToHash(parentHash),
	}
}

// oneEVMOnlyTestTx returns a funded transfer, so the block holding it changes EVM state.
func oneEVMOnlyTestTx(t *testing.T) []byte {
	t.Helper()
	raw, _ := signedEVMOnlyTestTx(t, evmOnlyTestChainID, 0)
	return raw
}

// A node keeping EVM history rebuilds the state every transaction of a past block executed against,
// skipping a transaction the block rejected, and replays the whole block to the state it committed.
func TestEVMOnlyApplicationReplaysTheStateBeforeEachTransaction(t *testing.T) {
	app := newUndoLogEVMOnlyTestApp(t)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	recipient := common.HexToAddress("0x1000000000000000000000000000000000000001")
	first := evmOnlyTestBlock(1, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 0))
	finalizeAndCommitEVMOnlyTestBlock(t, app, first)
	block := [][]byte{
		signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 1),
		// A spent nonce: the block rejects it, so it changes nothing.
		signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 1),
		signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 2),
	}
	finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(2, block...))
	// The undo log applies blocks behind the commit; until it has, their history is not held.
	app.storage.StateDB().UndoSS().WaitForPendingWrites()
	header := traceTestHeader(2, first.Hash)

	for _, tc := range []struct {
		name          string
		prefix        int
		wantNonce     uint64
		wantRecipient int64
	}{
		{name: "first transaction", prefix: 0, wantNonce: 1, wantRecipient: 1},
		{name: "rejected transaction", prefix: 1, wantNonce: 2, wantRecipient: 2},
		{name: "last transaction", prefix: 2, wantNonce: 2, wantRecipient: 2},
		{name: "after the block", prefix: 3, wantNonce: 3, wantRecipient: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state, release, err := app.EvmStateBeforeTx(t.Context(), header, block[:tc.prefix])
			require.NoError(t, err)
			defer release()
			require.Equal(t, tc.wantNonce, state.GetNonce(sender))
			// The recipient was funded with the default balance and received 1 wei per transfer.
			got := new(big.Int).Sub(state.GetBalance(recipient).ToBig(), evmOnlyBaseBalance)
			require.Equal(t, 0, big.NewInt(tc.wantRecipient).Cmp(got))
		})
	}

	// Replaying the whole block reaches the state the block committed, fees credited to the coinbase
	// included.
	replayed, releaseReplayed, err := app.EvmStateBeforeTx(t.Context(), header, block)
	require.NoError(t, err)
	defer releaseReplayed()
	committed, releaseCommitted, err := app.EvmStateAt(t.Context(), 2)
	require.NoError(t, err)
	defer releaseCommitted()
	for _, address := range []common.Address{sender, recipient, {}} {
		require.Equal(t, committed.GetNonce(address), replayed.GetNonce(address))
		require.Equal(t, 0, committed.GetBalance(address).Cmp(replayed.GetBalance(address)))
	}
}

// The block context rebuilt for a past block is the one the block executed at.
func TestEVMOnlyApplicationRebuildsABlockContext(t *testing.T) {
	app := newUndoLogEVMOnlyTestApp(t)
	first := evmOnlyTestBlock(1, oneEVMOnlyTestTx(t))
	finalizeAndCommitEVMOnlyTestBlock(t, app, first)
	header := traceTestHeader(2, first.Hash)

	got, err := app.EvmBlockContext(header)

	require.NoError(t, err)
	require.Equal(t, uint64(2), got.BlockNumber.Uint64())
	require.Equal(t, header.Time, got.Time)
	require.Equal(t, uint64(30_000_000), got.GasLimit)
	require.Equal(t, common.Address{}, got.Coinbase)
	require.Equal(t, 0, got.BaseFee.Sign())
	require.Equal(t, evmOnlyPrevRandao(header.Time), *got.Random)
	require.Equal(t, common.BytesToHash(first.Hash), got.GetHash(1))
	require.Equal(t, common.Hash{}, got.GetHash(0))
}

// A state no store holds, and the parent of block 0, are refused rather than served empty.
func TestEVMOnlyApplicationRefusesStateItDoesNotHold(t *testing.T) {
	t.Run("full node", func(t *testing.T) {
		app := newUndoLogEVMOnlyTestApp(t)
		finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(1, oneEVMOnlyTestTx(t)))
		app.storage.StateDB().UndoSS().WaitForPendingWrites()

		_, _, err := app.EvmStateAt(t.Context(), 9)
		require.Error(t, err)
		_, _, err = app.EvmStateBeforeTx(t.Context(), traceTestHeader(0, nil), nil)
		require.ErrorIs(t, err, errTraceGenesis)
	})

	t.Run("validator traces only the latest block's state", func(t *testing.T) {
		app := newInitializedEVMOnlyTestApp(t).(*evmOnlyApplication)
		first := evmOnlyTestBlock(1)
		finalizeAndCommitEVMOnlyTestBlock(t, app, first)
		finalizeAndCommitEVMOnlyTestBlock(t, app, evmOnlyTestBlock(2))

		state, release, err := app.EvmStateAt(t.Context(), 2)
		require.NoError(t, err)
		release()
		require.NotNil(t, state)
		// Without a state store there is no state after block 1 to replay block 2 from.
		_, _, err = app.EvmStateBeforeTx(t.Context(), traceTestHeader(2, first.Hash), nil)
		require.Error(t, err)
	})
}
