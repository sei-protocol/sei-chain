package giga

import (
	"context"
	"errors"
	"fmt"
	"os"

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
	if err := s.openSS(); err != nil {
		return nil, err
	}
	if err := s.discardStateAboveTheWAL(wal.head()); err != nil {
		return nil, err
	}
	if err := s.openSC(ctx); err != nil {
		return nil, err
	}
	if err := s.openWAL(); err != nil {
		return nil, err
	}
	s.startCheckpointSchedule(checkpointCfg)

	if err := s.sc.CleanupOrphanedReadOnlyDirs(); err != nil {
		return nil, fmt.Errorf("clean up orphaned state commit read-only dirs: %w", err)
	}
	return s, s.catchUpToWAL()
}

// NewStateDBWithRollback rolls SC, SS and the state WAL back to target and then opens them, so the
// returned StateDB commits target+1. It cuts the WAL's tail to target and points SC and SS at their
// newest snapshot at or below it, all while the stores are closed, then opens them the ordinary way.
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
		return nil, errors.Join(err, db.Close())
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

// discardStateAboveTheWAL puts SC and SS back on the WAL's head when either sits above it, rewinding
// the store onto its newest snapshot at or below head for the replay to carry forward. SC must not be
// open yet.
//
// A commit writes the WAL unflushed, so a crash can lose its tail while the state committed above that
// tail survives. Those blocks are re-executed from the block store, which a store still holding them
// cannot accept, so the state above the WAL is dropped rather than kept.
func (s *StateDB) discardStateAboveTheWAL(head int64) error {
	if head == 0 {
		// An empty WAL says nothing about where state belongs: one pruned away behind a snapshot leaves
		// the state it covered as the only record of it.
		return nil
	}
	if err := s.ensureStoresCanRewindTo(head); err != nil {
		return err
	}
	if err := s.discardSCAboveTheWAL(head); err != nil {
		return err
	}
	return s.discardSSAboveTheWAL(head)
}

// discardSCAboveTheWAL rewinds SC onto its newest snapshot at or below head when it would open above
// head. It reads the version SC opens at from its files, so it runs before SC opens and the store opens
// once, on that snapshot.
func (s *StateDB) discardSCAboveTheWAL(head int64) error {
	opensAt, err := flatkv.GetLatestVersion(s.flatkvCfg.DataDir)
	if err != nil {
		return fmt.Errorf("read the version the state commit store opens at: %w", err)
	}
	if opensAt <= head {
		return nil
	}
	if err := s.rewindSC(head); err != nil {
		return err
	}
	logger.Info("state commit store rewound onto the state WAL's head: the state above it is not one the "+
		"WAL can replay", "was", opensAt, "head", head)
	return nil
}

// discardSSAboveTheWAL rewinds SS onto its newest snapshot at or below head when it opened above head.
//
// SS keeps no working copy, so the version it holds is only known once it is open and the rewind needs
// it closed: it reopens on the snapshot whenever there was something above head to discard.
func (s *StateDB) discardSSAboveTheWAL(head int64) error {
	if s.ss == nil {
		return nil
	}
	openedAt := s.ss.GetLatestVersion()
	if openedAt <= head {
		return nil
	}
	if err := s.ss.Close(); err != nil {
		return fmt.Errorf("close the EVM state store to rewind it onto %d: %w", head, err)
	}
	s.ss = nil

	if err := s.rewindSS(head); err != nil {
		return err
	}
	logger.Info("EVM state store rewound onto the state WAL's head: the state above it is not one the "+
		"WAL can replay", "was", openedAt, "head", head)
	return s.openSS()
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
	if s.ss != nil {
		if err := s.ss.CommitBlock(blockNum, changeset); err != nil {
			return fmt.Errorf("commit block %d to the EVM state store: %w", blockNum, err)
		}
		s.ss.WaitForPendingWrites()
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

// rewindTo discards every snapshot of SC and SS above target and cuts the WAL's tail to it. All three
// stores must be closed. SS sitting above target is restored onto its newest snapshot at or below it.
//
// A target the surviving snapshots and the WAL cannot span is refused before anything moves.
func (s *StateDB) rewindTo(target int64) error {
	wal, err := s.storedWALRange()
	if err != nil {
		return err
	}
	if head := wal.head(); head < target {
		return fmt.Errorf("cannot roll back to %d: the state WAL ends at %d, so no replay reaches the "+
			"target", target, head)
	}
	if err := s.ensureStoresCanRewindTo(target); err != nil {
		return err
	}
	if err := s.ensureWALCanReplayTo(wal, target); err != nil {
		return err
	}

	if err := s.dropSnapshotsAbove(target); err != nil {
		return err
	}
	if err := s.rewindSSIfAbove(target); err != nil {
		return err
	}
	// Last, so that an interruption leaves the WAL still above target and a restart comes back here.
	return s.truncateWAL(target)
}

// dropSnapshotsAbove removes the snapshots of SC and SS above target.
//
// It runs whether or not either store is above target, because an interrupted rollback leaves exactly a
// store that is not: it reads as the snapshot it was repointed at, with the branch above it still on
// disk. Left there, a later rollback lands on a snapshot from the branch this one abandoned.
func (s *StateDB) dropSnapshotsAbove(target int64) error {
	if err := flatkv.DropSnapshotsAbove(s.flatkvCfg.DataDir, target); err != nil {
		return fmt.Errorf("cannot roll back the state commit store to %d: %w", target, err)
	}
	if !s.ssCfg.Enable {
		return nil
	}
	if err := evm.DropSnapshotsAbove(s.ssSnapshotRoot(), target); err != nil {
		return fmt.Errorf("cannot roll back the EVM state store to %d: %w", target, err)
	}
	return nil
}

// catchUpTo replays the WAL into SC and SS up to target, and reports a store that did not land on it.
//
// One pass feeds both. It spans from the lower of their two versions, and each block goes only to the
// store still below it, so the WAL is read once rather than once per store.
func (s *StateDB) catchUpTo(target int64) error {
	// Ahead of the pass, which is what erases the evidence it works from, and here rather than in the
	// open because every replay of this WAL comes through this function.
	if err := s.sc.RebuildIfUnreachable(target); err != nil {
		return fmt.Errorf("rebuild the state commit store's working copy: %w", err)
	}
	scFrom := s.sc.Version()
	ssFrom, ssReplays, err := s.ssReplayStart(target)
	if err != nil {
		return err
	}
	from := scFrom
	if ssReplays {
		from = min(from, ssFrom)
	}

	if err := s.replay(from, target, func(block int64, changesets []*proto.NamedChangeSet) error {
		if block > scFrom {
			// SC owns no WAL, so re-committing a block read from this one appends nothing. It does run
			// SC's commit path, so the checkpoint schedule is asked at each block SC takes.
			if err := s.sc.CommitStateChanges(block, changesets); err != nil {
				return err
			}
		}
		if ssReplays && block > ssFrom {
			return s.ss.ApplyReplayedBlock(block, changesets)
		}
		return nil
	}); err != nil {
		return err
	}
	return s.matchHeight(target)
}

// ssReplayStart returns the version SS replays forward from, and whether it replays at all. SS is left
// out when it is disabled, already on target, or empty with a WAL that can no longer rebuild it.
func (s *StateDB) ssReplayStart(target int64) (from int64, replays bool, err error) {
	if s.ss == nil {
		return 0, false, nil
	}
	from = s.ss.GetLatestVersion()
	if from >= target {
		return 0, false, nil
	}
	fillForward, err := s.ssFillsForward()
	if err != nil {
		return 0, false, err
	}
	if fillForward {
		logger.Info("EVM state store left empty to fill forward: it holds no history and the state WAL "+
			"no longer reaches block 1", "target", target)
		return 0, false, nil
	}
	return from, true, nil
}

// ssFillsForward reports whether SS holds no history the WAL can still rebuild, which leaves it out of
// the replay: it stays empty and starts filling at the block after the target.
//
// This is the treatment recoveryTarget gives an empty receipt store. It applies only to a WAL that has
// had a retention cut, where the alternative is refusing to start over a store that is merely new.
func (s *StateDB) ssFillsForward() (bool, error) {
	if s.ss == nil || s.ss.GetLatestVersion() > 0 {
		return false, nil
	}
	stored, first, _, err := s.wal.GetStoredRange()
	if err != nil {
		return false, fmt.Errorf("read state WAL range: %w", err)
	}
	return !stored || first > 1, nil
}

// matchHeight checks SC and SS against blockNum and reports the one that is not on it. An SS left empty
// to fill forward is not held to blockNum.
func (s *StateDB) matchHeight(blockNum int64) error {
	if got := s.sc.Version(); got != blockNum {
		return fmt.Errorf("rollback to %d left the state commit store on %d", blockNum, got)
	}
	if s.ss == nil {
		return nil
	}
	got := s.ss.GetLatestVersion()
	if got == blockNum {
		return nil
	}
	if got == 0 {
		fillForward, err := s.ssFillsForward()
		if err != nil {
			return err
		}
		if fillForward {
			return nil
		}
	}
	return fmt.Errorf("rollback to %d left the EVM state store on %d", blockNum, got)
}

// rewindSC points SC's files at the snapshot at or below target and drops the snapshots above it. It
// runs before SC opens, so the store opens once, on that snapshot.
//
// Moving the current link is the whole rewind: SC's databases are not read, and the version they hold
// is discarded with the working copy.
func (s *StateDB) rewindSC(target int64) error {
	if _, err := flatkv.RewindClosedStoreTo(s.flatkvCfg.DataDir, target); err != nil {
		return fmt.Errorf("rewind the state commit store to a snapshot at or below %d: %w", target, err)
	}
	return nil
}

// rewindSSIfAbove restores SS onto its newest snapshot at or below target when the live databases sit
// above it. DropSnapshotsAbove only removes snapshot directories.
func (s *StateDB) rewindSSIfAbove(target int64) error {
	if !s.ssCfg.Enable {
		return nil
	}
	openedAt, err := s.ssLatestVersion()
	if err != nil {
		return fmt.Errorf("cannot roll back the EVM state store to %d: %w", target, err)
	}
	if openedAt <= target {
		return nil
	}
	return s.rewindSS(target)
}

// ensureWALCanReplayTo returns an error when the WAL does not hold every block from the height a store
// would land on to target. It reads only.
func (s *StateDB) ensureWALCanReplayTo(wal storedWALRange, target int64) error {
	from, err := s.heightSCReplaysFrom(target)
	if err != nil {
		return err
	}
	if err := wal.mustCoverAfter(from, target); err != nil {
		return err
	}
	ssFrom, ssReplays, err := s.heightSSReplaysFrom(wal, target)
	if err != nil {
		return err
	}
	if !ssReplays {
		return nil
	}
	return wal.mustCoverAfter(ssFrom, target)
}

// heightSCReplaysFrom returns the version SC will hold after rewindTo, before catch-up replays to target.
func (s *StateDB) heightSCReplaysFrom(target int64) (int64, error) {
	opensAt, err := flatkv.GetWorkingCopyVersion(s.flatkvCfg.DataDir)
	if err != nil {
		return 0, fmt.Errorf("read the version the state commit store opens at: %w", err)
	}
	if opensAt <= target {
		return opensAt, nil
	}
	base, err := flatkv.SnapshotAtOrBelow(s.flatkvCfg.DataDir, target)
	if err != nil {
		return 0, fmt.Errorf("cannot roll back the state commit store to %d: %w", target, err)
	}
	return base, nil
}

// heightSSReplaysFrom returns the version SS will hold after rewindTo, and whether catch-up replays it
// to target. An empty store the WAL cannot rebuild is left out to fill forward.
func (s *StateDB) heightSSReplaysFrom(wal storedWALRange, target int64) (from int64, replays bool, err error) {
	if !s.ssCfg.Enable {
		return 0, false, nil
	}
	openedAt, err := s.ssLatestVersion()
	if err != nil {
		return 0, false, fmt.Errorf("cannot roll back the EVM state store to %d: %w", target, err)
	}
	if openedAt > target {
		base, err := evm.SnapshotAtOrBelow(s.ssSnapshotRoot(), target)
		if err != nil {
			return 0, false, fmt.Errorf("cannot roll back the EVM state store to %d: %w", target, err)
		}
		if base >= target {
			return 0, false, nil
		}
		return base, true, nil
	}
	if openedAt == target {
		return 0, false, nil
	}
	if openedAt == 0 && (wal.last == 0 || wal.first > 1) {
		return 0, false, nil
	}
	return openedAt, true, nil
}

// mustCoverAfter returns an error when this WAL does not hold every block in (from, target].
func (r storedWALRange) mustCoverAfter(from, target int64) error {
	if from >= target {
		return nil
	}
	start := from + 1
	if r.last == 0 {
		return fmt.Errorf("cannot roll back to %d: the state WAL holds no blocks %d-%d", target, start, target)
	}
	if r.first > uint64(start) { //nolint:gosec // start is from+1 with from >= 0
		return fmt.Errorf("cannot roll back to %d: the state WAL starts at block %d but replay must start "+
			"at block %d: blocks %d-%d are missing", target, r.first, start, start, r.first-1)
	}
	return nil
}

// rewindSS puts SS's files on the snapshot at or below target and drops the snapshots above it. It runs
// with SS closed, so the next open lands on that snapshot. A target with no snapshot at or below it is
// refused.
func (s *StateDB) rewindSS(target int64) error {
	if !s.ssCfg.Enable {
		return nil
	}
	if _, err := evm.RewindClosedStoreTo(
		s.ssCfg.EVMDBDirectory, s.ssSnapshotRoot(), s.ssCfg.SeparateEVMSubDBs, target); err != nil {
		return fmt.Errorf("rewind the EVM state store to a snapshot at or below %d: %w", target, err)
	}
	return nil
}

// ensureStoresCanRewindTo returns an error when SC or SS sits above target and has no snapshot at or
// below it. A store already at or below target needs no snapshot.
func (s *StateDB) ensureStoresCanRewindTo(target int64) error {
	if err := s.ensureSCCanRewindTo(target); err != nil {
		return err
	}
	return s.ensureSSCanRewindTo(target)
}

// ensureSCCanRewindTo returns an error when SC sits above target and has no snapshot at or below it.
// Snapshot 0 counts. A store already at or below target needs none.
func (s *StateDB) ensureSCCanRewindTo(target int64) error {
	_, snapErr := flatkv.SnapshotAtOrBelow(s.flatkvCfg.DataDir, target)
	if snapErr == nil {
		return nil
	}
	opensAt, err := flatkv.GetWorkingCopyVersion(s.flatkvCfg.DataDir)
	if err != nil {
		return fmt.Errorf("cannot roll back the state commit store to %d: %w", target, err)
	}
	if opensAt <= target {
		return nil
	}
	return fmt.Errorf("cannot roll back the state commit store to %d: %w", target, snapErr)
}

// ensureSSCanRewindTo returns an error when SS sits above target and has no snapshot at or below it.
// A store at or below target needs no snapshot.
func (s *StateDB) ensureSSCanRewindTo(target int64) error {
	if !s.ssCfg.Enable {
		return nil
	}
	base, err := evm.SnapshotAtOrBelow(s.ssSnapshotRoot(), target)
	if err != nil {
		return fmt.Errorf("cannot roll back the EVM state store to %d: %w", target, err)
	}
	if base > 0 {
		return nil
	}
	openedAt, err := s.ssLatestVersion()
	if err != nil {
		return fmt.Errorf("cannot roll back the EVM state store to %d: %w", target, err)
	}
	if openedAt <= target {
		return nil
	}
	return fmt.Errorf("cannot roll back the EVM state store to %d: no snapshot at or below target", target)
}

// ssLatestVersion returns the height SS currently holds, or 0 when the store has never been written.
func (s *StateDB) ssLatestVersion() (int64, error) {
	if s.ss != nil {
		return s.ss.GetLatestVersion(), nil
	}
	if _, err := os.Stat(s.ssCfg.EVMDBDirectory); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("stat the EVM state store: %w", err)
	}
	ss, err := evm.NewEVMStateStore(s.ssCfg.EVMDBDirectory, s.ssCfg)
	if err != nil {
		return 0, fmt.Errorf("read the EVM state store version: %w", err)
	}
	version := ss.GetLatestVersion()
	if err := ss.Close(); err != nil {
		return 0, fmt.Errorf("close the EVM state store after reading its version: %w", err)
	}
	return version, nil
}

// ssSnapshotRoot returns the directory SS keeps its snapshots in.
func (s *StateDB) ssSnapshotRoot() string {
	return utils.GetStateStoreSnapshotsSiblingPath(s.ssCfg.EVMDBDirectory)
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
	return storedWALRange{first: first, last: last}, nil
}

// storedWALRange is the block range a state WAL holds on disk. An empty WAL is the zero value.
type storedWALRange struct {
	first, last uint64
}

// head returns the highest block the WAL holds, or 0 when it holds none.
func (r storedWALRange) head() int64 {
	//nolint:gosec // a block number never approaches the int64 ceiling
	return int64(r.last)
}

// catchUpToWAL replays the WAL into SC and SS up to the last block it holds, which is the height state
// committed to. An empty WAL still rebuilds a working copy that sits above the current snapshot.
//
// A commit writes the WAL before either store, so a crash between the two leaves one of them a block
// behind. Committing from behind the WAL is rejected, so this is what makes an opened StateDB able to
// commit.
func (s *StateDB) catchUpToWAL() error {
	stored, _, last, err := s.wal.GetStoredRange()
	if err != nil {
		return fmt.Errorf("read state WAL range: %w", err)
	}
	if !stored {
		if err := s.sc.RebuildIfUnreachable(0); err != nil {
			return fmt.Errorf("rebuild the state commit store's working copy: %w", err)
		}
		return nil
	}
	//nolint:gosec // a block number never approaches the int64 ceiling
	return s.catchUpTo(int64(last))
}

// replay feeds apply every WAL block in (from, target], in order.
//
// Blocks are contiguous from block 1, so a replay always starts at from+1. A WAL that begins later is
// missing history the destination needs: starting at the WAL's own first block would skip those blocks
// and commit a state matching no chain history, so it is reported as data loss.
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
