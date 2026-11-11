package giga

import (
	"context"
	"errors"
	"fmt"
	"time"

	controllertypes "github.com/sei-protocol/sei-chain/sei-db/controller/types"

	"github.com/sei-protocol/seilog"
	"go.opentelemetry.io/otel"

	"github.com/sei-protocol/sei-chain/giga/apphash/bud"
	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
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
//
// It opens all three stores, brings them onto one height, and closes them.
type StateDB struct {
	// Where the state commit store and the state WAL live.
	flatkvCfg *flatkvconfig.Config

	// Where the EVM state store lives, and whether it is enabled at all.
	ssCfg config.StateStoreConfig

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

// NewStateDB opens the live state DB, the historical state DB and the state WAL from their configs, and
// puts the live state DB on its checkpoint schedule, so the returned StateDB commits the block after the WAL's
// head. It errors when the live state DB does not stand at that head; flatkv.RollBackLiveStateDB() brings it there.
//
// The returned StateDB owns all three stores and closes them on Close. A failed call closes whatever it
// had already opened.
func NewStateDB(
	ctx context.Context,
	flatkvCfg *flatkvconfig.Config,
	ssCfg config.StateStoreConfig,
	checkpointCfg config.CheckpointConfig,
	// The hasher each committed block's mutations are submitted to.
	budHasher bud.BUDHasher,
) (db *StateDB, retErr error) {
	start := time.Now()
	logger.Info("Opening the state DB")
	s := &StateDB{
		flatkvCfg: flatkvCfg,
		ssCfg:     ssCfg,
		budHasher: budHasher,
		commitPhases: metrics.NewPhaseTimerFactory(otel.Meter(gigaMeterName), commitPhaseTimerName).
			RecordLatencies().Build(),
	}
	s.closed = utils.MustClose(s, "giga state DB")
	defer s.closeOnFailure(&retErr)

	if err := s.openSS(); err != nil {
		return nil, err
	}
	liveStateDB, err := openLiveStateDB(ctx, flatkvCfg)
	if err != nil {
		return nil, err
	}
	s.sc = liveStateDB
	if err := s.openWAL(); err != nil {
		return nil, err
	}
	s.startCheckpointSchedule(checkpointCfg)

	if err := s.requireLiveStateDBAtTheWALHead(); err != nil {
		return nil, err
	}
	if err := s.resumeUndoLogOverHeldState(); err != nil {
		return nil, err
	}
	logger.Info("Opened the state DB", "elapsed", time.Since(start))
	return s, nil
}

// closeOnFailure closes the stores a failed open had reached, so a caller that gets an error holds no
// store this StateDB left open. It is deferred against the constructor's named error.
func (s *StateDB) closeOnFailure(retErr *error) {
	if *retErr == nil {
		return
	}
	if err := s.Close(); err != nil {
		*retErr = errors.Join(*retErr, fmt.Errorf("close a partially opened state DB: %w", err))
	}
}

// openWAL opens the state WAL this StateDB commits blocks to.
func (s *StateDB) openWAL() error {
	wal, err := flatkv.OpenStateWAL(s.flatkvCfg)
	if err != nil {
		return fmt.Errorf("open state WAL: %w", err)
	}
	s.wal = wal
	return nil
}

// openLiveStateDB opens the live state DB with no WAL of its own, on the version its files hold. It replays
// nothing.
func openLiveStateDB(ctx context.Context, flatkvCfg *flatkvconfig.Config) (_ *flatkv.CommitStore, retErr error) {
	liveStateDB, err := flatkv.NewCommitStore(ctx, flatkvCfg, nil)
	if err != nil {
		return nil, fmt.Errorf("open the live state DB: %w", err)
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, closeLiveStateDB(liveStateDB))
		}
	}()
	// Every readonly-* directory under the store is deleted, so this has to run before the process
	// opens a read-only view of its own: after that, the ones a crashed process left are no longer
	// the only ones there.
	if err := liveStateDB.CleanupOrphanedReadOnlyDirs(); err != nil {
		return nil, fmt.Errorf("clean up the live state DB's orphaned read-only dirs: %w", err)
	}
	if err := liveStateDB.LoadWorkingCopy(); err != nil {
		return nil, fmt.Errorf("load the live state DB: %w", err)
	}
	return liveStateDB, nil
}

// closeLiveStateDB closes liveStateDB, naming it in the error.
func closeLiveStateDB(liveStateDB *flatkv.CommitStore) error {
	if err := liveStateDB.Close(); err != nil {
		return fmt.Errorf("close the live state DB: %w", err)
	}
	return nil
}

// openSS opens the historical state DB, leaving it nil when it is disabled.
func (s *StateDB) openSS() error {
	if !s.ssCfg.Enable {
		return nil
	}
	db, err := undo.OpenDB(s.ssCfg.EVMDBDirectory, s.ssCfg)
	if err != nil {
		return fmt.Errorf("open EVM undo-log state store: %w", err)
	}
	s.undo = db
	return nil
}

// resumeUndoLogOverHeldState resumes the undo log when SC holds a block. Over an empty SC it refuses an
// undo log that holds blocks, and otherwise leaves the log for the first commit to resume.
//
// An empty SC may still be seeded to a later height, and seeding waits for SC to flush, which a view
// the undo log held over the empty store would block forever.
func (s *StateDB) resumeUndoLogOverHeldState() error {
	if s.sc.Version() != 0 {
		return s.resumeUndoLog()
	}
	if s.undo != nil && s.undo.GetLatestVersion() > 0 {
		return fmt.Errorf("resume the EVM undo-log state store: it holds blocks up to %d, but the state "+
			"commit store is empty", s.undo.GetLatestVersion())
	}
	return nil
}

// resumeUndoLog binds the undo log to SC's current block, which answers every read the log holds no
// later record for, unless it is already bound. A log behind SC stops serving the history below SC's
// height.
func (s *StateDB) resumeUndoLog() error {
	if s.undo == nil || s.undo.Resumed() {
		return nil
	}
	if err := s.undo.Resume(s.sc.Version(), s.sc.OpenView()); err != nil {
		return fmt.Errorf("resume the EVM undo-log state store: %w", err)
	}
	return nil
}

// startCheckpointSchedule puts the live state DB on its snapshot cadence. It runs before the live state DB
// is on a height, so the blocks it replays offer themselves to the schedule as live commits do.
func (s *StateDB) startCheckpointSchedule(cfg config.CheckpointConfig) {
	s.checkpointer = controller.NewCheckpointScheduler(cfg)
	s.sc.SetCheckpointScheduler(s.checkpointer)
}

// requireLiveStateDBAtTheWALHead errors unless the live state DB stands at the WAL's head. An empty WAL is the
// state a node starts from transplanted state in, with no head to check against, so it instead repairs a
// working copy an interrupted commit left torn.
func (s *StateDB) requireLiveStateDBAtTheWALHead() error {
	stored, _, last, err := s.wal.GetStoredRange()
	if err != nil {
		return fmt.Errorf("read state WAL range: %w", err)
	}
	if !stored {
		if err := s.sc.RebuildIfTorn(); err != nil {
			return fmt.Errorf("repair the live state DB's working copy: %w", err)
		}
		return nil
	}
	//nolint:gosec // live state DB versions are never negative
	if version := uint64(s.sc.Version()); version != last {
		return fmt.Errorf("the live state DB is at %d but the state WAL ends at %d; roll the live state DB "+
			"back to align them", version, last)
	}
	return nil
}

// Close closes SC, SS and the state WAL, reporting every failure rather than stopping at the first.
// The WAL closes last, since SC replays through it.
//
// How long each of the three took is logged, since each drains its own write queue and waits on the
// compactions behind it, and those dominate the time a shutdown takes.
func (s *StateDB) Close() error {
	s.closed.Close(s)
	var errs error
	var timer utils.CloseTimer
	// Before SC: the undo log holds a view of SC's current block until it closes.
	if s.undo != nil {
		if err := timer.Close("ss", s.undo.Close); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close EVM undo-log state store: %w", err))
		}
	}
	if s.sc != nil {
		if err := timer.Close("sc", s.sc.Close); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close state commit store: %w", err))
		}
	}
	if s.wal != nil {
		if err := timer.Close("wal", s.wal.Close); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close state WAL: %w", err))
		}
	}
	logger.Info("Closed the state DB", timer.Fields()...)
	return errs
}

// SC returns the state commit store.
func (s *StateDB) SC() *flatkv.CommitStore { return s.sc }

// UndoSS returns the historical state DB, or nil when it is disabled.
func (s *StateDB) UndoSS() *undo.Database { return s.undo }

// WAL returns the state WAL. It is the one this StateDB opened, and is not replaced for the StateDB's
// lifetime.
func (s *StateDB) WAL() statewal.StateWAL { return s.wal }

// CheckpointScheduler returns the schedule the live state DB takes its snapshot boundaries from.
func (s *StateDB) CheckpointScheduler() *controller.CheckpointScheduler { return s.checkpointer }

// PrunableStores returns the opened stores that can join a prune cycle.
func (s *StateDB) PrunableStores() []controllertypes.PrunableStore {
	stores := make([]controllertypes.PrunableStore, 0, 3)
	if s.sc != nil {
		stores = append(stores, s.sc)
	}
	if s.wal != nil {
		stores = append(stores, s.wal)
	}
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
	if err := s.resumeUndoLog(); err != nil {
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
