package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// applyAndCommit replays a single block into the store: it applies the changesets, commits the per-DB
// batches, advances the committed version, clones the working LtHash to committed, and clears the pending
// buffers. It is the shared per-block step used both by catchup (replaying this store's own WAL) and by
// read-only export replay (replaying the primary's WAL into a clone, see replayInto). It never touches the
// WAL — the data being applied was itself read from a WAL, so re-writing it would double-append.
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

// catchup replays this store's WAL from the current committedVersion up to (and including) targetVersion.
// If targetVersion <= 0, replay continues to the end of the WAL. Each block runs through applyAndCommit; a
// single global-metadata commit follows.
//
// catchup runs at startup (open/openTo) and during Rollback — never concurrently with live commits — so it
// reads s.wal without additional locking. (Concurrent read for state-sync export goes through replayInto,
// which locks around iterator construction.)
//
// The WAL must still reach back to the block after committedVersion. If it begins later than that, the
// blocks in between are gone and catchup fails rather than silently skipping them.
//
// With a nil WAL there is no replay: the store can only be at a version that is already committed or that
// exactly matches the snapshot it opened at. A load that would need to advance past the committed version
// is rejected — the nil-WAL contract, where the outer context owns the WAL pipeline.
func (s *CommitStore) catchup(targetVersion int64) (err error) {
	var replayed int
	var startBlock, endBlock uint64
	obs := s.observeOp("catchup", otelMetrics.CatchupLatency, "targetVersion", targetVersion)
	// Replayed blocks are reported regardless of outcome: even on a later error, blocks that did replay are
	// real progress. CurrentVersion is intentionally NOT recorded here — write-mode callers record it on
	// success themselves.
	defer func() {
		if replayed > 0 {
			otelMetrics.CatchupReplayNumBlocks.Add(s.ctx, int64(replayed))
		}
		obs.done(&err, nil, "startBlock", startBlock, "endBlock", endBlock, "replayed", replayed)
	}()

	if s.wal == nil {
		if targetVersion > 0 && s.committedVersion != targetVersion {
			return fmt.Errorf("catchup: nil WAL cannot replay to version %d (committed %d)",
				targetVersion, s.committedVersion)
		}
		return nil
	}

	ok, first, last, err := s.wal.GetStoredRange()
	if err != nil {
		return fmt.Errorf("catchup: WAL range: %w", err)
	}
	if !ok || last <= uint64(s.committedVersion) {
		// Empty WAL, or nothing past committedVersion: clean no-op.
		return nil
	}

	// Replay from the block after committedVersion. Blocks are contiguous and the first block is 1, so this
	// holds on a fresh store too: committedVersion 0 means replay must start at block 1, and a WAL that
	// begins later is a gap, caught below.
	startBlock = uint64(s.committedVersion) + 1 //nolint:gosec // committedVersion >= 0
	endBlock = last
	if targetVersion > 0 && uint64(targetVersion) < endBlock {
		endBlock = uint64(targetVersion)
	}
	if endBlock < startBlock {
		return nil
	}
	if first > startBlock {
		// We are about to replay, but the blocks between where this store sits and where the WAL begins are
		// gone. Starting at first would silently skip them and commit a state whose LtHash matches no real
		// chain history. Retention never drops blocks a store still needs, so this is loss or a bug.
		return fmt.Errorf("catchup: WAL starts at block %d but replay must start at block %d: "+
			"blocks %d-%d are missing (data loss or corruption)", first, startBlock, startBlock, first-1)
	}

	logger.Info("FlatKV catchup start",
		"targetVersion", targetVersion, "committedVersion", s.committedVersion,
		"startBlock", startBlock, "endBlock", endBlock)

	it, err := s.wal.Iterator(startBlock, endBlock)
	if err != nil {
		return fmt.Errorf("catchup: WAL iterator [%d,%d]: %w", startBlock, endBlock, err)
	}
	defer func() {
		if cerr := it.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("catchup: close WAL iterator: %w", cerr)
		}
	}()

	for {
		hasNext, nErr := it.Next()
		if nErr != nil {
			return fmt.Errorf("catchup: WAL iterate: %w", nErr)
		}
		if !hasNext {
			break
		}
		block, changesets := it.Entry()
		if err := s.applyAndCommit(int64(block), changesets); err != nil { //nolint:gosec // block <= endBlock
			return fmt.Errorf("catchup: replay block %d: %w", block, err)
		}
		replayed++
		if replayed%1000 == 0 {
			logger.Info("FlatKV catchup progress", "replayed", replayed, "version", block)
		}
	}

	if replayed > 0 {
		if !s.config.Fsync {
			// With Fsync=false, per-block batch commits may leave data only in OS/page cache. Flush once
			// before advancing global metadata so the global watermark never gets ahead of data durability.
			if err := s.flushAllDBs(); err != nil {
				return fmt.Errorf("catchup flush: %w", err)
			}
		}
		if err := s.commitGlobalMetadata(s.committedVersion, s.committedLtHash); err != nil {
			return fmt.Errorf("catchup global meta: %w", err)
		}
		logger.Info("FlatKV catchup complete",
			"replayed", replayed, "version", s.committedVersion, "elapsed", obs.elapsed())
	}
	return nil
}
