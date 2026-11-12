package giga

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb/undo"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
)

// replayLogInterval bounds how often a running replay reports how far it has got.
const replayLogInterval = 30 * time.Second

// rewindTo rolls the live state DB, the historical state DB and the WAL back to target, leaving a live
// state DB below target for the replay to carry forward. All three must be closed.
//
// A target the WAL and the live state DB's snapshots cannot span is refused before the WAL is cut, so
// every target this one could reach is still reachable on a retry.
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

// discardStateAboveTheWAL rolls the live state DB and the historical state DB back to the WAL's head when
// they hold state above it. Both must be closed.
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

// discardStateAbove rolls the live state DB and the historical state DB back to target when they hold
// state above it. Both must be closed. A target the WAL cannot replay the live state DB back up to is
// refused without moving it.
func (s *StateDB) discardStateAbove(wal storedWALRange, target int64) error {
	if _, err := flatkv.DiscardStateAbove(s.flatkvCfg.DataDir, target, wal.first); err != nil {
		return fmt.Errorf("the state commit store cannot reach %d: %w", target, err)
	}
	if !s.ssCfg.Enable {
		return nil
	}
	// Records above target describe blocks that are executed again, possibly differently.
	if err := undo.DiscardStateAbove(s.ssCfg.EVMDBDirectory, s.ssCfg, target); err != nil {
		return fmt.Errorf("the historical state DB cannot reach %d: %w", target, err)
	}
	return nil
}

// dropSnapshotsAbove removes the live state DB's snapshots above target.
//
// It runs whether or not the live state DB is above target, because an interrupted rollback leaves
// exactly one that is not: it reads as the snapshot it was repointed at, with the branch above it still on
// disk. Left there, a later rollback lands on a snapshot from the branch this one abandoned.
func (s *StateDB) dropSnapshotsAbove(target int64) error {
	if err := flatkv.DropSnapshotsAbove(s.flatkvCfg.DataDir, target); err != nil {
		return fmt.Errorf("cannot roll back the state commit store to %d: %w", target, err)
	}
	return nil
}

// catchUpToWAL replays the WAL into the live state DB up to the last block it holds, which is the height
// state committed to. An empty WAL leaves the live state DB where it is.
//
// A commit writes the WAL before the live state DB, so a crash between the two leaves it a block behind.
// Committing from behind the WAL is rejected, so this is what makes an opened StateDB able to commit.
func (s *StateDB) catchUpToWAL(ctx context.Context) error {
	wal, err := s.openWALRange()
	if err != nil {
		return err
	}
	if wal.last == 0 {
		// The WAL is empty when a node starts from transplanted state. There is nothing to replay and no
		// head to check the live state DB against, so its working copy is kept as it is (unless an
		// interrupted commit left its data DBs disagreeing, which would be a terminal failure with no WAL
		// present).
		if err := s.sc.RebuildIfTorn(); err != nil {
			return fmt.Errorf("repair the state commit store's working copy: %w", err)
		}
		return nil
	}

	head := wal.last
	if err := s.catchUpTo(ctx, head); err != nil {
		return err
	}
	if err := s.matchHeight(head); err != nil {
		// Named for the open, as discardStateAboveTheWAL's refusals are: no rollback ran, and an
		// operator sent looking for one is an operator not looking at the head that was not reached.
		return fmt.Errorf("cannot open on the state WAL's head %d: %w", head, err)
	}
	return nil
}

// catchUpTo replays the WAL into the live state DB up to target.
func (s *StateDB) catchUpTo(ctx context.Context, target int64) error {
	// Ahead of the pass, which is what erases the evidence it works from, and here rather than in the
	// open because every replay of this WAL comes through this function.
	if err := s.sc.RebuildIfUnreachable(target); err != nil {
		return fmt.Errorf("rebuild the state commit store's working copy: %w", err)
	}
	from := s.sc.Version()
	logReplayPlan(from, target)

	// The live state DB owns no WAL, so re-committing a block read from this one appends nothing. It does
	// run the commit path, so the checkpoint schedule is asked at each block it takes.
	return s.replay(ctx, from, target, s.sc.CommitStateChanges)
}

// logReplayPlan reports the height the live state DB resumes from and the blocks the pass about to run
// will feed it.
//
// It is logged even when nothing is replayed, because an open that had no catching up to do is
// otherwise indistinguishable from one still working through a long pass.
func logReplayPlan(from int64, target int64) {
	fields := []any{"target", target, "sc_version", from, "sc_replay_blocks", target - from}
	if target > from {
		// Omitted when there are none, so an empty plan reads as one.
		fields = append(fields, "sc_replay_from", from+1, "sc_replay_to", target)
	}
	logger.Info("State DB replay plan", fields...)
}

// replay feeds apply every WAL block in (from, target], in order.
//
// Blocks are contiguous from block 1, so a replay always starts at from+1. A WAL that begins later is
// missing history the destination needs: starting at the WAL's own first block would skip those blocks
// and commit a state matching no chain history, so it is reported as data loss.
//
// A cancelled ctx stops the replay between blocks and is reported as an error. The blocks already
// applied stay applied, and the next open resumes from the version they left behind.
func (s *StateDB) replay(
	ctx context.Context,
	from, target int64,
	apply func(int64, []mutation.Mutation) error,
) error {
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
		block, mutations := it.Entry()
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("replay stopped at block %d of %d: %w", block, end, err)
		}
		if err := apply(int64(block), mutations); err != nil { //nolint:gosec // block <= end
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

// matchHeight reports the live state DB when it is not on blockNum.
//
// The error names no path, since both the open and a rollback converge here; each caller supplies the
// height it asked for.
func (s *StateDB) matchHeight(blockNum int64) error {
	if got := s.sc.Version(); got != blockNum {
		return fmt.Errorf("the state commit store landed on %d", got)
	}
	return nil
}
