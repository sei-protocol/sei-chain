package giga

import (
	"context"
	"errors"
	"fmt"

	controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"

	"github.com/sei-protocol/seilog"
	"go.opentelemetry.io/otel"

	"github.com/sei-protocol/sei-chain/giga/apphash/bud"
	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/undo"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

var logger = seilog.NewLogger("db", "state-db", "giga")

var _ gigatypes.StateDB = (*StateDB)(nil)

// StateDB writes a committed block to the state WAL, the live state DB and the historical state DB, serves
// current-block reads from the live state DB, and serves past heights from the historical state DB.
type StateDB struct {
	// Converts a committed block's changes into the mutations the live state DB and the state WAL take.
	flatkvCfg *flatkvconfig.Config

	// The state WAL a committed block is written to.
	wal statewal.StateWAL

	// The hasher a committed block's mutations are submitted to.
	budHasher bud.BUDHasher

	// The state commit store, which both receives writes and serves current-block reads.
	sc *flatkv.CommitStore

	// undo is the historical state DB, nil when it is disabled.
	undo *undo.Database

	// The checkpoint schedule the live state DB takes its snapshot boundaries from.
	checkpointer *controller.CheckpointScheduler

	// Splits a commit into the three stores it writes. Driven only by CommitStateChanges, which
	// callers serialize.
	commitPhases *metrics.PhaseTimer

	// Closed by Close().
	closed utils.CloseMarker[StateDB]
}

// commitPhaseTimerName prefixes the instruments the commit phase breakdown is published on.
const commitPhaseTimerName = "giga_state_commit"

// gigaMeterName is the OTel meter this package's instruments are created on.
const gigaMeterName = "seidb_giga"

// NewStateDB assembles a StateDB over stores that are already open, so that it commits the block after the WAL's
// head. It errors unless the live state DB stands at that head, and attaches the historical state DB to the live
// state DB's current block.
//
// The returned StateDB owns the three stores and closes them on Close. A failed call leaves them with the caller.
func NewStateDB(
	// Converts a committed block's changes into mutations.
	flatkvCfg *flatkvconfig.Config,
	// Opened with no WAL of its own.
	liveStateDB *flatkv.CommitStore,
	// Nil when the historical state DB is disabled.
	historicalStateDB *undo.Database,
	wal statewal.StateWAL,
	// The schedule liveStateDB takes its snapshot boundaries from, already set on it.
	checkpointer *controller.CheckpointScheduler,
	// The hasher each committed block's mutations are submitted to.
	budHasher bud.BUDHasher,
) (*StateDB, error) {
	if err := requireLiveStateDBAtTheWALHead(liveStateDB, wal); err != nil {
		return nil, err
	}
	if err := resumeUndoLogOverHeldState(liveStateDB, historicalStateDB); err != nil {
		return nil, err
	}
	s := &StateDB{
		flatkvCfg:    flatkvCfg,
		wal:          wal,
		budHasher:    budHasher,
		sc:           liveStateDB,
		undo:         historicalStateDB,
		checkpointer: checkpointer,
		commitPhases: metrics.NewPhaseTimerFactory(otel.Meter(gigaMeterName), commitPhaseTimerName).
			RecordLatencies().Build(),
	}
	s.closed = utils.MustClose(s, "giga state DB")
	return s, nil
}

// requireLiveStateDBAtTheWALHead errors unless liveStateDB stands at the head of a WAL that holds blocks.
func requireLiveStateDBAtTheWALHead(liveStateDB *flatkv.CommitStore, wal statewal.StateWAL) error {
	stored, _, last, err := wal.GetStoredRange()
	if err != nil {
		return fmt.Errorf("read state WAL range: %w", err)
	}
	if !stored {
		return nil
	}
	//nolint:gosec // live state DB versions are never negative
	if version := uint64(liveStateDB.Version()); version != last {
		return fmt.Errorf("the live state DB is at %d but the state WAL ends at %d; roll the live state DB "+
			"back to align them", version, last)
	}
	return nil
}

// resumeUndoLogOverHeldState resumes historicalStateDB when liveStateDB holds a block. Over an empty live state DB
// it refuses an undo log that holds blocks, and otherwise leaves the log for the first commit to resume.
//
// An empty live state DB may still be seeded to a later height, and seeding waits for it to flush, which a view
// the undo log held over the empty store would block forever.
func resumeUndoLogOverHeldState(liveStateDB *flatkv.CommitStore, historicalStateDB *undo.Database) error {
	if liveStateDB.Version() != 0 {
		return resumeUndoLog(liveStateDB, historicalStateDB)
	}
	if historicalStateDB != nil && historicalStateDB.GetLatestVersion() > 0 {
		return fmt.Errorf("resume the historical state DB: it holds blocks up to %d, but the live state DB is "+
			"empty", historicalStateDB.GetLatestVersion())
	}
	return nil
}

// resumeUndoLog binds historicalStateDB to liveStateDB's current block, which answers every read the log holds no
// later record for, unless it is already bound. A log behind the live state DB stops serving the history below
// the live state DB's height.
func resumeUndoLog(liveStateDB *flatkv.CommitStore, historicalStateDB *undo.Database) error {
	if historicalStateDB == nil || historicalStateDB.Resumed() {
		return nil
	}
	if err := historicalStateDB.Resume(liveStateDB.Version(), liveStateDB.OpenView()); err != nil {
		return fmt.Errorf("resume the historical state DB: %w", err)
	}
	return nil
}

// Close closes the live state DB, the historical state DB and the state WAL, reporting every failure rather than
// stopping at the first.
//
// How long each of the three took is logged, since each drains its own write queue and waits on the
// compactions behind it, and those dominate the time a shutdown takes.
func (s *StateDB) Close() error {
	s.closed.Close(s)
	var errs error
	var timer utils.CloseTimer
	// Before the live state DB: the undo log holds a view of the live state DB's current block until it closes.
	if s.undo != nil {
		if err := timer.Close("ss", s.undo.Close); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close the historical state DB: %w", err))
		}
	}
	if err := timer.Close("sc", s.sc.Close); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close the live state DB: %w", err))
	}
	if err := timer.Close("wal", s.wal.Close); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close state WAL: %w", err))
	}
	logger.Info("Closed the state DB", timer.Fields()...)
	return errs
}

// SC returns the state commit store.
func (s *StateDB) SC() *flatkv.CommitStore { return s.sc }

// UndoSS returns the historical state DB, or nil when it is disabled.
func (s *StateDB) UndoSS() *undo.Database { return s.undo }

// WAL returns the state WAL. It is not replaced for the StateDB's lifetime.
func (s *StateDB) WAL() statewal.StateWAL { return s.wal }

// CheckpointScheduler returns the schedule the live state DB takes its snapshot boundaries from.
func (s *StateDB) CheckpointScheduler() *controller.CheckpointScheduler { return s.checkpointer }

// PrunableStores returns the opened stores that can join a prune cycle.
func (s *StateDB) PrunableStores() []controllertypes.PrunableStore {
	stores := []controllertypes.PrunableStore{s.sc, s.wal}
	if s.undo != nil {
		stores = append(stores, s.undo)
	}
	return stores
}

// CommitStateChanges writes a block's state changes to the state WAL, submits them to the BUD hasher, commits
// them to the live state DB, and queues them for the historical state DB. Commits must be made one at a time.
// Returns an error if writing this block or an earlier one has failed.
func (s *StateDB) CommitStateChanges(blockNum int64, changes *gigatypes.StateChangeSet) error {
	if blockNum < 0 {
		// The WAL numbers blocks with a uint64, so a negative height converts to a block far in the
		// future that the WAL has no way to recognize as a mistake.
		return fmt.Errorf("commit block %d: block number must not be negative", blockNum)
	}
	// Before SC moves past the block the log resumes on.
	if err := resumeUndoLog(s.sc, s.undo); err != nil {
		return fmt.Errorf("commit block %d: %w", blockNum, err)
	}
	// Ends the phase in flight, so the gap until the next commit is not charged to the last store.
	defer s.commitPhases.Reset()

	s.commitPhases.SetPhase("convert_mutations")
	// SC has not taken this block yet, so its view is of the block before.
	prior := s.sc.OpenView()
	mutations, err := flatkv.MutationsFromStateChangeSet(changes, uint64(blockNum), prior, s.flatkvCfg)
	prior.Close()
	if err != nil {
		return fmt.Errorf("convert block %d to mutations: %w", blockNum, err)
	}

	// No need to flush WAL, since this WAL isn't used for crash recoverability safety (that's the BlockDB's job).
	s.commitPhases.SetPhase("write_state_wal")
	if err := s.wal.Write(uint64(blockNum), mutations); err != nil {
		return fmt.Errorf("write block %d to state WAL: %w", blockNum, err)
	}

	s.commitPhases.SetPhase("submit_bud_hash")
	if err := s.budHasher.Submit(context.Background(), uint64(blockNum), mutations); err != nil {
		return fmt.Errorf("submit block %d to the BUD hasher: %w", blockNum, err)
	}

	s.commitPhases.SetPhase("commit_sc")
	if err := s.sc.CommitStateChanges(blockNum, mutations); err != nil {
		return fmt.Errorf("commit block %d to live state DB: %w", blockNum, err)
	}
	// The undo log takes SC's view along with the block. Opened before the next commit, it describes
	// exactly this block, which is what the log answers reads from until a later block replaces it. It
	// writes asynchronously and is not waited on, so what is timed here is the wait to hand the block over.
	if s.undo != nil {
		s.commitPhases.SetPhase("enqueue_ss")
		s.undo.ApplyBlock(blockNum, changes, s.sc.OpenView())
	}

	return nil
}

func (s *StateDB) OpenView() gigatypes.StateView {
	return s.sc.OpenView()
}

// OpenViewAt returns a view of the state after blockNum served from the historical state DB, and false
// when it does not hold blockNum or is disabled.
//
// The view holds back the live state DB's flushes and the historical state DB's pruning until it is
// closed, so it must be closed promptly.
func (s *StateDB) OpenViewAt(blockNum int64) (gigatypes.StateView, bool) {
	if s.undo == nil {
		return nil, false
	}
	view, ok := s.undo.OpenView(blockNum)
	if !ok {
		return nil, false
	}
	return &historicalView{height: blockNum, read: view.Get, release: view.Close}, true
}

// RegisterHashListener forwards to the state commit store, which is the layer that hashes blocks and
// so is the layer that dispatches them.
func (s *StateDB) RegisterHashListener(listener gigatypes.HashListener) (lthash.BlockHash, error) {
	mostRecentHash, err := s.sc.RegisterHashListener(listener)
	if err != nil {
		return mostRecentHash, fmt.Errorf("register hash listener on the state commit store: %w", err)
	}
	return mostRecentHash, nil
}
