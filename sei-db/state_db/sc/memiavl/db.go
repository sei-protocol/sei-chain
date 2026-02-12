package memiavl

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alitto/pond"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	"github.com/cosmos/iavl"
)

const LockFileName = "LOCK"

var errReadOnly = errors.New("db is read-only")

// DB implements DB-like functionalities on top of MultiTree:
// - async snapshot rewriting
// - Write-ahead-log
//
// The memiavl.db directory looks like this:
// ```
// > current -> snapshot-N
// > snapshot-N
// >  bank
// >    kvs
// >    nodes
// >    metadata
// >  acc
// >  ... other stores
// > rlog
// ```
type DB struct {
	MultiTree
	dir      string
	logger   logger.Logger
	fileLock FileLock
	readOnly bool
	opts     Options

	// streamHandler is the changelog WAL owned by MemIAVL.
	// It is opened during OpenDB (if present / allowed) and closed in DB.Close().
	streamHandler wal.ChangelogWAL
	// pendingLogEntry accumulates changes (changesets + upgrades) to be written
	// into the changelog WAL on the next Commit().
	pendingLogEntry proto.ChangelogEntry

	// result channel of snapshot rewrite goroutine
	snapshotRewriteChan chan snapshotResult
	// context cancel function to cancel the snapshot rewrite goroutine
	snapshotRewriteCancelFunc context.CancelFunc
	// the number of old snapshots to keep (excluding the latest one)
	snapshotKeepRecent uint32
	// block interval to take a new snapshot
	snapshotInterval uint32
	// minimum time interval between snapshots
	// Protected by db.mtx (only accessed in Commit call chain)
	snapshotMinTimeInterval time.Duration
	// timestamp of the last successful snapshot creation
	// Protected by db.mtx (only accessed in Commit call chain)
	lastSnapshotTime time.Time

	// pruneSnapshotLock guards concurrent prune operations; use TryLock in pruneSnapshots
	pruneSnapshotLock sync.Mutex
	// closed guards against double Close(), protected by db.mtx
	closed bool

	// walIndexDelta is the difference: version - walIndex for any entry.
	// Since both WAL indices and versions are strictly contiguous, this delta is constant.
	// Computed once when opening the DB from the first WAL entry.
	walIndexDelta int64

	// The assumptions to concurrency:
	// - The methods on DB are protected by a mutex
	// - Each call of OpenDB loads a separate instance, in query scenarios,
	//   it should be immutable, the cache stores will handle the temporary writes.
	// - The DB for the state machine will handle writes through the Commit call,
	//   this method is the sole entry point for tree modifications, and there's no concurrency internally
	//   (the background snapshot rewrite is handled separately), so we don't need locks in the Tree.
	mtx sync.Mutex
	// worker goroutine IdleTimeout = 5s
	snapshotWriterPool *pond.WorkerPool
}

const (
	SnapshotPrefix = "snapshot-"
	SnapshotDirLen = len(SnapshotPrefix) + 20
)

// getSnapshotModTime returns the modification time of the current snapshot directory.
// It reads the "current" symlink to get the actual snapshot directory.
// If the directory doesn't exist or there's an error, returns current time as fallback.
// This is safe for first startup (no snapshots yet) and error recovery scenarios.
//
// NOTE: Relies on filesystem ModTime which may be inaccurate after backup/restore operations.
// TODO: Consider storing timestamp in MultiTreeMetadata for reliability.
func getSnapshotModTime(logger logger.Logger, dir string) time.Time {
	// Read the "current" symlink to get the actual snapshot directory
	currentLink := filepath.Clean(currentPath(dir))
	snapshotName, err := os.Readlink(currentLink)
	if err != nil {
		if os.IsNotExist(err) {
			// First startup: no snapshot exists yet, use current time
			// This is expected and normal for new nodes
			logger.Debug("no current snapshot link found (first startup or after cleanup)", "path", currentLink)
		} else {
			// Unexpected error reading symlink
			logger.Error("failed to read current snapshot link, using current time as fallback", "error", err, "path", currentLink)
		}
		return time.Now()
	}

	// Clean the path and validate it's within the expected parent directory
	snapshotDir := filepath.Clean(filepath.Join(dir, snapshotName))
	expectedParent := filepath.Clean(dir)
	if !strings.HasPrefix(snapshotDir, expectedParent+string(filepath.Separator)) &&
		snapshotDir != expectedParent {
		logger.Error("invalid snapshot path detected, possible path traversal", "snapshot_dir", snapshotDir, "expected_parent", expectedParent)
		return time.Now()
	}

	info, err := os.Stat(snapshotDir)
	if err != nil {
		if os.IsNotExist(err) {
			// Snapshot directory was deleted but symlink exists (inconsistent state)
			logger.Error("snapshot directory not found but symlink exists", "path", snapshotDir)
		} else {
			// Other stat errors (permission, I/O error, etc.)
			logger.Error("failed to stat snapshot directory, using current time as fallback", "error", err, "path", snapshotDir)
		}
		return time.Now()
	}

	return info.ModTime()
}

func OpenDB(logger logger.Logger, targetVersion int64, opts Options) (database *DB, _err error) {
	startTime := time.Now()
	defer func() {
		otelMetrics.RestartLatency.Record(
			context.Background(),
			time.Since(startTime).Seconds(),
			metric.WithAttributes(attribute.Bool("success", _err == nil)),
		)
	}()
	var (
		err      error
		fileLock FileLock
	)
	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid commit store options: %w", err)
	}
	opts.FillDefaults()
	opts.Logger = logger
	if opts.CreateIfMissing {
		if err := createDBIfNotExist(opts.Dir, opts.InitialVersion); err != nil {
			return nil, fmt.Errorf("fail to load db: %w", err)
		}
	}

	if !opts.ReadOnly {
		fileLock, err = LockFile(filepath.Join(opts.Dir, LockFileName))
		if err != nil {
			return nil, fmt.Errorf("fail to lock db: %w", err)
		}

		// cleanup any temporary directories left by interrupted snapshot rewrite
		if err := removeTmpDirs(opts.Dir); err != nil {
			return nil, fmt.Errorf("fail to cleanup tmp directories: %w", err)
		}
	}

	snapshot := "current"
	if targetVersion > 0 {
		// find the biggest snapshot version that's less than or equal to the target version
		snapshotVersion, err := seekSnapshot(opts.Dir, targetVersion)
		if err != nil {
			return nil, fmt.Errorf("fail to seek snapshot: %w", err)
		}
		snapshot = snapshotName(snapshotVersion)
	}

	path := filepath.Join(opts.Dir, snapshot)
	mtree, err := LoadMultiTree(path, opts)
	if err != nil {
		return nil, err
	}

	// Snapshot mmap files are loaded with MADV_RANDOM in OpenSnapshot().

	// MemIAVL owns changelog lifecycle: always open the WAL here.
	// Even in read-only mode we may need WAL replay to reconstruct non-snapshot versions.
	streamHandler, err := wal.NewChangelogWAL(logger, utils.GetChangelogPath(opts.Dir), wal.Config{
		WriteBufferSize: opts.AsyncCommitBuffer,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open changelog WAL: %w", err)
	}

	// Compute WAL index delta (only needed once per DB open)
	var walIndexDelta int64
	var walHasEntries bool
	walIndexDelta, walHasEntries, err = computeWALIndexDelta(streamHandler)
	if err != nil {
		return nil, fmt.Errorf("failed to compute WAL index delta: %w", err)
	}
	// If WAL is empty, set delta so first WAL entry aligns with NextVersion().
	if !walHasEntries {
		walIndexDelta = mtree.WorkingCommitInfo().Version - 1
	}

	// Replay WAL to catch up to target version (if WAL has entries)
	if walHasEntries && (targetVersion == 0 || targetVersion > mtree.Version()) {
		logger.Info("Start catching up and replaying the MemIAVL changelog file")
		if err := mtree.Catchup(context.Background(), streamHandler, walIndexDelta, targetVersion); err != nil {
			return nil, err
		}
		logger.Info(fmt.Sprintf("Finished the replay and caught up to version %d", targetVersion))
	}

	if opts.LoadForOverwriting && targetVersion > 0 {
		currentSnapshot, err := os.Readlink(currentPath(opts.Dir))
		if err != nil {
			return nil, fmt.Errorf("fail to read current version: %w", err)
		}

		if snapshot != currentSnapshot {
			// downgrade `"current"` link first
			logger.Info("downgrade current link to %s", snapshot)
			if err := updateCurrentSymlink(opts.Dir, snapshot); err != nil {
				return nil, fmt.Errorf("fail to update current snapshot link: %w", err)
			}
		}

		// truncate the rlog file (if WAL is provided and has entries)
		if walHasEntries {
			logger.Info("truncate rlog after version: %d", targetVersion)
			// Use O(1) conversion: walIndex = version - delta
			truncateIndex := targetVersion - walIndexDelta
			if truncateIndex > 0 {
				if err := streamHandler.TruncateAfter(uint64(truncateIndex)); err != nil {
					return nil, fmt.Errorf("fail to truncate rlog file: %w", err)
				}
			}
		}

		// prune snapshots that's larger than the target version
		if err := traverseSnapshots(opts.Dir, false, func(version int64) (bool, error) {
			if version <= targetVersion {
				return true, nil
			}

			if err := atomicRemoveDir(filepath.Join(opts.Dir, snapshotName(version))); err != nil {
				logger.Error("fail to prune snapshot, version: %d", version)
			} else {
				logger.Info("prune snapshot, version: %d", version)
			}
			return false, nil
		}); err != nil {
			return nil, fmt.Errorf("fail to prune snapshots: %w", err)
		}
	}

	// create worker pool. recv tasks to write snapshot
	workerPool := pond.New(opts.SnapshotWriterLimit, opts.SnapshotWriterLimit*10)

	// Initialize lastSnapshotTime from the current snapshot directory's modification time
	// This ensures accurate time tracking even after restarts
	// Read the "current" symlink to get the actual snapshot directory's ModTime
	lastSnapshotTime := getSnapshotModTime(logger, opts.Dir)

	db := &DB{
		MultiTree:               *mtree,
		logger:                  logger,
		dir:                     opts.Dir,
		fileLock:                fileLock,
		readOnly:                opts.ReadOnly,
		walIndexDelta:           walIndexDelta,
		streamHandler:           streamHandler,
		snapshotKeepRecent:      opts.SnapshotKeepRecent,
		snapshotInterval:        opts.SnapshotInterval,
		snapshotMinTimeInterval: opts.SnapshotMinTimeDuration(),
		lastSnapshotTime:        lastSnapshotTime,
		snapshotWriterPool:      workerPool,
		opts:                    opts,
	}

	// Apply initial stores on a fresh DB (version 0) so they get persisted to WAL.
	// This creates the trees and populates pendingLogEntry, which will be written
	// to WAL on the first Commit().
	// ApplyUpgrades is idempotent (skips existing trees), so this is safe.
	if !opts.ReadOnly && db.Version() == 0 && len(opts.InitialStores) > 0 {
		var upgrades []*proto.TreeNameUpgrade
		for _, name := range opts.InitialStores {
			upgrades = append(upgrades, &proto.TreeNameUpgrade{Name: name})
		}
		if err := db.ApplyUpgrades(upgrades); err != nil {
			return nil, fmt.Errorf("failed to apply initial stores: %w", err)
		}
	}

	return db, nil
}

// GetWAL returns the WAL handler for changelog operations.
func (db *DB) GetWAL() wal.ChangelogWAL {
	return db.streamHandler
}

// GetWALIndexDelta returns the precomputed delta between version and WAL index.
// This allows O(1) conversion: version = walIndex + delta, walIndex = version - delta
func (db *DB) GetWALIndexDelta() int64 {
	return db.walIndexDelta
}

func removeTmpDirs(rootDir string) error {
	entries, err := os.ReadDir(rootDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasSuffix(entry.Name(), "-tmp") {
			continue
		}

		if err := os.RemoveAll(filepath.Join(rootDir, entry.Name())); err != nil {
			return err
		}
	}

	return nil
}

// ReadOnly returns whether the DB is opened in read-only mode.
func (db *DB) ReadOnly() bool {
	return db.readOnly
}

// SetInitialVersion wraps `MultiTree.SetInitialVersion`.
// it will do a snapshot rewrite, because we can't use rlog to record this change,
// we need it to convert versions to rlog index in the first place.
func (db *DB) SetInitialVersion(initialVersion int64) error {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	if db.readOnly {
		return errReadOnly
	}

	if db.lastCommitInfo.Version > 0 {
		return errors.New("initial version can only be set before any commit")
	}

	if err := db.MultiTree.SetInitialVersion(initialVersion); err != nil {
		return err
	}

	return initEmptyDB(db.dir, db.initialVersion.Load())
}

// ApplyUpgrades wraps MultiTree.ApplyUpgrades to add a lock.
func (db *DB) ApplyUpgrades(upgrades []*proto.TreeNameUpgrade) error {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	if db.readOnly {
		return errReadOnly
	}

	if len(upgrades) > 0 {
		db.pendingLogEntry.Upgrades = append(db.pendingLogEntry.Upgrades, upgrades...)
	}
	return db.MultiTree.ApplyUpgrades(upgrades)
}

// ApplyChangeSets wraps MultiTree.ApplyChangeSets to add a lock.
func (db *DB) ApplyChangeSets(changeSets []*proto.NamedChangeSet) (_err error) {
	if len(changeSets) == 0 {
		return nil
	}

	startTime := time.Now()
	defer func() {
		otelMetrics.ApplyChangesetLatency.Record(
			context.Background(),
			time.Since(startTime).Seconds(),
			metric.WithAttributes(attribute.Bool("success", _err == nil)),
		)
	}()

	db.mtx.Lock()
	defer db.mtx.Unlock()

	if db.readOnly {
		return errReadOnly
	}

	// Overwrite pending changesets for this commit; callers typically provide them once per block.
	db.pendingLogEntry.Changesets = changeSets
	return db.MultiTree.ApplyChangeSets(changeSets)
}

// ApplyChangeSet wraps MultiTree.ApplyChangeSet to add a lock.
func (db *DB) ApplyChangeSet(name string, changeSet iavl.ChangeSet) error {
	if len(changeSet.Pairs) == 0 {
		return nil
	}

	db.mtx.Lock()
	defer db.mtx.Unlock()

	if db.readOnly {
		return errReadOnly
	}

	db.pendingLogEntry.Changesets = append(db.pendingLogEntry.Changesets, &proto.NamedChangeSet{
		Name:      name,
		Changeset: changeSet,
	})
	return db.MultiTree.ApplyChangeSet(name, changeSet)
}

// checkAsyncTasks checks the status of background tasks non-blocking-ly and process the result
func (db *DB) checkAsyncTasks() error {
	return db.checkBackgroundSnapshotRewrite()
}

// CommittedVersion returns the current version of the MultiTree.
func (db *DB) CommittedVersion() (int64, error) {
	lastOffset, err := db.GetWAL().LastOffset()
	if err != nil {
		return 0, err
	}
	if lastOffset == 0 {
		return db.SnapshotVersion(), nil
	}
	return db.walIndexToVersion(lastOffset), nil
}

// checkBackgroundSnapshotRewrite check the result of background snapshot rewrite, cleans up the old snapshots and switches to a new multitree
func (db *DB) checkBackgroundSnapshotRewrite() error {
	// check the completeness of background snapshot rewriting
	select {
	case result, ok := <-db.snapshotRewriteChan:
		db.snapshotRewriteChan = nil
		db.snapshotRewriteCancelFunc = nil

		if !ok {
			// channel was closed without sending a result
			// Still prune old snapshots to prevent accumulation
			go db.pruneSnapshots()
			return errors.New("snapshot rewrite channel closed unexpectedly")
		}

		if result.mtree == nil {
			// background snapshot rewrite failed
			// Still prune old snapshots to prevent accumulation
			go db.pruneSnapshots()
			return fmt.Errorf("background snapshot rewriting failed: %w", result.err)
		}

		// wait for potential pending writes to finish, to make sure we catch up to latest state.
		// in real world, block execution should be slower than tree updates, so this should not block for long.
		for {
			committedVersion, err := db.CommittedVersion()
			if err != nil {
				return fmt.Errorf("get committed version failed: %w", err)
			}
			if db.lastCommitInfo.Version == committedVersion {
				break
			}
			time.Sleep(time.Nanosecond)
		}

		// catchup the remaining entries in rlog
		if wal := db.GetWAL(); wal != nil {
			if err := result.mtree.Catchup(context.Background(), wal, db.walIndexDelta, 0); err != nil {
				return fmt.Errorf("catchup failed: %w", err)
			}
		}

		// do the switch
		if err := db.reloadMultiTree(result.mtree); err != nil {
			return fmt.Errorf("switch multitree failed: %w", err)
		}
		// reset memnode counter
		TotalMemNodeSize.Store(0)
		TotalNumOfMemNode.Store(0)
		db.logger.Info("switched to new memiavl snapshot", "version", db.MultiTree.Version())
		go db.pruneSnapshots()

	default:
	}

	return nil
}

// pruneSnapshots prunes old snapshots, keeping only snapshotKeepRecent recent ones.
// Note: WAL truncation is now handled by CommitStore after each commit.
func (db *DB) pruneSnapshots() {
	if !db.pruneSnapshotLock.TryLock() {
		db.logger.Info("pruneSnapshots skipped, previous prune still in progress")
		return
	}
	defer db.pruneSnapshotLock.Unlock()

	db.logger.Info("pruneSnapshots started")
	startTime := time.Now()
	defer func() {
		db.logger.Info("pruneSnapshots completed", "duration_sec", fmt.Sprintf("%.2fs", time.Since(startTime).Seconds()))
	}()

	currentVersion, err := currentVersion(db.dir)
	if err != nil {
		db.logger.Error("failed to read current snapshot version", "err", err)
		return
	}

	counter := db.snapshotKeepRecent
	if err := traverseSnapshots(db.dir, false, func(version int64) (bool, error) {
		if version >= currentVersion {
			// ignore any newer snapshot directories, there could be ongoning snapshot rewrite.
			return false, nil
		}

		if counter > 0 {
			counter--
			return false, nil
		}

		name := snapshotName(version)
		db.logger.Info("prune snapshot", "name", name)

		if err := atomicRemoveDir(filepath.Join(db.dir, name)); err != nil {
			db.logger.Error("failed to prune snapshot", "err", err)
		}

		return false, nil
	}); err != nil {
		db.logger.Error("fail to prune snapshots", "err", err)
		return
	}
}

// computeWALIndexDelta computes the constant delta between version and WAL index.
// Since both are strictly contiguous, we only need to read one entry.
// Returns (delta, hasEntries, error). hasEntries is false if WAL is empty.
func computeWALIndexDelta(stream wal.ChangelogWAL) (int64, bool, error) {
	firstIndex, err := stream.FirstOffset()
	if err != nil {
		return 0, false, err
	}
	if firstIndex == 0 {
		return 0, false, nil // empty WAL
	}

	// Read just the first entry to compute delta
	var firstVersion int64
	err = stream.Replay(firstIndex, firstIndex, func(index uint64, entry proto.ChangelogEntry) error {
		firstVersion = entry.Version
		return nil
	})
	if err != nil {
		return 0, false, err
	}

	// delta = version - index, so for any entry: version = index + delta
	// #nosec G115 -- WAL indices are always much smaller than MaxInt64 in practice
	return firstVersion - int64(firstIndex), true, nil
}

// versionToWALIndex converts a version to its corresponding WAL index using the precomputed delta.
// Returns 0 if the version would result in an invalid (negative or zero) index.
func (db *DB) versionToWALIndex(version int64) uint64 {
	index := version - db.walIndexDelta
	if index <= 0 {
		return 0
	}
	// #nosec G115 -- index is guaranteed positive by the check above
	return uint64(index)
}

// walIndexToVersion converts a WAL index to its corresponding version using the precomputed delta.
func (db *DB) walIndexToVersion(index uint64) int64 {
	// #nosec G115 -- WAL indices are always much smaller than MaxInt64 in practice
	return int64(index) + db.walIndexDelta
}

// Commit wraps SaveVersion to bump the version and finalize the tree state.
// MemIAVL owns the changelog: it writes the pending changelog entry before committing the tree.
func (db *DB) Commit() (version int64, _err error) {
	startTime := time.Now()
	defer func() {
		ctx := context.Background()
		otelMetrics.CommitLatency.Record(
			ctx,
			time.Since(startTime).Seconds(),
			metric.WithAttributes(attribute.Bool("success", _err == nil)),
		)
		otelMetrics.MemNodeTotalSize.Record(ctx, TotalMemNodeSize.Load())
		otelMetrics.NumOfMemNode.Record(ctx, TotalNumOfMemNode.Load())
	}()

	db.mtx.Lock()
	defer db.mtx.Unlock()
	if db.readOnly {
		return 0, errReadOnly
	}

	// Commit the in-memory tree state FIRST.
	// MemIAVL is purely in-memory; SaveVersion() doesn't persist anything.
	// The changelog WAL is our persistence layer.
	v, err := db.MultiTree.SaveVersion(true)
	if err != nil {
		return 0, err
	}

	// Write to WAL AFTER successful SaveVersion.
	// Rationale: If SaveVersion fails but we already wrote to WAL, we'd have
	// a WAL entry for a version that was never committed. On replay, this would
	// corrupt state. By writing WAL after SaveVersion succeeds, we ensure WAL
	// only contains valid committed versions. If WAL write fails after SaveVersion,
	// we lose this version on crash (rollback to prior state), but remain consistent.
	//
	// Note: Write() automatically checks for any previous async write errors.
	if wal := db.GetWAL(); wal != nil {
		entry := db.pendingLogEntry
		entry.Version = v
		if err := wal.Write(entry); err != nil {
			return 0, fmt.Errorf("failed to write changelog WAL: %w", err)
		}
	}
	db.pendingLogEntry = proto.ChangelogEntry{}

	if err := db.checkAsyncTasks(); err != nil {
		return 0, err
	}

	// Rewrite tree snapshot if applicable
	db.rewriteIfApplicable(v)
	db.tryTruncateWAL()

	return v, nil
}

// tryTruncateWAL best-effort truncates old WAL entries that are older than the earliest snapshot.
func (db *DB) tryTruncateWAL() {
	if db.streamHandler == nil {
		return
	}
	firstWALIndex, err := db.streamHandler.FirstOffset()
	if err != nil || firstWALIndex == 0 {
		return
	}
	earliestSnapshotVersion, err := GetEarliestVersion(db.dir)
	if err != nil {
		return
	}
	if firstWALIndex > uint64(math.MaxInt64) {
		db.logger.Error("WAL first offset overflows int64; skipping truncation", "firstWALIndex", firstWALIndex)
		return
	}
	walEarliestVersion := db.walIndexToVersion(firstWALIndex)
	if walEarliestVersion >= earliestSnapshotVersion {
		return
	}
	truncateIndex := db.versionToWALIndex(earliestSnapshotVersion)
	if truncateIndex == 0 || truncateIndex <= firstWALIndex {
		return
	}
	if err := db.streamHandler.TruncateBefore(truncateIndex); err != nil {
		db.logger.Error("failed to truncate changelog WAL", "err", err, "truncateIndex", truncateIndex)
	}
}

func (db *DB) Copy() *DB {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	return db.copy()
}

func (db *DB) copy() *DB {
	mtree := db.MultiTree.Copy()

	return &DB{
		MultiTree:          *mtree,
		logger:             db.logger,
		dir:                db.dir,
		snapshotWriterPool: db.snapshotWriterPool,
		opts:               db.opts,
	}
}

// RewriteSnapshot writes the current version of memiavl into a snapshot, and update the `current` symlink.
func (db *DB) RewriteSnapshot(ctx context.Context) error {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	if db.readOnly {
		return errReadOnly
	}

	snapshotDir := snapshotName(db.lastCommitInfo.Version)
	targetPath := filepath.Clean(filepath.Join(db.dir, snapshotDir))

	// Check if snapshot already exists
	if info, err := os.Stat(targetPath); err == nil {
		if info.IsDir() {
			db.logger.Info("snapshot already exists, skipping",
				"snapshot_dir", snapshotDir,
				"version", db.lastCommitInfo.Version)
			return nil
		} else {
			// targetPath exists but is not a directory - this is unexpected
			db.logger.Error("snapshot path exists but is not a directory",
				"path", targetPath)
			return fmt.Errorf("snapshot path exists but is not a directory: %s", targetPath)
		}
	}

	tmpDir := snapshotDir + "-tmp"
	path := filepath.Clean(filepath.Join(db.dir, tmpDir))

	writeStart := time.Now()
	err := db.WriteSnapshotWithRateLimit(ctx, path, db.snapshotWriterPool, db.opts.SnapshotWriteRateMBps)
	writeElapsed := time.Since(writeStart).Seconds()

	if err != nil {
		db.logger.Error("snapshot write failed, cleaning up temporary directory",
			"tmpDir", tmpDir,
			"error", err,
		)
		cleanupErr := os.RemoveAll(path)
		if cleanupErr != nil {
			db.logger.Error("failed to clean up temporary snapshot directory",
				"tmpDir", tmpDir,
				"cleanup_error", cleanupErr,
			)
		} else {
			db.logger.Debug("temporary snapshot directory cleaned up successfully",
				"tmpDir", tmpDir,
			)
		}
		return errorutils.Join(err, cleanupErr)
	}

	db.logger.Info("snapshot rewrite completed", "duration_sec", writeElapsed)

	// Rename temporary directory to final location
	if err := os.Rename(path, targetPath); err != nil {
		db.logger.Error("failed to rename snapshot directory, cleaning up",
			"tmpDir", tmpDir,
			"targetDir", snapshotDir,
			"error", err,
		)
		// Clean up temporary directory on rename failure
		if cleanupErr := os.RemoveAll(path); cleanupErr != nil {
			db.logger.Error("failed to clean up temporary snapshot directory after rename failure",
				"tmpDir", tmpDir,
				"cleanup_error", cleanupErr,
			)
			return errorutils.Join(err, cleanupErr)
		}
		db.logger.Info("temporary snapshot directory cleaned up after rename failure",
			"tmpDir", tmpDir,
		)
		return err
	}

	return updateCurrentSymlink(db.dir, snapshotDir)
}

func (db *DB) Reload() error {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	return db.reload()
}

func (db *DB) reload() error {
	mtree, err := LoadMultiTree(currentPath(db.dir), db.opts)
	if err != nil {
		return err
	}
	return db.reloadMultiTree(mtree)
}

func (db *DB) reloadMultiTree(mtree *MultiTree) error {
	// The caller is responsible for ensuring mtree is caught up to the latest state
	// (either via Catchup from WAL or by loading a current snapshot).
	return db.ReplaceWith(mtree)
}

// rewriteIfApplicable execute the snapshot rewrite strategy according to current height
func (db *DB) rewriteIfApplicable(height int64) {
	if db.snapshotRewriteChan != nil {
		return
	}

	if db.snapshotInterval <= 0 || height <= 0 || height < db.SnapshotVersion() {
		return
	}

	snapshotVersion := db.SnapshotVersion()
	blocksSinceLastSnapshot := height - snapshotVersion

	// Create snapshot when all conditions are met:
	// 1. Block height interval is reached (height - last snapshot >= interval)
	// 2. Minimum time interval has elapsed (prevents excessive snapshots during catch-up)
	// 3. Block height % snapshot interval == 0
	if blocksSinceLastSnapshot >= int64(db.snapshotInterval) && height%int64(db.snapshotInterval) == 0 {
		timeSinceLastSnapshot := time.Since(db.lastSnapshotTime)

		if timeSinceLastSnapshot < db.snapshotMinTimeInterval {
			db.logger.Debug("skipping snapshot (minimum time interval not reached)",
				"blocks_since_last", blocksSinceLastSnapshot,
				"time_since_last", timeSinceLastSnapshot,
				"min_time_interval", db.snapshotMinTimeInterval,
			)
			return
		}

		db.logger.Info("creating snapshot",
			"blocks_since_last", blocksSinceLastSnapshot,
			"time_since_last", timeSinceLastSnapshot,
		)

		if err := db.rewriteSnapshotBackground(); err != nil {
			db.logger.Error("failed to rewrite snapshot in background", "err", err)
		}
	}
}

type snapshotResult struct {
	mtree *MultiTree
	err   error
}

// RewriteSnapshotBackground rewrite snapshot in a background goroutine,
// `Commit` will check the complete status, and switch to the new snapshot.
func (db *DB) RewriteSnapshotBackground() error {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	if db.readOnly {
		return errReadOnly
	}

	return db.rewriteSnapshotBackground()
}

func (db *DB) rewriteSnapshotBackground() error {
	if db.snapshotRewriteChan != nil {
		return errors.New("there's another ongoing snapshot rewriting process")
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Use buffered channel to avoid blocking the goroutine when sending result
	ch := make(chan snapshotResult, 1)
	db.snapshotRewriteChan = ch
	db.snapshotRewriteCancelFunc = cancel

	// Update snapshot timestamp at start (not at completion) for accurate interval calculation
	db.lastSnapshotTime = time.Now()

	cloned := db.copy()
	go func() {
		defer close(ch)
		startTime := time.Now()
		cloned.logger.Info("start rewriting snapshot", "version", cloned.Version())

		rewriteStart := time.Now()
		if err := cloned.RewriteSnapshot(ctx); err != nil {
			cloned.logger.Error("failed to rewrite snapshot", "error", err, "elapsed", time.Since(rewriteStart).Seconds())
			ch <- snapshotResult{err: err}
			return
		}
		cloned.logger.Info("finished rewriting snapshot", "version", cloned.Version(), "elapsed", time.Since(rewriteStart).Seconds())

		loadStart := time.Now()

		// Disable prefetch when loading newly created snapshot in background.
		// Profiling shows: with prefetch = 35 min, without = 15 min (20 min difference!)
		// The snapshot was just written, so some data is still in page cache, but mincore()
		// checks mmap pages (not file cache) and reports low residency because mmap hasn't
		// been accessed yet. Prefetch causes unnecessary I/O that competes with ongoing
		// commits and evicts hot pages from the active snapshot still being used by main chain.
		// Use cloned.opts instead of db.opts to avoid race condition with Close()
		loadOpts := cloned.opts
		loadOpts.SnapshotPrefetchThreshold = 0

		mtree, err := LoadMultiTree(currentPath(cloned.dir), loadOpts)
		if err != nil {
			cloned.logger.Error("failed to load multitree after snapshot", "error", err)
			ch <- snapshotResult{err: err}
			return
		}

		// Snapshot mmap files are loaded with MADV_RANDOM in OpenSnapshot().

		cloned.logger.Info("loaded multitree after snapshot", "elapsed", time.Since(loadStart).Seconds())

		// do a best effort catch-up, will do another final catch-up in main thread.
		if wal := db.GetWAL(); wal != nil {
			catchupStart := time.Now()
			if err := mtree.Catchup(ctx, wal, db.walIndexDelta, 0); err != nil {
				cloned.logger.Error("failed to catchup after snapshot", "error", err)
				ch <- snapshotResult{err: err}
				return
			}
			cloned.logger.Info("finished best-effort catchup", "version", cloned.Version(), "latest", mtree.Version(), "elapsed", time.Since(catchupStart).Seconds())
		}

		ch <- snapshotResult{mtree: mtree}
		totalElapsed := time.Since(startTime).Seconds()
		cloned.logger.Info("snapshot rewrite process completed", "duration_sec", totalElapsed, "duration_min", totalElapsed/60)
		otelMetrics.SnapshotCreationLatency.Record(
			context.Background(),
			totalElapsed,
		)
	}()

	return nil
}

func (db *DB) Close() error {
	db.logger.Info("Closing memiavl db...")
	db.mtx.Lock()
	defer db.mtx.Unlock()
	if db.closed {
		return nil
	}
	db.closed = true
	// Wait for any ongoing prune to finish, then block new prunes
	db.pruneSnapshotLock.Lock()
	defer db.pruneSnapshotLock.Unlock()
	errs := []error{}

	errs := []error{}
	// Close rewrite channel first - must wait for background goroutine before closing WAL
	db.logger.Info("Closing rewrite channel...")
	if db.snapshotRewriteChan != nil {
		db.snapshotRewriteCancelFunc()
		// Wait for goroutine to finish and send result
		if result, ok := <-db.snapshotRewriteChan; ok {
			if result.err != nil {
				db.logger.Error("snapshot rewrite failed during close", "error", result.err)
			}
			// Close the returned mtree if it wasn't switched in, to avoid resource leaks.
			if result.mtree != nil {
				errs = append(errs, result.mtree.Close())
			}
		}
		db.snapshotRewriteChan = nil
		db.snapshotRewriteCancelFunc = nil
	}

	// Close WAL after snapshot rewrite goroutine has fully exited.
	if db.streamHandler != nil {
		errs = append(errs, db.streamHandler.Close())
		db.streamHandler = nil
	}

	errs = append(errs, db.MultiTree.Close())

	// Stop snapshot writer workers
	if db.snapshotWriterPool != nil {
		db.snapshotWriterPool.StopAndWait()
		db.snapshotWriterPool = nil
	}

	// Close file lock
	db.logger.Info("Closing file lock...")
	if db.fileLock != nil {
		errs = append(errs, db.fileLock.Unlock())
		errs = append(errs, db.fileLock.Destroy())
		db.fileLock = nil
	}
	db.logger.Info("Closed memiavl db.")
	return errorutils.Join(errs...)
}

// TreeByName wraps MultiTree.TreeByName to add a lock.
func (db *DB) TreeByName(name string) *Tree {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	return db.MultiTree.TreeByName(name)
}

// Version wraps MultiTree.Version to add a lock.
func (db *DB) Version() int64 {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	return db.MultiTree.Version()
}

// LastCommitInfo returns the last commit info.
func (db *DB) LastCommitInfo() *proto.CommitInfo {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	return db.MultiTree.LastCommitInfo()
}

func (db *DB) SaveVersion(updateCommitInfo bool) (int64, error) {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	if db.readOnly {
		return 0, errReadOnly
	}

	return db.MultiTree.SaveVersion(updateCommitInfo)
}

func (db *DB) WorkingCommitInfo() *proto.CommitInfo {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	return db.MultiTree.WorkingCommitInfo()
}

// UpdateCommitInfo wraps MultiTree.UpdateCommitInfo to add a lock.
func (db *DB) UpdateCommitInfo() {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	if db.readOnly {
		panic("can't update commit info in read-only mode")
	}

	db.MultiTree.UpdateCommitInfo()
}

// WriteSnapshot wraps MultiTree.WriteSnapshot to add a lock.
func (db *DB) WriteSnapshot(dir string) error {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	return db.WriteSnapshotWithRateLimit(context.Background(), dir, db.snapshotWriterPool, db.opts.SnapshotWriteRateMBps)
}

func snapshotName(version int64) string {
	return fmt.Sprintf("%s%020d", SnapshotPrefix, version)
}

func currentPath(root string) string {
	return filepath.Join(root, "current")
}

func currentTmpPath(root string) string {
	return filepath.Join(root, "current-tmp")
}

func currentVersion(root string) (int64, error) {
	name, err := os.Readlink(currentPath(root))
	if err != nil {
		return 0, err
	}

	version, err := parseVersion(name)
	if err != nil {
		return 0, err
	}

	return version, nil
}

func parseVersion(name string) (int64, error) {
	if !isSnapshotName(name) {
		return 0, fmt.Errorf("invalid snapshot name %s", name)
	}

	v, err := strconv.ParseUint(name[len(SnapshotPrefix):], 10, 32)
	if err != nil {
		return 0, fmt.Errorf("snapshot version overflows: %d", err)
	}

	return int64(v), nil
}

// seekSnapshot find the biggest snapshot version that's smaller than or equal to the target version,
// returns 0 if not found.
func seekSnapshot(root string, targetVersion int64) (int64, error) {
	var (
		snapshotVersion int64
		found           bool
	)
	if err := traverseSnapshots(root, false, func(version int64) (bool, error) {
		if version <= targetVersion {
			found = true
			snapshotVersion = version
			return true, nil
		}
		return false, nil
	}); err != nil {
		return 0, err
	}

	if !found {
		return 0, fmt.Errorf("target version is pruned: %d", targetVersion)
	}

	return snapshotVersion, nil
}

// GetEarliestVersion returns the earliest snapshot name in the db
func GetEarliestVersion(root string) (int64, error) {
	var found int64
	if err := traverseSnapshots(root, true, func(version int64) (bool, error) {
		found = version
		return true, nil
	}); err != nil {
		return 0, err
	}

	if found == 0 {
		return 0, errors.New("empty memiavl db")
	}

	return found, nil
}

// init a empty memiavl db
//
// ```
// snapshot-0
//
//	commit_info
//
// current -> snapshot-0
// ```
func initEmptyDB(dir string, initialVersion uint32) error {
	tmp := NewEmptyMultiTree(initialVersion)
	snapshotDir := snapshotName(0)
	// create tmp worker pool
	concurrency := runtime.NumCPU()
	pool := pond.New(concurrency, concurrency*10)
	defer pool.Stop()

	if err := tmp.WriteSnapshot(context.Background(), filepath.Join(dir, snapshotDir), pool); err != nil {
		return err
	}
	return updateCurrentSymlink(dir, snapshotDir)
}

// updateCurrentSymlink creates or replace the current symbolic link atomically.
// it could fail under concurrent usage for tmp file conflicts.
func updateCurrentSymlink(dir, snapshot string) error {
	tmpPath := currentTmpPath(dir)
	if err := os.Symlink(snapshot, tmpPath); err != nil {
		return err
	}
	// assuming file renaming operation is atomic
	return os.Rename(tmpPath, currentPath(dir))
}

// traverseSnapshots traverse the snapshot list in specified order.
func traverseSnapshots(dir string, ascending bool, callback func(int64) (bool, error)) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	process := func(entry os.DirEntry) (bool, error) {
		if !entry.IsDir() || !isSnapshotName(entry.Name()) {
			return false, nil
		}

		version, err := parseVersion(entry.Name())
		if err != nil {
			return true, fmt.Errorf("invalid snapshot name: %w", err)
		}

		return callback(version)
	}

	if ascending {
		for i := 0; i < len(entries); i++ {
			stop, err := process(entries[i])
			if stop || err != nil {
				return err
			}
		}
	} else {
		for i := len(entries) - 1; i >= 0; i-- {
			stop, err := process(entries[i])
			if stop || err != nil {
				return err
			}
		}
	}

	return nil
}

// atomicRemoveDir is equavalent to `mv snapshot snapshot-tmp && rm -r snapshot-tmp`
func atomicRemoveDir(path string) error {
	tmpPath := path + "-tmp"
	if err := os.Rename(path, tmpPath); err != nil {
		return err
	}

	return os.RemoveAll(tmpPath)
}

// createDBIfNotExist detects if db does not exist and try to initialize an empty one.
func createDBIfNotExist(dir string, initialVersion uint32) error {
	_, err := os.Stat(filepath.Join(dir, "current", MetadataFileName))
	if err != nil && os.IsNotExist(err) {
		return initEmptyDB(dir, initialVersion)
	}
	return nil
}

func isSnapshotName(name string) bool {
	return strings.HasPrefix(name, SnapshotPrefix) && len(name) == SnapshotDirLen
}

// GetLatestVersion finds the latest version number without loading the whole db,
// it's needed for upgrade module to check store upgrades,
// it returns 0 if db doesn't exist or is empty.
func GetLatestVersion(dir string) (int64, error) {
	metadata, err := readMetadata(currentPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	lastIndex, err := wal.GetLastIndex(wal.LogPath(dir))
	if err != nil {
		return 0, err
	}
	if metadata.InitialVersion < 0 || metadata.InitialVersion > math.MaxUint32 {
		return 0, fmt.Errorf("invalid initial version: %d", metadata.InitialVersion)
	}
	return utils.IndexToVersion(lastIndex, uint32(metadata.InitialVersion)), nil
}
