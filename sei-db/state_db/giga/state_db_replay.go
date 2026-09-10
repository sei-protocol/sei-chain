package giga

import (
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
)

// replayLogInterval bounds how often a running replay reports how far it has got.
const replayLogInterval = 30 * time.Second

// rewindTo puts whichever of SC and SS holds state above target on its newest snapshot at or below it,
// drops every snapshot of both above target, and cuts the WAL's tail to it. All three stores must be
// closed, and a store holding nothing above target is left where it is, for the replay to carry forward.
//
// A target the surviving snapshots and the WAL cannot span is refused before the WAL is cut, so every
// target this one could reach is still reachable on a retry. SS is asked once SC has moved, so a
// refusal from SS leaves SC on its snapshot and a retry replays from there.
func (s *StateDB) rewindTo(target int64) error {
	wal, err := s.storedWALRange()
	if err != nil {
		return err
	}
	if wal.last < target {
		return fmt.Errorf("cannot roll back to %d: the state WAL ends at %d, so no replay reaches the "+
			"target", target, wal.last)
	}

	// First, so that a refusal from SC comes back with every snapshot still on disk. Once SC has moved,
	// its own snapshots above where it landed are gone.
	if err := s.discardStateAbove(wal, target); err != nil {
		return fmt.Errorf("cannot roll back to %d: %w", target, err)
	}
	if err := s.dropSnapshotsAbove(target); err != nil {
		return err
	}
	// Last, so that an interruption leaves the WAL still above target and a restart comes back here.
	return s.truncateWAL(target)
}

// discardStateAboveTheWAL puts each store back on the WAL's head when it sits above it, onto its
// newest snapshot at or below the head for the replay to carry forward. Every store must be closed.
//
// A commit writes the WAL unflushed, so a crash can lose its tail while the state committed above that
// tail survives. Those blocks are re-executed from the block store, which a store still holding them
// cannot accept, so the state above the WAL is dropped rather than kept.
func (s *StateDB) discardStateAboveTheWAL(wal storedWALRange) error {
	head := wal.last
	if head == 0 {
		// An empty WAL says nothing about where state belongs: one pruned away behind a snapshot leaves
		// the state it covered as the only record of it.
		return nil
	}
	if err := s.discardStateAbove(wal, head); err != nil {
		// Named for the open, not for a rollback: nobody asked for one, and an operator sent looking for
		// the rollback they did not run is an operator not looking at the WAL head that refused.
		return fmt.Errorf("cannot open on the state WAL's head %d: %w", head, err)
	}
	return nil
}

// discardStateAbove puts whichever of SC and SS holds state above target onto its newest snapshot at or
// below it. Both stores must be closed, and a store holding nothing above target is left where it is,
// for the replay to carry forward.
//
// Each store is handed the WAL's first block and refuses, without moving, a target this WAL cannot
// replay it back up to. SC is put back first, so a refusal from SS can leave SC already rewound.
func (s *StateDB) discardStateAbove(wal storedWALRange, target int64) error {
	if _, err := flatkv.DiscardStateAbove(s.flatkvCfg.DataDir, target, wal.first); err != nil {
		return fmt.Errorf("the state commit store cannot reach %d: %w", target, err)
	}
	if !s.ssCfg.Enable {
		return nil
	}
	if _, err := evm.DiscardStateAbove(
		s.ssCfg, s.ssSnapshotRoot(), target, wal.first); err != nil {
		return fmt.Errorf("the EVM state store cannot reach %d: %w", target, err)
	}
	return nil
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

// catchUpToWAL replays the WAL into SC and SS up to the last block it holds, which is the height state
// committed to. An empty WAL leaves both stores where they are.
//
// A commit writes the WAL before either store, so a crash between the two leaves one of them a block
// behind. Committing from behind the WAL is rejected, so this is what makes an opened StateDB able to
// commit.
func (s *StateDB) catchUpToWAL() error {
	wal, err := s.openWALRange()
	if err != nil {
		return err
	}
	if wal.last == 0 {
		// Neither store is carried forward, for the reason discardStateAboveTheWAL gives: with no head to
		// measure against, a working copy above the current snapshot is the only record of the blocks it
		// holds, and dropping it on one store alone would leave the two at different heights. A rollback
		// that empties the WAL brings both down in rewindTo, where the target says where they belong.
		//
		// An interrupted commit is still repaired, since the disagreement it leaves needs no head to be
		// recognised. Nothing below reaches the repair catchUpTo runs.
		if err := s.sc.RebuildIfTorn(); err != nil {
			return fmt.Errorf("repair the state commit store's working copy: %w", err)
		}
		return nil
	}

	head := wal.last
	if err := s.catchUpTo(head); err != nil {
		return err
	}
	if err := s.matchHeight(head); err != nil {
		// Named for the open, as discardStateAboveTheWAL's refusals are: no rollback ran, and an
		// operator sent looking for one is an operator not looking at the head that was not reached.
		return fmt.Errorf("cannot open on the state WAL's head %d: %w", head, err)
	}
	return nil
}

// catchUpTo replays the WAL into SC and SS up to target.
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
	return nil
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

	//nolint:gosec // both are WAL block numbers, which never approach the int64 ceiling
	progress := newReplayProgress(int64(start), int64(end))
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
		progress.observe(int64(block)) //nolint:gosec // block <= end
	}
	progress.finish()
	return nil
}

// replayProgress reports where a replay has got to while it runs. A replay spans every block between a
// store's version and the WAL's head, which after a crash is the longest step of an open.
type replayProgress struct {
	first, last int64
	started     time.Time
	lastReport  time.Time
}

// newReplayProgress announces a replay of the blocks in [first, last] and starts timing it.
func newReplayProgress(first, last int64) *replayProgress {
	logger.Info("Replaying the state WAL", "from", first, "to", last, "blocks", last-first+1)
	now := time.Now()
	return &replayProgress{first: first, last: last, started: now, lastReport: now}
}

// observe records that block has been applied, reporting the position, the rate and the time left no
// more often than replayLogInterval.
func (p *replayProgress) observe(block int64) {
	now := time.Now()
	if now.Sub(p.lastReport) < replayLogInterval {
		return
	}
	p.lastReport = now

	remaining := p.last - block
	rate := float64(block-p.first+1) / now.Sub(p.started).Seconds()
	logger.Info("Replaying the state WAL",
		"block", block,
		"to", p.last,
		"remaining", remaining,
		"blocks_per_second", int64(rate),
		"time_left", estimate(remaining, rate))
}

// finish reports the replay that has just completed.
func (p *replayProgress) finish() {
	logger.Info("Replayed the state WAL",
		"from", p.first,
		"to", p.last,
		"blocks", p.last-p.first+1,
		"elapsed", time.Since(p.started).Truncate(time.Millisecond))
}

// estimate returns how long remaining blocks take at rate, or 0 when there is no rate to project from.
func estimate(remaining int64, rate float64) time.Duration {
	if rate <= 0 {
		return 0
	}
	return (time.Duration(float64(remaining)/rate) * time.Second).Truncate(time.Second)
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

// ssFillsForward reports whether the open SS is left out of the replay to fill forward, which is the
// treatment recoveryTarget gives an empty receipt store. It covers a store with no history of its own
// behind a WAL that has had a retention cut, where no replay rebuilds it and the alternative is
// refusing to start over a store that is merely new.
func (s *StateDB) ssFillsForward() (bool, error) {
	if s.ss == nil {
		return false, nil
	}
	wal, err := s.openWALRange()
	if err != nil {
		return false, err
	}
	return s.ss.GetLatestVersion() == 0 && (wal.last == 0 || wal.first > 1), nil
}

// matchHeight checks SC and SS against blockNum and reports the one that is not on it. An SS left empty
// to fill forward is not held to blockNum.
//
// The error names no path, since both the open and a rollback converge here; each caller supplies the
// height it asked for.
func (s *StateDB) matchHeight(blockNum int64) error {
	if got := s.sc.Version(); got != blockNum {
		return fmt.Errorf("the state commit store landed on %d", got)
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
	return fmt.Errorf("the EVM state store landed on %d", got)
}
