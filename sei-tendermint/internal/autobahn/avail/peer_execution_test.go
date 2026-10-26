package avail

import (
	"context"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/data"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// A peer's AppVote records how far it executed before PushAppVote waits for, or drops on, its CommitQC.
func TestPushAppVoteRecordsPeerExecutionFirst(t *testing.T) {
	rng := utils.TestRng()
	registry, keys := epoch.GenRegistry(rng, 3)
	state := utils.OrPanic1(NewState(keys[0], newTestDataState(&data.Config{Registry: registry}), utils.None[string]()))
	push := func(key types.SecretKey, vote *types.AppVote) {
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
		defer cancel()
		_ = state.PushAppVote(ctx, types.Sign(key, vote))
	}

	low, high := types.GenAppVote(rng), types.GenAppVote(rng)
	if high.Proposal().GlobalRange().Next < low.Proposal().GlobalRange().Next {
		low, high = high, low
	}
	push(keys[1], high)
	push(keys[1], low)
	push(keys[0], high)
	push(types.GenSecretKey(rng), high)

	got := state.PeerExecutions()
	require.Equal(t, 1, len(got))
	require.Equal(t, high.Proposal().GlobalRange().Next, got[keys[1].Public()].Next)
}
