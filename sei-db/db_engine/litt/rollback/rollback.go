// Package rollback implements an offline rollback utility for LittDB. It rewinds a database to a chosen
// point by discarding the most recently written keys, and is intended for operational use (for example,
// rolling a node's state back to a specific block height) while the database is not running.
package rollback

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/segment"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// RollbackFilter decides where a rollback stops. It is invoked once per key-file record, walking each
// table from the most recently written key to the oldest. isPrimary is true for primary keys (standalone
// primaries and primaries that own secondary keys) and false for secondary keys. The first record for
// which the filter returns true is the rollback point: that record's group is kept along with everything
// written before it, and everything written after the group is discarded.
type RollbackFilter func(key []byte, isPrimary bool) (bool, error)

// RollbackLittDB performs an offline rollback of the LittDB instance stored across the given data
// directories (the same paths passed to the database as its storage roots).
//
// For every table found under dataDirs, RollbackLittDB walks the key files from newest to oldest and
// invokes rollbackFilter for each key. The first key for which the filter returns true marks the rollback
// point: that key's group (a primary plus any secondary keys written with it) and everything written
// before it are retained; everything written after the group is permanently deleted from the segment
// files. A table for which the filter never returns true is left unchanged.
//
// The keymap and any snapshot are discarded rather than edited: the database rebuilds both from the
// truncated segment files the next time it starts, which keeps them exactly consistent with the
// rolled-back data (the same approach cli/prune.go uses after an offline mutation).
//
// The database must NOT be running while this is called. RollbackLittDB takes the same directory locks the
// database uses, so it will fail rather than corrupt a live database, and it assumes nothing else mutates
// the files while it works.
//
// The operation is idempotent: if it is interrupted, re-running it with the same filter completes the
// rollback. (An interrupted run may briefly leave a segment's recorded key count stale, but that value only
// feeds metrics and self-corrects on the next run.)
func RollbackLittDB(dataDirs []string, rollbackFilter RollbackFilter) error {
	logger := slog.Default()

	if len(dataDirs) == 0 {
		return fmt.Errorf("no data directories provided")
	}
	if rollbackFilter == nil {
		return fmt.Errorf("rollback filter must not be nil")
	}

	roots := make([]string, len(dataDirs))
	for i, dir := range dataDirs {
		sanitized, err := util.SanitizePath(dir)
		if err != nil {
			return fmt.Errorf("invalid data directory %q: %w", dir, err)
		}
		roots[i] = sanitized
	}

	// Refuse to operate on a database that is in active use. The DB holds these same locks while running.
	releaseLocks, err := util.LockDirectories(logger, roots, util.LockfileName, true)
	if err != nil {
		return fmt.Errorf("failed to lock data directories %v: %w", roots, err)
	}
	defer releaseLocks()

	tables, err := findTables(roots)
	if err != nil {
		return fmt.Errorf("failed to enumerate tables under %v: %w", roots, err)
	}

	for _, table := range tables {
		if err := rollbackTable(logger, roots, table, rollbackFilter); err != nil {
			return fmt.Errorf("failed to roll back table %q: %w", table, err)
		}
	}

	return nil
}

// rollbackPoint identifies where a table's rollback boundary falls: the segment that contains the matched
// key and the number of key-file records to retain in that segment. Everything after that prefix in the
// rollback segment, and every newer segment, is discarded.
type rollbackPoint struct {
	segmentIndex      uint32
	survivingKeyCount uint32
}

// rollbackTable rolls back a single table.
func rollbackTable(
	logger *slog.Logger,
	roots []string,
	tableName string,
	rollbackFilter RollbackFilter,
) error {
	errorMonitor := util.NewErrorMonitor(context.Background(), logger, nil)

	segmentPaths, err := segment.BuildSegmentPaths(roots, "", tableName)
	if err != nil {
		return fmt.Errorf("failed to build segment paths: %w", err)
	}

	lowestSegmentIndex, highestSegmentIndex, segments, err := segment.GatherSegmentFiles(
		logger, errorMonitor, segmentPaths, false /* snapshottingEnabled */, time.Now(),
		true /* cleanOrphans */, true /* fsync */)
	if err != nil {
		return fmt.Errorf("failed to gather segment files: %w", err)
	}
	if len(segments) == 0 {
		logger.Info("table has no segments, nothing to roll back", "table", tableName)
		return nil
	}

	// Refuse to operate on a symlinked snapshot directory: truncating symlinked value files would corrupt
	// the real segment data they point at. Rollback must run against the database's storage roots, not a
	// snapshot. (cli/prune.go makes the same check.)
	isSnapshot, err := segments[lowestSegmentIndex].IsSnapshot()
	if err != nil {
		return fmt.Errorf("failed to determine whether table %q is a snapshot: %w", tableName, err)
	}
	if isSnapshot {
		return fmt.Errorf("table %q is a symlinked snapshot; refusing to roll back "+
			"(point the tool at the database's storage roots, not a snapshot)", tableName)
	}

	pivot, err := findRollbackPoint(segments, lowestSegmentIndex, highestSegmentIndex, rollbackFilter)
	if err != nil {
		return err
	}
	if pivot == nil {
		logger.Warn("no rollback point found, leaving table unchanged", "table", tableName)
		return nil
	}

	logger.Info("rolling back table",
		"table", tableName,
		"rollbackSegment", pivot.segmentIndex,
		"survivingRecordsInRollbackSegment", pivot.survivingKeyCount,
		"deletedSegments", highestSegmentIndex-pivot.segmentIndex,
	)

	// 1. Discard the derived keymap and snapshot first. They are rebuilt from the segment files on the next
	// start, so doing this before touching the segments guarantees that however the steps below are
	// interrupted, the database rebuilds the keymap from whatever segment state exists rather than trusting
	// a keymap that points into truncated or deleted data.
	if err = discardDerivedState(roots, tableName); err != nil {
		return fmt.Errorf("failed to discard derived state: %w", err)
	}

	// 2. Delete whole segments newer than the rollback segment, highest index first so that an interruption
	// never leaves a gap in the middle of the segment sequence.
	for segmentIndex := highestSegmentIndex; segmentIndex > pivot.segmentIndex; segmentIndex-- {
		for _, filePath := range segments[segmentIndex].GetFilePaths() {
			if err = util.DeepDelete(filePath); err != nil {
				return fmt.Errorf("failed to delete %s: %w", filePath, err)
			}
		}
	}

	// 3. Truncate the rollback segment down to the surviving records.
	if err = segments[pivot.segmentIndex].RollbackToKeyCount(pivot.survivingKeyCount); err != nil {
		return fmt.Errorf("failed to truncate segment %d: %w", pivot.segmentIndex, err)
	}

	return nil
}

// findRollbackPoint walks the table's key files from the newest record to the oldest, invoking the filter
// on each. The first record for which the filter returns true is the rollback point. The whole group that
// contains that record (a standalone primary, or a primary together with the secondaries that follow it)
// is retained, so the surviving boundary is set to the end of that group. Returns nil if no record matches.
func findRollbackPoint(
	segments map[uint32]*segment.Segment,
	lowestSegmentIndex uint32,
	highestSegmentIndex uint32,
	rollbackFilter RollbackFilter,
) (*rollbackPoint, error) {

	for segmentIndex := highestSegmentIndex; ; segmentIndex-- {
		keys, err := segments[segmentIndex].GetKeys()
		if err != nil {
			return nil, fmt.Errorf("failed to read keys from segment %d: %w", segmentIndex, err)
		}

		for i := len(keys) - 1; i >= 0; i-- {
			match, err := rollbackFilter(keys[i].Key, keys[i].Kind.IsPrimary())
			if err != nil {
				return nil, fmt.Errorf("rollback filter returned an error in segment %d: %w", segmentIndex, err)
			}
			if match {
				groupEnd, err := groupEndIndex(keys, i)
				if err != nil {
					return nil, fmt.Errorf("segment %d: %w", segmentIndex, err)
				}
				return &rollbackPoint{
					segmentIndex:      segmentIndex,
					survivingKeyCount: uint32(groupEnd + 1), //nolint:gosec // bounded by the segment's key count
				}, nil
			}
		}

		if segmentIndex == lowestSegmentIndex {
			break
		}
	}

	return nil, nil
}

// groupEndIndex returns the index of the last record in the group that contains record i. A group is
// either a single standalone primary, or a primary followed by one or more secondaries terminated by a
// KeyKindFinalSecondary record.
func groupEndIndex(keys []*types.ScopedKey, i int) (int, error) {
	if keys[i].Kind == types.KeyKindStandalone {
		return i, nil
	}
	for j := i; j < len(keys); j++ {
		if keys[j].Kind == types.KeyKindFinalSecondary {
			return j, nil
		}
	}
	return 0, fmt.Errorf("key group starting at record %d has no terminating final-secondary record", i)
}

// discardDerivedState removes the keymap and snapshot directories for a table from every root. Both are
// derived entirely from the segment files: the database rebuilds the keymap (via reloadKeymap) and the
// snapshot on its next start, so deleting them forces both back into sync with the truncated segments.
// Leaving them would let a stale keymap reference discarded keys, or let a snapshot's hard links pin the
// rolled-back data on disk. Removing a directory that does not exist is a no-op.
func discardDerivedState(roots []string, tableName string) error {
	for _, root := range roots {
		dirs := []string{
			filepath.Join(root, tableName, keymap.KeymapDirectoryName),
			filepath.Join(root, tableName, segment.HardLinkDirectory),
		}
		for _, dir := range dirs {
			if err := os.RemoveAll(dir); err != nil {
				return fmt.Errorf("failed to remove %s: %w", dir, err)
			}
		}
	}
	return nil
}

// findTables returns the names of all LittDB tables found under the given roots. A table is any directory
// that contains a "segments" sub-directory.
func findTables(roots []string) ([]string, error) {
	tableSet := make(map[string]struct{})
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			return nil, fmt.Errorf("failed to read directory %s: %w", root, err)
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			segmentsDir := filepath.Join(root, entry.Name(), segment.SegmentDirectory)
			isDir, err := util.IsDirectory(segmentsDir)
			if err != nil {
				return nil, fmt.Errorf("failed to check directory %s: %w", segmentsDir, err)
			}
			if isDir {
				tableSet[entry.Name()] = struct{}{}
			}
		}
	}

	tables := make([]string, 0, len(tableSet))
	for table := range tableSet {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	return tables, nil
}
