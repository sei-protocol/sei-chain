package flatkv

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

// replayLogInterval bounds how often a running replay reports how far it has got.
const replayLogInterval = 30 * time.Second

// RollBackLiveStateDB discards the live state DB's state and snapshots above target, working on its files. It
// leaves the live state DB at or below target, for CatchUpToStateWAL() to replay up to it.
//
// It errors when target is 0, or when the state WAL or the live state DB's snapshots cannot reach it. Rolling back
// to the same target again is a no-op. One that failed or was interrupted completes when run again.
func RollBackLiveStateDB(cfg *config.Config, target uint64) error {
	if target == 0 {
		return errors.New("rollback target 0 is invalid: version 0 means no state, so there is nothing to " +
			"roll back to")
	}
	_, first, last, err := statewal.GetRange(StateWALConfig(cfg.DataDir))
	if err != nil {
		return fmt.Errorf("read state WAL range: %w", err)
	}
	if last < target {
		return fmt.Errorf("cannot roll back to %d: the state WAL ends at %d, so no replay reaches the target",
			target, last)
	}
	if _, err := DiscardStateAbove(cfg.DataDir, liveStateVersion(target), liveStateVersion(first)); err != nil {
		return fmt.Errorf("cannot roll back to %d: the live state DB cannot reach %d: %w", target, target, err)
	}
	// Whether or not the live state DB moved: an interrupted rollback leaves one that reads as the snapshot it
	// was repointed at, with the branch above it still on disk for a later rollback to land on.
	if err := DropSnapshotsAbove(cfg.DataDir, liveStateVersion(target)); err != nil {
		return fmt.Errorf("cannot roll back to %d: drop the live state DB's snapshots above it: %w",
			target, err)
	}
	logger.Info("Rolled back the live state DB", "target", target)
	return nil
}

// OpenLiveStateDB opens the live state DB under cfg with no WAL of its own, on the version its files hold. It
// replays nothing.
func OpenLiveStateDB(ctx context.Context, cfg *config.Config) (_ *CommitStore, retErr error) {
	liveStateDB, err := NewCommitStore(ctx, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("open the live state DB: %w", err)
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, closeLiveStateDB(liveStateDB))
		}
	}()
	// Every readonly-* directory under the store is deleted, so this has to run before the process opens a
	// read-only view of its own: after that, the ones a crashed process left are no longer the only ones there.
	if err := liveStateDB.CleanupOrphanedReadOnlyDirs(); err != nil {
		return nil, fmt.Errorf("clean up the live state DB's orphaned read-only dirs: %w", err)
	}
	if err := liveStateDB.LoadWorkingCopy(); err != nil {
		return nil, fmt.Errorf("load the live state DB: %w", err)
	}
	return liveStateDB, nil
}

// closeLiveStateDB closes liveStateDB, naming it in the error.
func closeLiveStateDB(liveStateDB *CommitStore) error {
	if err := liveStateDB.Close(); err != nil {
		return fmt.Errorf("close the live state DB: %w", err)
	}
	return nil
}

// CatchUpToStateWAL brings liveStateDB to the last block wal holds. Set liveStateDB's checkpoint schedule first:
// the blocks it replays are offered to it as committed blocks are.
func CatchUpToStateWAL(ctx context.Context, liveStateDB *CommitStore, wal statewal.StateWAL) error {
	stored, _, head, err := wal.GetStoredRange()
	if err != nil {
		return fmt.Errorf("read state WAL range: %w", err)
	}
	if !stored {
		if err := liveStateDB.RebuildIfTorn(); err != nil {
			return fmt.Errorf("repair the live state DB's working copy: %w", err)
		}
		return nil
	}
	// Ahead of the pass, which is what erases the evidence it works from.
	if err := liveStateDB.RebuildIfUnreachable(liveStateVersion(head)); err != nil {
		return fmt.Errorf("rebuild the live state DB's working copy: %w", err)
	}
	from := blockHeight(liveStateDB.Version())
	logReplayPlan(from, head)
	err = replay(ctx, wal, from, head, func(block uint64, mutations []mutation.Mutation) error {
		return liveStateDB.commitReplayedStateChanges(block, mutations)
	})
	if err != nil {
		return fmt.Errorf("replay the state WAL up to %d: %w", head, err)
	}
	if got := blockHeight(liveStateDB.Version()); got != head {
		return fmt.Errorf("the live state DB landed on %d, not the state WAL's head %d", got, head)
	}
	if err := liveStateDB.requireAlignedDBs(); err != nil {
		return fmt.Errorf("align the live state DB's DBs on the state WAL's head %d: %w", head, err)
	}
	return nil
}

// logReplayPlan reports the height the live state DB resumes from and the blocks the pass about to run
// will feed it.
//
// It is logged even when nothing is replayed, because a rollback that had no catching up to do is
// otherwise indistinguishable from one still working through a long pass.
func logReplayPlan(from uint64, target uint64) {
	fields := []any{"target", target, "live_state_db_version", from}
	if target > from {
		// Omitted when there are none, so an empty plan reads as one.
		fields = append(fields, "replay_blocks", target-from, "replay_from", from+1, "replay_to", target)
	}
	logger.Info("Live state DB replay plan", fields...)
}

// replay feeds apply every block wal holds in (from, target], in order.
//
// Blocks are contiguous from block 1, so a replay always starts at from+1. A WAL that begins later is
// missing history the destination needs: starting at the WAL's own first block would skip those blocks
// and commit a state matching no chain history, so it is reported as data loss.
//
// A cancelled ctx stops the replay between blocks and is reported as an error. The blocks already
// applied stay applied, and the next rollback resumes from the version they left behind.
func replay(
	ctx context.Context,
	wal statewal.StateWAL,
	from uint64,
	target uint64,
	apply func(uint64, []mutation.Mutation) error,
) error {
	stored, first, last, err := wal.GetStoredRange()
	if err != nil {
		return fmt.Errorf("read state WAL range: %w", err)
	}
	if !stored {
		return nil
	}

	start := from + 1
	end := min(last, target)
	if end < start {
		return nil
	}
	if first > start {
		return fmt.Errorf("state WAL starts at block %d but replay must start at block %d: blocks %d-%d "+
			"are missing (data loss or corruption)", first, start, start, first-1)
	}

	it, err := wal.Iterator(start, end)
	if err != nil {
		return fmt.Errorf("state WAL iterator [%d,%d]: %w", start, end, err)
	}
	defer func() { _ = it.Close() }()

	progress := newReplayProgress(start, end)
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
		if err := apply(block, mutations); err != nil {
			return fmt.Errorf("replay block %d: %w", block, err)
		}
		progress.observe(block)
	}
	progress.finish()
	return nil
}

// replayProgress reports where a replay has got to while it runs. A replay spans every block between the
// live state DB's version and the target, which after a crash is the longest step of a startup.
type replayProgress struct {
	first      uint64
	last       uint64
	started    time.Time
	lastReport time.Time
}

// newReplayProgress announces a replay of the blocks in [first, last] and starts timing it.
func newReplayProgress(first uint64, last uint64) *replayProgress {
	logger.Info("Replaying the state WAL", "from", first, "to", last, "blocks", last-first+1)
	now := time.Now()
	return &replayProgress{first: first, last: last, started: now, lastReport: now}
}

// observe records that block has been applied, reporting the position, the rate and the time left no
// more often than replayLogInterval.
func (p *replayProgress) observe(block uint64) {
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
func estimate(remaining uint64, rate float64) time.Duration {
	if rate <= 0 {
		return 0
	}
	return (time.Duration(float64(remaining)/rate) * time.Second).Truncate(time.Second)
}

// liveStateVersion returns height as the version the live state DB numbers it by.
func liveStateVersion(height uint64) int64 {
	return int64(height) //nolint:gosec // block heights never approach the int64 ceiling
}

// blockHeight returns a live state DB version as the block height it numbers.
func blockHeight(version int64) uint64 {
	return uint64(version) //nolint:gosec // live state DB versions are never negative
}
