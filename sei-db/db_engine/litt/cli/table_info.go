package main

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/segment"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/urfave/cli/v2"
)

// TableInfo contains high level information about a table in LittDB.
type TableInfo struct {
	// The number of key-value pairs in the table.
	KeyCount uint64
	// The size of the table in bytes.
	Size uint64
	// If true, the table at the specified path is a snapshot of another table.
	IsSnapshot bool
	// The time when the oldest segment was sealed.
	OldestSegmentSealTime time.Time
	// The time when the newest segment was sealed.
	NewestSegmentSealTime time.Time
	// The index of the oldest segment in the table.
	LowestSegmentIndex uint32
	// The index of the newest segment in the table.
	HighestSegmentIndex uint32
	// The type of the keymap used by the table. If "", then this table doesn't have a keymap (i.e. it will rebuild
	// a keymap the next time it is loaded).
	KeymapType string
}

// tableInfoCommand is the CLI command handler for the "table-info" command.
func tableInfoCommand(ctx *cli.Context) error {
	if ctx.NArg() != 1 {
		return fmt.Errorf(
			"table-info command requires exactly at least one argument: <table-name>")
	}

	logger := slog.Default()

	tableName := ctx.Args().Get(0)

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

	info, err := tableInfo(logger, tableName, sources, true)
	if err != nil {
		return fmt.Errorf("failed to get table info for table %s at paths %v: %w", tableName, sources, err)
	}

	oldestSegmentAge := uint64(time.Since(info.OldestSegmentSealTime).Nanoseconds())
	newestSegmentAge := uint64(time.Since(info.NewestSegmentSealTime).Nanoseconds())
	segmentSpan := oldestSegmentAge - newestSegmentAge

	logger.Info("Table", "name", tableName)
	logger.Info("Key count", "count", util.CommaOMatic(info.KeyCount))
	logger.Info("Size", "bytes", util.PrettyPrintBytes(info.Size))
	logger.Info("Is snapshot", "value", info.IsSnapshot)
	logger.Info("Oldest segment age", "age", util.PrettyPrintTime(oldestSegmentAge))
	logger.Info("Oldest segment seal time", "time", info.OldestSegmentSealTime.Format(time.RFC3339))
	logger.Info("Newest segment age", "age", util.PrettyPrintTime(newestSegmentAge))
	logger.Info("Newest segment seal time", "time", info.NewestSegmentSealTime.Format(time.RFC3339))
	logger.Info("Segment span", "span", util.PrettyPrintTime(segmentSpan))
	logger.Info("Lowest segment index", "index", info.LowestSegmentIndex)
	logger.Info("Highest segment index", "index", info.HighestSegmentIndex)
	logger.Info("Key map type", "name", info.KeymapType)

	return nil
}

// tableInfo retrieves information about a table at the specified path.
func tableInfo(logger *slog.Logger, tableName string, paths []string, fsync bool) (*TableInfo, error) {
	if !litt.IsTableNameValid(tableName) {
		return nil, fmt.Errorf("table name '%s' is invalid, "+
			"must be at least one character long and contain only letters, numbers, underscores, and dashes",
			tableName)
	}

	// Forbid touching tables in active use.
	releaseLocks, err := util.LockDirectories(logger, paths, util.LockfileName, fsync)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire locks on paths %v: %w", paths, err)
	}
	defer releaseLocks()

	segmentPaths, err := segment.BuildSegmentPaths(paths, "", tableName)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to build segment paths for table %s at paths %v: %w", tableName, paths, err)
	}

	for _, segmentPath := range segmentPaths {
		if err = util.ErrIfNotExists(segmentPath.SegmentDirectory()); err != nil {
			return nil, fmt.Errorf("segment directory %s does not exist", segmentPath.SegmentDirectory())
		}
	}

	errorMonitor := util.NewErrorMonitor(context.Background(), logger, nil)

	lowestSegmentIndex, highestSegmentIndex, segments, err := segment.GatherSegmentFiles(
		logger,
		errorMonitor,
		segmentPaths,
		false,
		time.Now(),
		false,
		fsync)

	if err != nil {
		return nil, fmt.Errorf("failed to gather segment files for table %s at paths %v: %w",
			tableName, paths, err)
	}
	if ok, err := errorMonitor.IsOk(); !ok {
		// This should be impossible since we aren't doing anything on background threads that report to the
		// error monitor, but it doesn't hurt to check.
		return nil, fmt.Errorf("error monitor reports errors: %w", err)
	}

	if len(segments) == 0 {
		return nil, fmt.Errorf("no segments found for table %s at paths %v", tableName, paths)
	}

	isSnapshot, err := segments[lowestSegmentIndex].IsSnapshot()
	if err != nil {
		return nil, fmt.Errorf("failed to check if segment %d is a snapshot: %w", lowestSegmentIndex, err)
	}

	if isSnapshot {
		if len(paths) != 1 {
			return nil, fmt.Errorf("table %s is a snapshot, but multiple paths were provided: %v",
				tableName, paths)
		}

		upperBoundFile, err := disktable.LoadBoundaryFile(disktable.UpperBound, path.Join(paths[0], tableName))
		if err != nil {
			return nil, fmt.Errorf("failed to load boundary file for table %s at path %s: %w",
				tableName, paths[0], err)
		}

		if upperBoundFile.IsDefined() {
			highestSegmentIndex = upperBoundFile.BoundaryIndex()
		}
	}

	keyCount := uint64(0)
	size := uint64(0)
	for _, seg := range segments {
		if seg.SegmentIndex() > highestSegmentIndex {
			// Do not attempt to read segments outside the limit set by the boundary file.
			break
		}

		keyCount += uint64(seg.KeyCount())
		size += seg.Size()
	}

	_, _, keymapTypeFile, err := littbuilder.FindKeymapLocation(paths, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to find keymap location for table %s at paths %v: %w",
			tableName, paths, err)
	}

	keymapType := "none (will be rebuilt on next LittDB startup)"
	if keymapTypeFile != nil {
		keymapType = (string)(keymapTypeFile.Type())
	}

	return &TableInfo{
		KeyCount:              keyCount,
		Size:                  size,
		IsSnapshot:            isSnapshot,
		OldestSegmentSealTime: segments[lowestSegmentIndex].GetSealTime(),
		NewestSegmentSealTime: segments[highestSegmentIndex].GetSealTime(),
		LowestSegmentIndex:    lowestSegmentIndex,
		HighestSegmentIndex:   highestSegmentIndex,
		KeymapType:            keymapType,
	}, nil
}
