package giga

import (
	"context"
	"errors"
	"fmt"
	"math"

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

var _ gigatypes.StateDB = (*StateDB)(nil)

// StateDB fans a committed block out to the state WAL and the two halves of state, and serves
// current-block reads from the state commit store.
//
// It owns all three stores: it opens them, converges them onto one height, and closes them. The WAL in
// particular it owns outright — SC and SS each run without one, so this is the only writer, and the
// replay that brings either of them onto a height reads through this WAL rather than theirs.
type StateDB struct {
	// The state WAL a committed block is written to.
	wal statewal.StateWAL

	// Where wal lives, so a tail truncation can reopen it. A truncation is offline, so the handle above
	// has to be closed and replaced rather than mutated.
	walCfg *statewal.Config

	// The state commit store, which both receives writes and serves current-block reads.
	sc *flatkv.CommitStore

	// ss is nil when the EVM state store is disabled, which leaves it out of the fan-out and out of
	// convergence.
	ss *evm.EVMStateStore

	// The checkpoint schedule both halves of state take their snapshot boundaries from.
	checkpointer *controller.CheckpointScheduler
}

// NewStateDB opens the state commit store, the state WAL and the EVM state store from their configs
// and puts the two halves of state on one checkpoint schedule.
//
// It opens them where it finds them and converges nothing, so the two halves may sit at different
// heights and the returned StateDB is not yet ready to commit. RollbackTo puts them on a height and is
// what makes it ready; the caller names that height, because only it knows what the stores outside
// this StateDB can serve.
//
// SC is constructed with no WAL of its own, since this StateDB writes the WAL on its behalf, and it is
// loaded before the WAL is opened: a store carrying no WAL resolves its own version by reading the WAL
// directory out of band, which takes that directory's exclusive lock. ss is left unopened when
// ssCfg.Enable is false.
//
// The returned StateDB owns all three stores and closes them on Close. A failed call closes whatever it
// had already opened.
func NewStateDB(
	ctx context.Context,
	flatkvCfg *flatkvconfig.Config,
	ssCfg config.StateStoreConfig,
	checkpointCfg config.CheckpointConfig,
) (db *StateDB, retErr error) {
	s := &StateDB{walCfg: flatkv.StateWALConfig(flatkvCfg)}
	defer func() {
		if retErr != nil {
			if closeErr := s.Close(); closeErr != nil {
				retErr = errors.Join(retErr, fmt.Errorf("close a partially opened state DB: %w", closeErr))
			}
		}
	}()

	if err := s.openSC(ctx, flatkvCfg); err != nil {
		return nil, err
	}
	if err := s.openWAL(); err != nil {
		return nil, err
	}
	if err := s.openSS(ssCfg); err != nil {
		return nil, err
	}
	s.startCheckpointSchedule(checkpointCfg)

	if err := s.sc.CleanupOrphanedReadOnlyDirs(); err != nil {
		return nil, fmt.Errorf("clean up orphaned state commit read-only dirs: %w", err)
	}
	return s, nil
}

// openWAL opens the state WAL this StateDB writes both halves of state through.
func (s *StateDB) openWAL() error {
	wal, err := statewal.New(s.walCfg)
	if err != nil {
		return fmt.Errorf("open state WAL at %s: %w", s.walCfg.Path, err)
	}
	s.wal = wal
	return nil
}

// openSC opens the state commit store with no WAL of its own and loads it. The load must precede the
// WAL being opened, since a store with no WAL reads the WAL directory out of band to resolve its
// version, and that read takes the directory's exclusive lock.
func (s *StateDB) openSC(ctx context.Context, cfg *flatkvconfig.Config) error {
	sc, err := flatkv.NewCommitStore(ctx, cfg, nil)
	if err != nil {
		return fmt.Errorf("open state commit store: %w", err)
	}
	s.sc = sc
	if err := s.sc.LoadLatest(); err != nil {
		return fmt.Errorf("load the state commit store: %w", err)
	}
	return nil
}

// openSS opens the EVM state store and its snapshot manager, leaving it nil when the store is disabled.
func (s *StateDB) openSS(cfg config.StateStoreConfig) error {
	if !cfg.Enable {
		return nil
	}
	ss, err := evm.NewEVMStateStore(cfg.EVMDBDirectory, cfg)
	if err != nil {
		return fmt.Errorf("open EVM state store: %w", err)
	}
	s.ss = ss
	if err := s.ss.StartSnapshots(utils.GetStateStoreSnapshotsSiblingPath(ss.Dir()), cfg, nil); err != nil {
		return fmt.Errorf("start EVM state store snapshot manager: %w", err)
	}
	return nil
}

// startCheckpointSchedule puts both halves of state on one snapshot cadence. It is in place before the
// StateDB is put on a height, so a replay that crosses a boundary snapshots there.
func (s *StateDB) startCheckpointSchedule(cfg config.CheckpointConfig) {
	s.checkpointer = controller.NewCheckpointScheduler(cfg)
	s.sc.SetCheckpointScheduler(s.checkpointer)
	if s.ss != nil {
		s.ss.SetCheckpointScheduler(s.checkpointer)
	}
}

// SC returns the state commit store.
func (s *StateDB) SC() *flatkv.CommitStore { return s.sc }

// SS returns the EVM state store, or nil when it is disabled.
func (s *StateDB) SS() *evm.EVMStateStore { return s.ss }

// WAL returns the state WAL. A tail truncation replaces the handle, so this must be re-read after any
// call to RollbackTo rather than cached across one.
func (s *StateDB) WAL() statewal.StateWAL { return s.wal }

// CheckpointScheduler returns the schedule both halves of state take their snapshot boundaries from.
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
	// TODO: Commit changes to SS

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

// Close closes the two halves of state and the WAL they were recovered from, reporting every failure
// rather than stopping at the first. SS goes first and the WAL last, since SC replays through the WAL.
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

// walHead returns the last block the state WAL holds. ok is false on an empty WAL.
func (s *StateDB) walHead() (int64, bool, error) {
	stored, _, last, err := s.wal.GetStoredRange()
	if err != nil {
		return 0, false, fmt.Errorf("read state WAL range: %w", err)
	}
	if !stored {
		return 0, false, nil
	}
	if last > math.MaxInt64 {
		return 0, false, fmt.Errorf("state WAL last block %d exceeds max int64", last)
	}
	return int64(last), true, nil //nolint:gosec // bounds checked above
}

// RollbackTo puts both halves of state on blockNum, rewinding a half above it and replaying a half
// below it. Handling the two directions independently is what lets them have crashed at different
// heights, and is what makes this the call that readies a freshly opened StateDB as well as the one
// that rewinds a running one.
//
// blockNum must be a height the WAL can reach: at or below its head, and at or above the earliest
// block it still holds. Rolling a half of state back needs a snapshot at or below blockNum to restore
// from, which SC always has and SS has only where a checkpoint landed there.
//
// The state WAL ends at blockNum afterwards, so the next commit is blockNum+1. Truncating its tail is
// offline, which closes the handle and opens a replacement, so any reference a caller holds to the WAL
// is invalid after this call and nothing re-hands the new one out. It must therefore not run once the
// prune cycle has taken the WAL, which would leave the collector pruning through a closed instance.
func (s *StateDB) RollbackTo(blockNum int64) error {
	if err := s.rewindSC(blockNum); err != nil {
		return err
	}
	if err := s.truncateWAL(blockNum); err != nil {
		return err
	}
	if err := s.rewindSS(blockNum); err != nil {
		return err
	}
	if err := s.catchUpSC(blockNum); err != nil {
		return err
	}
	return s.catchUpSS(blockNum)
}

// rewindSC drops SC back to a snapshot boundary at or below target when it sits above target, leaving
// catchUpSC to replay the WAL from there. A store already at or below target is left alone.
//
// SC moves between snapshot boundaries on its own and replays nothing itself, which is what keeps the
// WAL on this side of the split.
func (s *StateDB) rewindSC(target int64) error {
	if s.sc.Version() <= target {
		return nil
	}
	if _, err := s.sc.RewindToSnapshotAtOrBelow(target); err != nil {
		return fmt.Errorf("rewind the state commit store to a snapshot at or below %d: %w", target, err)
	}
	return nil
}

// rewindSS drops SS back to target when it sits above it, restoring its own snapshot and replaying the
// WAL lent for the call.
func (s *StateDB) rewindSS(target int64) error {
	if s.ss == nil || s.ss.GetLatestVersion() <= target {
		return nil
	}
	if err := s.ss.RollbackTo(target, s.wal); err != nil {
		return fmt.Errorf("roll the EVM state store back to %d: %w", target, err)
	}
	return nil
}

// truncateWAL drops every WAL block above target so the next commit is target+1, and is a no-op when
// the WAL already ends at or below target.
//
// The truncation runs against the directory rather than the open WAL, since a live WAL prunes only from
// its start, so the handle is closed and replaced by one over the truncated directory.
func (s *StateDB) truncateWAL(target int64) error {
	head, ok, err := s.walHead()
	if err != nil {
		return err
	}
	if !ok || head <= target {
		return nil
	}

	if err := s.wal.Close(); err != nil {
		return fmt.Errorf("close state WAL before truncating it to %d: %w", target, err)
	}
	//nolint:gosec // a WAL head above target means target >= 0
	if err := statewal.PruneAfter(s.walCfg.Path, uint64(target)); err != nil {
		return fmt.Errorf("truncate state WAL to %d: %w", target, err)
	}
	wal, err := statewal.New(s.walCfg)
	if err != nil {
		return fmt.Errorf("reopen state WAL truncated to %d: %w", target, err)
	}
	s.wal = wal
	return nil
}

// catchUpSC replays the WAL blocks above SC's version into it, up to target.
//
// SC owns no WAL, so committing these blocks appends nothing: re-writing blocks that were read from the
// WAL is the double-append this split exists to prevent.
func (s *StateDB) catchUpSC(target int64) error {
	from := s.sc.Version()
	if from >= target {
		return nil
	}
	return s.replay(from, target, func(block int64, changesets []*proto.NamedChangeSet) error {
		return s.sc.CommitStateChanges(block, changesets)
	})
}

// catchUpSS replays the state WAL into the EVM state store up to target.
func (s *StateDB) catchUpSS(target int64) error {
	if s.ss == nil {
		return nil
	}
	if err := s.ss.CatchUpFrom(s.wal, target); err != nil {
		return fmt.Errorf("catch the EVM state store up to %d: %w", target, err)
	}
	return nil
}

// replay feeds apply every WAL block in (from, target], in order.
//
// Blocks are contiguous and the first is 1, so replay always starts at from+1. A WAL beginning later
// than that is missing history the destination still needs rather than simply holding a shorter range:
// starting at the WAL's own first block instead would skip those blocks and commit a state matching no
// chain history. Retention never drops a block a store still needs, so reaching that is data loss.
func (s *StateDB) replay(from, target int64, apply func(int64, []*proto.NamedChangeSet) error) error {
	stored, first, last, err := s.wal.GetStoredRange()
	if err != nil {
		return fmt.Errorf("read state WAL range: %w", err)
	}
	if !stored {
		return nil
	}

	start := uint64(from) + 1        //nolint:gosec // callers replay forward from a version >= 0
	end := min(last, uint64(target)) //nolint:gosec // target > from >= 0
	if end < start {
		return nil
	}
	if first > start {
		return fmt.Errorf("state WAL starts at block %d but replay must start at block %d: blocks %d-%d "+
			"are missing (data loss or corruption)", first, start, start, first-1)
	}

	it, err := s.wal.Iterator(start, end)
	if err != nil {
		return fmt.Errorf("state WAL iterator [%d,%d]: %w", start, end, err)
	}
	defer func() { _ = it.Close() }()

	for {
		hasNext, err := it.Next()
		if err != nil {
			return fmt.Errorf("iterate state WAL: %w", err)
		}
		if !hasNext {
			break
		}
		block, changesets := it.Entry()
		if err := apply(int64(block), changesets); err != nil { //nolint:gosec // block <= end
			return fmt.Errorf("replay block %d: %w", block, err)
		}
	}
	return nil
}
