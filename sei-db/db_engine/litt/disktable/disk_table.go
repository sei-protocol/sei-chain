package disktable

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/segment"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

var _ litt.ManagedTable = (*DiskTable)(nil)

// keymapReloadBatchSize is the size of the batch used for reloading keys from segments into the keymap.
const keymapReloadBatchSize = 1024

// DiskTable manages a table's Segments.
type DiskTable struct {
	// The logger for the disk table.
	logger *slog.Logger

	// errorMonitor is a struct that permits the DB to "panic". There are many goroutines that function under the
	// hood, and many of these threads could, in theory, encounter errors which are unrecoverable. In such situations,
	// the desirable outcome is for the DB to report the error and then refuse to do additional work. If the DB is in a
	// broken state, it is much better to refuse to do work than to continue to do work and potentially corrupt data.
	errorMonitor *util.ErrorMonitor

	// The root directories for the disk table. Each of these directories' name matches the name of the table.
	roots []string

	// Configures the location where segment data is stored.
	segmentPaths []*segment.SegmentPath

	// The table's name.
	name string

	// The table's sharding factor, supplied at creation time and held only in memory (not persisted across
	// restarts). Accessed/modified by concurrent goroutines (the control loop and read sites).
	shardingFactor atomic.Uint32

	// A map of keys to their addresses.
	keymap keymap.Keymap

	// The path to the keymap directory.
	keymapPath string

	// The type file for the keymap.
	keymapTypeFile *keymap.KeymapTypeFile

	// unflushedDataCache is a map of keys to their values that may not have been flushed to disk yet. This is used as a
	// lookup table when data is requested from the table before it has been flushed to disk.
	unflushedDataCache sync.Map

	// clock is the time source used by the disk table.
	clock func() time.Time

	// The number of bytes contained within all segments, including the mutable segment. This tracks the number of
	// bytes that are on disk, not bytes in memory.
	size atomic.Uint64

	// The number of keys in the table.
	keyCount atomic.Int64

	// The control loop is a goroutine responsible for scheduling operations that mutate the table.
	controlLoop *controlLoop

	// The GC manager is a goroutine that performs garbage collection: it schedules keymap deletes for expired
	// segments and durably advances the gc-watermark. The control loop later reclaims the collected files.
	gcManager *gcManager

	// The flush loop is a goroutine responsible for blocking on flush operations.
	flushLoop *flushLoop

	// The keymap manager is a goroutine responsible for asynchronously applying keymap mutations: puts (once
	// segment data is crash durable) and deletes (during garbage collection).
	keymapManager *keymapManager

	// Encapsulates metrics for the database.
	metrics *metrics.LittDBMetrics

	// Set to true when the table is closed. This is used to prevent double closing.
	closed atomic.Bool

	// Set to true when the table is destroyed. This is used to prevent double destroying.
	destroyed atomic.Bool

	// If true then ensure file operations are synced to disk.
	fsync bool

	// The algorithm used to compress values written to new segments. types.CompressionNone means values
	// are stored verbatim. Held only in memory; each segment records its own algorithm for reads.
	compressionAlgorithm types.CompressionAlgorithm

	// Manages flush requests and flush request batching. This is a performance optimization.
	flushCoordinator *flushCoordinator
}

// NewDiskTable creates a new DiskTable.
func NewDiskTable(
	config *litt.Config,
	runtimeConfig *litt.RuntimeConfig,
	name string,
	tableConfig litt.TableConfig,
	keymap keymap.Keymap,
	keymapPath string,
	keymapTypeFile *keymap.KeymapTypeFile,
	roots []string,
	reloadKeymap bool,
	metrics *metrics.LittDBMetrics) (litt.ManagedTable, error) {

	if config.GCPeriod <= 0 {
		return nil, errors.New("garbage collection period must be greater than 0")
	}

	qualifiedRoots := make([]string, len(roots))
	for i, root := range roots {
		qualifiedRoots[i] = path.Join(root, name)
	}

	// For each root directory, create a segment directory if it doesn't exist.
	segmentPaths, err := segment.BuildSegmentPaths(roots, config.SnapshotDirectory, name)
	if err != nil {
		return nil, fmt.Errorf("failed to build segment paths: %w", err)
	}
	for _, segmentPath := range segmentPaths {
		err = segmentPath.MakeDirectories(config.Fsync)
		if err != nil {
			return nil, fmt.Errorf("failed to create segment directories: %w", err)
		}
	}

	// Delete any orphaned swap files:
	for _, root := range qualifiedRoots {
		err = util.DeleteOrphanedSwapFiles(root)
		if err != nil {
			return nil, fmt.Errorf("failed to delete orphaned swap files in %s: %w", root, err)
		}
	}

	errorMonitor := util.NewErrorMonitor(runtimeConfig.CTX, runtimeConfig.Logger, runtimeConfig.FatalErrorCallback)

	table := &DiskTable{
		logger:               runtimeConfig.Logger,
		errorMonitor:         errorMonitor,
		clock:                runtimeConfig.Clock,
		roots:                qualifiedRoots,
		segmentPaths:         segmentPaths,
		name:                 name,
		keymap:               keymap,
		keymapPath:           keymapPath,
		keymapTypeFile:       keymapTypeFile,
		metrics:              metrics,
		fsync:                config.Fsync,
		compressionAlgorithm: tableConfig.Compression,
	}
	// Sharding factor is supplied at creation time and held only in memory; it is not persisted across restarts.
	// (TTL is likewise in-memory, but it lives on the GC manager — its only consumer — and is seeded there.)
	table.setShardingFactor(tableConfig.ShardingFactor)
	table.flushCoordinator = newFlushCoordinator(errorMonitor, table.flushInternal, config.MinimumFlushInterval)

	snapshottingEnabled := config.SnapshotDirectory != ""

	// Load segments.
	lowestSegmentIndex, highestSegmentIndex, segments, err :=
		segment.GatherSegmentFiles(
			runtimeConfig.Logger,
			errorMonitor,
			table.segmentPaths,
			snapshottingEnabled,
			runtimeConfig.Clock(),
			true,
			config.Fsync)
	if err != nil {
		return nil, fmt.Errorf("failed to gather segment files: %w", err)
	}

	keyCount := int64(0)
	for _, seg := range segments {
		keyCount += int64(seg.KeyCount())
	}
	table.keyCount.Store(keyCount)

	immutableSegmentSize := uint64(0)
	for _, seg := range segments {
		immutableSegmentSize += seg.Size()
	}

	// Create the mutable segment
	creatingFirstSegment := len(segments) == 0

	var nextSegmentIndex uint32
	if creatingFirstSegment {
		nextSegmentIndex = 0
	} else {
		nextSegmentIndex = highestSegmentIndex + 1
	}
	mutableSegment, err := segment.CreateSegment(
		runtimeConfig.Logger,
		errorMonitor,
		nextSegmentIndex,
		segmentPaths,
		snapshottingEnabled,
		table.getShardingFactor(),
		table.compressionAlgorithm,
		config.Fsync,
		config.ShardControlChannelSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create mutable segment: %w", err)
	}
	if !creatingFirstSegment {
		segments[highestSegmentIndex].SetNextSegment(mutableSegment)
		highestSegmentIndex++
	}
	segments[nextSegmentIndex] = mutableSegment

	// Load the durable GC watermark (lowest readable segment). It lives at the table root so it survives a
	// keymap rebuild. Reconcile against the lowest segment actually present on disk: a value at or below it
	// means those segments were already physically deleted (clamp up to the lowest present); a value above it
	// means GC durably deleted some segments' keymap entries but crashed before their files were removed, so
	// segments [lowestSegmentIndex, lowestReadableSegment) are still on disk but logically deleted (the control
	// loop will reclaim their files, and repair/reload will not resurrect them).
	var gcWatermarkFile *GCWatermarkFile
	for _, root := range qualifiedRoots {
		f, err := LoadGCWatermarkFile(root)
		if err != nil {
			return nil, fmt.Errorf("failed to load gc-watermark file from %s: %w", root, err)
		}
		if f.IsDefined() {
			gcWatermarkFile = f
			break
		}
		if gcWatermarkFile == nil {
			gcWatermarkFile = f
		}
	}
	lowestReadableSegment := lowestSegmentIndex
	if gcWatermarkFile.IsDefined() && gcWatermarkFile.LowestReadableSegment() > lowestReadableSegment {
		lowestReadableSegment = gcWatermarkFile.LowestReadableSegment()
	}

	// The gc-watermark must never point above the highest segment on disk. Reclamation removes a contiguous
	// prefix, so the surviving segments [lowestSegmentIndex, highestSegmentIndex] are contiguous and all present
	// in the map, and the mutable segment at highestSegmentIndex is never collected — so a readable floor at or
	// below highestSegmentIndex always lands on a segment that exists. A value above it means the durable
	// gc-watermark file is inconsistent with the segment files on disk (corruption or an external edit); refuse to
	// start rather than nil-dereference a missing segment during keymap repair/purge or boundary-key reads.
	if lowestReadableSegment > highestSegmentIndex {
		return nil, fmt.Errorf(
			"gc-watermark (lowest readable segment %d) exceeds highest segment %d on disk; "+
				"the gc-watermark file is inconsistent with the segment files",
			lowestReadableSegment, highestSegmentIndex)
	}

	if reloadKeymap {
		runtimeConfig.Logger.Info("reloading keymap from segments")
		err = table.reloadKeymap(segments, lowestReadableSegment, highestSegmentIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to load keymap from segments: %w", err)
		}
	} else {
		err = table.repairKeymap(segments, lowestReadableSegment, highestSegmentIndex)
		if err != nil {
			return nil, fmt.Errorf("failed to repair keymap: %w", err)
		}
	}

	// Purge keymap entries for segments GC logically deleted (the durable gc-watermark was advanced past them)
	// but whose asynchronous keymap deletes may have been lost in a crash. repair/reload never delete, so
	// without this a lost delete would leave entries pointing at segments [lowestSegmentIndex,
	// lowestReadableSegment) that the control loop is about to reclaim, resurrecting collected keys. Doing it
	// here, synchronously and before any goroutine can reclaim files, restores keymap-delete-before-file-delete
	// ordering across a crash.
	err = table.purgeCollectedKeymapEntries(segments, lowestSegmentIndex, lowestReadableSegment)
	if err != nil {
		return nil, fmt.Errorf("failed to purge collected keymap entries: %w", err)
	}

	var upperBoundSnapshotFile *BoundaryFile
	if config.SnapshotDirectory != "" {
		// Initialize snapshot files if snapshotting is enabled.
		upperBoundSnapshotFile, err = table.repairSnapshot(
			config.SnapshotDirectory,
			lowestReadableSegment,
			highestSegmentIndex,
			segments)
		if err != nil {
			return nil, fmt.Errorf("failed to repair snapshot: %w", err)
		}
	}

	// Start the keymap manager. The flush loop schedules keymap puts onto it once segment data is durable, and
	// the GC manager schedules keymap deletes onto it during collection. At startup every segment below
	// lowestReadableSegment is logically deleted (its keymap entries are durably gone), so both the deletion
	// watermark (which gates the control loop's file deletion) and the GC manager's "scheduled for deletion"
	// cursor start just below the lowest readable segment. Seeding the watermark this way makes the control
	// loop's first file-deletion pass reclaim any files in [lowestSegmentIndex, lowestReadableSegment) left
	// behind by a crash after the keymap entries were deleted but before the files were removed. The manager
	// reports subsequent deletion-watermark advances by calling controlLoop.publishDeletionWatermark.
	initialDeletionWatermark := int64(lowestReadableSegment) - 1

	// The control loop hands each segment to the GC manager (via gcManager.registerImmutableSegment) as it is
	// sealed; the GC manager keeps its own local view of sealed segments rather than reading the control loop's
	// map. Seed that view with the sealed segments already on disk: [lowestReadableSegment, highestSegmentIndex).
	// The range is contiguous and fully present (see above), excludes the mutable highest segment, and excludes
	// the already-collected [lowestSegmentIndex, lowestReadableSegment) prefix the control loop reclaims first.
	// Reserve each seeded segment so its files survive while it sits in the GC manager's local view (where
	// collectExpiredSegments may read its keys). The GC manager releases the reservation once it is done with the
	// segment, mirroring registerImmutableSegment for segments sealed later. This runs synchronously before any
	// goroutine starts and each segment has a reservation count of exactly 1, so Reserve always succeeds.
	initialSealedSegments := make(map[uint32]*segment.Segment)
	for i := lowestReadableSegment; i < highestSegmentIndex; i++ {
		if !segments[i].Reserve() {
			return nil, fmt.Errorf("failed to reserve sealed segment %d for gc manager seeding", i)
		}
		initialSealedSegments[i] = segments[i]
	}

	// Build the goroutines' structs first, then wire their cross-references, then start them. The keymap manager
	// writes to the control-loop-owned deletion-watermark channel via controlLoop.publishDeletionWatermark, so
	// kManager.controlLoop must be set before kManager.run() starts (established below, before the go statements).
	kManager := newKeymapManager(
		errorMonitor,
		keymap,
		&table.unflushedDataCache,
		metrics,
		runtimeConfig.Clock,
		name,
		config.KeymapManagerChannelSize,
		config.KeymapManagerMaxBatchSize,
		config.KeymapManagerMaxBatchBytes,
		config.GCBatchSize,
		config.KeymapManagerMaxInterval,
		config.KeymapManagerMaxBufferedDeletes,
	)
	table.keymapManager = kManager

	fLoop := &flushLoop{
		logger:                 runtimeConfig.Logger,
		keymapManager:          kManager,
		errorMonitor:           errorMonitor,
		flushChannel:           make(chan any, config.FlushChannelSize),
		metrics:                metrics,
		clock:                  runtimeConfig.Clock,
		name:                   name,
		upperBoundSnapshotFile: upperBoundSnapshotFile,
	}
	table.flushLoop = fLoop

	cLoop := &controlLoop{
		logger:                  runtimeConfig.Logger,
		diskTable:               table,
		errorMonitor:            errorMonitor,
		controllerChannel:       make(chan any, config.ControlChannelSize),
		highestSegmentIndex:     highestSegmentIndex,
		segments:                segments,
		size:                    &table.size,
		keyCount:                &table.keyCount,
		targetFileSize:          config.TargetSegmentFileSize,
		targetKeyFileSize:       config.TargetSegmentKeyFileSize,
		maxKeyCount:             config.MaxSegmentKeyCount,
		autoFlushByteThreshold:  config.AutoFlushByteThreshold,
		shardControlChannelSize: config.ShardControlChannelSize,
		clock:                   runtimeConfig.Clock,
		segmentPaths:            segmentPaths,
		snapshottingEnabled:     snapshottingEnabled,
		fsync:                   config.Fsync,
		metrics:                 metrics,
		name:                    name,
		keymap:                  keymap,
		keymapManager:           kManager,
		flushLoop:               fLoop,
		garbageCollectionPeriod: config.GCPeriod,
		immutableSegmentSize:    immutableSegmentSize,
		deletionWatermarkChan:   make(chan int64, config.KeymapManagerWatermarkChannelSize),
		keymapDeletionWatermark: initialDeletionWatermark,
		compressionAlgorithm:    tableConfig.Compression,
	}
	cLoop.lowestSegmentIndex = lowestSegmentIndex
	cLoop.threadsafeHighestSegmentIndex.Store(highestSegmentIndex)

	// Wire the compression stage in front of the control loop when compression is enabled. enqueue sends
	// to inputChannel; the compression loop compresses write requests and forwards every message (in
	// order, so flush stays ordered behind its writes) to controllerChannel. When compression is
	// disabled, enqueue targets controllerChannel directly and no compression goroutine runs.
	var cmpLoop *compressionLoop
	if tableConfig.Compression == types.CompressionNone {
		cLoop.inputChannel = cLoop.controllerChannel
	} else {
		cmpLoop = &compressionLoop{
			logger:        runtimeConfig.Logger,
			errorMonitor:  errorMonitor,
			algorithm:     tableConfig.Compression,
			inputChannel:  make(chan any, config.ControlChannelSize),
			outputChannel: cLoop.controllerChannel,
			metrics:       metrics,
			name:          name,
			clock:         runtimeConfig.Clock,
		}
		cLoop.inputChannel = cmpLoop.inputChannel
	}

	table.controlLoop = cLoop
	cLoop.updateCurrentSize()

	// The keymap manager reports deletion-watermark advances by calling cLoop.publishDeletionWatermark; give it
	// the reference now, before its goroutine starts.
	kManager.controlLoop = cLoop

	// Start the GC manager. It performs collection off the control loop: reading expired segments' keys,
	// advancing the durable gc-watermark, and scheduling their keymap deletes. Its "scheduled for deletion"
	// cursor starts just below the lowest readable segment, so it does not re-schedule deletes for segments
	// already collected before a crash. It owns the channel by which the control loop hands over sealed segments.
	gcMgr := newGCManager(
		runtimeConfig.Logger,
		errorMonitor,
		config.GCSegmentChannelSize,
		initialSealedSegments,
		kManager,
		runtimeConfig.Clock,
		metrics,
		name,
		config.GCPeriod,
		gcWatermarkFile,
		tableConfig.GCFilter,
		initialDeletionWatermark,
		tableConfig.TTL,
	)
	table.gcManager = gcMgr

	// Everything is wired; start the goroutines.
	go kManager.run()
	go fLoop.run()
	go cLoop.run()
	go gcMgr.run()
	if cmpLoop != nil {
		go cmpLoop.run()
	}

	return table, nil
}

func (d *DiskTable) KeyCount() uint64 {
	return uint64(d.keyCount.Load()) //nolint:gosec // key count non-negative
}

func (d *DiskTable) Size() uint64 {
	return d.size.Load()
}

// repairSnapshot is responsible for making any required repairs to the snapshot directories. This is needed
// if there is a crash, resulting in a segment not being fully snapshotted. It is also needed if LittDB has
// been rebased (which breaks symlinks) or manually modified (e.g. by the LittDB cli). Returns the new upper bound
// file for the repaired snapshot.
func (d *DiskTable) repairSnapshot(
	symlinkDirectory string,
	lowestReadableSegment uint32,
	highestSegmentIndex uint32,
	segments map[uint32]*segment.Segment) (*BoundaryFile, error) {

	symlinkTableDirectory := path.Join(symlinkDirectory, d.name)

	err := util.EnsureDirectoryExists(symlinkTableDirectory, d.fsync)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure symlink table directory exists: %w", err)
	}

	upperBoundSnapshotFile, err := LoadBoundaryFile(UpperBound, symlinkTableDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to load snapshot boundary file: %w", err)
	}

	// Prevent other processes from messing with the symlink table directory while we are working on it.
	lockPath := path.Join(symlinkTableDirectory, util.LockfileName)
	lock, err := util.NewFileLock(d.logger, lockPath, false)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock on symlink table directory: %w", err)
	}
	defer lock.Release()

	symlinkSegmentsDirectory := path.Join(symlinkTableDirectory, segment.SegmentDirectory)
	exists, err := util.Exists(symlinkSegmentsDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to check if symlink segments directory exists: %w", err)
	}
	if exists {
		// Delete all data from the previous snapshot. This directory will contain a bunch of symlinks. It's a lot
		// simpler to just rebuild this from scratch than it is to try to figure out which symlinks are valid
		// and which are not. Building this is super fast, so this is not a performance concern.
		err = os.RemoveAll(symlinkSegmentsDirectory)
		if err != nil {
			return nil, fmt.Errorf("failed to remove symlink segments directory: %w", err)
		}
	}

	err = os.MkdirAll(symlinkSegmentsDirectory, 0750)
	if err != nil {
		return nil, fmt.Errorf("failed to create symlink segments directory: %w", err)
	}

	if len(segments) <= 1 {
		// There is only the mutable segment, nothing else to do.
		return upperBoundSnapshotFile, nil
	}

	lowerBoundSnapshotFile, err := LoadBoundaryFile(LowerBound, symlinkTableDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed to load snapshot boundary file: %w", err)
	}

	firstSegmentToConsider := lowestReadableSegment
	if lowerBoundSnapshotFile.IsDefined() {
		// The lower bound file contains the index of the highest segment that has been GC'd by an external process.
		// We should ignore the segment at this index, and all segments with lower indices. But never drop below the
		// gc-watermark (lowestReadableSegment): a stale external bound below the watermark must not pull
		// logically-deleted segments back into the snapshot, which would resurrect collected keys on restore.
		if next := lowerBoundSnapshotFile.BoundaryIndex() + 1; next > firstSegmentToConsider {
			firstSegmentToConsider = next
		}
	}

	// Skip iterating over the highest segment index (i.e. don't do i <= highestSegmentIndex). The highest segment
	// index is mutable and cannot be snapshotted until it has been sealed.
	for i := firstSegmentToConsider; i < highestSegmentIndex; i++ {
		seg := segments[i]
		err = seg.Snapshot()
		if err != nil {
			return nil, fmt.Errorf("failed to snapshot segment %d: %w", i, err)
		}
	}

	// Signal that the segment files are now fully snapshotted and safe to use.
	// The highest segment index is the mutable segment, which is not snapshotted.
	err = upperBoundSnapshotFile.Update(highestSegmentIndex - 1)
	if err != nil {
		return nil, fmt.Errorf("failed to update upper bound snapshot file: %w", err)
	}

	return upperBoundSnapshotFile, nil
}

// reloadKeymap reloads the keymap from the segments. This is necessary when the keymap is lost, the keymap doesn't
// save its data on disk, or we are migrating from one keymap type to another.
//
// Segments below lowestReadableSegment are skipped: their keys were durably deleted by garbage collection (the
// gc-watermark survives a keymap rebuild precisely so this rebuild can honor it). Reloading them would resurrect
// keys that were already collected. NOTE: if BOTH the keymap and the gc-watermark file are lost, this rebuild
// has no record of in-flight GC and may resurrect keys from segments that were being collected; that
// total-loss case predates the gc-watermark and remains the only window in which it can happen.
func (d *DiskTable) reloadKeymap(
	segments map[uint32]*segment.Segment,
	lowestReadableSegment uint32,
	highestSegmentIndex uint32,
) error {

	start := d.clock()
	defer func() {
		d.logger.Info("reloaded keymap", "duration", d.clock().Sub(start))
	}()

	batch := make([]*types.ScopedKey, 0, keymapReloadBatchSize)

	for i := lowestReadableSegment; i <= highestSegmentIndex; i++ {
		if !segments[i].IsSealed() {
			// ignore unsealed segment, this will have been created in the current session and will not
			// yet contain any data.
			continue
		}

		keys, err := segments[i].GetKeys()
		if err != nil {
			return fmt.Errorf("failed to get keys from segment %d: %w", i, err)
		}
		for keyIndex := len(keys) - 1; keyIndex >= 0; keyIndex-- {
			key := keys[keyIndex]

			batch = append(batch, key)
			if len(batch) == keymapReloadBatchSize {
				err = d.keymap.Put(batch)
				if err != nil {
					return fmt.Errorf("failed to put keys for segment %d: %w", i, err)
				}
				batch = make([]*types.ScopedKey, 0, keymapReloadBatchSize)
			}
		}
	}

	if len(batch) > 0 {
		err := d.keymap.Put(batch)
		if err != nil {
			return fmt.Errorf("failed to put keys: %w", err)
		}
	}

	// Now that the keymap is loaded, write the marker file that indicates that the keymap is fully loaded.
	// If we crash prior to writing this file, the keymap will reload from the segments again.
	keymapInitializedFile := path.Join(d.keymapPath, keymap.KeymapInitializedFileName)
	err := os.MkdirAll(d.keymapPath, 0750)
	if err != nil {
		return fmt.Errorf("failed to create keymap directory: %w", err)
	}

	f, err := os.Create(keymapInitializedFile) //nolint:gosec // path within keymap directory
	if err != nil {
		return fmt.Errorf("failed to create keymap initialized file after reload: %w", err)
	}
	err = f.Close()
	if err != nil {
		return fmt.Errorf("failed to close keymap initialized file after reload: %w", err)
	}

	return nil
}

// After a crash, it's possible that there may be a small number of key-value pairs in segment files that
// never made it into the keymap. This method detects these orphaned key-value pairs and puts them into the keymap.
//
// Segments below lowestReadableSegment are never examined: their keys were durably deleted by garbage collection
// (the gc-watermark advanced past them before their keymap entries were deleted), so a key of theirs missing
// from the keymap is an intentional GC deletion, not a lost async write. Rescuing it would resurrect collected
// data. This is the durable barrier that lets repair distinguish the two; the in-memory deletion watermark used
// to be reset on reboot, which is what allowed the resurrection bug.
func (d *DiskTable) repairKeymap(
	segments map[uint32]*segment.Segment,
	lowestReadableSegment uint32,
	highestSegmentIndex uint32,
) error {

	start := d.clock()
	var rescued []*types.ScopedKey
	defer func() {
		d.logger.Debug("repaired keymap", "keysRescued", len(rescued), "duration", d.clock().Sub(start))
	}()

	// The keymap is always written in the same order that keys are appended to the segment key files, and the
	// keymap recovers to a prefix of those writes. Any keys missing from the keymap are therefore a contiguous
	// suffix (the most recently written keys). We walk keys newest-first, collecting any that are absent from the
	// keymap, and stop at the first key that is present: everything older than it is already durable.
	//
	// The rescued keys MUST be written in a single atomic batch. If we instead flushed incrementally, a crash
	// partway through would leave the newest rescued keys present while older ones were still missing. The next
	// repair would walk newest-first, immediately hit one of those newly-written keys, stop, and never rescue
	// the older keys that are still absent. A single batch makes repair all-or-nothing: a crash either leaves
	// nothing written (the next repair redoes it) or everything written (the next repair sees an intact prefix).

	reachedDurablePrefix := false
	for i := highestSegmentIndex; !reachedDurablePrefix; i-- {
		seg := segments[i]
		if seg.IsSealed() {
			keys, err := seg.GetKeys()
			if err != nil {
				return fmt.Errorf("failed to get keys from segment %d: %w", i, err)
			}
			for keyIndex := len(keys) - 1; keyIndex >= 0; keyIndex-- {
				key := keys[keyIndex]

				_, present, err := d.keymap.Get(key.Key)
				if err != nil {
					return fmt.Errorf("failed to check keymap for key in segment %d: %w", i, err)
				}
				if present {
					// Reached the durable prefix; every older key is already in the keymap.
					reachedDurablePrefix = true
					break
				}

				rescued = append(rescued, key)
			}
		}

		if i == lowestReadableSegment {
			break
		}
	}

	if len(rescued) == 0 {
		return nil
	}

	if err := d.keymap.Put(rescued); err != nil {
		return fmt.Errorf("failed to put rescued keys into keymap: %w", err)
	}

	return nil
}

// purgeCollectedKeymapEntries deletes keymap entries for the segments in [lowestSegmentIndex,
// lowestReadableSegment): segments that garbage collection logically deleted (the durable gc-watermark was
// advanced past them) but whose asynchronous keymap deletes may have been lost in a crash before they were
// applied. repairKeymap and reloadKeymap only add keys, never remove them, so a lost delete would otherwise
// leave entries pointing at segments the control loop is about to reclaim, resurrecting collected keys via
// Get/Exists. This runs synchronously at startup, before any goroutine can reclaim files, so a segment's keymap
// entries are durably gone before its files are deleted (preserving keymap-delete-before-file-delete ordering
// across a crash). Deleting an already-absent key is a no-op, so this is safe to repeat and is a complete no-op
// on a clean restart, where lowestReadableSegment == lowestSegmentIndex.
func (d *DiskTable) purgeCollectedKeymapEntries(
	segments map[uint32]*segment.Segment,
	lowestSegmentIndex uint32,
	lowestReadableSegment uint32,
) error {

	if lowestReadableSegment <= lowestSegmentIndex {
		// No collected-but-unreclaimed segments (the common case on a clean restart).
		return nil
	}

	start := d.clock()
	purged := 0
	defer func() {
		d.logger.Debug("purged collected keymap entries", "keysPurged", purged, "duration", d.clock().Sub(start))
	}()

	batch := make([]*types.ScopedKey, 0, keymapReloadBatchSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := d.keymap.Delete(batch); err != nil {
			return fmt.Errorf("failed to delete keys from keymap: %w", err)
		}
		purged += len(batch)
		batch = make([]*types.ScopedKey, 0, keymapReloadBatchSize)
		return nil
	}

	for i := lowestSegmentIndex; i < lowestReadableSegment; i++ {
		seg := segments[i]
		if seg == nil {
			// Should be impossible: collected-but-unreclaimed segments are a contiguous prefix and are all
			// present in the map (the load-time gc-watermark check guarantees lowestReadableSegment is in range).
			// Bubble up rather than nil-dereference if that invariant is ever violated.
			return fmt.Errorf("segment %d missing while purging collected keymap entries", i)
		}
		if !seg.IsSealed() {
			// Defensive: collected segments are always sealed, so they have durable keys to purge.
			continue
		}
		keys, err := seg.GetKeys()
		if err != nil {
			return fmt.Errorf("failed to get keys from segment %d: %w", i, err)
		}
		for _, key := range keys {
			batch = append(batch, key)
			if len(batch) == keymapReloadBatchSize {
				if err := flush(); err != nil {
					return err
				}
			}
		}
	}

	return flush()
}

func (d *DiskTable) Name() string {
	return d.name
}

// Close stops the disk table. Flushes all data out to disk.
func (d *DiskTable) Close() error {
	firstTimeClosing := d.closed.CompareAndSwap(false, true)
	if !firstTimeClosing {
		return nil
	}

	if ok, err := d.errorMonitor.IsOk(); !ok {
		return fmt.Errorf("cannot process Stop() request, DB is in panicked state due to error: %w", err)
	}

	d.errorMonitor.Shutdown()

	shutdownCompleteChan := make(chan struct{}, 1)
	request := &controlLoopShutdownRequest{
		shutdownCompleteChan: shutdownCompleteChan,
	}

	err := d.controlLoop.enqueue(request)
	if err != nil {
		return fmt.Errorf("failed to send shutdown request: %w", err)
	}

	_, err = util.Await(d.errorMonitor, shutdownCompleteChan)
	if err != nil {
		return fmt.Errorf("failed to shutdown: %w", err)
	}

	return nil
}

// IsDropped returns true if the table has been dropped (see Drop).
func (d *DiskTable) IsDropped() bool {
	return d.destroyed.Load()
}

// Drop stops the disk table and deletes all files.
func (d *DiskTable) Drop() error {
	firstTimeDestroying := d.destroyed.CompareAndSwap(false, true)
	if !firstTimeDestroying {
		return nil // already dropped
	}

	err := d.Close()
	if err != nil {
		return fmt.Errorf("failed to stop: %w", err)
	}

	d.logger.Info("deleting disk table", "paths", d.roots)

	// release all segments
	segments, err := d.controlLoop.getSegments()
	if err != nil {
		return fmt.Errorf("failed to get segments: %w", err)
	}
	for _, seg := range segments {
		seg.Release()
	}
	// wait for segments to delete themselves
	for _, seg := range segments {
		err = seg.BlockUntilFullyDeleted()
		if err != nil {
			return fmt.Errorf("failed to delete segment: %w", err)
		}
	}

	// delete all segment directories (ignore snapshots -- this is the responsibility of an outside process to clean)
	for _, segmentPath := range d.segmentPaths {
		err = os.Remove(segmentPath.SegmentDirectory())
		if err != nil {
			return fmt.Errorf("failed to remove segment directory: %w", err)
		}
	}

	// delete the snapshot hardlink directory
	for _, root := range d.roots {
		snapshotDir := path.Join(root, segment.HardLinkDirectory)
		exists, err := util.Exists(snapshotDir)
		if err != nil {
			return fmt.Errorf("failed to check if snapshot directory exists: %w", err)
		}
		if exists {
			err = os.RemoveAll(snapshotDir)
			if err != nil {
				return fmt.Errorf("failed to remove snapshot directory: %w", err)
			}
		}
	}

	// destroy the keymap
	err = d.keymap.Destroy()
	if err != nil {
		return fmt.Errorf("failed to destroy keymap: %w", err)
	}
	err = d.keymapTypeFile.Delete()
	if err != nil {
		return fmt.Errorf("failed to delete keymap type file: %w", err)
	}
	exists, err := util.Exists(d.keymapPath)
	if err != nil {
		return fmt.Errorf("failed to check if keymap directory exists: %w", err)
	}
	if exists {
		err = os.RemoveAll(d.keymapPath)
		if err != nil {
			return fmt.Errorf("failed to remove keymap directory: %w", err)
		}
	}

	// Delete the gc-watermark file (it lives at the table root, so the root cannot be removed while it remains).
	for _, root := range d.roots {
		gcWatermarkPath := path.Join(root, GCWatermarkFileName)
		exists, err := util.Exists(gcWatermarkPath)
		if err != nil {
			return fmt.Errorf("failed to check if gc-watermark file exists: %w", err)
		}
		if exists {
			err = os.Remove(gcWatermarkPath)
			if err != nil {
				return fmt.Errorf("failed to remove gc-watermark file: %w", err)
			}
		}
	}

	// delete the root directories for the table
	for _, root := range d.roots {
		err = os.Remove(root)
		if err != nil {
			return fmt.Errorf("failed to remove root directory: %w", err)
		}
	}

	return nil
}

// getShardingFactor returns the in-memory sharding factor for the table. Capped at litt.MaxShardingFactor
// (255) so the value always fits in a single byte.
func (d *DiskTable) getShardingFactor() uint8 {
	return uint8(d.shardingFactor.Load()) //nolint:gosec // bounded to uint8 by setShardingFactor / constructor
}

// setShardingFactor sets the in-memory sharding factor for the table.
func (d *DiskTable) setShardingFactor(shardingFactor uint8) {
	d.shardingFactor.Store(uint32(shardingFactor))
}

// SetTTL sets the TTL for the disk table. If set to 0, no TTL is enforced. This setting affects both new
// data and data already written. The TTL is consumed only by the GC manager, so it is pushed there directly.
func (d *DiskTable) SetTTL(ttl time.Duration) error {
	if ok, err := d.errorMonitor.IsOk(); !ok {
		return fmt.Errorf("cannot process SetTTL() request, DB is in panicked state due to error: %w", err)
	}

	d.gcManager.setTTL(ttl)
	return nil
}

func (d *DiskTable) SetShardingFactor(shardingFactor uint8) error {
	if ok, err := d.errorMonitor.IsOk(); !ok {
		return fmt.Errorf(
			"cannot process SetShardingFactor() request, DB is in panicked state due to error: %w", err)
	}

	if shardingFactor == 0 {
		return fmt.Errorf("sharding factor must be greater than 0")
	}

	request := &controlLoopSetShardingFactorRequest{
		shardingFactor: shardingFactor,
	}
	err := d.controlLoop.enqueue(request)
	if err != nil {
		return fmt.Errorf("failed to send sharding factor request: %w", err)
	}

	return nil
}

func (d *DiskTable) Get(key []byte) (value []byte, exists bool, err error) {
	if ok, err := d.errorMonitor.IsOk(); !ok {
		return nil, false, fmt.Errorf(
			"cannot process Get() request, DB is in panicked state due to error: %w", err)
	}

	// First, check if the key is in the unflushed data map.
	// If so, return it from there.
	if value, ok := d.unflushedDataCache.Load(util.UnsafeBytesToString(key)); ok {
		bytes := value.([]byte)
		return bytes, true, nil
	}

	// Look up the address of the data.
	address, ok, err := d.keymap.Get(key)
	if err != nil {
		return nil, false, fmt.Errorf("failed to get address: %w", err)
	}
	if !ok {
		return nil, false, nil
	}

	// Reserve the segment that contains the data.
	seg, ok := d.controlLoop.getReservedSegment(address.Index())
	if !ok {
		return nil, false, nil
	}
	defer seg.Release()

	// Read the data from disk.
	data, err := seg.Read(key, address)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read data: %w", err)
	}

	return data, true, nil
}

func (d *DiskTable) Put(key []byte, value []byte, secondaryKeys ...*types.SecondaryKey) error {
	return d.PutBatch([]*types.PutRequest{{Key: key, Value: value, SecondaryKeys: secondaryKeys}})
}

func (d *DiskTable) PutBatch(batch []*types.PutRequest) error {
	if ok, err := d.errorMonitor.IsOk(); !ok {
		return fmt.Errorf("cannot process PutBatch() request, DB is in panicked state due to error: %w", err)
	}

	// Per-request key count (primary + secondaries). Pre-computed during validation so we can use
	// it both for metrics and for the keyCount.Add() at the end.
	totalKeys := int64(0)
	totalSize := uint64(0)

	for _, kv := range batch {
		if kv.Key == nil {
			return fmt.Errorf("nil keys are not supported")
		}
		if kv.Value == nil {
			return fmt.Errorf("nil values are not supported")
		}
		if len(kv.Key) > math.MaxUint16 {
			return fmt.Errorf("key is too large, length must not exceed 2^16 bytes: %d bytes", len(kv.Key))
		}
		if len(kv.Value) > math.MaxUint32 {
			return fmt.Errorf("value is too large, length must not exceed 2^32 - 1 bytes: %d bytes", len(kv.Value))
		}

		// Validate every secondary key in this request, and detect duplicate keys (primary vs
		// secondary, secondary vs secondary) within the request. Cross-request collisions remain
		// the caller's responsibility, matching existing semantics for primary keys.
		seen := make(map[string]struct{}, 1+len(kv.SecondaryKeys))
		seen[util.UnsafeBytesToString(kv.Key)] = struct{}{}
		for _, sk := range kv.SecondaryKeys {
			if sk == nil {
				return fmt.Errorf("nil secondary key is not supported")
			}
			if sk.Key == nil {
				return fmt.Errorf("nil secondary key bytes are not supported")
			}
			if len(sk.Key) > math.MaxUint16 {
				return fmt.Errorf("secondary key is too large, length must not exceed 2^16 bytes: %d bytes",
					len(sk.Key))
			}
			end := uint64(sk.Offset) + uint64(sk.Length)
			if end > uint64(len(kv.Value)) {
				return fmt.Errorf(
					"secondary key range [%d, %d) exceeds value length %d", sk.Offset, end, len(kv.Value))
			}
			// On a compressed table, a secondary key may only alias the entire value. A compressed blob
			// cannot be sliced, so a strict sub-range would require storing a second (duplicated)
			// compressed copy, which is a documented future optimization rather than current behavior.
			if d.compressionAlgorithm != types.CompressionNone &&
				(sk.Offset != 0 || uint64(sk.Length) != uint64(len(kv.Value))) {
				return fmt.Errorf(
					"secondary key range [%d, %d) is a strict sub-range of the value (length %d), which is "+
						"not supported on a compressed table; secondary keys must alias the entire value",
					sk.Offset, end, len(kv.Value))
			}
			skKey := util.UnsafeBytesToString(sk.Key)
			if _, dup := seen[skKey]; dup {
				return fmt.Errorf("duplicate key %x within PutRequest", sk.Key)
			}
			seen[skKey] = struct{}{}
		}

		totalKeys += int64(1 + len(kv.SecondaryKeys))
		totalSize += uint64(len(kv.Value))
	}

	if d.metrics != nil {
		start := d.clock()
		defer func() {
			end := d.clock()
			delta := end.Sub(start)
			d.metrics.ReportWriteOperation(d.name, delta, uint64(totalKeys), totalSize) //nolint:gosec // totalKeys non-negative
		}()
	}

	// All requests validated. Populate the unflushed data cache: each key (primary or secondary)
	// is stored under its own key, with secondaries pointing at a zero-copy sub-slice of the parent
	// value. This makes Get/Exists/CacheAwareGet treat secondaries identically to primaries before
	// the data is durable.
	for _, kv := range batch {
		d.unflushedDataCache.Store(util.UnsafeBytesToString(kv.Key), kv.Value)
		for _, sk := range kv.SecondaryKeys {
			d.unflushedDataCache.Store(
				util.UnsafeBytesToString(sk.Key),
				kv.Value[sk.Offset:sk.Offset+sk.Length])
		}
	}

	request := &controlLoopWriteRequest{
		values: batch,
	}
	err := d.controlLoop.enqueue(request)
	if err != nil {
		return fmt.Errorf("failed to send write request: %w", err)
	}

	d.keyCount.Add(totalKeys)

	return nil
}

func (d *DiskTable) Exists(key []byte) (bool, error) {
	_, ok := d.unflushedDataCache.Load(util.UnsafeBytesToString(key))
	if ok {
		return true, nil
	}

	address, ok, err := d.keymap.Get(key)
	if err != nil {
		return false, fmt.Errorf("failed to get address: %w", err)
	}
	if !ok {
		return false, nil
	}

	// A keymap entry can outlive its segment: a keymap delete lost in a crash leaves a stale entry pointing at
	// a segment the control loop then reclaims. Confirm the backing segment is still live (mirroring Get) so a
	// stale entry never reports a key that Get would not return. This is a map lookup + reservation, no disk read.
	seg, ok := d.controlLoop.getReservedSegment(address.Index())
	if !ok {
		return false, nil
	}
	seg.Release()

	return true, nil
}

// Flush flushes all data to disk. Blocks until all data previously submitted to Put has been written to disk.
func (d *DiskTable) Flush() error {
	// The flush coordinator batches flush requests together to improve performance if
	// flushes are being requested very frequently.
	err := d.flushCoordinator.Flush()
	if err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}
	return nil
}

// actually flushes the internal DB
func (d *DiskTable) flushInternal() error {
	if ok, err := d.errorMonitor.IsOk(); !ok {
		return fmt.Errorf("cannot process Flush() request, DB is in panicked state due to error: %w", err)
	}

	if d.metrics != nil {
		start := d.clock()
		defer func() {
			end := d.clock()
			delta := end.Sub(start)
			d.metrics.ReportFlushOperation(d.name, delta)
		}()
	}

	flushReq := &controlLoopFlushRequest{
		responseChan: make(chan struct{}, 1),
	}
	err := d.controlLoop.enqueue(flushReq)
	if err != nil {
		return fmt.Errorf("failed to send flush request: %w", err)
	}

	_, err = util.Await(d.errorMonitor, flushReq.responseChan)
	if err != nil {
		return fmt.Errorf("failed to flush: %w", err)
	}

	return nil
}

func (d *DiskTable) SetWriteCacheSize(size uint64) error {
	if ok, err := d.errorMonitor.IsOk(); !ok {
		return fmt.Errorf(
			"cannot process SetWriteCacheSize() request, DB is in panicked state due to error: %w", err)
	}

	// this implementation does not provide a cache, if a cache is needed then it must be provided by a wrapper
	return nil
}

func (d *DiskTable) SetReadCacheSize(size uint64) error {
	if ok, err := d.errorMonitor.IsOk(); !ok {
		return fmt.Errorf(
			"cannot process SetReadCacheSize() request, DB is in panicked state due to error: %w", err)
	}

	// this implementation does not provide a cache, if a cache is needed then it must be provided by a wrapper
	return nil
}

func (d *DiskTable) RunGC() error {
	if ok, err := d.errorMonitor.IsOk(); !ok {
		return fmt.Errorf(
			"cannot process RunGC() request, DB is in panicked state due to error: %w", err)
	}

	// Collection and the keymap deletes it schedules happen asynchronously across the GC manager and the keymap
	// manager. To make an explicit RunGC() deterministic, drive the steps in order: run a collection pass on the
	// GC manager (scheduling keymap deletes for expired segments and durably advancing the gc-watermark), sync
	// the keymap manager so those deletes are applied and the deletion watermark advances, then have the control
	// loop delete the now-eligible segment files.
	if err := d.gcManager.runOnce(); err != nil {
		return err
	}
	if err := d.keymapManager.sync(); err != nil {
		return fmt.Errorf("failed to sync keymap manager during GC: %w", err)
	}
	if err := d.runGCPass(); err != nil {
		return err
	}

	return nil
}

// runGCPass triggers the control loop's collected-segment file deletion and waits for it to complete.
func (d *DiskTable) runGCPass() error {
	request := &controlLoopGCRequest{
		completionChan: make(chan struct{}, 1),
	}

	if err := d.controlLoop.enqueue(request); err != nil {
		return fmt.Errorf("failed to send GC request: %w", err)
	}

	if _, err := util.Await(d.errorMonitor, request.completionChan); err != nil {
		return fmt.Errorf("failed to await GC completion: %w", err)
	}

	return nil
}

// Iterator returns a new iterator over the keys in the table.
func (d *DiskTable) Iterator(reverse bool) (litt.Iterator, error) {
	if ok, err := d.errorMonitor.IsOk(); !ok {
		return nil, fmt.Errorf("cannot process Iterator() request, DB is in panicked state due to error: %w", err)
	}

	request := &controlLoopOpenIteratorRequest{
		responseChan: make(chan []*segment.Segment, 1),
	}
	err := d.controlLoop.enqueue(request)
	if err != nil {
		return nil, fmt.Errorf("failed to send open iterator request: %w", err)
	}

	segs, err := util.Await(d.errorMonitor, request.responseChan)
	if err != nil {
		return nil, fmt.Errorf("failed to await iterator open: %w", err)
	}

	if reverse {
		return newReverseIterator(d, segs), nil
	}
	return newForwardIterator(d, segs), nil
}

// GetOldestKey returns the oldest non-deleted primary key in the table.
func (d *DiskTable) GetOldestKey() (key []byte, exists bool, err error) {
	resp, err := d.boundaryKeys()
	if err != nil {
		return nil, false, fmt.Errorf("failed to get oldest key: %w", err)
	}
	return resp.oldestKey, resp.oldestExists, nil
}

// GetNewestKey returns the newest primary key in the table.
func (d *DiskTable) GetNewestKey() (key []byte, exists bool, err error) {
	resp, err := d.boundaryKeys()
	if err != nil {
		return nil, false, fmt.Errorf("failed to get newest key: %w", err)
	}
	return resp.newestKey, resp.newestExists, nil
}

// boundaryKeys sends a request to the control loop for the oldest and newest primary keys.
func (d *DiskTable) boundaryKeys() (*boundaryKeysResponse, error) {
	if ok, err := d.errorMonitor.IsOk(); !ok {
		return nil, fmt.Errorf("cannot process request, DB is in panicked state due to error: %w", err)
	}

	request := &controlLoopBoundaryKeysRequest{
		responseChan: make(chan *boundaryKeysResponse, 1),
	}
	err := d.controlLoop.enqueue(request)
	if err != nil {
		return nil, fmt.Errorf("failed to send boundary keys request: %w", err)
	}

	resp, err := util.Await(d.errorMonitor, request.responseChan)
	if err != nil {
		return nil, fmt.Errorf("failed to await boundary keys: %w", err)
	}
	if resp.err != nil {
		return nil, resp.err
	}
	return resp, nil
}
