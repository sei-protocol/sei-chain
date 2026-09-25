package giga

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	gigatypes "github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashvault"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/evm"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

// replayLogInterval bounds how often a running replay reports how far it has got.
const replayLogInterval = 30 * time.Second

// dropSnapshotsAbove removes the snapshots of SC and SS above target. Both stores must be closed.
//
// It runs whether or not either store is above target, because an interrupted rollback leaves exactly a
// store that is not: it reads as the snapshot it was repointed at, with the branch above it still on
// disk. Left there, a later rollback lands on a snapshot from the branch this one abandoned.
func dropSnapshotsAbove(flatkvCfg *flatkvconfig.Config, ssCfg config.StateStoreConfig, target uint64) error {
	//nolint:gosec // a WAL block number never approaches the int64 ceiling
	if err := flatkv.DropSnapshotsAbove(flatkvCfg.DataDir, int64(target)); err != nil {
		return fmt.Errorf("cannot roll back the state commit store to %d: %w", target, err)
	}
	if !ssCfg.Enable {
		return nil
	}
	snapshotRoot := utils.GetStateStoreSnapshotsSiblingPath(ssCfg.EVMDBDirectory)
	//nolint:gosec // a WAL block number never approaches the int64 ceiling
	if err := evm.DropSnapshotsAbove(snapshotRoot, int64(target)); err != nil {
		return fmt.Errorf("cannot roll back the EVM state store to %d: %w", target, err)
	}
	return nil
}

// catchUpToWAL replays the WAL into SC and SS up to the last block it holds, which is the height state
// committed to. ss is nil when SS is disabled. An empty WAL leaves both stores where they are.
//
// A commit writes the WAL before either store, so a crash between the two leaves one of them a block
// behind. Committing from behind the WAL is rejected, so this is what makes an opened StateDB able to
// commit.
func catchUpToWAL(
	ctx context.Context,
	sc *flatkv.CommitStore,
	ss *evm.EVMStateStore,
	wal statewal.StateWAL,
) error {
	stored, _, last, err := wal.GetStoredRange()
	if err != nil {
		return fmt.Errorf("find the head to catch up to: %w", err)
	}
	if !stored {
		// Neither store is carried forward, for the reason planRecovery gives: with no head to measure
		// against, a working copy above the current snapshot is the only record of the blocks it holds, and
		// dropping it on one store alone would leave the two at different heights. A rollback that empties
		// the WAL brings both down in applyRecoveryPlan, where the target says where they belong.
		//
		// An interrupted commit is still repaired, since the disagreement it leaves needs no head to be
		// recognised. Nothing below reaches the repair catchUpTo runs.
		if err := sc.RebuildIfTorn(); err != nil {
			return fmt.Errorf("repair the state commit store's working copy: %w", err)
		}
		return nil
	}

	head := int64(last) //nolint:gosec // a WAL block number never approaches the int64 ceiling
	if err := catchUpTo(ctx, sc, ss, wal, head); err != nil {
		return fmt.Errorf("catch up to the state WAL's head %d: %w", head, err)
	}
	if err := matchHeight(sc, ss, wal, head); err != nil {
		// Named for the open, as a plan's refusals are when no rollback ran: an operator sent looking for
		// one is an operator not looking at the head that was not reached.
		return fmt.Errorf("cannot open on the state WAL's head %d: %w", head, err)
	}
	return nil
}

// catchUpTo replays the WAL into SC and SS up to target. ss is nil when SS is disabled.
//
// One pass feeds both. It spans from the lower of their two versions, and each block goes only to the
// store still below it, so the WAL is read once rather than once per store.
func catchUpTo(
	ctx context.Context,
	sc *flatkv.CommitStore,
	ss *evm.EVMStateStore,
	wal statewal.StateWAL,
	target int64,
) error {
	// Ahead of the pass, which is what erases the evidence it works from, and here rather than in the
	// open because every replay of this WAL comes through this function.
	if err := sc.RebuildIfUnreachable(target); err != nil {
		return fmt.Errorf("rebuild the state commit store's working copy: %w", err)
	}
	scFrom := sc.Version()
	ssFrom, ssReplays, err := ssReplayStart(ss, wal, target)
	if err != nil {
		return fmt.Errorf("find where the EVM state store replays from: %w", err)
	}
	from := scFrom
	if ssReplays {
		from = min(from, ssFrom)
	}
	logReplayPlan(ss, scFrom, ssFrom, ssReplays, target)

	if err := replay(ctx, wal, from, target, func(block int64, changesets []*proto.NamedChangeSet) error {
		if block > scFrom {
			// SC owns no WAL, so re-committing a block read from this one appends nothing. It does run
			// SC's commit path, so the checkpoint schedule is asked at each block SC takes.
			if err := sc.CommitStateChanges(block, changesets); err != nil {
				return fmt.Errorf("commit the block to the state commit store: %w", err)
			}
		}
		if ssReplays && block > ssFrom {
			if err := ss.ApplyReplayedBlock(block, changesets); err != nil {
				return fmt.Errorf("apply the block to the EVM state store: %w", err)
			}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("replay the state WAL up to %d: %w", target, err)
	}
	return nil
}

// logReplayPlan reports the height each store resumes from and the blocks the pass about to run will
// feed it.
//
// It is logged even when nothing is replayed, because an open that had no catching up to do is
// otherwise indistinguishable from one still working through a long pass.
func logReplayPlan(ss *evm.EVMStateStore, scFrom int64, ssFrom int64, ssReplays bool, target int64) {
	fields := append([]any{"target", target}, replayPlanFields("sc", scFrom, target)...)
	if ss != nil {
		// A store left out of the pass replays nothing, which is a range ending where it already sits.
		to := target
		if !ssReplays {
			ssFrom = ss.GetLatestVersion()
			to = ssFrom
		}
		fields = append(fields, replayPlanFields("ss", ssFrom, to)...)
	}
	logger.Info("State DB replay plan", fields...)
}

// replayPlanFields describes one store's share of a replay: the version it holds, and the blocks it is
// about to take. The range is omitted when there are none, so an empty plan reads as one.
func replayPlanFields(store string, from int64, to int64) []any {
	fields := []any{store + "_version", from, store + "_replay_blocks", to - from}
	if to > from {
		fields = append(fields, store+"_replay_from", from+1, store+"_replay_to", to)
	}
	return fields
}

// replay feeds apply every WAL block in (from, target], in order.
//
// Blocks are contiguous from block 1, so a replay always starts at from+1. A WAL that begins later is
// missing history the destination needs: starting at the WAL's own first block would skip those blocks
// and commit a state matching no chain history, so it is reported as data loss.
//
// A cancelled ctx stops the replay between blocks and is reported as an error. The blocks already
// applied stay applied, and the next open resumes from the version they left behind.
func replay(
	ctx context.Context,
	wal statewal.StateWAL,
	from int64,
	target int64,
	apply func(int64, []*proto.NamedChangeSet) error,
) error {
	stored, first, last, err := wal.GetStoredRange()
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

	it, err := wal.Iterator(start, end)
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
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("replay stopped at block %d of %d: %w", block, end, err)
		}
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
// out when it is disabled (ss is nil), already on target, or empty with a WAL that can no longer rebuild
// it.
func ssReplayStart(ss *evm.EVMStateStore, wal statewal.StateWAL, target int64) (from int64, replays bool, err error) {
	if ss == nil {
		return 0, false, nil
	}
	from = ss.GetLatestVersion()
	if from >= target {
		return 0, false, nil
	}
	fillForward, err := ssFillsForward(ss, wal)
	if err != nil {
		return 0, false, fmt.Errorf("decide whether the EVM state store fills forward: %w", err)
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
func ssFillsForward(ss *evm.EVMStateStore, wal statewal.StateWAL) (bool, error) {
	if ss == nil {
		return false, nil
	}
	stored, first, _, err := wal.GetStoredRange()
	if err != nil {
		return false, fmt.Errorf("find whether the state WAL reaches block 1: %w", err)
	}
	return ss.GetLatestVersion() == 0 && (!stored || first > 1), nil
}

// matchHeight checks SC and SS against blockNum and reports the one that is not on it. ss is nil when SS
// is disabled, and an SS left empty to fill forward is not held to blockNum.
//
// The error names no path, since both the open and a rollback converge here; each caller supplies the
// height it asked for.
func matchHeight(sc *flatkv.CommitStore, ss *evm.EVMStateStore, wal statewal.StateWAL, blockNum int64) error {
	if got := sc.Version(); got != blockNum {
		return fmt.Errorf("the state commit store landed on %d", got)
	}
	if ss == nil {
		return nil
	}
	got := ss.GetLatestVersion()
	if got == blockNum {
		return nil
	}
	if got == 0 {
		fillForward, err := ssFillsForward(ss, wal)
		if err != nil {
			return fmt.Errorf("check the EVM state store's height: %w", err)
		}
		if fillForward {
			return nil
		}
	}
	return fmt.Errorf("the EVM state store landed on %d", got)
}

// requireAgreementWithoutWAL refuses a WAL that holds no blocks unless SC, SS and the hash vault already
// agree on the block SC is on, since no replay can bring them together. ss is nil when SS is disabled.
func requireAgreementWithoutWAL(
	sc *flatkv.CommitStore,
	ss *evm.EVMStateStore,
	vault *hashvault.PebbleHashVault,
	wal statewal.StateWAL,
) error {
	stored, _, _, err := wal.GetStoredRange()
	if err != nil {
		return fmt.Errorf("read the state WAL's range: %w", err)
	}
	if stored {
		return nil
	}
	height := sc.Version()
	if height == 0 {
		return nil
	}
	if ss != nil {
		if got := ss.GetLatestVersion(); got != height {
			return fmt.Errorf("the state commit store is on block %d but the EVM state store is on "+
				"block %d", height, got)
		}
	}
	_, status, err := vault.Get(uint64(height)) //nolint:gosec // a committed version is never negative
	if err != nil {
		return fmt.Errorf("read the hash vault at block %d: %w", height, err)
	}
	if status != gigatypes.BlockHashStatusFound {
		return fmt.Errorf("the hash vault holds no hash for block %d, the block the state commit store is on",
			height)
	}
	return nil
}
