package giga

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
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

// fakeStateWAL stands in for the state WAL so a test can watch what StateDB writes to it, and fail it
// on demand. It embeds StateWAL without implementing it, so any method StateDB is not expected to call
// panics on the nil interface rather than answering with a zero value.
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

// newTestStateDB builds a StateDB over a fake WAL and a real FlatKV store. The store is constructed
// with no WAL of its own, which is the arrangement StateDB requires: it writes the WAL on the store's
// behalf, and replays that WAL into the store to catch it up.
func newTestStateDB(t *testing.T) (gigatypes.StateDB, *fakeStateWAL, *flatkv.CommitStore) {
	t.Helper()

	cfg := flatkvconfig.DefaultTestConfig(t)
	liveStateDB, err := flatkv.NewCommitStore(t.Context(), cfg, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, liveStateDB.Close()) })
	require.NoError(t, liveStateDB.LoadLatest())

	wal := &fakeStateWAL{}
	return &StateDB{wal: wal, sc: liveStateDB, flatkvCfg: cfg}, wal, liveStateDB
}

// gapWAL reports a stored range beginning above the block a replay has to start from, which is what a
// WAL pruned past a store's head looks like.
type gapWAL struct {
	statewal.StateWAL
	first, last uint64
}

func (w *gapWAL) GetStoredRange() (bool, uint64, uint64, error) { return true, w.first, w.last, nil }

// A WAL pruned past a store has dropped blocks that store still needs. Applying only the blocks the WAL
// happens to hold and then reporting the target as reached is silent divergence, so the replay refuses
// instead.
//
// SC and SS both have to reach this check, which is why they replay through one function rather than
// each walking the WAL: the store that goes around it is the one that diverges quietly.
func TestCatchUpRefusesAWALMissingTheBlocksAStoreNeeds(t *testing.T) {
	const missingBlocks = "missing (data loss or corruption)"

	t.Run("the state commit store", func(t *testing.T) {
		_, _, sc := newTestStateDB(t)
		s := &StateDB{wal: &gapWAL{first: 3, last: 4}, sc: sc}

		require.ErrorContains(t, s.catchUpTo(4), missingBlocks)
	})

	t.Run("the EVM state store", func(t *testing.T) {
		_, _, sc := newTestStateDB(t)
		s := &StateDB{wal: &gapWAL{first: 3, last: 4}, sc: sc, ss: &evm.EVMStateStore{}}

		// The store holds nothing, so the gap is its whole history rather than a hole in it. Refusing
		// here would report data loss for a store that is merely new, and would do it on every node
		// past its first retention cut, so it is left out of the replay to fill forward from the target.
		_, replays, err := s.ssReplayStart(4)

		require.NoError(t, err)
		require.False(t, replays)
		require.Zero(t, s.ss.GetLatestVersion())
	})
}

// A store left to fill forward is not held to the target afterwards. Holding it there would fail the
// rollback over exactly the state the catch-up had just decided was the right outcome.
func TestMatchHeightExcusesAStoreLeftToFillForward(t *testing.T) {
	_, _, sc := newTestStateDB(t)
	for block := int64(1); block <= 4; block++ {
		require.NoError(t, sc.CommitStateChanges(block, changeset("k", "v")))
	}
	s := &StateDB{wal: &gapWAL{first: 3, last: 4}, sc: sc, ss: &evm.EVMStateStore{}}

	require.NoError(t, s.matchHeight(4))
}

// An empty SS is only excused when the WAL cannot rebuild it. Excusing every version-0 store would
// treat a wiped history as a brand-new one whenever the WAL still reaches block 1.
func TestMatchHeightDoesNotExcuseAnEmptyStoreTheWALCanRebuild(t *testing.T) {
	_, _, sc := newTestStateDB(t)
	for block := int64(1); block <= 4; block++ {
		require.NoError(t, sc.CommitStateChanges(block, changeset("k", "v")))
	}
	s := &StateDB{wal: &gapWAL{first: 1, last: 4}, sc: sc, ss: &evm.EVMStateStore{}}

	err := s.matchHeight(4)

	require.ErrorContains(t, err, "EVM state store")
	// Both the open and a rollback converge here, so the height belongs to whichever asked. Naming a
	// rollback would send an operator whose node will not start looking for one nobody ran.
	require.NotContains(t, err.Error(), "roll back")
	require.NotContains(t, err.Error(), "4")
}

// A store that holds nothing is only left empty when the WAL cannot rebuild it. One the WAL still
// reaches back far enough for comes out of recovery holding real history, which is strictly better, and
// is how a store that lagged the WAL is populated on restart.
func TestCatchUpRebuildsAnEmptyStoreTheWALStillCovers(t *testing.T) {
	_, _, sc := newTestStateDB(t)
	s := &StateDB{wal: &gapWAL{first: 1, last: 4}, sc: sc, ss: &evm.EVMStateStore{}}

	fillForward, err := s.ssFillsForward()
	require.NoError(t, err)
	require.False(t, fillForward, "a WAL starting at block 1 can rebuild an empty store")
}

// A node that keeps no EVM state store is recognised where the rollback is planned, so nothing later
// probes a store it does not have. The directory is one an earlier run with SS on could have left, and
// the WAL reaches block 1, so a plan that read it would come back with a rewind and a replay to run.
func TestPlanSSRewindLeavesANodeThatKeepsNoEVMStoreAlone(t *testing.T) {
	const target = int64(7)
	dir := t.TempDir()
	s := &StateDB{ssCfg: config.StateStoreConfig{Enable: false, EVMDBDirectory: dir}}
	wal := storedWALRange{first: 1, last: 9}

	plan, err := s.planSSRewind(wal, target)
	require.NoError(t, err)
	require.Equal(t, ssIsAbsent, plan.action)
	require.False(t, plan.movesFiles())

	from, replays := plan.replaysFrom(wal, target)
	require.False(t, replays, "a store that is not there must not be held to the target")
	require.Zero(t, from)

	require.NoError(t, s.applySSRewind(plan, target))
	require.DirExists(t, dir, "a rollback must not write to the directory of a store this node has not opened")
}

// An interrupted rewind of a separate-DB SS leaves some of its databases holding blocks above the
// target and the rest empty. The head is the lowest of them, so the store reads as below the target and
// planning from that alone would hold position, leaving those blocks for a replay that cannot delete
// them. The plan has to see the rewind through instead.
func TestPlanSSRewindSeesAnInterruptedRewind(t *testing.T) {
	const target = int64(3)
	ssCfg := config.DefaultStateStoreConfig()
	ssCfg.EVMDBDirectory = filepath.Join(t.TempDir(), "ss")
	ssCfg.SeparateEVMSubDBs = true
	writeTornSS(t, ssCfg, 5)
	s := &StateDB{ssCfg: ssCfg}

	plan, err := s.planSSRewind(storedWALRange{first: 1, last: 9}, target)

	require.NoError(t, err)
	require.Equal(t, ssRebuildsFromEmpty, plan.action,
		"the databases still holding block 5 have to be emptied, not left for the replay")
}

// writeTornSS leaves a separate-DB EVM state store with its databases disagreeing, as an interrupted
// rewind between two of them does: all but the first record version, and that one reopens empty.
func writeTornSS(t *testing.T, ssCfg config.StateStoreConfig, version int64) {
	t.Helper()
	ss, err := evm.NewEVMStateStore(ssCfg.EVMDBDirectory, ssCfg)
	require.NoError(t, err)
	require.NoError(t, ss.SetLatestVersion(version))
	require.NoError(t, ss.Close())
	require.NoError(t, os.RemoveAll(
		filepath.Join(ssCfg.EVMDBDirectory, evm.StoreTypeName(evm.AllEVMStoreTypes()[0]))))
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

// The EVM state store is a layer of the same fan-out: a commit that reaches WAL and SC but not SS
// leaves historical EVM reads a block behind with every block.
func TestCommitStateChangesReachesTheEVMStateStore(t *testing.T) {
	stateDB, _, _ := newTestStateDB(t)
	ss, err := evm.NewEVMStateStore(t.TempDir(), config.DefaultStateStoreConfig())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, ss.Close()) })
	stateDB.(*StateDB).ss = ss

	key := append([]byte{0x0a}, make([]byte, 20)...)
	value := append(make([]byte, 7), byte(1))
	cs := []*proto.NamedChangeSet{{
		Name: evm.EVMStoreKey,
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{{Key: key, Value: value}},
		},
	}}
	require.NoError(t, stateDB.CommitStateChanges(1, cs))
	// The commit hands SS its block asynchronously and does not wait, so the read does.
	ss.WaitForPendingWrites()

	require.Equal(t, int64(1), ss.GetLatestVersion())
	got, err := ss.Get(evm.EVMStoreKey, 1, key)
	require.NoError(t, err)
	require.Equal(t, value, got)
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
