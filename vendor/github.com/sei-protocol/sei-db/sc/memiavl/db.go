package memiavl

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/alitto/pond"
	"github.com/cosmos/iavl"
	errorutils "github.com/sei-protocol/sei-db/common/errors"
	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/common/utils"
	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/stream/changelog"
	"github.com/sei-protocol/sei-db/stream/types"
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

	// result channel of snapshot rewrite goroutine
	snapshotRewriteChan chan snapshotResult
	// context cancel function to cancel the snapshot rewrite goroutine
	snapshotRewriteCancelFunc context.CancelFunc
	// the number of old snapshots to keep (excluding the latest one)
	snapshotKeepRecent uint32
	// block interval to take a new snapshot
	snapshotInterval uint32
	// make sure only one snapshot rewrite is running
	pruneSnapshotLock sync.Mutex

	// the changelog stream persists all the changesets
	streamHandler types.Stream[proto.ChangelogEntry]

	// pending change, will be written into rlog file in next Commit call
	pendingLogEntry proto.ChangelogEntry

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

func OpenDB(logger logger.Logger, targetVersion int64, opts Options) (*DB, error) {
	var (
		err      error
		fileLock FileLock
	)

	if err := opts.Validate(); err != nil {
		return nil, fmt.Errorf("invalid commit store options: %w", err)
	}
	opts.FillDefaults()

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
	mtree, err := LoadMultiTree(path, opts.ZeroCopy, opts.CacheSize)
	if err != nil {
		return nil, err
	}

	// Create rlog manager and open the rlog file
	streamHandler, err := changelog.NewStream(logger, utils.GetChangelogPath(opts.Dir), changelog.Config{
		DisableFsync:    true,
		ZeroCopy:        true,
		WriteBufferSize: opts.AsyncCommitBuffer,
	})
	if err != nil {
		return nil, err
	}

	if targetVersion == 0 || targetVersion > mtree.Version() {
		logger.Info("Start catching up and replaying the MemIAVL changelog file")
		if err := mtree.Catchup(streamHandler, targetVersion); err != nil {
			return nil, errorutils.Join(err, streamHandler.Close())
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

		// truncate the rlog file
		logger.Info("truncate rlog after version: %d", targetVersion)
		truncateIndex := utils.VersionToIndex(targetVersion, mtree.initialVersion)
		if err := streamHandler.TruncateAfter(truncateIndex); err != nil {
			return nil, fmt.Errorf("fail to truncate rlog file: %w", err)
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

	db := &DB{
		MultiTree:          *mtree,
		logger:             logger,
		dir:                opts.Dir,
		fileLock:           fileLock,
		readOnly:           opts.ReadOnly,
		streamHandler:      streamHandler,
		snapshotKeepRecent: opts.SnapshotKeepRecent,
		snapshotInterval:   opts.SnapshotInterval,
		snapshotWriterPool: workerPool,
	}

	if !db.readOnly && db.Version() == 0 && len(opts.InitialStores) > 0 {
		// do the initial upgrade with the `opts.InitialStores`
		var upgrades []*proto.TreeNameUpgrade
		for _, name := range opts.InitialStores {
			upgrades = append(upgrades, &proto.TreeNameUpgrade{Name: name})
		}
		if err := db.ApplyUpgrades(upgrades); err != nil {
			return nil, errorutils.Join(err, db.Close())
		}
	}
	if db.streamHandler == nil {
		fmt.Println("[Debug] DB steam handler is nil??")
	}
	return db, nil
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

	return initEmptyDB(db.dir, db.initialVersion)
}

// ApplyUpgrades wraps MultiTree.ApplyUpgrades, it also appends the upgrades in a pending log,
// which will be persisted to the rlog in next Commit call.
func (db *DB) ApplyUpgrades(upgrades []*proto.TreeNameUpgrade) error {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	if db.readOnly {
		return errReadOnly
	}

	if err := db.MultiTree.ApplyUpgrades(upgrades); err != nil {
		return err
	}

	db.pendingLogEntry.Upgrades = append(db.pendingLogEntry.Upgrades, upgrades...)
	return nil
}

// ApplyChangeSets wraps MultiTree.ApplyChangeSets, it also appends the changesets in the pending log,
// which will be persisted to the rlog in next Commit call.
func (db *DB) ApplyChangeSets(changeSets []*proto.NamedChangeSet) error {
	if len(changeSets) == 0 {
		return nil
	}

	db.mtx.Lock()
	defer db.mtx.Unlock()

	if db.readOnly {
		return errReadOnly
	}

	if len(db.pendingLogEntry.Changesets) > 0 {
		return errors.New("don't support multiple ApplyChangeSets calls in the same version")
	}
	db.pendingLogEntry.Changesets = changeSets

	return db.MultiTree.ApplyChangeSets(changeSets)
}

// ApplyChangeSet wraps MultiTree.ApplyChangeSet, it also appends the changesets in the pending log,
// which will be persisted to the rlog in next Commit call.
func (db *DB) ApplyChangeSet(name string, changeSet iavl.ChangeSet) error {
	if len(changeSet.Pairs) == 0 {
		return nil
	}

	db.mtx.Lock()
	defer db.mtx.Unlock()

	if db.readOnly {
		return errReadOnly
	}

	for _, cs := range db.pendingLogEntry.Changesets {
		if cs.Name == name {
			return errors.New("don't support multiple ApplyChangeSet calls with the same name in the same version")
		}
	}

	db.pendingLogEntry.Changesets = append(db.pendingLogEntry.Changesets, &proto.NamedChangeSet{
		Name:      name,
		Changeset: changeSet,
	})
	sort.SliceStable(db.pendingLogEntry.Changesets, func(i, j int) bool {
		return db.pendingLogEntry.Changesets[i].Name < db.pendingLogEntry.Changesets[j].Name
	})

	return db.MultiTree.ApplyChangeSet(name, changeSet)
}

// checkAsyncTasks checks the status of background tasks non-blocking-ly and process the result
func (db *DB) checkAsyncTasks() error {
	return errorutils.Join(
		db.streamHandler.CheckError(),
		db.checkBackgroundSnapshotRewrite(),
	)
}

// CommittedVersion returns the latest version written in rlog file, or snapshot version if rlog is empty.
func (db *DB) CommittedVersion() (int64, error) {
	lastIndex, err := db.streamHandler.LastOffset()
	if err != nil {
		return 0, err
	}
	if lastIndex == 0 {
		return db.SnapshotVersion(), nil
	}
	return utils.IndexToVersion(lastIndex, db.initialVersion), nil
}

// checkBackgroundSnapshotRewrite check the result of background snapshot rewrite, cleans up the old snapshots and switches to a new multitree
func (db *DB) checkBackgroundSnapshotRewrite() error {
	// check the completeness of background snapshot rewriting
	select {
	case result := <-db.snapshotRewriteChan:
		db.snapshotRewriteChan = nil
		db.snapshotRewriteCancelFunc = nil

		if result.mtree == nil {
			// background snapshot rewrite failed
			return fmt.Errorf("background snapshot rewriting failed: %w", result.err)
		}

		// wait for potential pending rlog writings to finish, to make sure we catch up to latest state.
		// in real world, block execution should be slower than rlog writing, so this should not block for long.
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
		if err := result.mtree.Catchup(db.streamHandler, 0); err != nil {
			return fmt.Errorf("catchup failed: %w", err)
		}

		// do the switch
		if err := db.reloadMultiTree(result.mtree); err != nil {
			return fmt.Errorf("switch multitree failed: %w", err)
		}
		db.logger.Info("switched to new memiavl snapshot", "version", db.MultiTree.Version())

		db.pruneSnapshots()
	default:
	}

	return nil
}

// pruneSnapshot prune the old snapshots
func (db *DB) pruneSnapshots() {
	// wait until last prune finish
	db.pruneSnapshotLock.Lock()

	go func() {
		defer db.pruneSnapshotLock.Unlock()

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

		// truncate Rlog until the earliest remaining snapshot
		earliestVersion, err := GetEarliestVersion(db.dir)
		if err != nil {
			db.logger.Error("failed to find first snapshot", "err", err)
		}

		if err := db.streamHandler.TruncateBefore(utils.VersionToIndex(earliestVersion+1, db.initialVersion)); err != nil {
			db.logger.Error("failed to truncate rlog", "err", err, "version", earliestVersion+1)
		}
	}()
}

// Commit wraps SaveVersion to bump the version and writes the pending changes into log files to persist on disk
func (db *DB) Commit() (int64, error) {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	if db.readOnly {
		return 0, errReadOnly
	}

	v, err := db.MultiTree.SaveVersion(true)
	if err != nil {
		return 0, err
	}

	// write to changelog
	if db.streamHandler != nil {
		db.pendingLogEntry.Version = v
		err := db.streamHandler.Write(utils.VersionToIndex(v, db.initialVersion), db.pendingLogEntry)
		if err != nil {
			return 0, err
		}
	}

	db.pendingLogEntry = proto.ChangelogEntry{}

	if err := db.checkAsyncTasks(); err != nil {
		return 0, err
	}

	// Rewrite tree snapshot if applicable
	db.rewriteIfApplicable(v)

	return v, nil
}

func (db *DB) Copy() *DB {
	db.mtx.Lock()
	defer db.mtx.Unlock()

	return db.copy(db.cacheSize)
}

func (db *DB) copy(cacheSize int) *DB {
	mtree := db.MultiTree.Copy(cacheSize)

	return &DB{
		MultiTree:          *mtree,
		logger:             db.logger,
		dir:                db.dir,
		snapshotWriterPool: db.snapshotWriterPool,
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
	tmpDir := snapshotDir + "-tmp"
	path := filepath.Join(db.dir, tmpDir)
	if err := db.MultiTree.WriteSnapshot(ctx, path, db.snapshotWriterPool); err != nil {
		return errorutils.Join(err, os.RemoveAll(path))
	}
	if err := os.Rename(path, filepath.Join(db.dir, snapshotDir)); err != nil {
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
	mtree, err := LoadMultiTree(currentPath(db.dir), db.zeroCopy, db.cacheSize)
	if err != nil {
		return err
	}
	return db.reloadMultiTree(mtree)
}

func (db *DB) reloadMultiTree(mtree *MultiTree) error {
	// catch-up the pending changes
	if err := mtree.apply(db.pendingLogEntry); err != nil {
		return err
	}
	return db.MultiTree.ReplaceWith(mtree)
}

// rewriteIfApplicable execute the snapshot rewrite strategy according to current height
func (db *DB) rewriteIfApplicable(height int64) {
	if height%int64(db.snapshotInterval) != 0 {
		return
	}

	if err := db.rewriteSnapshotBackground(); err != nil {
		db.logger.Error("failed to rewrite snapshot in background", "err", err)
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
	ch := make(chan snapshotResult)
	db.snapshotRewriteChan = ch
	db.snapshotRewriteCancelFunc = cancel

	cloned := db.copy(0)
	go func() {
		defer close(ch)

		cloned.logger.Info("start rewriting snapshot", "version", cloned.Version())
		if err := cloned.RewriteSnapshot(ctx); err != nil {
			ch <- snapshotResult{err: err}
			return
		}
		cloned.logger.Info("finished rewriting snapshot", "version", cloned.Version())
		mtree, err := LoadMultiTree(currentPath(cloned.dir), cloned.zeroCopy, 0)
		if err != nil {
			ch <- snapshotResult{err: err}
			return
		}

		// do a best effort catch-up, will do another final catch-up in main thread.
		if err := mtree.Catchup(db.streamHandler, 0); err != nil {
			ch <- snapshotResult{err: err}
			return
		}

		cloned.logger.Info("finished best-effort catchup", "version", cloned.Version(), "latest", mtree.Version())

		ch <- snapshotResult{mtree: mtree}
	}()

	return nil
}

func (db *DB) Close() error {
	db.mtx.Lock()
	defer db.mtx.Unlock()
	errs := []error{}

	// Close stream handler
	if db.streamHandler != nil {
		err := db.streamHandler.Close()
		errs = append(errs, err)
		db.streamHandler = nil
	}

	// Close rewrite channel
	if db.snapshotRewriteChan != nil {
		db.snapshotRewriteCancelFunc()
		<-db.snapshotRewriteChan
		db.snapshotRewriteChan = nil
		db.snapshotRewriteCancelFunc = nil
	}

	errs = append(errs, db.MultiTree.Close())

	// Close file lock
	if db.fileLock != nil {
		errs = append(errs, db.fileLock.Unlock())
		errs = append(errs, db.fileLock.Destroy())
		db.fileLock = nil
	}

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

	return db.MultiTree.WriteSnapshot(context.Background(), dir, db.snapshotWriterPool)
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
	tmp := NewEmptyMultiTree(initialVersion, 0)
	snapshotDir := snapshotName(0)
	// create tmp worker pool
	pool := pond.New(config.DefaultSnapshotWriterLimit, config.DefaultSnapshotWriterLimit*10)
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
	lastIndex, err := changelog.GetLastIndex(changelog.LogPath(dir))
	if err != nil {
		return 0, err
	}
	return utils.IndexToVersion(lastIndex, uint32(metadata.InitialVersion)), nil
}
