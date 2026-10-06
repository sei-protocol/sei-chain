package offline

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/segment"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// NewIterator opens an offline iterator over the table at tableName under config.Paths, without starting
// a live database. It iterates oldest-to-newest if reverse is false, newest-to-oldest if reverse is true.
//
// The database must NOT be running while this is called: NewIterator takes the same directory lock the
// database uses, held for the iterator's lifetime and released by Close, so it will fail rather than read
// alongside a live database.
func NewIterator(config *litt.Config, tableName string, reverse bool) (litt.Iterator, error) {
	logger := slog.Default()

	if config == nil || len(config.Paths) == 0 {
		return nil, fmt.Errorf("at least one path must be provided")
	}
	if err := config.SanitizePaths(); err != nil {
		return nil, fmt.Errorf("failed to sanitize data directories: %w", err)
	}
	roots := config.Paths

	for _, root := range roots {
		if err := util.EnsureDirectoryExists(root, config.Fsync); err != nil {
			return nil, fmt.Errorf("failed to ensure data directory %q: %w", root, err)
		}
	}

	releaseLocks, err := util.LockDirectories(logger, roots, util.LockfileName, config.Fsync)
	if err != nil {
		return nil, fmt.Errorf("failed to lock data directories %v: %w", roots, err)
	}

	segs, err := gatherOrderedSegments(logger, roots, tableName, config.Fsync)
	if err != nil {
		releaseLocks()
		return nil, err
	}

	if reverse {
		return disktable.NewOfflineReverseIterator(segs, releaseLocks), nil
	}
	return disktable.NewOfflineForwardIterator(segs, releaseLocks), nil
}

// gatherOrderedSegments enumerates a table's segments across roots, offline, and returns them ordered from
// lowest to highest index. Segments already logically garbage collected (below the table's durable
// gc-watermark) are excluded, matching what a live table's own reads would see. Returns an empty slice, not
// an error, if the table has no segments.
func gatherOrderedSegments(
	logger *slog.Logger,
	roots []string,
	tableName string,
	fsync bool,
) ([]*segment.Segment, error) {
	exists, err := tableExists(roots, tableName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, nil
	}

	errorMonitor := util.NewErrorMonitor(context.Background(), logger, nil)

	segmentPaths, err := segment.BuildSegmentPaths(roots, "", tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to build segment paths: %w", err)
	}

	lowestSegmentIndex, highestSegmentIndex, segments, err := segment.GatherSegmentFiles(
		logger, errorMonitor, segmentPaths, false /* snapshottingEnabled */, time.Now(),
		true /* cleanOrphans */, fsync)
	if err != nil {
		return nil, fmt.Errorf("failed to gather segment files: %w", err)
	}
	if len(segments) == 0 {
		return nil, nil
	}

	watermark, defined, err := highestGCWatermark(roots, tableName)
	if err != nil {
		return nil, err
	}
	if defined && watermark > highestSegmentIndex {
		// Everything present is below the watermark; no readable segments remain.
		return nil, nil
	}
	floor := lowestSegmentIndex
	if defined && watermark > floor {
		floor = watermark
	}

	ordered := make([]*segment.Segment, 0, highestSegmentIndex-floor+1)
	for index := floor; index <= highestSegmentIndex; index++ {
		ordered = append(ordered, segments[index])
	}
	return ordered, nil
}

// tableExists reports whether tableName has ever been created under any of roots. A table that has never
// been written has no segments directory, and scanning for its segment files would otherwise fail with a
// "no such file or directory" error rather than reporting it as merely empty.
func tableExists(roots []string, tableName string) (bool, error) {
	for _, root := range roots {
		segmentsDir := filepath.Join(root, tableName, segment.SegmentDirectory)
		isDir, err := util.IsDirectory(segmentsDir)
		if err != nil {
			return false, fmt.Errorf("failed to check directory %s: %w", segmentsDir, err)
		}
		if isDir {
			return true, nil
		}
	}
	return false, nil
}
