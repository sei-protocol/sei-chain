package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/seiwal"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
)

// This file holds every path that replays WAL blocks into a store. The two entry points below differ only in
// where the blocks land and in what happens afterwards — the live store persists a watermark, a throwaway clone
// persists nothing. Everything under them is shared, ordered callers first.

// replayIntoMutableStore brings this store up to targetVersion from its own WAL, or to the end of the WAL when
// targetVersion <= 0, and rejects a store whose data DBs did not all reach that version.
//
// It runs at startup (open/openTo) and during Rollback, never concurrently with live commits.
func (s *CommitStore) replayIntoMutableStore(targetVersion int64) error {
	if err := s.catchUpFromWAL(targetVersion); err != nil {
		return err
	}
	return s.requireAlignedDataDBs()
}

// catchUpFromWAL replays this store's own WAL up to targetVersion, or to the end of the WAL when
// targetVersion <= 0.
//
// It reads the WAL unlocked; callers must not run it concurrently with live commits. A nil WAL is legal only
// if this store already sits at targetVersion — the outer context owns the WAL pipeline in that case.
func (s *CommitStore) catchUpFromWAL(targetVersion int64) (err error) {
	var replayed int
	obs := s.observeOp("catchup", otelMetrics.CatchupLatency, "targetVersion", targetVersion)
	// Replayed blocks are reported regardless of outcome. CurrentVersion is intentionally NOT recorded here —
	// write-mode callers record it on success themselves.
	defer func() {
		if replayed > 0 {
			otelMetrics.CatchupReplayNumBlocks.Add(s.ctx, int64(replayed))
		}
		obs.done(&err, nil, "replayed", replayed)
	}()

	if s.wal == nil {
		if targetVersion > 0 && s.committedVersion != targetVersion {
			return fmt.Errorf("catchup: nil WAL cannot replay to version %d (committed %d)",
				targetVersion, s.committedVersion)
		}
		return nil
	}

	start, end, ok, err := resolveReplayRange(s.wal, s.committedVersion, targetVersion)
	if err != nil {
		return fmt.Errorf("catchup: %w", err)
	}
	if !ok {
		return nil
	}

	logger.Info("FlatKV catchup start",
		"targetVersion", targetVersion, "committedVersion", s.committedVersion,
		"startBlock", start, "endBlock", end)

	it, err := s.wal.Iterator(start, end)
	if err != nil {
		return fmt.Errorf("catchup: WAL iterator [%d,%d]: %w", start, end, err)
	}
	if replayed, err = replayBlocks(s, it); err != nil {
		return fmt.Errorf("catchup: %w", err)
	}

	if !s.config.Fsync {
		// With Fsync=false, per-block batch commits may leave data only in OS/page cache. Flushing here bounds
		// how much of a long catchup a crash forces us to redo.
		if err = s.flushAllDBs(); err != nil {
			return fmt.Errorf("catchup flush: %w", err)
		}
	}
	logger.Info("FlatKV catchup complete",
		"replayed", replayed, "version", s.committedVersion, "elapsed", obs.elapsed())
	return nil
}

// replayIntoReadOnlyCopy advances a read-only clone from the snapshot boundary it opened at up to targetVersion,
// or to this store's latest WAL block when targetVersion <= 0, and rejects a clone whose data DBs did not all
// reach that version.
//
// The clone has a nil WAL of its own — this store owns the WAL — so the blocks it needs have to be fed to it
// from here. Nothing is persisted afterwards: the clone's databases live in a directory discarded on Close. A
// gap between the clone's snapshot boundary and the start of the WAL fails only the export; this store's own
// state is untouched.
func (s *CommitStore) replayIntoReadOnlyCopy(clone *CommitStore, targetVersion int64) error {
	if err := s.feedWALToReadOnlyCopy(clone, targetVersion); err != nil {
		return err
	}
	return clone.requireAlignedDataDBs()
}

// feedWALToReadOnlyCopy replays this store's WAL into clone up to targetVersion, or to the latest WAL block
// when targetVersion <= 0.
func (s *CommitStore) feedWALToReadOnlyCopy(clone *CommitStore, targetVersion int64) error {
	if s.wal == nil {
		if targetVersion > 0 && clone.committedVersion != targetVersion {
			return fmt.Errorf("readonly: nil WAL cannot replay to version %d (opened at %d)",
				targetVersion, clone.committedVersion)
		}
		return nil
	}

	it, ok, err := s.openReplayIterator(clone.committedVersion, targetVersion)
	if err != nil {
		return fmt.Errorf("readonly: %w", err)
	}
	if !ok {
		return nil
	}
	if _, err := replayBlocks(clone, it); err != nil {
		return fmt.Errorf("readonly: %w", err)
	}
	return nil
}

// openReplayIterator resolves the replay range for a destination at fromVersion and opens an iterator over it,
// holding s.mu for both so a concurrent Commit cannot touch the WAL in between. ok is false when there is
// nothing to replay.
//
// The lock is not held for the replay itself: by then the iterator is a point-in-time snapshot, and holding it
// that long would stall commits for the whole export.
func (s *CommitStore) openReplayIterator(
	fromVersion int64,
	targetVersion int64,
) (seiwal.Iterator[[]*proto.NamedChangeSet], bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	start, end, ok, err := resolveReplayRange(s.wal, fromVersion, targetVersion)
	if err != nil || !ok {
		return nil, false, err
	}
	it, err := s.wal.Iterator(start, end)
	if err != nil {
		return nil, false, fmt.Errorf("WAL iterator [%d,%d]: %w", start, end, err)
	}
	return it, true, nil
}

// resolveReplayRange resolves the inclusive WAL block range to replay into a destination currently sitting at
// fromVersion. targetVersion caps the range; <= 0 means replay to the end of the WAL. When ok is false there
// is nothing to replay and start/end are meaningless.
//
// Replay always begins at fromVersion+1. Blocks are contiguous and the first block is 1, so this holds for a
// fresh destination too: fromVersion 0 means replay must start at block 1.
//
// A WAL that begins later than fromVersion+1 is an error, not a shortened range. Starting at the WAL's first
// block instead would silently skip the missing blocks and commit a state whose LtHash matches no real chain
// history. Retention never drops blocks a store still needs, so reaching this means data loss or a bug.
//
// Callers hold whatever lock their concurrency context requires; this only reads the WAL's stored range.
func resolveReplayRange(
	wal statewal.StateWAL,
	fromVersion int64,
	targetVersion int64,
) (start uint64, end uint64, ok bool, err error) {
	stored, first, last, err := wal.GetStoredRange()
	if err != nil {
		return 0, 0, false, fmt.Errorf("WAL range: %w", err)
	}
	if !stored {
		// Empty WAL.
		return 0, 0, false, nil
	}

	start = uint64(fromVersion) + 1 //nolint:gosec // fromVersion >= 0
	end = last
	if targetVersion > 0 && uint64(targetVersion) < end {
		end = uint64(targetVersion) //nolint:gosec // guarded by targetVersion > 0
	}
	if end < start {
		// The destination is already at or past the target. This also covers a WAL holding nothing beyond it.
		return 0, 0, false, nil
	}
	if first > start {
		return 0, 0, false, fmt.Errorf("WAL starts at block %d but replay must start at block %d: "+
			"blocks %d-%d are missing (data loss or corruption)", first, start, start, first-1)
	}
	return start, end, true, nil
}

// replayBlocks drains it into dest, applying every block it yields, and reports how many it applied. The count
// is meaningful only when err is nil. It takes ownership of it and closes it.
//
// It holds no locks: the caller builds the iterator under whatever serialization its context requires, and the
// iterator then reads a point-in-time snapshot that concurrent appends and prunes cannot disturb.
func replayBlocks(dest *CommitStore, it seiwal.Iterator[[]*proto.NamedChangeSet]) (replayed int, err error) {
	defer func() {
		if cerr := it.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close WAL iterator: %w", cerr)
		}
	}()

	for {
		hasNext, nErr := it.Next()
		if nErr != nil {
			return 0, fmt.Errorf("WAL iterate: %w", nErr)
		}
		if !hasNext {
			break
		}
		block, changesets := it.Entry()
		if err := dest.applyAndCommit(int64(block), changesets); err != nil { //nolint:gosec // block <= end
			return 0, fmt.Errorf("replay block %d: %w", block, err)
		}
		replayed++
		// Liveness, not context: only the loop can report that a multi-hour replay is still moving.
		if replayed%1000 == 0 {
			logger.Info("FlatKV WAL replay progress", "replayed", replayed, "version", block)
		}
	}
	return replayed, nil
}

// applyAndCommit replays a single block into the store: it applies the changesets, commits the per-DB batches,
// advances the committed version, clones the working LtHash to committed, and clears the pending buffers. It
// never touches the WAL — the data being applied was itself read from a WAL, so re-writing it would
// double-append.
func (s *CommitStore) applyAndCommit(version int64, changesets []*proto.NamedChangeSet) error {
	if err := s.ApplyChangeSets(version, changesets); err != nil {
		return fmt.Errorf("apply v%d: %w", version, err)
	}
	if err := s.commitBatches(version); err != nil {
		return fmt.Errorf("commit v%d: %w", version, err)
	}
	s.committedVersion = version
	s.committedLtHash = s.workingLtHash.Clone()
	s.clearPendingWrites()
	recordPendingWrites(s.ctx, accountDBDir, 0)
	recordPendingWrites(s.ctx, codeDBDir, 0)
	recordPendingWrites(s.ctx, storageDBDir, 0)
	recordPendingWrites(s.ctx, miscDBDir, 0)
	return nil
}
