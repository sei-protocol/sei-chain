package flatkv

import (
	"context"
	"errors"
	"fmt"
	"time"

	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/controller"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

// replayLogInterval bounds how often a running replay reports how far it has got.
const replayLogInterval = 30 * time.Second

// RollBackLiveStateDB brings the live state DB under cfg onto target, and returns with it closed. It rewinds to
// a snapshot at or below target and replays the state WAL up to target. The state WAL is read, not cut.
//
// It errors when target is 0, or when the state WAL or the live state DB's snapshots cannot reach it. Rolling back
// to the same target again is a no-op. One that failed or was interrupted completes when run again.
func RollBackLiveStateDB(
	ctx context.Context,
	cfg *config.Config,
	// The snapshot schedule the live state DB follows while blocks replay into it.
	checkpointCfg seidbconfig.CheckpointConfig,
	target uint64,
) (retErr error) {
	if target == 0 {
		return errors.New("rollback target 0 is invalid: version 0 means no state, so there is nothing to " +
			"roll back to")
	}
	start := time.Now()
	logger.Info("Rolling back the live state DB", "target", target)

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

	liveStateDB, err := openWithoutWAL(ctx, cfg)
	if err != nil {
		return fmt.Errorf("cannot roll back to %d: %w", target, err)
	}
	wal, err := OpenStateWAL(cfg)
	if err != nil {
		return errors.Join(fmt.Errorf("cannot roll back to %d: open state WAL: %w", target, err),
			closeLiveStateDB(liveStateDB))
	}
	defer func() {
		// The WAL closes last, since the live state DB replays through it.
		retErr = errors.Join(retErr, closeLiveStateDB(liveStateDB))
		if err := wal.Close(); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close state WAL: %w", err))
		}
	}()
	liveStateDB.SetCheckpointScheduler(controller.NewCheckpointScheduler(checkpointCfg))

	if err := catchUpTo(ctx, liveStateDB, wal, target); err != nil {
		return fmt.Errorf("cannot roll back to %d: %w", target, err)
	}
	if got := blockHeight(liveStateDB.Version()); got != target {
		return fmt.Errorf("cannot roll back to %d: the live state DB landed on %d", target, got)
	}
	logger.Info("Rolled back the live state DB", "target", target, "elapsed", time.Since(start))
	return nil
}

// openWithoutWAL opens the live state DB under cfg with no WAL of its own, on the version its files hold.
func openWithoutWAL(ctx context.Context, cfg *config.Config) (_ *CommitStore, retErr error) {
	liveStateDB, err := NewCommitStore(ctx, cfg, nil)
	if err != nil {
		return nil, fmt.Errorf("open the live state DB: %w", err)
	}
	defer func() {
		if retErr != nil {
			retErr = errors.Join(retErr, closeLiveStateDB(liveStateDB))
		}
	}()
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

// catchUpTo replays wal into liveStateDB up to target.
func catchUpTo(ctx context.Context, liveStateDB *CommitStore, wal statewal.StateWAL, target uint64) error {
	// Ahead of the pass, which is what erases the evidence it works from. The WAL is cut to target once this
	// rollback returns, so target is the head it is checked against.
	if err := liveStateDB.RebuildIfUnreachable(liveStateVersion(target)); err != nil {
		return fmt.Errorf("rebuild the live state DB's working copy: %w", err)
	}
	from := blockHeight(liveStateDB.Version())
	logReplayPlan(from, target)

	// The live state DB owns no WAL, so re-committing a block read from this one appends nothing. It does run
	// the commit path, so the checkpoint schedule is asked at each block it takes.
	return replay(ctx, wal, from, target, func(block uint64, mutations []mutation.Mutation) error {
		return liveStateDB.CommitStateChanges(liveStateVersion(block), mutations)
	})
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
