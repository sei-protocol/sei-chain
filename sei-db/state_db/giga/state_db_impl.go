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

// StateDB fans a committed block out to the state WAL and the two halves of state, and serves
// current-block reads from the state commit store.
//
// It owns all three stores: it opens them, converges them onto one height, and closes them. The WAL in
// particular it owns outright — SC and SS each run without one, so this is the only writer, and the
// replay that brings either of them onto a height reads through this WAL rather than theirs.
type StateDB struct {
	// Where the state commit store and the state WAL live.
	flatkvCfg *flatkvconfig.Config

	// Where the EVM state store lives, and whether it is enabled at all.
	ssCfg config.StateStoreConfig

	// The state WAL a committed block is written to.
	wal statewal.StateWAL

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
// It opens them where it finds them and converges them on the WAL: both halves are replayed up to its
// head, which is the height state committed to, and the returned StateDB commits the block after it. A
// caller that needs them on an earlier height names it to NewStateDBWithRollback instead.
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

	if err := s.sc.CleanupOrphanedReadOnlyDirs(); err != nil {
		return nil, fmt.Errorf("clean up orphaned state commit read-only dirs: %w", err)
	}
	return s, s.catchUpToWAL()
}

// NewStateDBWithRollback rolls the three stores back to target and then opens them, so the returned
// StateDB is ready to commit target+1.
//
// The rollback is over before the open begins: it cuts the WAL's tail to target and points each half of
// state at the newest snapshot at or below it, all of which needs those stores closed. What is left is
// a set of stores a plain open converges on the WAL's head, and that head is now target — which is why
// this ends in the ordinary constructor rather than an open of its own.
//
// target must be positive, and one the surviving snapshots and the WAL cannot span is refused before
// anything moves.
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
	return NewStateDB(ctx, flatkvCfg, ssCfg, checkpointCfg)
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

// openWAL opens the state WAL this StateDB writes both halves of state through.
func (s *StateDB) openWAL() error {
	wal, err := flatkv.OpenStateWAL(s.flatkvCfg)
	if err != nil {
		return fmt.Errorf("open state WAL: %w", err)
	}
	s.wal = wal
	return nil
}

// openSC opens the state commit store with no WAL of its own, on the version its files hold: the
// working copy, or the snapshot a rollback has just repointed it at.
//
// It replays nothing, so the store comes up at or below the WAL's head and catchUpTo is what carries it
// the rest of the way. That keeps every block SC applies coming through the WAL this StateDB owns,
// rather than through a WAL SC opens behind it.
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

// startCheckpointSchedule puts both halves of state on one snapshot cadence. It is wired before either
// half is on a height, so SC's catch-up commits ask it as live blocks do, while SS replays outside its
// commit path and asks nothing.
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

// WAL returns the state WAL, which is the one this StateDB was opened with: nothing replaces it for the
// lifetime of the StateDB.
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

// rewindTo brings both halves of state down to target and cuts the WAL's tail to it, discarding the
// state and the snapshots above it. It runs while all three stores are closed, which is what every
// step here needs: each moves the files a store is about to open on, or the WAL's own directory.
//
// The WAL's own head decides which of the three cases this is, because every commit reaches the WAL
// before either half of state: neither half can be above a block the WAL does not hold. A head below
// target is a target no replay reaches; a head on target means nothing above target was ever committed,
// so there is nothing to rewind and the open that follows converges both halves there anyway, which is
// the normal startup; only a head above target is a rollback.
//
// The cut comes last so that an interruption leaves a WAL still above target, which is what brings a
// restart back through here rather than down the case that finds nothing to do.
func (s *StateDB) rewindTo(target int64) error {
	wal, err := s.storedWALRange()
	if err != nil {
		return err
	}
	if head := wal.head(); head < target {
		return fmt.Errorf("cannot roll back to %d: the state WAL ends at %d, and neither half of state "+
			"holds a block it does not, so nothing reaches the target", target, head)
	} else if head == target {
		return nil
	}

	if err := s.requireReachable(target, wal); err != nil {
		return err
	}
	if err := s.rewindSC(target); err != nil {
		return err
	}
	if err := s.rewindSS(target); err != nil {
		return err
	}
	return s.truncateWAL(target)
}

// catchUpTo replays the WAL into both halves of state up to target and reports one that did not land on
// it.
//
// One pass feeds both halves. Each starts from where its own files left it, so the pass spans from the
// lower of the two and every block goes only to the half still below it, which reads the WAL once for
// the pair rather than once each.
//
// SC owns no WAL, so re-committing a block read from this one appends nothing — the double-append the
// split exists to prevent. It does run SC's commit path otherwise, so the checkpoint schedule is asked
// at each block SC takes, while SS applies outside its own commit path and asks nothing.
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

// ssReplayStart returns the version SS replays forward from and whether it replays at all. A store that
// is disabled, already on target, or left empty to fill forward takes no part in the pass.
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

// requireReachable establishes that the rollback's replay can bridge the gap the rewinds are about to
// open, and names what is missing when it cannot.
//
// It runs before the rollback moves anything because every step of one is irreversible — snapshots above
// the target are deleted and the WAL is cut back to it — while the replay that needs the blocks in
// between runs last. Failing there leaves a node that will not start and no longer holds what a second
// attempt at a different height would need.
func (s *StateDB) requireReachable(target int64, wal storedWALRange) error {
	base, err := s.rollbackBase(target)
	if err != nil {
		return err
	}
	if base >= target {
		return nil
	}
	//nolint:gosec // base >= 0 and target > 0
	from, to := uint64(base)+1, uint64(target)
	if wal.first > from || wal.last < to {
		return fmt.Errorf("cannot roll back to %d: replaying onto %d needs blocks %d-%d, but the state "+
			"WAL only holds %d-%d", target, base, from, to, wal.first, wal.last)
	}
	return nil
}

// rollbackBase returns the height a rollback to target replays forward from: the lower of the snapshots
// the two halves land on, since each is rewound to its own newest snapshot at or below target.
//
// It reads the snapshot trees rather than the stores, both because neither has opened yet and because
// where they land does not depend on where they are now. A half with no snapshot at or below target
// lands on 0 and is rebuilt from block 1, which is a demand on the WAL like any other and is refused
// here when the WAL cannot meet it.
func (s *StateDB) rollbackBase(target int64) (int64, error) {
	base, err := flatkv.SnapshotAtOrBelow(s.flatkvCfg.DataDir, target)
	if err != nil {
		return 0, fmt.Errorf("cannot roll back to %d: read the state commit store's snapshots: %w",
			target, err)
	}
	if !s.ssCfg.Enable {
		return base, nil
	}

	ssBase, err := evm.SnapshotAtOrBelow(s.ssSnapshotRoot(), target)
	if err != nil {
		return 0, fmt.Errorf("cannot roll back to %d: read the EVM state store's snapshots: %w",
			target, err)
	}
	return min(base, ssBase), nil
}

// ssFillsForward reports whether SS holds no history that the WAL can still rebuild, which leaves it
// out of convergence: it stays empty and starts filling at the block after the target.
//
// A store that holds nothing has nothing to disagree with, which is the treatment recoveryTarget gives
// an empty receipt store. Replaying one the WAL still covers is better, since it comes out holding real
// history, so this is the answer only for a WAL that has had a retention cut — where the alternative is
// refusing to start and calling a store that is merely new data loss.
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

// matchHeight checks both halves of state against blockNum and reports the one that is not on it.
//
// An SS still holding nothing is one ssReplayStart left out of the pass to fill forward, since any
// replay of it would have landed on blockNum, so it is not held to the height it was left off.
func (s *StateDB) matchHeight(blockNum int64) error {
	if got := s.sc.Version(); got != blockNum {
		return fmt.Errorf("rollback to %d left the state commit store on %d", blockNum, got)
	}
	if s.ss == nil || s.ss.GetLatestVersion() == 0 {
		return nil
	}
	if got := s.ss.GetLatestVersion(); got != blockNum {
		return fmt.Errorf("rollback to %d left the EVM state store on %d", blockNum, got)
	}
	return nil
}

// rewindSC points SC's files at the snapshot at or below target and drops the snapshots above it,
// leaving the catch-up to replay the WAL from there. It runs before SC opens, so the store opens once,
// on that snapshot.
//
// Where SC's open lands is set by the snapshot its current link names, so moving the link is the whole
// rewind: nothing here reads what its databases hold, and the version they hold is discarded along with
// the working copy. SC replays nothing itself, which is what keeps the WAL on this side of the split.
func (s *StateDB) rewindSC(target int64) error {
	if _, err := flatkv.RewindClosedStoreTo(s.flatkvCfg.DataDir, target); err != nil {
		return fmt.Errorf("rewind the state commit store to a snapshot at or below %d: %w", target, err)
	}
	return nil
}

// rewindSS puts SS's files on the snapshot at or below target and drops the snapshots above it, leaving
// the catch-up to replay the WAL from there. It runs before SS opens, so the store opens once, on that
// snapshot.
//
// SS keeps no working copy, so the rewind restores its databases from the snapshot outright. With no
// snapshot at or below target it is left empty for the replay to rebuild from block 1, which
// requireReachable has established the WAL can still do.
func (s *StateDB) rewindSS(target int64) error {
	if !s.ssCfg.Enable {
		return nil
	}
	_, err := evm.RewindClosedStoreTo(
		s.ssCfg.EVMDBDirectory, s.ssSnapshotRoot(), s.ssCfg.SeparateEVMSubDBs, target)
	if err != nil {
		return fmt.Errorf("rewind the EVM state store to a snapshot at or below %d: %w", target, err)
	}
	return nil
}

// ssSnapshotRoot locates the EVM state store's snapshots, which is where they are read and rewound
// before the store opens.
func (s *StateDB) ssSnapshotRoot() string {
	return utils.GetStateStoreSnapshotsSiblingPath(s.ssCfg.EVMDBDirectory)
}

// truncateWAL drops every WAL block above target so the next commit is target+1.
//
// A live WAL prunes only from its start, so this cuts the tail through the directory instead, which
// requires that no WAL be open on it.
func (s *StateDB) truncateWAL(target int64) error {
	//nolint:gosec // target > 0 here, checked by NewStateDBWithRollback
	if err := statewal.PruneAfter(s.walConfig(), uint64(target)); err != nil {
		return fmt.Errorf("truncate state WAL to %d: %w", target, err)
	}
	return nil
}

// walConfig locates the state WAL, which is where it is read and cut before it opens.
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

// head returns the highest block the WAL holds, and 0 for an empty WAL, which is below every height a
// rollback can target.
func (r storedWALRange) head() int64 {
	//nolint:gosec // a block number never approaches the int64 ceiling
	return int64(r.last)
}

// catchUpToWAL replays the WAL into both halves of state up to the last block it holds, which is the
// height the WAL says state committed to.
//
// A commit writes the WAL before either half, so a crash between the two leaves a half one block behind,
// and each opens where its own files left it. Committing from behind the WAL is rejected outright — the
// block is already written — so this is what makes an opened StateDB able to commit.
func (s *StateDB) catchUpToWAL() error {
	stored, _, last, err := s.wal.GetStoredRange()
	if err != nil {
		return fmt.Errorf("read state WAL range: %w", err)
	}
	if !stored {
		return nil
	}
	//nolint:gosec // a block number never approaches the int64 ceiling
	return s.catchUpTo(int64(last))
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
