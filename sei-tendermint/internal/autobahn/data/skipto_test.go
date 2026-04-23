package data

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils/require"
)

// TestState_SkipTo_FreshAdvance: on a freshly constructed State, SkipTo(n)
// should advance all cursors to n. This is the giga state-sync use case:
// after the app is at height M via snapshot, call SkipTo(M+1) on the local
// fresh data state so subsequent peer-streamed blocks insert starting at
// M+1 instead of genesis.
func TestState_SkipTo_FreshAdvance(t *testing.T) {
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 3)
	state := utils.OrPanic1(NewState(&Config{Committee: committee},
		utils.OrPanic1(NewDataWAL(utils.None[string](), committee))))

	// Sanity: fresh state starts at committee.FirstBlock().
	require.Equal(t, committee.FirstBlock(), state.NextBlock())

	target := committee.FirstBlock() + 1000
	state.SkipTo(target)

	// Every cursor visible through the public API should now reflect
	// target. We only check NextBlock() (public); the other cursors
	// (first, nextAppProposal, nextQC, nextBlockToPersist) are verified
	// implicitly by the fact that subsequent Push* calls would fail
	// with a gap error if any cursor was left behind.
	require.Equal(t, target, state.NextBlock())
}

// TestState_SkipTo_NoBackwards: SkipTo with a target <= current first is
// a no-op (never rewinds). Callers shouldn't rely on this but we guarantee
// it so an accidental call on a state that's already advanced doesn't
// corrupt state.
func TestState_SkipTo_NoBackwards(t *testing.T) {
	rng := utils.TestRng()
	committee, _ := types.GenCommittee(rng, 3)
	state := utils.OrPanic1(NewState(&Config{Committee: committee},
		utils.OrPanic1(NewDataWAL(utils.None[string](), committee))))

	// First advance to some height.
	state.SkipTo(committee.FirstBlock() + 500)
	after := state.NextBlock()

	// SkipTo at or below first must not rewind.
	state.SkipTo(committee.FirstBlock())
	require.Equal(t, after, state.NextBlock())

	state.SkipTo(committee.FirstBlock() + 100)
	require.Equal(t, after, state.NextBlock())
}
