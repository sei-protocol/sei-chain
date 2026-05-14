package app

import (
	"testing"

	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

// TestFinalizeBlocker_ClearsStaleHeadEvent pins the defensive clear at
// FinalizeBlocker entry: a leftover pendingHeadEvent (from a prior
// FinalizeBlock that didn't reach a successful Commit, or from a return
// path that didn't stash) must not survive into a subsequent block. The
// invariant matters because App.Commit publishes whatever stash it
// finds; a stale tuple would announce the wrong block.
func TestFinalizeBlocker_ClearsStaleHeadEvent(t *testing.T) {
	app := Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{Height: 1, ChainID: "sei-test"})

	// Drive the optimistic + EthReplay early-return path: FinalizeBlocker
	// returns an empty response without calling stashPendingHead, so only
	// the entry-clear logic can prevent the stale stash from being
	// republished by a later Commit.
	hash := []byte("current-hash")
	completion := make(chan struct{}, 1)
	completion <- struct{}{}
	app.optimisticProcessingInfoMutex.Lock()
	app.optimisticProcessingInfo = OptimisticProcessingInfo{
		Height:     ctx.BlockHeight(),
		Hash:       hash,
		Completion: completion,
		Aborted:    false,
	}
	app.optimisticProcessingInfoMutex.Unlock()

	originalEthReplayEnabled := app.EvmKeeper.EthReplayConfig.Enabled
	app.EvmKeeper.EthReplayConfig.Enabled = true
	defer func() { app.EvmKeeper.EthReplayConfig.Enabled = originalEthReplayEnabled }()

	app.pendingHeadEvent = &pendingHeadEvent{
		hash:     []byte("stale-hash"),
		header:   &tmproto.Header{Height: 0},
		response: &abci.ResponseFinalizeBlock{},
	}

	_, err := app.FinalizeBlocker(ctx, &abci.RequestFinalizeBlock{
		Hash:   hash,
		Header: &tmproto.Header{Height: 1, ChainID: "sei-test"},
	})
	require.NoError(t, err)
	require.Nil(t, app.pendingHeadEvent, "FinalizeBlocker must clear stale pendingHeadEvent on entry")
}
