package evmonlyapp

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rpc"

	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/proxy"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// traceTestHeader returns the header a test block at height carries, with parentHash the hash of
// the block before it.
func traceTestHeader(height int64, parentHash common.Hash) proxy.BlockRef {
	return proxy.BlockRef{
		Height:     atypes.RoadIndex(height),
		Time:       uint64(evmOnlyTestBlockTime(height).Unix()), //nolint:gosec // G115: test times are positive.
		ParentHash: parentHash,
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
	first := evmOnlyTestBlock(app, 1, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 0))
	finalizeEVMOnlyTestBlock(t, app, first)
	block := [][]byte{
		signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 1),
		// A spent nonce: the block rejects it, so it changes nothing.
		signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 1),
		signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 2),
	}
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 2, block...))
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
			state, err := app.EvmStateBeforeTx(t.Context(), header, block[:tc.prefix])
			require.NoError(t, err)
			defer state.Close()
			require.Equal(t, tc.wantNonce, state.GetNonce(sender))
			// The recipient was funded with the default balance and received 1 wei per transfer.
			got := new(big.Int).Sub(state.GetBalance(recipient).ToBig(), evmOnlyBaseBalance)
			require.Equal(t, 0, big.NewInt(tc.wantRecipient).Cmp(got))
		})
	}

	// Replaying the whole block reaches the state the block committed, fees credited to the coinbase
	// included.
	replayed, err := app.EvmStateBeforeTx(t.Context(), header, block)
	require.NoError(t, err)
	defer replayed.Close()
	committed, err := app.EvmStateAt(t.Context(), 2)
	require.NoError(t, err)
	defer committed.Close()
	for _, address := range []common.Address{sender, recipient, {}} {
		require.Equal(t, committed.GetNonce(address), replayed.GetNonce(address))
		require.Equal(t, 0, committed.GetBalance(address).Cmp(replayed.GetBalance(address)))
	}
}

// The block context rebuilt for a past block is the one the block executed at.
func TestEVMOnlyApplicationRebuildsABlockContext(t *testing.T) {
	app := newUndoLogEVMOnlyTestApp(t)
	first := evmOnlyTestBlock(app, 1, oneEVMOnlyTestTx(t))
	finalizeEVMOnlyTestBlock(t, app, first)
	header := traceTestHeader(2, first.Hash)

	got, err := app.EvmBlockContext(header)

	require.NoError(t, err)
	require.Equal(t, uint64(2), got.BlockNumber.Uint64())
	require.Equal(t, header.Time, got.Time)
	require.Equal(t, uint64(30_000_000), got.GasLimit)
	require.Equal(t, common.Address{}, got.Coinbase)
	require.Equal(t, 0, got.BaseFee.Sign())
	require.Equal(t, evmOnlyPrevRandao(header.Time), *got.Random)
	require.Equal(t, first.Hash, got.GetHash(1))
	require.Equal(t, common.Hash{}, got.GetHash(0))
}

// A state no store holds, and the parent of block 0, are refused rather than served empty.
func TestEVMOnlyApplicationRefusesStateItDoesNotHold(t *testing.T) {
	t.Run("full node", func(t *testing.T) {
		app := newUndoLogEVMOnlyTestApp(t)
		finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 1, oneEVMOnlyTestTx(t)))
		app.storage.StateDB().UndoSS().WaitForPendingWrites()

		_, err := app.EvmStateAt(t.Context(), 9)
		require.Error(t, err)
		_, err = app.EvmStateBeforeTx(t.Context(), traceTestHeader(0, common.Hash{}), nil)
		require.ErrorIs(t, err, errTraceGenesis)
	})

	t.Run("validator traces only the latest block's state", func(t *testing.T) {
		app := newInitializedEVMOnlyTestApp(t).(*evmOnlyApplication)
		first := evmOnlyTestBlock(app, 1)
		finalizeEVMOnlyTestBlock(t, app, first)
		finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 2))

		state, err := app.EvmStateAt(t.Context(), 2)
		require.NoError(t, err)
		state.Close()
		require.NotNil(t, state)
		// Without a state store there is no state after block 1 to replay block 2 from.
		_, err = app.EvmStateBeforeTx(t.Context(), traceTestHeader(2, first.Hash), nil)
		require.Error(t, err)

		// Once block 3 commits, the state after block 2 is no longer held, and the latest one is.
		finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 3))
		_, err = app.EvmStateAt(t.Context(), 2)
		require.Error(t, err)
		latest, err := app.EvmStateAt(t.Context(), rpc.LatestBlockNumber.Int64())
		require.NoError(t, err)
		latest.Close()
		require.NotNil(t, latest)
	})
}

// Block 1 traces against the state the chain started from.
func TestEVMOnlyApplicationReplaysTheFirstBlock(t *testing.T) {
	app := newUndoLogEVMOnlyTestApp(t)
	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	sender := crypto.PubkeyToAddress(key.PublicKey)
	finalizeEVMOnlyTestBlock(t, app, evmOnlyTestBlock(app, 1, signedEVMOnlyTestTxFrom(t, key, evmOnlyTestChainID, 0)))
	app.storage.StateDB().UndoSS().WaitForPendingWrites()

	state, err := app.EvmStateBeforeTx(t.Context(), traceTestHeader(1, common.Hash{}), nil)

	require.NoError(t, err)
	defer state.Close()
	require.Zero(t, state.GetNonce(sender))
}
