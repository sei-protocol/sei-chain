package gigasim

import (
	"testing"

	"github.com/stretchr/testify/require"

	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
)

// readCountingView counts the reads it serves and finds nothing, which is what makes a read that was
// answered elsewhere visible. It embeds StateView without implementing it, so a method the tests do not
// expect to be called panics on the nil interface rather than answering with a zero value.
type readCountingView struct {
	gigatypes.StateView

	reads int
}

func (v *readCountingView) Get(_ string, _ []byte) ([]byte, bool) {
	v.reads++
	return nil, false
}

func (v *readCountingView) GetBlockHeight() int64 { return 0 }

func (v *readCountingView) Close() {}

// newTestState returns an execution state over a view that serves no reads, which is enough for
// anything that does not commit.
func newTestState() (*executionState, *readCountingView) {
	view := &readCountingView{}
	return &executionState{view: view, setupWrites: newStateBatch(0)}, view
}

// TestReadsAlwaysReachTheStateDB pins the property the benchmark's fidelity rests on: no read is
// served from memory, not even one whose key this block writes.
//
// The regression it guards against is real and shipped once: Get answered from the block being
// executed, and because every transaction reads the fee account while every transaction writes it,
// roughly a sixth of a run's reads never reached the DB at all.
func TestReadsAlwaysReachTheStateDB(t *testing.T) {
	t.Parallel()

	state, view := newTestState()
	key := testAccountKey(1)
	state.Put(key, []byte("staged"))

	value, found := state.Get(key)
	require.Nil(t, value)
	require.False(t, found, "the view serves no reads, so a read that reached it cannot have found one")
	require.Equal(t, 1, view.reads, "the read must have reached the view")
}

// Setup stages its writes through the state, and draining hands them over as the block to commit.
func TestSetupWritesDrainIntoTheBlockToCommit(t *testing.T) {
	t.Parallel()

	state, _ := newTestState()
	account, slot := testAccountKey(1), testSlotKey(1)
	state.Put(account, []byte("account value"))
	state.Put(slot, []byte("slot value"))

	writes := state.drainSetupWrites(identifierCounters{nextAccountID: 3, nextErc20ContractID: 4})
	require.Len(t, writes.changeSets, 1)
	require.Len(t, writes.changeSets[0].Changeset.Pairs, 2+len(counterKeys))

	require.Zero(t, state.setupWrites.count(), "draining must leave the batch empty for the next block")
}
