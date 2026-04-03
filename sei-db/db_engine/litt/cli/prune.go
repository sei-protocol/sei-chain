package main

import (
	"context"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/Layr-Labs/eigenda/common"
	"github.com/Layr-Labs/eigenda/litt/disktable"
	"github.com/Layr-Labs/eigenda/litt/disktable/keymap"
	"github.com/Layr-Labs/eigenda/litt/disktable/segment"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigensdk-go/logging"
	"github.com/urfave/cli/v2"
)

// pruneCommand can be used to remove data from a LittDB instance/snapshot.
func pruneCommand(ctx *cli.Context) error {

	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	sources := ctx.StringSlice("src")
	if len(sources) == 0 {
		return fmt.Errorf("no sources provided")
	}
	for i, src := range sources {
		var err error
		sources[i], err = util.SanitizePath(src)
		if err != nil {
			return fmt.Errorf("invalid source path: %s", src)
		}
	}

	tables := ctx.StringSlice("table")

	maxAgeSeconds := ctx.Uint64("max-age")

	return prune(logger, sources, tables, maxAgeSeconds, true)
}

// prune deletes data from a littDB database/snapshot.
func prune(logger logging.Logger, sources []string, allowedTables []string, maxAgeSeconds uint64, fsync bool) error {
	allowedTablesSet := make(map[string]struct{})
	for _, table := range allowedTables {
		allowedTablesSet[table] = struct{}{}
	}

	// Forbid touching tables in active use.
	releaseLocks, err := util.LockDirectories(logger, sources, util.LockfileName, fsync)
	if err != nil {
		return fmt.Errorf("failed to acquire locks on paths %v: %w", sources, err)
	}
	defer releaseLocks()

	// Determine which tables to prune.
	var tables []string
	foundTables, err := lsPaths(logger, sources, false, fsync)
	if err != nil {
		return fmt.Errorf("failed to list tables in paths %v: %w", sources, err)
	}
	if len(allowedTables) == 0 {
		tables = foundTables
	} else {
		for _, table := range foundTables {
			if _, ok := allowedTablesSet[table]; ok {
				tables = append(tables, table)
			}
		}
	}

	// Prune each table.
	for _, table := range tables {
		bytesDeleted, err := pruneTable(logger, sources, table, maxAgeSeconds, fsync)
		if err != nil {
			return fmt.Errorf("failed to prune table %s in paths %v: %w", table, sources, err)
		}

		logger.Infof("Deleted %s from table '%s'.", common.PrettyPrintBytes(bytesDeleted), table)
	}

	return nil
}

// pruneTable performs offline garbage collection on a LittDB database/snapshot.
func pruneTable(
	logger logging.Logger,
	sources []string,
	tableName string,
	maxAgeSeconds uint64,
	fsync bool) (uint64, error) {

	errorMonitor := util.NewErrorMonitor(context.Background(), logger, nil)

	segmentPaths, err := segment.BuildSegmentPaths(sources, "", tableName)
	if err != nil {
		return 0, fmt.Errorf("failed to build segment paths for table %s at paths %v: %w",
			tableName, sources, err)
	}

	lowestSegmentIndex, highestSegmentIndex, segments, err := segment.GatherSegmentFiles(
		logger,
		errorMonitor,
		segmentPaths,
		false,
		time.Now(),
		true,
		fsync)
	if err != nil {
		return 0, fmt.Errorf("failed to gather segment files for table %s at paths %v: %w",
			tableName, sources, err)
	}

	if len(segments) == 0 {
		return 0, fmt.Errorf("no segments found for table %s at paths %v", tableName, sources)
	}

	// Determine if we are working on the snapshot directory (i.e. the directory with symlinks to the segments).
	isSnapshot, err := segments[lowestSegmentIndex].IsSnapshot()
	if err != nil {
		return 0, fmt.Errorf("failed to check if segment %d is a snapshot: %w", lowestSegmentIndex, err)
	}

	if isSnapshot {
		// If we are dealing with a snapshot, respect the snapshot upper bound specified by LittDB.
		if len(sources) > 1 {
			return 0, fmt.Errorf("this is a symlinked snapshot directory, " +
				"snapshot directory cannot be spread across multiple sources")
		}
		upperBoundFile, err := disktable.LoadBoundaryFile(disktable.UpperBound, path.Join(sources[0], tableName))
		if err != nil {
			return 0, fmt.Errorf("failed to load boundary file for table %s at path %s: %w",
				tableName, sources[0], err)
		}
		if upperBoundFile.IsDefined() {
			highestSegmentIndex = upperBoundFile.BoundaryIndex()
		}
	}

	// Delete old segments.
	bytesDeleted := uint64(0)
	deletedSegments := make([]*segment.Segment, 0)
	for segmentIndex := lowestSegmentIndex; segmentIndex <= highestSegmentIndex; segmentIndex++ {
		seg := segments[segmentIndex]
		segmentAge := time.Since(seg.GetSealTime())

		if segmentAge < time.Duration(maxAgeSeconds)*time.Second {
			// We've pruned all segments that we can.
			break
		}

		deletedSegments = append(deletedSegments, seg)
		bytesDeleted += seg.Size()
		seg.Release()
	}

	// Wait for deletion to complete.
	for _, seg := range deletedSegments {
		err = seg.BlockUntilFullyDeleted()
		if err != nil {
			return 0, fmt.Errorf("failed to block until segment %d is fully deleted: %w",
				seg.SegmentIndex(), err)
		}
	}

	if ok, err := errorMonitor.IsOk(); !ok {
		return 0, fmt.Errorf("error monitor reports errors: %w", err)
	}

	if isSnapshot {
		// This is a snapshot. Write a lower bound file to tell the DB not to re-snapshot files than have been pruned.
		err = writeLowerBoundFile(sources[0], tableName, deletedSegments)
		if err != nil {
			return 0, fmt.Errorf("failed to write lower bound file for table %s at path %s: %w",
				tableName, sources[0], err)
		}
	} else {
		// If we are doing GC on a table that isn't a snapshot, then we need to delete the snapshots/keymap
		// for the table. The DB will automatically rebuild the snapshots directory & keymap on the next startup.
		err = deleteSnapshots(sources, tableName)
		if err != nil {
			return 0, fmt.Errorf("failed to delete snapshots/keymap for table %s at paths %v: %w",
				tableName, sources, err)
		}
	}

	return bytesDeleted, nil
}

// Updates the lower bound file after segments have been deleted.
func writeLowerBoundFile(snapshotRoot string, tableName string, deletedSegments []*segment.Segment) error {
	if len(deletedSegments) == 0 {
		// No segments were deleted, no need to write a lower bound file.
		return nil
	}
	lowerBoundFile, err := disktable.LoadBoundaryFile(disktable.LowerBound, path.Join(snapshotRoot, tableName))
	if err != nil {
		return fmt.Errorf("failed to load boundary file for table %s at path %s: %w",
			tableName, snapshotRoot, err)
	}
	err = lowerBoundFile.Update(deletedSegments[len(deletedSegments)-1].SegmentIndex())
	if err != nil {
		return fmt.Errorf("failed to update lower bound file for table %s at path %s: %w",
			tableName, snapshotRoot, err)
	}

	return nil
}

// deletes the snapshot directories in all sources for the given table
func deleteSnapshots(sources []string, tableName string) error {
	for _, source := range sources {
		snapshotsPath := path.Join(source, tableName, segment.HardLinkDirectory)
		exists, err := util.Exists(snapshotsPath)
		if err != nil {
			return fmt.Errorf("failed to check if snapshots path %s exists: %w", snapshotsPath, err)
		}
		if exists {
			err = os.RemoveAll(snapshotsPath)
			if err != nil {
				return fmt.Errorf("failed to remove snapshots path %s: %w", snapshotsPath, err)
			}
		}

		keymapPath := path.Join(source, tableName, keymap.KeymapDirectoryName)
		exists, err = util.Exists(keymapPath)
		if err != nil {
			return fmt.Errorf("failed to check if keymap path %s exists: %w", keymapPath, err)
		}
		if exists {
			err = os.RemoveAll(keymapPath)
			if err != nil {
				return fmt.Errorf("failed to remove keymap path %s: %w", keymapPath, err)
			}
		}
	}

	return nil
}
