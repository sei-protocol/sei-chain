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
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/stateview"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

var logger = seilog.NewLogger("db", "state-db", "giga")

var _ gigatypes.StateDB = (*StateDB)(nil)

// StateDB writes a committed block to the state WAL, the live state DB and the historical state DB, serves
// current-block reads from the live state DB, and serves past heights from the historical state DB.
type StateDB struct {
	// The state WAL a committed block is written to.
	wal statewal.StateWAL

	// The hasher a committed block's mutations are submitted to.
	budHasher bud.BUDHasher

	// The live state DB, which both receives writes and serves current-block reads.
	liveStateDB gigatypes.LiveStateStore

	// The historical state DB, nil when it is disabled.
	historicalStateDB gigatypes.HistoricalStateStore

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
	// Opened with no WAL of its own.
	liveStateDB gigatypes.LiveStateStore,
	// Nil when the historical state DB is disabled.
	historicalStateDB gigatypes.HistoricalStateStore,
	wal statewal.StateWAL,
	// The schedule liveStateDB takes its snapshot boundaries from, already set on it.
	checkpointer *controller.CheckpointScheduler,
	// The hasher each committed block's mutations are submitted to.
	budHasher bud.BUDHasher,
) (*StateDB, error) {
	if err := requireLiveStateDBAtTheWALHead(liveStateDB, wal); err != nil {
		return nil, err
	}
	if err := resumeHistoricalStateDBOverHeldState(liveStateDB, historicalStateDB); err != nil {
		return nil, err
	}
	s := &StateDB{
		wal:               wal,
		budHasher:         budHasher,
		liveStateDB:       liveStateDB,
		historicalStateDB: historicalStateDB,
		checkpointer:      checkpointer,
		commitPhases: metrics.NewPhaseTimerFactory(otel.Meter(gigaMeterName), commitPhaseTimerName).
			RecordLatencies().Build(),
	}
	s.closed = utils.MustClose(s, "giga state DB")
	return s, nil
}

// requireLiveStateDBAtTheWALHead errors unless liveStateDB stands at the head of a WAL that holds blocks.
func requireLiveStateDBAtTheWALHead(liveStateDB gigatypes.LiveStateStore, wal statewal.StateWAL) error {
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

// resumeHistoricalStateDBOverHeldState resumes historicalStateDB when liveStateDB holds a block. Over an empty
// live state DB it refuses a historical state DB that holds blocks, and otherwise leaves it for the first commit
// to resume.
//
// An empty live state DB may still be seeded to a later height, and seeding waits for it to flush, which a view
// the historical state DB held over the empty store would block forever.
func resumeHistoricalStateDBOverHeldState(
	liveStateDB gigatypes.LiveStateStore,
	historicalStateDB gigatypes.HistoricalStateStore,
) error {
	if liveStateDB.Version() != 0 {
		return resumeHistoricalStateDB(liveStateDB, historicalStateDB)
	}
	if historicalStateDB != nil && historicalStateDB.GetLatestVersion() > 0 {
		return fmt.Errorf("resume the historical state DB: it holds blocks up to %d, but the live state DB is "+
			"empty", historicalStateDB.GetLatestVersion())
	}
	return nil
}

// resumeHistoricalStateDB binds historicalStateDB to liveStateDB's current block, unless it is disabled or
// already bound.
func resumeHistoricalStateDB(
	liveStateDB gigatypes.LiveStateStore,
	historicalStateDB gigatypes.HistoricalStateStore,
) error {
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
	timer := utils.NewLoggingPhaseTimer(logger)
	// Before the live state DB: the historical state DB holds a view of the live state DB's current block until
	// it closes.
	if s.historicalStateDB != nil {
		timer.SetPhase("historical_state_db")
		if err := s.historicalStateDB.Close(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close the historical state DB: %w", err))
		}
	}
	timer.SetPhase("live_state_db")
	if err := s.liveStateDB.Close(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close the live state DB: %w", err))
	}
	timer.SetPhase("state_wal")
	if err := s.wal.Close(); err != nil {
		errs = errors.Join(errs, fmt.Errorf("close state WAL: %w", err))
	}
	timer.Log("Closed the state DB")
	return errs
}

// LiveStateDB returns the live state DB.
func (s *StateDB) LiveStateDB() gigatypes.LiveStateStore { return s.liveStateDB }

// HistoricalStateDB returns the historical state DB, or nil when it is disabled.
func (s *StateDB) HistoricalStateDB() gigatypes.HistoricalStateStore { return s.historicalStateDB }

// WAL returns the state WAL. It is not replaced for the StateDB's lifetime.
func (s *StateDB) WAL() statewal.StateWAL { return s.wal }

// CheckpointScheduler returns the schedule the live state DB takes its snapshot boundaries from.
func (s *StateDB) CheckpointScheduler() *controller.CheckpointScheduler { return s.checkpointer }

// PrunableStores returns the opened stores that can join a prune cycle.
func (s *StateDB) PrunableStores() []controllertypes.PrunableStore {
	stores := []controllertypes.PrunableStore{s.liveStateDB, s.wal}
	if s.historicalStateDB != nil {
		stores = append(stores, s.historicalStateDB)
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
	// Before the live state DB moves past the block the historical state DB resumes on.
	if err := resumeHistoricalStateDB(s.liveStateDB, s.historicalStateDB); err != nil {
		return fmt.Errorf("commit block %d: %w", blockNum, err)
	}
	// Ends the phase in flight, so the gap until the next commit is not charged to the last store.
	defer s.commitPhases.Reset()

	s.commitPhases.SetPhase("convert_mutations")
	mutations, err := s.liveStateDB.MutationsFromStateChangeSet(blockNum, changes)
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
	if err := s.liveStateDB.CommitStateChanges(blockNum, mutations); err != nil {
		return fmt.Errorf("commit block %d to live state DB: %w", blockNum, err)
	}
	// The historical state DB takes the live state DB's view along with the block. Opened before the next commit,
	// it describes exactly this block. The historical state DB may write in the background and is not waited on,
	// so what is timed here is the wait to hand the block over.
	if s.historicalStateDB != nil {
		s.commitPhases.SetPhase("enqueue_ss")
		s.historicalStateDB.ApplyBlock(blockNum, mutations, s.liveStateDB.OpenView())
	}

	return nil
}

func (s *StateDB) OpenView() gigatypes.StateView {
	return stateview.New(s.liveStateDB.OpenView())
}

// OpenViewAt returns a view of the state after blockNum served from the historical state DB, and false
// when it does not hold blockNum or is disabled.
//
// The view holds back the live state DB's flushes and the historical state DB's pruning until it is
// closed, so it must be closed promptly.
func (s *StateDB) OpenViewAt(blockNum int64) (gigatypes.StateView, bool) {
	if s.historicalStateDB == nil {
		return nil, false
	}
	raw, ok := s.historicalStateDB.OpenView(blockNum)
	if !ok {
		return nil, false
	}
	return stateview.New(raw), true
}

// RegisterHashListener registers listener for the block hashes the live state DB produces.
func (s *StateDB) RegisterHashListener(listener gigatypes.HashListener) (lthash.BlockHash, error) {
	mostRecentHash, err := s.liveStateDB.RegisterHashListener(listener)
	if err != nil {
		return mostRecentHash, fmt.Errorf("register hash listener on the live state DB: %w", err)
	}
	return mostRecentHash, nil
}
