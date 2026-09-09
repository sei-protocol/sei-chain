package giga

import (
	"context"
	"errors"
	"fmt"

	"github.com/sei-protocol/seilog"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

var logger = seilog.NewLogger("db", "state-db", "giga")

var _ gigatypes.StateDB = (*StateDB)(nil)

// StateDB writes a committed block to the state WAL, the state commit store (SC) and the EVM state
// store (SS), and serves current-block reads from SC.
//
// It opens all three stores, brings them onto one height, and closes them. SC and SS run without a WAL
// of their own, so every block either of them replays is read from the WAL here.
type StateDB struct {
	// Where the state commit store and the state WAL live.
	flatkvCfg *flatkvconfig.Config

	// Where the EVM state store lives, and whether it is enabled at all.
	ssCfg config.StateStoreConfig

	// The state WAL a committed block is written to.
	wal statewal.StateWAL

	// The state commit store, which both receives writes and serves current-block reads.
	sc *flatkv.CommitStore

	// ss is nil when the EVM state store is disabled.
	ss *evm.EVMStateStore

	// The checkpoint schedule SC and SS take their snapshot boundaries from.
	checkpointer *controller.CheckpointScheduler
}

// NewStateDB opens SC, SS and the state WAL from their configs and puts SC and SS on one checkpoint
// schedule.
//
// Both stores are put on the WAL's head — replayed up to it, and rewound onto it when a lost WAL tail
// left them above it — so the returned StateDB commits the block after it. NewStateDBWithRollback opens
// them on an earlier height instead.
//
// The returned StateDB owns all three stores and closes them on Close. A failed call closes whatever it
// had already opened.
func NewStateDB(
	ctx context.Context,
	flatkvCfg *flatkvconfig.Config,
	ssCfg config.StateStoreConfig,
	checkpointCfg config.CheckpointConfig,
) (db *StateDB, retErr error) {
	s := &StateDB{flatkvCfg: flatkvCfg, ssCfg: ssCfg}
	defer s.closeOnFailure(&retErr)

	wal, err := s.storedWALRange()
	if err != nil {
		return nil, err
	}
	// Before either store opens, the rewinds it may run needing their files closed.
	if err := s.discardStateAboveTheWAL(wal); err != nil {
		return nil, err
	}
	if err := s.openSS(); err != nil {
		return nil, err
	}
	if err := s.openSC(ctx); err != nil {
		return nil, err
	}
	if err := s.openWAL(); err != nil {
		return nil, err
	}
	s.startCheckpointSchedule(checkpointCfg)

	if err := s.catchUpToWAL(); err != nil {
		return nil, err
	}
	return s, nil
}

// NewStateDBWithRollback rolls SC, SS and the state WAL back to target and then opens them, so the
// returned StateDB commits target+1. It cuts the WAL's tail to target and puts whichever of SC and SS
// sits above target on its newest snapshot at or below it, all while the stores are closed, then opens
// them the ordinary way and checks both landed on target.
//
// target must be positive, and a target the surviving snapshots and the WAL cannot span is refused
// before anything moves.
func NewStateDBWithRollback(
	ctx context.Context,
	flatkvCfg *flatkvconfig.Config,
	ssCfg config.StateStoreConfig,
	checkpointCfg config.CheckpointConfig,
	target int64,
) (*StateDB, error) {
	if target <= 0 {
		// An empty WAL has a head of 0, which rewindTo reads as nothing to rewind, so without this a
		// caller asking for a rollback would get a plain open instead.
		return nil, fmt.Errorf("rollback target %d is invalid: version 0 means no state, so there is "+
			"nothing to roll back to", target)
	}

	// rewindTo only moves files, so it needs no store open, only where they live.
	offline := &StateDB{flatkvCfg: flatkvCfg, ssCfg: ssCfg}
	if err := offline.rewindTo(target); err != nil {
		return nil, err
	}
	db, err := NewStateDB(ctx, flatkvCfg, ssCfg, checkpointCfg)
	if err != nil {
		return nil, err
	}
	if err := db.matchHeight(target); err != nil {
		return nil, errors.Join(fmt.Errorf("cannot roll back to %d: %w", target, err), db.Close())
	}
	return db, nil
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

// openSC opens SC with no WAL of its own, on the version its files hold: the working copy, or the
// snapshot a rollback has just repointed it at. It replays nothing, so it comes up at or below the
// WAL's head and catchUpTo carries it forward from there.
func (s *StateDB) openSC(ctx context.Context) error {
	sc, err := flatkv.NewCommitStore(ctx, s.flatkvCfg, nil)
	if err != nil {
		return fmt.Errorf("open state commit store: %w", err)
	}
	s.sc = sc
	// Every readonly-* directory under the store is deleted, so this has to run before the process
	// opens a read-only view of its own: after that, the ones a crashed process left are no longer
	// the only ones there.
	if err := s.sc.CleanupOrphanedReadOnlyDirs(); err != nil {
		return fmt.Errorf("clean up orphaned state commit read-only dirs: %w", err)
	}
	if err := s.sc.LoadWorkingCopy(); err != nil {
		return fmt.Errorf("load the state commit store: %w", err)
	}
	return nil
}

// openSS opens the EVM state store and its snapshot manager, leaving it nil when the store is disabled.
func (s *StateDB) openSS() error {
	if !s.ssCfg.Enable {
		return nil
	}
	ss, err := evm.NewEVMStateStore(s.ssCfg.EVMDBDirectory, s.ssCfg)
	if err != nil {
		return fmt.Errorf("open EVM state store: %w", err)
	}
	s.ss = ss
	if err := s.ss.StartSnapshots(s.ssSnapshotRoot(), s.ssCfg, nil); err != nil {
		return fmt.Errorf("start EVM state store snapshot manager: %w", err)
	}
	return nil
}

// startCheckpointSchedule puts SC and SS on one snapshot cadence. It runs before either store is on a
// height, so the blocks SC replays offer themselves to the schedule as live commits do.
func (s *StateDB) startCheckpointSchedule(cfg config.CheckpointConfig) {
	s.checkpointer = controller.NewCheckpointScheduler(cfg)
	s.sc.SetCheckpointScheduler(s.checkpointer)
	if s.ss != nil {
		s.ss.SetCheckpointScheduler(s.checkpointer)
	}
}

// ssSnapshotRoot returns the directory SS keeps its snapshots in.
func (s *StateDB) ssSnapshotRoot() string {
	return utils.GetStateStoreSnapshotsSiblingPath(s.ssCfg.EVMDBDirectory)
}

// storedWALRange is the block range a state WAL holds on disk: the lowest and highest blocks in it,
// both 0 when it holds none.
type storedWALRange struct {
	first, last int64
}

// walConfig returns the config that locates the state WAL on disk.
func (s *StateDB) walConfig() *statewal.Config {
	return flatkv.StateWALConfig(s.flatkvCfg.DataDir)
}

// storedWALRange reads the state WAL's block range from its directory. It takes that directory's
// exclusive lock, so it is only for the window before the WAL opens; GetStoredRange on the open handle
// answers the same question afterwards.
func (s *StateDB) storedWALRange() (storedWALRange, error) {
	stored, first, last, err := statewal.GetRange(s.walConfig())
	if err != nil {
		return storedWALRange{}, fmt.Errorf("read state WAL range: %w", err)
	}
	if !stored {
		return storedWALRange{}, nil
	}
	//nolint:gosec // a block number never approaches the int64 ceiling
	return storedWALRange{first: int64(first), last: int64(last)}, nil
}

// openWALRange reads the block range from the open WAL handle, which storedWALRange's directory lock
// rules out reading once the WAL is open.
func (s *StateDB) openWALRange() (storedWALRange, error) {
	stored, first, last, err := s.wal.GetStoredRange()
	if err != nil {
		return storedWALRange{}, fmt.Errorf("read state WAL range: %w", err)
	}
	if !stored {
		return storedWALRange{}, nil
	}
	//nolint:gosec // a block number never approaches the int64 ceiling
	return storedWALRange{first: int64(first), last: int64(last)}, nil
}

// truncateWAL drops every WAL block above target so the next commit is target+1. A live WAL prunes only
// from its start, so this cuts the tail through the directory, which requires that no WAL be open on it.
func (s *StateDB) truncateWAL(target int64) error {
	//nolint:gosec // target > 0 here, checked by NewStateDBWithRollback
	if err := statewal.PruneAfter(s.walConfig(), uint64(target)); err != nil {
		return fmt.Errorf("truncate state WAL to %d: %w", target, err)
	}
	return nil
}

// Close closes SC, SS and the state WAL, reporting every failure rather than stopping at the first.
// The WAL closes last, since SC replays through it.
func (s *StateDB) Close() error {
	var errs error
	if s.ss != nil {
		if err := s.ss.Close(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close EVM state store: %w", err))
		}
	}
	if s.sc != nil {
		if err := s.sc.Close(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close state commit store: %w", err))
		}
	}
	if s.wal != nil {
		if err := s.wal.Close(); err != nil {
			errs = errors.Join(errs, fmt.Errorf("close state WAL: %w", err))
		}
	}
	return errs
}

// SC returns the state commit store.
func (s *StateDB) SC() *flatkv.CommitStore { return s.sc }

// SS returns the EVM state store, or nil when it is disabled.
func (s *StateDB) SS() *evm.EVMStateStore { return s.ss }

// WAL returns the state WAL. It is the one this StateDB opened, and is not replaced for the StateDB's
// lifetime.
func (s *StateDB) WAL() statewal.StateWAL { return s.wal }

// CheckpointScheduler returns the schedule SC and SS take their snapshot boundaries from.
func (s *StateDB) CheckpointScheduler() *controller.CheckpointScheduler { return s.checkpointer }

// PrunableStores returns the opened stores that can join a prune cycle.
func (s *StateDB) PrunableStores() []controller.PrunableStore {
	stores := make([]controller.PrunableStore, 0, 3)
	if s.sc != nil {
		stores = append(stores, s.sc)
	}
	if s.wal != nil {
		stores = append(stores, s.wal)
	}
	if s.ss != nil {
		stores = append(stores, s.ss)
	}
	return stores
}

func (s *StateDB) CommitStateChanges(blockNum int64, changeset []*proto.NamedChangeSet) error {
	if blockNum < 0 {
		// The WAL numbers blocks with a uint64, so a negative height converts to a block far in the
		// future that the WAL has no way to recognize as a mistake.
		return fmt.Errorf("commit block %d: block number must not be negative", blockNum)
	}

	// No need to flush WAL, since this WAL isn't used for crash recoverability safety (that's the BlockDB's job).
	if err := s.wal.Write(uint64(blockNum), changeset); err != nil {
		return fmt.Errorf("write block %d to state WAL: %w", blockNum, err)
	}
	if err := s.wal.SignalEndOfBlock(); err != nil {
		return fmt.Errorf("end block %d in state WAL: %w", blockNum, err)
	}

	if err := s.sc.CommitStateChanges(blockNum, changeset); err != nil {
		return fmt.Errorf("commit block %d to live state DB: %w", blockNum, err)
	}
	// SS takes the block asynchronously and is not waited on: the WAL is written first, so a shutdown
	// that loses the queue leaves SS behind the WAL, which is the gap catchUpTo replays on the next open.
	if s.ss != nil {
		if err := s.ss.CommitBlock(blockNum, changeset); err != nil {
			return fmt.Errorf("commit block %d to the EVM state store: %w", blockNum, err)
		}
	}

	return nil
}

func (s *StateDB) OpenView() gigatypes.StateView {
	return s.sc.OpenView()
}

// OpenViewAt panics. Serving a past height requires the historical state DB, which is not wired into
// StateDB.
func (s *StateDB) OpenViewAt(blockNum int64) (gigatypes.StateView, bool) {
	panic(fmt.Sprintf(
		"giga: OpenViewAt(%d) is not implemented: the historical state DB is not wired in", blockNum))
}
