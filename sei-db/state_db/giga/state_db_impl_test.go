package giga_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

// The module the test changesets are written under. Any non-EVM module routes to misc storage, where a
// plain key round-trips without EVM key encoding getting in the way of what is being tested.
const testModule = "bank"

// Names recorded in the WAL call log, one per call the fan-out is expected to make.
const (
	walWriteCall      = "wal.Write"
	walEndOfBlockCall = "wal.SignalEndOfBlock"
)

// fakeStateWAL stands in for the state WAL so a test can watch the WAL half of the fan-out and fail it
// on demand. It embeds StateWAL without implementing it, so any method the fan-out is not expected to
// call panics on the nil interface rather than answering with a zero value.
//
// It reports an empty range, which is what makes it usable here: NewStateDB converges the stores onto
// the WAL's head, and an empty WAL has none, so construction leaves the store where it found it.
type fakeStateWAL struct {
	statewal.StateWAL

	// Calls made, in order.
	calls []string

	// Block numbers passed to Write, in call order.
	writtenBlocks []uint64

	// Changesets passed to Write, in call order.
	writtenChangesets [][]*proto.NamedChangeSet

	// The error Write returns.
	writeErr error

	// The error SignalEndOfBlock returns.
	endOfBlockErr error
}

func (w *fakeStateWAL) Write(blockNumber uint64, cs []*proto.NamedChangeSet) error {
	w.calls = append(w.calls, walWriteCall)
	w.writtenBlocks = append(w.writtenBlocks, blockNumber)
	w.writtenChangesets = append(w.writtenChangesets, cs)
	return w.writeErr
}

func (w *fakeStateWAL) SignalEndOfBlock() error {
	w.calls = append(w.calls, walEndOfBlockCall)
	return w.endOfBlockErr
}

func (w *fakeStateWAL) GetStoredRange() (bool, uint64, uint64, error) {
	return false, 0, 0, nil
}

// newTestStateDB builds a StateDB over a fake WAL and a real FlatKV store. The store is constructed
// with no WAL of its own, which is the arrangement StateDB requires: it writes the WAL on the store's
// behalf, and replays that WAL into the store to catch it up.
func newTestStateDB(t *testing.T) (gigatypes.StateDB, *fakeStateWAL, *flatkv.CommitStore) {
	t.Helper()

	liveStateDB, err := flatkv.NewCommitStore(t.Context(), config.DefaultTestConfig(t), nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, liveStateDB.Close()) })

	wal := &fakeStateWAL{}
	stateDB, err := giga.NewStateDBOverWAL(wal, liveStateDB, nil)
	require.NoError(t, err)
	return stateDB, wal, liveStateDB
}

// changeset builds a changeset setting key to value in the test module.
func changeset(key string, value string) []*proto.NamedChangeSet {
	return []*proto.NamedChangeSet{{
		Name: testModule,
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{{Key: []byte(key), Value: []byte(value)}},
		},
	}}
}

// Fanning a block out to every layer is the whole job. A layer silently missed here is a layer that
// falls a block further behind the others with every block committed.
func TestCommitStateChangesReachesWALAndLiveStateDB(t *testing.T) {
	stateDB, wal, liveStateDB := newTestStateDB(t)

	cs := changeset("key", "value")
	require.NoError(t, stateDB.CommitStateChanges(1, cs))

	require.Equal(t, []uint64{1}, wal.writtenBlocks, "the WAL must receive the block that was committed")
	require.Equal(t, [][]*proto.NamedChangeSet{cs}, wal.writtenChangesets,
		"the WAL must receive the changeset it was handed, unaltered")

	require.Equal(t, int64(1), liveStateDB.Version(), "the live state DB must have committed the block")
	value, found := liveStateDB.Get(testModule, []byte("key"))
	require.True(t, found, "the committed key must be readable from the live state DB")
	require.Equal(t, []byte("value"), value)
}

// The WAL yields a block to readers only once it has been told the block is over, and discards an
// un-ended one on Close. A commit that writes without ending leaves nothing anyone can read back.
func TestCommitStateChangesEndsTheBlockInTheWAL(t *testing.T) {
	stateDB, wal, _ := newTestStateDB(t)

	require.NoError(t, stateDB.CommitStateChanges(1, changeset("key", "value")))

	require.Equal(t, []string{walWriteCall, walEndOfBlockCall}, wal.calls,
		"a block must be ended in the WAL after it is written")
}

// A block that could not be written to every layer is not committed. The caller has to learn that from
// the error, rather than from a later read finding the layers disagree.
func TestCommitStateChangesStopsWhenTheWALWriteFails(t *testing.T) {
	stateDB, wal, liveStateDB := newTestStateDB(t)
	wal.writeErr = errors.New("wal is bricked")

	err := stateDB.CommitStateChanges(1, changeset("key", "value"))

	require.ErrorContains(t, err, "write block 1 to state WAL")
	require.ErrorContains(t, err, "wal is bricked")
	require.Equal(t, int64(0), liveStateDB.Version(),
		"the live state DB must not commit once the WAL has refused the block")
}

// A block the WAL was never told had ended is one it will not yield to a reader, so the fan-out did
// not complete and must not be reported as though it had.
func TestCommitStateChangesStopsWhenEndingTheBlockFails(t *testing.T) {
	stateDB, wal, liveStateDB := newTestStateDB(t)
	wal.endOfBlockErr = errors.New("wal is bricked")

	err := stateDB.CommitStateChanges(1, changeset("key", "value"))

	require.ErrorContains(t, err, "end block 1 in state WAL")
	require.ErrorContains(t, err, "wal is bricked")
	require.Equal(t, int64(0), liveStateDB.Version(),
		"the live state DB must not commit once the block could not be ended")
}

// The live state DB refusing a block is not something the caller can be left to discover later. The
// error has to reach them, and the refused block must not advance it.
func TestCommitStateChangesReportsALiveStateDBFailure(t *testing.T) {
	stateDB, _, liveStateDB := newTestStateDB(t)
	require.NoError(t, stateDB.CommitStateChanges(1, changeset("key", "value")))

	// The live state DB numbers blocks contiguously, so a block that skips a height is one it refuses.
	err := stateDB.CommitStateChanges(9, changeset("key", "later"))

	require.ErrorContains(t, err, "commit block 9 to live state DB")
	require.Equal(t, int64(1), liveStateDB.Version(), "a refused block must not advance the live state DB")
}

// The WAL numbers blocks with a uint64. An unguarded negative height converts to a block far in the
// future, which the WAL accepts and which strands every real block behind it.
func TestCommitStateChangesRefusesANegativeBlockNumber(t *testing.T) {
	stateDB, wal, liveStateDB := newTestStateDB(t)

	err := stateDB.CommitStateChanges(-1, changeset("key", "value"))

	require.ErrorContains(t, err, "block number must not be negative")
	require.Empty(t, wal.calls, "a refused block must reach neither the WAL nor the live state DB")
	require.Equal(t, int64(0), liveStateDB.Version())
}

// A block that changed nothing is still a block: the layers below number them contiguously, so skipping
// an empty one puts every later block a height out of step.
func TestCommitStateChangesCommitsABlockWithNoChanges(t *testing.T) {
	stateDB, wal, liveStateDB := newTestStateDB(t)

	require.NoError(t, stateDB.CommitStateChanges(1, nil))

	require.Equal(t, []uint64{1}, wal.writtenBlocks)
	require.Equal(t, int64(1), liveStateDB.Version(), "the live state DB must commit an empty block rather than skip it")
}

// Current-block reads are the live state DB's to answer. Anything synthesized here instead would be a
// second opinion about what the current block contains.
func TestOpenViewServesTheBlockCommittedToTheLiveStateDB(t *testing.T) {
	stateDB, _, _ := newTestStateDB(t)
	require.NoError(t, stateDB.CommitStateChanges(1, changeset("key", "value")))

	view := stateDB.OpenView()
	defer view.Close()

	require.Equal(t, int64(1), view.GetBlockHeight(), "the view must be of the block that was committed")
	value, found := view.Get(testModule, []byte("key"))
	require.True(t, found)
	require.Equal(t, []byte("value"), value)
}

// Serving a past height needs the historical state DB, which is not wired in. Answering from the live
// state DB instead would return the current block under the name of a historical one.
func TestOpenViewAtPanicsUntilTheHistoricalStateDBIsWired(t *testing.T) {
	stateDB, _, _ := newTestStateDB(t)

	require.PanicsWithValue(t,
		"giga: OpenViewAt(5) is not implemented: the historical state DB is not wired in",
		func() { stateDB.OpenViewAt(5) })
}
