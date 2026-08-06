package mvcc

import (
	"bytes"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/seilog"
)

// Rollback discards MVCC versions above a target by replaying the changelog as
// an undo index: every key an entry above the target wrote is point-deleted at
// that entry's version, which restores whatever version was visible before it.
//
// That makes the changelog load-bearing for correctness, so two invariants have
// to hold for a rollback to be sound:
//
//   - Every write above the target is represented in the changelog. Only
//     ApplyChangesetAsync logs; ApplyChangesetSync writes straight through. A
//     store advanced through the sync path (recovery replay is its only caller
//     today, and it replays entries the changelog already holds) would carry
//     writes the undo index cannot see, and the preflight below cannot detect
//     them.
//   - Changelog versions are non-decreasing. An SC-only rollback followed by
//     block replay deliberately violates that ordering while SS remains ahead,
//     so the preflight rejects that log rather than truncating the wrong tail.
//   - No writes are accepted for the duration. Callers quiesce the write path;
//     CompositeStateStore.Rollback does so by stopping background maintenance
//     before it delegates here.

var logger = seilog.NewLogger("db", "db-engine", "pebbledb", "mvcc")

type rollbackCoverageError struct {
	targetVersion             int64
	earliestRecoverableTarget int64
	earliestRetainedVersion   int64
}

func (e rollbackCoverageError) Error() string {
	return fmt.Sprintf(
		"state store rollback to version %d is not supported: changelog starts at version %d, earliest recoverable target is %d; restore from state sync or an archive for deeper rollback",
		e.targetVersion,
		e.earliestRetainedVersion,
		e.earliestRecoverableTarget,
	)
}

// effectiveWriteVersion maps a changelog entry's version to the version its
// data actually landed at. ApplyChangesetAsync logs the genesis changeset as
// version 0, but ApplyChangesetSync — which the async writer ultimately calls —
// remaps 0 to 1 because Pebble treats version 0 as special. Undoing a genesis
// entry at the logged 0 would delete nothing.
func effectiveWriteVersion(version int64) int64 {
	if version == 0 {
		return 1
	}
	return version
}

// prunePauser is the optional capability a changelog exposes to hold its
// retained offset range still. Implemented by wal.WAL.
type prunePauser interface {
	SuspendPrune()
	ResumePrune()
}

// SuspendChangelogPruning keeps the retained WAL range stable for a
// coordinated multi-backend rollback. The WAL suspension is nestable, so the
// database rollback can still protect itself when its composite caller already
// holds an outer suspension.
func (db *Database) SuspendChangelogPruning() {
	if pauser, ok := db.streamHandler.(prunePauser); ok {
		pauser.SuspendPrune()
	}
}

func (db *Database) ResumeChangelogPruning() {
	if pauser, ok := db.streamHandler.(prunePauser); ok {
		pauser.ResumePrune()
	}
}

// changelogBounds returns the retained offset range. ok is false when the log
// holds no entries, which an emptied log reports as first == last+1 rather than
// as a zero bound.
func (db *Database) changelogBounds() (first, last uint64, ok bool, err error) {
	if db.streamHandler == nil {
		return 0, 0, false, fmt.Errorf("changelog is not open")
	}
	first, err = db.streamHandler.FirstOffset()
	if err != nil {
		return 0, 0, false, fmt.Errorf("read changelog first offset: %w", err)
	}
	last, err = db.streamHandler.LastOffset()
	if err != nil {
		return 0, 0, false, fmt.Errorf("read changelog last offset: %w", err)
	}
	if first == 0 || last == 0 || first > last {
		return 0, 0, false, nil
	}
	return first, last, true, nil
}

// validateMonotonicChangelog verifies the ordering required by both coverage
// checks and tail truncation. An SC-only rollback deliberately leaves SS ahead;
// replaying lower versions after that creates a version decrease in the WAL.
func (db *Database) validateMonotonicChangelog(first, last uint64) error {
	var (
		havePrevious   bool
		previousOffset uint64
		previous       int64
	)
	err := db.streamHandler.Replay(first, last, func(index uint64, entry proto.ChangelogEntry) error {
		version := effectiveWriteVersion(entry.Version)
		if havePrevious && version < previous {
			return fmt.Errorf(
				"changelog is not monotonic at offset %d: version %d follows version %d at offset %d; "+
					"use --skip-state-store or restore the state store before attempting an SS rollback",
				index, version, previous, previousOffset,
			)
		}
		havePrevious = true
		previousOffset = index
		previous = version
		return nil
	})
	if err != nil {
		return fmt.Errorf("state store rollback is not supported: %w", err)
	}
	return nil
}

// loggedVersionAbove reports whether the changelog retains an entry that wrote
// above targetVersion, i.e. whether there is anything for a rollback to undo.
func (db *Database) loggedVersionAbove(last uint64, targetVersion int64) (bool, error) {
	lastEntry, err := db.streamHandler.ReadAt(last)
	if err != nil {
		return false, fmt.Errorf("read changelog last entry: %w", err)
	}
	return effectiveWriteVersion(lastEntry.Version) > targetVersion, nil
}

// CheckRollbackCoverage verifies that the changelog can prove every write a
// rollback to targetVersion would have to undo. It mutates nothing, so callers
// coordinating several backends can use it as a preflight and abort with every
// backend still intact.
func (db *Database) CheckRollbackCoverage(targetVersion int64) error {
	if db.storage == nil {
		return fmt.Errorf("pebbledb: database is closed")
	}
	if targetVersion < 0 {
		return fmt.Errorf("rollback target must be non-negative: %d", targetVersion)
	}
	db.SuspendChangelogPruning()
	defer db.ResumeChangelogPruning()

	if earliestVersion := db.GetEarliestVersion(); earliestVersion > 0 && targetVersion < earliestVersion {
		return fmt.Errorf(
			"state store rollback to version %d is not supported: earliest retained version is %d; restore from state sync or an archive for deeper rollback",
			targetVersion,
			earliestVersion,
		)
	}

	firstOffset, lastOffset, ok, err := db.changelogBounds()
	if err != nil {
		return err
	}
	if !ok {
		// Nothing logged. Sound only if the marker has nothing above the target
		// either, in which case the rollback is a no-op.
		if db.GetLatestVersion() > targetVersion {
			return fmt.Errorf(
				"state store rollback to version %d is not supported: changelog is empty but the state store is at version %d; restore from state sync or an archive",
				targetVersion, db.GetLatestVersion(),
			)
		}
		return nil
	}
	if err := db.validateMonotonicChangelog(firstOffset, lastOffset); err != nil {
		return err
	}

	undoNeeded, err := db.loggedVersionAbove(lastOffset, targetVersion)
	if err != nil {
		return err
	}
	if !undoNeeded {
		return nil
	}

	firstEntry, err := db.streamHandler.ReadAt(firstOffset)
	if err != nil {
		return fmt.Errorf("read changelog first entry: %w", err)
	}
	// The undo index must reach down to the first version above the target.
	// Equality is fine: an entry at target+1 is the shallowest one that has to
	// be undone, and everything below it is already at or under the target.
	if firstVersion := effectiveWriteVersion(firstEntry.Version); firstVersion > targetVersion+1 {
		return rollbackCoverageError{
			targetVersion:             targetVersion,
			earliestRecoverableTarget: firstVersion - 1,
			earliestRetainedVersion:   firstVersion,
		}
	}
	return nil
}

// Rollback discards all MVCC versions above targetVersion. It must be called
// while no new writes are being accepted; see the invariants at the top of this
// file.
//
// The steps run marker-first — lower the latest-version marker, then delete,
// then truncate the changelog — and the order is load-bearing for crash
// recovery. RecoverCompositeStateStore replays the changelog forward from the
// marker on every open, and replays nothing when the log ends at or below it.
// Lowering the marker first therefore makes any crash mid-rollback recoverable:
// the next open replays target+1..N and converges on the pre-rollback state,
// which the operator can roll back again. Deleting first would leave the marker
// at N with the versions below it already gone and nothing for recovery to
// replay — a silent hole rather than a retry.
//
// Rollback is also re-runnable in-process: it decides there is no work left
// from the changelog as well as the marker, so a retry after a failure that
// already lowered the marker still finishes the deletes and the truncation.
func (db *Database) Rollback(targetVersion int64) error {
	if db.storage == nil {
		return fmt.Errorf("pebbledb: database is closed")
	}
	if targetVersion < 0 {
		return fmt.Errorf("rollback target must be non-negative: %d", targetVersion)
	}
	db.WaitForPendingWrites()

	// Hold the changelog's retained range still: the offsets read here are
	// replayed below, and the WAL's own prune ticker would otherwise be free to
	// drop entries off the front in between.
	db.SuspendChangelogPruning()
	defer db.ResumeChangelogPruning()

	if err := db.CheckRollbackCoverage(targetVersion); err != nil {
		return err
	}

	firstOffset, lastOffset, ok, err := db.changelogBounds()
	if err != nil {
		return err
	}
	undoNeeded := false
	if ok {
		if undoNeeded, err = db.loggedVersionAbove(lastOffset, targetVersion); err != nil {
			return err
		}
	}
	if !undoNeeded && db.GetLatestVersion() <= targetVersion {
		return nil
	}

	if db.GetLatestVersion() > targetVersion {
		if err := db.SetLatestVersion(targetVersion); err != nil {
			return fmt.Errorf("set latest version to rollback target: %w", err)
		}
	}
	if !undoNeeded {
		return nil
	}

	lastKeptOffset, firstDeletedKey, lastDeletedKey, err := db.deleteVersionsAboveTarget(firstOffset, lastOffset, targetVersion)
	if err != nil {
		return err
	}
	if lastKeptOffset == 0 {
		// Every retained entry was above the target, which is reachable
		// whenever the front of the log has already been pruned.
		if err := db.streamHandler.TruncateAll(); err != nil {
			return fmt.Errorf("truncate changelog after rollback: %w", err)
		}
	} else if err := db.streamHandler.TruncateAfter(lastKeptOffset); err != nil {
		return fmt.Errorf("truncate changelog after rollback: %w", err)
	}

	// The rollback is durable once the changelog is truncated. Compaction only
	// reclaims the space the deletes tombstoned, so a failure here is a
	// space-usage problem, not a correctness one — reporting it as a failed
	// rollback would strand callers coordinating other backends.
	if err := db.compactPrunedRange(firstDeletedKey, lastDeletedKey); err != nil {
		logger.Error("failed to compact range deleted by state store rollback",
			"targetVersion", targetVersion, "error", err)
	}
	return nil
}

func (db *Database) deleteVersionsAboveTarget(firstOffset, lastOffset uint64, targetVersion int64) (uint64, []byte, []byte, error) {
	batch := db.storage.NewBatch()
	defer func() { _ = batch.Close() }()

	var (
		lastKeptOffset  uint64
		firstDeletedKey []byte
		lastDeletedKey  []byte
		pendingWrites   int
		touchedStores   = make(map[string]struct{})
	)

	err := db.streamHandler.Replay(firstOffset, lastOffset, func(index uint64, entry proto.ChangelogEntry) error {
		version := effectiveWriteVersion(entry.Version)
		if version <= targetVersion {
			lastKeptOffset = index
			return nil
		}
		for _, changeset := range entry.Changesets {
			touchedStores[changeset.Name] = struct{}{}
			for _, pair := range changeset.Changeset.Pairs {
				encodedKey := db.mvccEncode(prependStoreKey(changeset.Name, pair.Key), version)
				if err := batch.Delete(encodedKey, nil); err != nil {
					return fmt.Errorf("delete rollback key at version %d: %w", version, err)
				}
				db.trackRollbackDeletedKey(&firstDeletedKey, &lastDeletedKey, encodedKey)
				pendingWrites++
				if pendingWrites >= PruneCommitBatchSize {
					writeCount := int64(batch.Count())
					if err := batch.Commit(defaultWriteOpts); err != nil {
						return fmt.Errorf("commit rollback batch: %w", err)
					}
					db.operationMetrics.AddWrite(writeCount)
					batch.Reset()
					pendingWrites = 0
				}
			}
		}
		return nil
	})
	if err != nil {
		return 0, nil, nil, fmt.Errorf("replay changelog for rollback: %w", err)
	}
	if pendingWrites > 0 {
		writeCount := int64(batch.Count())
		if err := batch.Commit(defaultWriteOpts); err != nil {
			return 0, nil, nil, fmt.Errorf("commit final rollback batch: %w", err)
		}
		db.operationMetrics.AddWrite(writeCount)
	}
	// Rewind rather than drop: the prune scan treats a store with no entry as
	// "seek past it entirely", so deleting would hide these stores from pruning
	// until something wrote to them again.
	for store := range touchedStores {
		db.storeKeyDirty.Store(store, targetVersion)
	}
	return lastKeptOffset, firstDeletedKey, lastDeletedKey, nil
}

func (db *Database) rollbackKeyCompare(a, b []byte) int {
	if db.config.UseDefaultComparer {
		return bytes.Compare(a, b)
	}
	return MVCCKeyCompare(a, b)
}

// trackRollbackDeletedKey widens [first, last] to cover key, which is the range
// compaction will be asked for. key belongs to the caller's batch buffer, so it
// is copied only when it is actually retained.
func (db *Database) trackRollbackDeletedKey(first, last *[]byte, key []byte) {
	if *first == nil || db.rollbackKeyCompare(key, *first) < 0 {
		*first = append([]byte(nil), key...)
	}
	if *last == nil || db.rollbackKeyCompare(key, *last) > 0 {
		*last = append([]byte(nil), key...)
	}
}
