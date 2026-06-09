package app

import (
	"testing"

	"github.com/sei-protocol/sei-chain/evmrpc"
	abci "github.com/sei-protocol/sei-chain/sei-tendermint/abci/types"
	tmutils "github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

// TestFinalizeBlocker_ClearsStaleHeadEvent pins the defensive
// ClearStash at FinalizeBlocker entry: a leftover stash (from a prior
// FinalizeBlock that didn't reach a successful Commit, or from a return
// path that didn't Stash) must not survive into a subsequent block. The
// invariant matters because App.Commit publishes whatever the notifier
// has stashed; a stale tuple would announce the wrong block.
func TestFinalizeBlocker_ClearsStaleHeadEvent(t *testing.T) {
	app := Setup(t, false, false, false)
	ctx := app.BaseApp.NewContext(false, tmproto.Header{Height: 1, ChainID: "sei-test"})

	// Setup() does not enable Autobahn, so the notifier is nil. Install
	// one for this test so the defensive ClearStash actually has state
	// to act on.
	notifier := evmrpc.NewBlockHeaderNotifier(1)
	app.blockHeaderNotifier = tmutils.Some(notifier)

	// Drive the optimistic + EthReplay early-return path: FinalizeBlocker
	// returns an empty response without calling Stash, so only the
	// entry-clear logic can prevent the stale stash from being
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

	notifier.Stash(&abci.RequestFinalizeBlock{
		Hash:   []byte("stale-hash"),
		Header: &tmproto.Header{Height: 0},
	}, &abci.ResponseFinalizeBlock{})

	_, err := app.FinalizeBlocker(ctx, &abci.RequestFinalizeBlock{
		Hash:   hash,
		Header: &tmproto.Header{Height: 1, ChainID: "sei-test"},
	})
	require.NoError(t, err)

	// PublishStashed reports false iff the notifier had nothing to
	// publish — i.e. FinalizeBlocker's entry ClearStash dropped the
	// stale tuple as required.
	require.False(t, notifier.PublishStashed(), "FinalizeBlocker must clear stale stash on entry")
}
