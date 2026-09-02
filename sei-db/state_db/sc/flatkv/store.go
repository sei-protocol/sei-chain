package flatkv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/zbiljic/go-filelock"
	"go.opentelemetry.io/otel/attribute"

	commonerrors "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/sview"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "state-db", "sc", "flatkv")

var _ giga.LiveStateStore = (*CommitStore)(nil)

// CommitStore implements giga.LiveStateStore for EVM state.
//
// Reads, writes and iterator construction are safe to call concurrently. Lifecycle operations
// (LoadLatest, Rollback, snapshot, import, export, Close) must be serialized by the caller.
//
// An iterator is a fixed view of the instant it was created and may be held across later commits; it
// will not observe them. It must still be closed — it pins resources in the underlying databases, and
// reading one after Close is undefined behaviour, which Close reports on a best-effort basis.
type CommitStore struct {
	// mu serializes the exported entry points against one another: the write path (ApplyChangeSets,
	// Commit), the reads (Get, Has, GetBlockHeightModified) and iterator construction (Iterator,
	// RawGlobalIterator). It does not protect the block being written — that lives inside the stores,
	// which do their own locking.
	//
	// TODO(concurrency): this is a coarse lock taken at the exported entry
	// points. Commit in particular holds the write lock across its WAL fsync
	// and periodic auto-snapshot. That is acceptable while commits are not
	// pipelined with reads; revisit with a finer-grained scheme if/when
	// pipelining is introduced.
	mu sync.RWMutex

	// Store-private context, cancelled by cancel when the store closes. Metric recording and the
	// opening of the pebble instances hang off it, so work the store started stops when it does.
	ctx context.Context

	// Cancels ctx. Called by Close.
	cancel context.CancelFunc

	// The configuration this store was opened with. Not modified after open.
	config config.Config

	// The directory holding this store's databases and its snapshot tree.
	dbDir string

	// The metadata each database most recently persisted, keyed by database directory name.
	//
	// A LocalMeta records a database's committed height, its LtHash, and its per-module hashes and
	// stats. Sealing a block hands each store its own LocalMeta as the block's finalization writes, so
	// the metadata lands in the same atomic batch as the data it describes and a database on disk can
	// never disagree with its own bookkeeping. This map is the in-memory copy of what was written, and
	// is adopted only once every store has accepted the seal.
	localMeta map[string]*LocalMeta

	// The height of the most recently committed block. The next Commit must be exactly this plus one.
	committedVersion int64

	// The hash state read off disk at load, which the hash engine is seeded from and which every
	// hash query answers from until the first block has been finalized. Rebuilt by every path that
	// reopens the databases underneath the engine.
	loadedHashes *lthash.BlockHash

	// hashEngine folds each sealed block into the running lattice hash, off the execution goroutine.
	// Built by openStores once the stores exist and torn down by closeStores. Nil on a read-only store,
	// which never commits — such a store answers hash queries from what it loaded.
	hashEngine *lthash.HashEngine

	// finalizer records each block's hashes onto that block's own views, and is the sole consumer of
	// the engine's stream. Same lifecycle as hashEngine.
	finalizer *FinalizationManager

	// hashLogger receives each block's hashes as it is finalized. Held here because restartHashing
	// rebuilds the finalizer, which is what reports to it. Never nil.
	hashLogger hashlog.HashLogger

	// The four data stores below mediate every read and write of their databases. The block being
	// applied accumulates its writes inside each store, so a read through a store already sees what
	// that same block staged, with no separate overlay to consult.
	//
	// They are constructed as the last step of open, after any replay or rollback has run, and are nil
	// until then — the bootstrap and import paths deliberately write raw pebble before they exist.

	// Mediates the account database.
	accountStore view.ViewManager

	// Mediates the code database.
	codeStore view.ViewManager

	// Mediates the storage database.
	storageStore view.ViewManager

	// Mediates the misc database.
	miscStore view.ViewManager

	// All four stores, for the paths that treat them uniformly.
	stores []view.ViewManager

	// The views of the most recently committed block, one reservation held for as long as they stay
	// installed, which is what keeps any later block out of pebble. Nil outside the window in which the
	// view managers exist.
	lastSealed *sview.AtomicStoreView

	// The state WAL. Injected at construction: non-nil ⇒ FlatKV writes/replays/prunes it; nil ⇒ the outer
	// context owns the whole WAL pipeline and FlatKV no-ops every WAL operation. FlatKV owns Close of whatever
	// instance it currently holds (rollback/restore may replace it via close→prune/delete→reopen); its
	// lifecycle is decoupled from the DB open/close cycle, so closeDBsOnly does not touch it.
	wal statewal.StateWAL

	// Changes to feed into the WAL at the next commit.
	pendingChangeSets []*proto.NamedChangeSet

	// pendingBlockHeight is the version stamped by the current buffered ApplyChangeSets. 0 means no
	// pending apply. It records the staged height; it does not constrain it — neither ApplyChangeSets
	// nor Commit validates its version against this field.
	pendingBlockHeight int64

	// Writes snapshots off the execution thread. Built by openStores once the view managers exist and
	// torn down by closeStores, so its lifetime is exactly the window in which the databases it
	// checkpoints are open. Nil on a read-only store, which never commits.
	snapshotWriter *SnapshotWriter

	// File lock prevents multiple processes from opening the same DB.
	fileLock filelock.TryLockerSafe

	phaseTimer *metrics.PhaseTimer

	// readOnly marks stores opened via LoadVersionReadOnly.
	readOnly bool

	readOnlyWorkDir string // Temp working dir for readonly store; removed by Close.

	// A work pool for reading from the DBs.
	//
	// Uses a fixed-size pool.
	readPool threading.Pool

	// A work pool for miscellaneous operations that are neither computationally intensive nor IO bound.
	//
	// Uses an elasticly-sized pool, so it is safe to submit tasks that have dependencies on other tasks in the pool.
	miscPool threading.Pool

	// A work pool for lattice-hash computation (CPU-bound).
	//
	// Uses a fixed-size pool, same lifecycle as readPool / miscPool.
	ltHashPool threading.Pool

	// moduleOf names the module a physical key belongs to, for bucketing a block's pairs into per-module
	// hashes. A field rather than a direct call to moduleOfKey, and read on every call rather than
	// captured, so that a test can inject a failing one into an open store.
	moduleOf lthash.ModuleParser
}

// routePhysicalKey names the database directory a physical DB key belongs to.
// Non-EVM modules are routed to miscDB; EVM keys are routed by kind.
//
// It answers with a directory name rather than a database handle so that deciding where a key goes stays
// separate from holding the thing it goes into.
func routePhysicalKey(physicalKey []byte) (string, error) {
	moduleName, innerKey, err := ktype.StripModulePrefix(physicalKey)
	if err != nil {
		return "", err
	}
	if moduleName == "" {
		// An empty module name would fold into moduleLtHash[""]/moduleStats[""]
		// and later persist as the per-module meta key "_meta/x:/hash", which
		// ParseModuleLtHashKey rejects on the very next reopen — permanently
		// bricking the store (root != sum(modules) forever). Reject it here,
		// at the state-sync import dispatch boundary, mirroring the
		// classifyAndPrefix guard on the live-commit path (store_apply.go).
		return "", fmt.Errorf("flatkv: empty module name in physical key %q", physicalKey)
	}
	if moduleName != keys.EVMStoreKey {
		return miscDBDir, nil
	}
	kind, _ := keys.ParseEVMKey(innerKey)
	switch kind {
	case ktype.EVMKeyAccount, keys.EVMKeyCodeHash:
		return accountDBDir, nil
	case keys.EVMKeyCode:
		return codeDBDir, nil
	case keys.EVMKeyStorage:
		return storageDBDir, nil
	default:
		return miscDBDir, nil
	}
}

// NewCommitStore creates a new (unopened) FlatKV commit store.
// Call LoadLatest to open and initialize.
//
// A non-nil stateWAL is owned by the store: it writes to it and closes, deletes, prunes and reopens it as
// rollback and restore require, recreating it under cfg.DataDir — so pass the instance OpenStateWAL(cfg)
// returns for this same cfg. Pass nil to leave the WAL outside the store, which then performs no WAL
// operations at all. There is no middle setting: the WAL is either owned here or entirely external.
func NewCommitStore(
	ctx context.Context,
	cfg *config.Config,
	stateWAL statewal.StateWAL,
	// Receives each block's hashes as it is finalized. Nil records nothing.
	hl hashlog.HashLogger,
) (*CommitStore, error) {

	if hl == nil {
		hl = hashlog.NewNoOpHashLogger()
	}
	cfg = resolveConfig(cfg)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("failed to validate config: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)

	coreCount := runtime.NumCPU()

	readPoolSize := int(cfg.ReaderThreadsPerCore*float64(coreCount) + float64(cfg.ReaderConstantThreadCount))
	readPool := threading.NewFixedPool("flatkv-read", readPoolSize, cfg.ReaderPoolQueueSize)

	miscPoolSize := int(cfg.MiscPoolThreadsPerCore*float64(coreCount) + float64(cfg.MiscConstantThreadCount))
	miscPool := threading.NewElasticPool("flatkv-misc", miscPoolSize)

	ltHashPoolSize := lthashWorkerCount(cfg, coreCount)
	ltHashPool := threading.NewFixedPool("flatkv-lthash", ltHashPoolSize, ltHashPoolSize)

	return &CommitStore{
		ctx:               ctx,
		cancel:            cancel,
		hashLogger:        hl,
		config:            *cfg,
		localMeta:         make(map[string]*LocalMeta),
		pendingChangeSets: make([]*proto.NamedChangeSet, 0),
		loadedHashes:      lthash.NewBlockHash(dataDBDirs),
		phaseTimer:        metrics.NewPhaseTimer(flatkvMeter, "seidb_main_thread"),
		readPool:          readPool,
		miscPool:          miscPool,
		ltHashPool:        ltHashPool,
		moduleOf:          moduleOfKey,
		wal:               stateWAL,
	}, nil
}

// resolveConfig returns cfg with every value a store derives for itself filled in: each database's
// data directory, taken from DataDir where the caller left it empty, and the store-wide pebble
// metrics and fsync settings fanned out to the per-database configs. The databases live under the
// working directory, at <DataDir>/working/<subdir>. cfg itself is left untouched.
func resolveConfig(cfg *config.Config) *config.Config {
	resolved := cfg.Copy()

	workDir := filepath.Join(resolved.DataDir, workingDirName)
	if resolved.AccountDBConfig.DataDir == "" {
		resolved.AccountDBConfig.DataDir = filepath.Join(workDir, accountDBDir)
	}
	if resolved.CodeDBConfig.DataDir == "" {
		resolved.CodeDBConfig.DataDir = filepath.Join(workDir, codeDBDir)
	}
	if resolved.StorageDBConfig.DataDir == "" {
		resolved.StorageDBConfig.DataDir = filepath.Join(workDir, storageDBDir)
	}
	if resolved.MiscDBConfig.DataDir == "" {
		resolved.MiscDBConfig.DataDir = filepath.Join(workDir, miscDBDir)
	}

	applyPebbleMetricsConfig(resolved)
	applyFlushSyncConfig(resolved)
	return resolved
}

func applyPebbleMetricsConfig(c *config.Config) {
	// Keep a single FlatKV-level knob for Pebble internal metrics. Per-DB
	// EnableMetrics values are intentionally overwritten here.
	c.AccountDBConfig.EnableMetrics = c.EnablePebbleMetrics
	c.CodeDBConfig.EnableMetrics = c.EnablePebbleMetrics
	c.StorageDBConfig.EnableMetrics = c.EnablePebbleMetrics
	c.MiscDBConfig.EnableMetrics = c.EnablePebbleMetrics

	c.AccountDBConfig.EnableReadWriteMetrics = c.EnableReadWriteMetrics
	c.CodeDBConfig.EnableReadWriteMetrics = c.EnableReadWriteMetrics
	c.StorageDBConfig.EnableReadWriteMetrics = c.EnableReadWriteMetrics
	c.MiscDBConfig.EnableReadWriteMetrics = c.EnableReadWriteMetrics
}

func applyFlushSyncConfig(c *config.Config) {
	c.AccountStoreConfig.FlushSync = c.Fsync
	c.CodeStoreConfig.FlushSync = c.Fsync
	c.StorageStoreConfig.FlushSync = c.Fsync
	c.MiscStoreConfig.FlushSync = c.Fsync
}

// lthashWorkerCount computes the fixed lattice-hash pool worker count from
// config, clamped to at least 1 (LtHash computation always needs a worker).
func lthashWorkerCount(cfg *config.Config, coreCount int) int {
	n := int(cfg.LtHashThreadsPerCore * float64(coreCount))
	if n < 1 {
		n = 1
	}
	return n
}

// resetPools recreates the context and thread pools after a full Close().
func (s *CommitStore) resetPools() {
	coreCount := runtime.NumCPU()

	s.ctx, s.cancel = context.WithCancel(context.Background())

	readPoolSize := int(s.config.ReaderThreadsPerCore*float64(coreCount) + float64(s.config.ReaderConstantThreadCount))
	s.readPool = threading.NewFixedPool("flatkv-read", readPoolSize, s.config.ReaderPoolQueueSize)

	miscPoolSize := int(s.config.MiscPoolThreadsPerCore*float64(coreCount) + float64(s.config.MiscConstantThreadCount))
	s.miscPool = threading.NewElasticPool("flatkv-misc", miscPoolSize)

	ltHashPoolSize := lthashWorkerCount(&s.config, coreCount)
	s.ltHashPool = threading.NewFixedPool("flatkv-lthash", ltHashPoolSize, ltHashPoolSize)
}

func (s *CommitStore) flatkvDir() string {
	return s.config.DataDir
}

// LoadLatest opens the database at the latest persisted version, leaving this store open for writing.
// It is the only way to obtain a store that can commit.
func (s *CommitStore) LoadLatest() (retErr error) {
	logger.Info("FlatKV LoadLatest")
	obs := s.observeOp("LoadLatest", otelMetrics.OpenLatency).
		withAttrs(attribute.Bool("read_only", false))
	defer obs.done(&retErr, func() {
		otelMetrics.CurrentVersion.Record(s.ctx, s.committedVersion)
		logger.Info("FlatKV LoadLatest complete", "version", s.committedVersion, "elapsed", obs.elapsed())
	})

	if s.readOnly {
		return errReadOnly
	}

	_ = s.closeDBsOnly()

	// Track whether we acquire the lock in this call so we can release it
	// on any error path (open() won't track a pre-held lock).
	lockHeldBefore := s.fileLock != nil
	defer func() {
		if retErr != nil && !lockHeldBefore && s.fileLock != nil {
			_ = s.fileLock.Unlock()
			s.fileLock = nil
		}
	}()

	if err := s.openTo(0); err != nil {
		return fmt.Errorf("failed to open FlatKV store: %w", err)
	}
	return nil
}

// LoadVersionReadOnly returns an isolated read-only view of the database at targetVersion (0 = latest).
// This store is left untouched and keeps committing; the caller owns the view and must Close it.
//
// If the writer lock has not yet been acquired (e.g. the store was freshly constructed),
// CleanupOrphanedReadOnlyDirs is called lazily to acquire it and clean up any leftover directories. When the
// lock is acquired lazily, ownership is transferred to the returned view so that closing the view releases
// it; this prevents leaking the lock when the caller never explicitly closes this store.
func (s *CommitStore) LoadVersionReadOnly(targetVersion int64) (opened giga.LiveStateStore, retErr error) {
	logger.Info("FlatKV LoadVersionReadOnly", "targetVersion", targetVersion)
	obs := s.observeOp("LoadVersionReadOnly", otelMetrics.OpenLatency, "targetVersion", targetVersion).
		withAttrs(attribute.Bool("read_only", true))
	defer obs.done(&retErr, func() {
		var reached int64
		if opened != nil {
			reached = opened.Version()
		}
		logger.Info("FlatKV LoadVersionReadOnly complete",
			"targetVersion", targetVersion,
			"version", reached,
			"elapsed", obs.elapsed())
	})

	if s.readOnly {
		return nil, errReadOnly
	}

	lazyLock := s.fileLock == nil
	if lazyLock {
		if err := s.CleanupOrphanedReadOnlyDirs(); err != nil {
			return nil, fmt.Errorf("readonly pre-init cleanup: %w", err)
		}
	}

	// The view gets an independent context, not one derived from s.ctx: callers close this store while
	// still reading from the view, and a derived context would cancel those reads.
	// No logger: a read-only store replays blocks to reach its target height, and reporting them would
	// duplicate rows the committing store already logged.
	ro, err := NewCommitStore(context.Background(), &s.config, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create readonly store: %w", err)
	}

	defer func() {
		if retErr != nil {
			if closeErr := ro.Close(); closeErr != nil {
				logger.Error("failed to close readonly store during error cleanup", "err", closeErr)
			}
		}
	}()

	workDir, err := os.MkdirTemp(ro.flatkvDir(), readOnlyDirPrefix)
	if err != nil {
		return nil, fmt.Errorf("create readonly temp dir: %w", err)
	}
	ro.readOnlyWorkDir = workDir

	ro.config.AccountDBConfig.DataDir = filepath.Join(workDir, accountDBDir)
	ro.config.CodeDBConfig.DataDir = filepath.Join(workDir, codeDBDir)
	ro.config.StorageDBConfig.DataDir = filepath.Join(workDir, storageDBDir)
	ro.config.MiscDBConfig.DataDir = filepath.Join(workDir, miscDBDir)

	// View manager metrics are labelled by manager name, which the read-only clone shares with this store,
	// so leaving them enabled would publish two conflicting values for every series.
	ro.config.AccountStoreConfig.MetricsEnabled = false
	ro.config.CodeStoreConfig.MetricsEnabled = false
	ro.config.StorageStoreConfig.MetricsEnabled = false
	ro.config.MiscStoreConfig.MetricsEnabled = false

	// Transfer the lazily-acquired lock to the view so that ro.Close()
	// releases it, preventing a leak when this store is never closed.
	if lazyLock && s.fileLock != nil {
		ro.fileLock = s.fileLock
		s.fileLock = nil
	}

	// Read-only from the moment it exists, so no window exists in which the view would accept a caller's
	// write. Replay reaches the apply path below ApplyChangeSets, which is where the refusal lives, so
	// marking it here does not block the catch-up that follows.
	ro.readOnly = true

	// The clone shares this store's flatkv root, so the writer that owns that root — and prunes it —
	// is this store's, not the clone's. The clone never has one of its own.
	if err := ro.openReadOnly(targetVersion, s.currentSnapshotWriter()); err != nil {
		return nil, fmt.Errorf("readonly open: %w", err)
	}

	// The clone is open at a snapshot boundary with a nil WAL. Replay this (primary) store's WAL into it up
	// to targetVersion so it reflects the exact requested height.
	if err := s.replayIntoReadOnlyCopy(ro, targetVersion); err != nil {
		return nil, err
	}

	if targetVersion > 0 && ro.committedVersion != targetVersion {
		return nil, fmt.Errorf("readonly version mismatch: requested %d, reached %d",
			targetVersion, ro.committedVersion)
	}

	logger.Info("FlatKV readonly store opened", "version", ro.committedVersion, "dir", ro.readOnlyWorkDir)
	return ro, nil
}

// openReadOnly opens PebbleDBs in readOnlyWorkDir at the snapshot boundary at or below targetVersion,
// leaving committedVersion at that snapshot version. It never modifies the global "current" symlink.
//
// owner is the snapshot writer of the store that owns the snapshot tree being read, which is the
// primary rather than this clone. Nil means no writer is running against that tree.
//
// This clone has a nil WAL of its own, so it does NOT replay: advancing from the snapshot boundary up to
// targetVersion — and marking the store read-only — is driven by the primary via LoadVersionReadOnly /
// replayIntoReadOnlyCopy, which feeds the primary's WAL into this clone.
func (s *CommitStore) openReadOnly(targetVersion int64, owner *SnapshotWriter) (retErr error) {
	s.clearPendingBlock()

	if err := s.cloneSnapshotToWorkDir(targetVersion, owner); err != nil {
		return err
	}

	dbs, err := s.openRawDBs()
	if err != nil {
		return err
	}
	defer func() {
		if retErr != nil {
			_ = dbs.close()
		}
	}()

	if err := s.loadLocalMeta(dbs); err != nil {
		return err
	}

	if err := s.loadGlobalMetadata(); err != nil {
		return err
	}

	// A read-only clone still needs stores: it replays the primary's WAL to reach its target version
	// and reuses the one apply path to do it.
	if err := s.openStores(dbs); err != nil {
		return err
	}

	logger.Info("FlatKV readonly base opened", "version", s.committedVersion,
		"dir", s.readOnlyWorkDir)
	return nil
}

// cloneSnapshotToWorkDir materializes the snapshot this read-only clone opens against into its
// working directory.
//
// It goes through owner rather than copying here, because the copy has to be serialized against the
// pruning that owner also performs: a snapshot resolved on this goroutine can be deleted before the
// copy has finished reading it. A nil owner means no writer is running against that tree, so there
// is nothing to serialize against and the copy is done inline.
func (s *CommitStore) cloneSnapshotToWorkDir(targetVersion int64, owner *SnapshotWriter) error {
	if owner != nil {
		if err := owner.CloneSnapshot(targetVersion, s.readOnlyWorkDir); err != nil {
			return fmt.Errorf("create readonly working dir: %w", err)
		}
		return nil
	}

	snapDir, err := resolveSnapshotToClone(s.flatkvDir(), targetVersion)
	if err != nil {
		return err
	}
	if err := createWorkingDir(snapDir, s.readOnlyWorkDir); err != nil {
		return fmt.Errorf("create readonly working dir: %w", err)
	}
	return nil
}

// openTo opens all DBs and catches up via WAL to the given version.
//   - 0  -> replay to end of WAL (latest).
//   - >0 -> replay up to (and including) that version.
func (s *CommitStore) openTo(catchupTarget int64) error {
	if err := s.open(); err != nil {
		return err
	}
	if err := s.rebuildIfAnyDataDBIsUnreachable(); err != nil {
		return err
	}
	return s.replayIntoMutableStore(catchupTarget)
}

// rebuildIfAnyDataDBIsUnreachable discards the working copy when a data DB records a version that
// neither the snapshots nor the WAL can account for, leaving the store at a version replay can reach.
//
// It must run before replay, because replay erases the evidence: applying an older block to a DB that
// is past it rewrites that DB's version record downward to match the others while leaving the later
// block's rows in place. Nothing after replay can then tell the two apart.
//
// The blocks discarded here were never servable. A block present in one data DB and absent from the
// WAL is a block no consistent state includes, so rebuilding from the snapshot loses nothing that
// could have been read back — which is why this repairs rather than refusing and taking the node down.
func (s *CommitStore) rebuildIfAnyDataDBIsUnreachable() error {
	reachable, err := latestVersion(s.flatkvDir(), s.wal)
	if err != nil {
		return err
	}

	unreachable := make([]string, 0, len(dataDBDirs))
	for _, dbDir := range dataDBDirs {
		if meta := s.localMeta[dbDir]; meta.CommittedVersion > reachable {
			unreachable = append(unreachable, fmt.Sprintf("%s at %d", dbDir, meta.CommittedVersion))
		}
	}
	if len(unreachable) == 0 {
		return nil
	}

	logger.Warn("FlatKV rebuilding the working copy: a data DB holds blocks nothing can account for",
		"dataDBs", strings.Join(unreachable, ", "), "highestReachableVersion", reachable)
	return s.rebuildWorkingCopy()
}

// rebuildWorkingCopy throws away the working directory and reopens from the snapshot the current
// symlink names, discarding everything the working copy held beyond that snapshot. Snapshots are left
// in place; the current one is at or below the highest reachable version by construction, so replaying
// forward from it is always valid.
func (s *CommitStore) rebuildWorkingCopy() error {
	if err := s.closeDBsOnly(); err != nil {
		return fmt.Errorf("flatkv: close before rebuilding the working copy: %w", err)
	}
	workDir := filepath.Join(s.flatkvDir(), workingDirName)
	if err := atomicRemoveDir(workDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("flatkv: remove %s while rebuilding the working copy: %w", workingDirName, err)
	}
	if err := s.open(); err != nil {
		return fmt.Errorf("flatkv: reopen after rebuilding the working copy: %w", err)
	}
	return nil
}

// open opens all database instances.
//
// Layout:
//
//	flatkv/
//	  current -> snapshot-NNNNN
//	  snapshot-NNNNN/{account,code,...}/  (immutable)
//	  working/{account,code,...}/          (mutable clone)
//	  changelog/                           (WAL, shared)
//
// The baseline snapshot is cloned into working/ so that PebbleDB writes never mutate snapshot
// directories. The clone is skipped when working/ already records the same snapshot as its source.
func (s *CommitStore) open() (retErr error) {
	s.clearPendingBlock()

	dir := s.flatkvDir()
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create base directory: %w", err)
	}

	acquiredLock := false
	if s.fileLock == nil {
		if err := s.acquireFileLock(dir); err != nil {
			return err
		}
		acquiredLock = true
	}
	defer func() {
		if retErr != nil {
			_ = s.closeDBsOnly()
			if acquiredLock && s.fileLock != nil {
				_ = s.fileLock.Unlock()
				s.fileLock = nil
			}
		}
	}()

	if err := removeTmpDirs(dir); err != nil {
		return fmt.Errorf("cleanup tmp dirs: %w", err)
	}

	snapDir, err := s.resolveSnapshotDir(dir)
	if err != nil {
		return fmt.Errorf("resolve snapshot dir: %w", err)
	}
	if snapVersion, err := parseSnapshotVersion(filepath.Base(snapDir)); err == nil {
		otelMetrics.CurrentSnapshotHeight.Record(s.ctx, snapVersion)
	}

	workDir := filepath.Join(dir, workingDirName)
	if err := createWorkingDir(snapDir, workDir); err != nil {
		return fmt.Errorf("create working dir: %w", err)
	}

	dbs, err := s.openRawDBs()
	if err != nil {
		return err
	}
	defer func() {
		if retErr != nil {
			_ = dbs.close()
		}
	}()

	// Global and per-DB metadata are read off Pebble first, while the databases are still the only
	// thing holding state, and only then are the stores built on top.
	if err := s.loadLocalMeta(dbs); err != nil {
		return err
	}

	if err := s.loadGlobalMetadata(); err != nil {
		return err
	}

	if err := s.openStores(dbs); err != nil {
		return err
	}

	logger.Info("FlatKV store opened", "dir", dir, "version", s.committedVersion)
	return nil
}

func (s *CommitStore) acquireFileLock(dir string) error {
	lockPath, err := filepath.Abs(filepath.Join(dir, lockFileName))
	if err != nil {
		return fmt.Errorf("abs lock path: %w", err)
	}
	fl, err := filelock.New(lockPath)
	if err != nil {
		return fmt.Errorf("create file lock: %w", err)
	}
	locked, err := fl.TryLock()
	if err != nil {
		if errors.Is(err, filelock.ErrLocked) {
			return fmt.Errorf("%w: %v", commonerrors.ErrFileLockUnavailable, err)
		}
		return fmt.Errorf("acquire file lock: %w", err)
	}
	if !locked {
		return fmt.Errorf("%w: held by another process (%s)", commonerrors.ErrFileLockUnavailable, lockPath)
	}
	s.fileLock = fl
	return nil
}

// openPebbleDB creates the directory at cfg.DataDir and opens a bare PebbleDB instance.
func (s *CommitStore) openPebbleDB(cfg *pebbledb.PebbleDBConfig) (seidbtypes.KeyValueDB, error) {
	if err := os.MkdirAll(cfg.DataDir, 0750); err != nil {
		return nil, fmt.Errorf("create directory %s: %w", cfg.DataDir, err)
	}
	db, err := pebbledb.Open(s.ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", cfg.DataDir, err)
	}
	return db, nil
}

// rawDBs holds the four raw pebble handles between opening them and handing them to the stores.
type rawDBs struct {
	account seidbtypes.KeyValueDB
	code    seidbtypes.KeyValueDB
	storage seidbtypes.KeyValueDB
	misc    seidbtypes.KeyValueDB
}

// forDir returns the handle for the named database directory, or nil when the name is not one of them.
func (d rawDBs) forDir(name string) seidbtypes.KeyValueDB {
	switch name {
	case accountDBDir:
		return d.account
	case codeDBDir:
		return d.code
	case storageDBDir:
		return d.storage
	case miscDBDir:
		return d.misc
	}
	return nil
}

// close closes every handle, joining whatever errors come back.
func (d rawDBs) close() error {
	return errors.Join(
		closeDB(accountDBDir, d.account),
		closeDB(codeDBDir, d.code),
		closeDB(storageDBDir, d.storage),
		closeDB(miscDBDir, d.misc),
	)
}

// closeDB closes db, naming dir in any error. A nil handle is nothing to close.
func closeDB(dir string, db seidbtypes.KeyValueDB) error {
	if db == nil {
		return nil
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("%s close: %w", dir, err)
	}
	return nil
}

// openRawDBs opens the four pebble instances. The caller owns them until the stores take over; on
// failure nothing is left open.
func (s *CommitStore) openRawDBs() (dbs rawDBs, retErr error) {
	defer func() {
		if retErr != nil {
			_ = dbs.close()
		}
	}()

	var err error
	if dbs.account, err = s.openPebbleDB(&s.config.AccountDBConfig); err != nil {
		return dbs, fmt.Errorf("failed to open account DB: %w", err)
	}
	if dbs.code, err = s.openPebbleDB(&s.config.CodeDBConfig); err != nil {
		return dbs, fmt.Errorf("failed to open code DB: %w", err)
	}
	if dbs.storage, err = s.openPebbleDB(&s.config.StorageDBConfig); err != nil {
		return dbs, fmt.Errorf("failed to open storage DB: %w", err)
	}
	if dbs.misc, err = s.openPebbleDB(&s.config.MiscDBConfig); err != nil {
		return dbs, fmt.Errorf("failed to open misc DB: %w", err)
	}
	return dbs, nil
}

// loadLocalMeta reads each data database's persisted metadata into localMeta.
func (s *CommitStore) loadLocalMeta(dbs rawDBs) error {
	s.localMeta = make(map[string]*LocalMeta)
	for _, dir := range dataDBDirs {
		meta, err := loadLocalMeta(dbs.forDir(dir))
		if err != nil {
			return fmt.Errorf("failed to load %s local meta: %w", dir, err)
		}
		s.localMeta[dir] = meta
	}
	return nil
}

// openStores wraps the four already-open PebbleDBs in view managers. It is the last step of opening a
// store: each database is handed to the store that wraps it, which owns it from then on, and every later
// access goes through that store. Reaching a database directly after this point is possible only through
// rawDBFor, whose doc gives the rules for it.
//
// On failure every store already constructed is closed, leaving the store store-less rather than
// half-wired.
func (s *CommitStore) openStores(dbs rawDBs) (retErr error) {
	defer func() {
		if retErr == nil {
			return
		}
		// A store that fails to close may leave its database open, and the next open then fails on the
		// file lock with an error that looks unrelated. Joining it here names the real cause.
		if closeErr := s.closeStores(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close partially opened stores: %w", closeErr))
		}
	}()

	var err error

	// readPool and miscPool must stay distinct pools: misc tasks block on read results, so sharing one
	// bounded pool can deadlock. Nothing may sit between a store and its database that schedules its
	// own reads onto either pool, for the same reason.
	open := func(cfg *view.ViewManagerConfig, db seidbtypes.KeyValueDB) (view.ViewManager, error) {
		store, storeErr := view.NewViewManager(cfg, db, s.readPool, s.miscPool)
		if storeErr != nil {
			return nil, fmt.Errorf("failed to create %s view manager: %w", cfg.Name, storeErr)
		}
		return store, nil
	}

	s.accountStore, err = open(&s.config.AccountStoreConfig, dbs.account)
	if err != nil {
		return err
	}
	s.codeStore, err = open(&s.config.CodeStoreConfig, dbs.code)
	if err != nil {
		return err
	}
	s.storageStore, err = open(&s.config.StorageStoreConfig, dbs.storage)
	if err != nil {
		return err
	}
	s.miscStore, err = open(&s.config.MiscStoreConfig, dbs.misc)
	if err != nil {
		return err
	}

	s.stores = []view.ViewManager{
		s.accountStore, s.codeStore, s.storageStore, s.miscStore,
	}

	// Every store gets a baseline seal, read-only views included. A view replays blocks to reach its target
	// height, and each replayed block is hashed against the snapshot before it; without a baseline there is no
	// "before", so every key in the first replayed block would be mixed in as new with nothing mixed out and
	// the view would report a hash matching no real chain history.
	if err := s.sealBaseline(); err != nil {
		return err
	}

	if err := s.startHashing(); err != nil {
		return err
	}

	if !s.readOnly {
		// Built last, and only here: it checkpoints the databases the view managers above own, so it must
		// not outlive them. closeStores drains it before those managers go away.
		s.snapshotWriter = newSnapshotWriter(
			s.ctx,
			s.flatkvDir(),
			s.config.SnapshotKeepRecent,
			s.config.ExternalPruning,
			s.config.SnapshotInterval,
			s.config.MaxSnapshotLagBlocks,
			s.checkpointables(),
		)
	}

	return nil
}

// checkpointables returns the handle each database is checkpointed through, keyed by database
// directory name. Captured once while the view managers exist, so a snapshot being written off-thread
// never has to reach back into the store for a handle that teardown may have cleared.
//
// A checkpoint addresses a database as a file rather than as a key-value store, which is the one thing
// a view manager cannot express — so this is the single place FlatKV reaches past one, and the
// manager's escape hatch names checkpointing as its only sanctioned use.
func (s *CommitStore) checkpointables() map[string]seidbtypes.Checkpointable {
	dbs := make(map[string]seidbtypes.Checkpointable, len(dataDBDirs))
	for _, name := range dataDBDirs {
		if db, ok := s.rawDBFor(name).(seidbtypes.Checkpointable); ok {
			dbs[name] = db
		}
	}
	return dbs
}

// viewManagerFor returns the view manager mediating the named database, or nil before the managers exist.
//
// It answers with the manager, not the database beneath it, so that every access through it is an access the
// manager has sanctioned. The one operation that genuinely needs the database — taking a Pebble checkpoint,
// which addresses it as a file rather than as a key-value store — reaches past the manager at its own call
// site, where the reason is written down.
func (s *CommitStore) viewManagerFor(name string) view.ViewManager {
	switch name {
	case accountDBDir:
		return s.accountStore
	case codeDBDir:
		return s.codeStore
	case storageDBDir:
		return s.storageStore
	case miscDBDir:
		return s.miscStore
	}
	return nil
}

// rawDBFor returns the raw database behind the named view manager, bypassing every guarantee that manager
// provides. Apply intense scrutiny at every call site.
//
// Reading data through it is a bug: it sees only what the flusher has written, missing both staged and
// finalized-but-unflushed rows, silently. Use Get/BatchGet/Iterator instead.
//
// Returns nil before the managers exist; callers in that window hold the handles directly.
func (s *CommitStore) rawDBFor(name string) seidbtypes.KeyValueDB {
	manager := s.viewManagerFor(name)
	if manager == nil {
		return nil
	}
	return manager.EscapeHatchUnderlyingDB()
}

// closeStores tears down whichever stores exist and clears them, so a store that is being reopened
// (rollback, restore) does not keep stores pointed at closed databases. Errors are joined rather
// than short-circuited: every store must be given its chance to stop.
func (s *CommitStore) closeStores() error {
	var errs []error

	// Hashing stops before the writer, which can be waiting for a block to flush — something only
	// finalization makes possible.
	if err := s.stopHashing(); err != nil {
		errs = append(errs, err)
	}

	// The writer must stop before anything below runs: closing a view manager closes the database it
	// owns, and a checkpoint in progress would then be reading a closed handle. This is the choke point
	// every teardown path reaches — Close directly, Rollback and resetForImport through closeDBsOnly —
	// so the guard lives here rather than at each of them.
	if s.snapshotWriter != nil {
		if err := s.snapshotWriter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close snapshot writer: %w", err))
		}
		s.snapshotWriter = nil
	}

	// Release the reservations on the last sealed block and forget the handles. They belong to the
	// stores being torn down here, so keeping them would leave a reopened store (rollback, restore)
	// awaiting a flush on views whose store is already gone.
	if s.lastSealed != nil {
		if err := s.lastSealed.Close(); err != nil {
			errs = append(errs, fmt.Errorf("release sealed views: %w", err))
		}
		s.lastSealed = nil
	}

	for _, store := range s.stores {
		if store == nil {
			continue
		}
		if err := store.Close(); err != nil {
			errs = append(errs, fmt.Errorf("%s store close: %w", store.Name(), err))
		}
	}

	s.accountStore = nil
	s.codeStore = nil
	s.storageStore = nil
	s.miscStore = nil
	s.stores = nil
	return errors.Join(errs...)
}

// computeStoreHeights reports the height each database actually reached on disk, keyed by database
// directory name, or nil when they all sit at the store's committed version and replay has nothing to
// skip.
//
// Replay starts from the committed version, which deriveGlobalState has already set to the lowest of
// these heights; the databases that are past that block skip it individually rather than the whole
// block being refused.
//
// Must run after each database's LocalMeta has been read off pebble, and before the stores exist.
func (s *CommitStore) computeStoreHeights() map[string]int64 {
	heights := make(map[string]int64, len(dataDBDirs))
	for _, dir := range dataDBDirs {
		heights[dir] = s.localMeta[dir].CommittedVersion
	}

	// A database ahead of the store-wide version flushed a block whose peers did not. Only when all four
	// agree is there nothing to catch up.
	for _, height := range heights {
		if height != s.committedVersion {
			logger.Info("FlatKV stores are at different heights; replay will catch them up",
				"storeWide", s.committedVersion, "perDB", heights)
			return heights
		}
	}
	return nil
}

// loadGlobalMetadata rebuilds the store's in-memory global state from the data DBs' metadata.
func (s *CommitStore) loadGlobalMetadata() error {
	if err := s.hydratePerDBState(); err != nil {
		return err
	}
	s.deriveGlobalState()
	return nil
}

// hydratePerDBState rebuilds loadedHashes from each data DB's LocalMeta. It rejects a DB whose
// per-module hashes do not sum to its recorded root.
func (s *CommitStore) hydratePerDBState() error {
	s.loadedHashes = lthash.NewBlockHash(dataDBDirs)
	for _, dbDir := range dataDBDirs {
		meta := s.localMeta[dbDir]
		if err := validatePerModuleMetadata(dbDir, meta); err != nil {
			return err
		}
		if meta != nil && meta.LtHash != nil {
			s.loadedHashes.PerDB[dbDir] = meta.LtHash.Clone()
		} else {
			s.loadedHashes.PerDB[dbDir] = lthash.New()
		}
		if meta != nil {
			s.loadedHashes.PerModule[dbDir] = cloneModuleHashes(meta.ModuleLtHashes)
			s.loadedHashes.PerModuleStats[dbDir] = cloneModuleStats(meta.ModuleStats)
		}
	}
	return nil
}

// deriveGlobalState sets the committed version to the lowest version any data DB
// reached and the committed LtHash to the homomorphic sum of their roots.
func (s *CommitStore) deriveGlobalState() {
	version := s.localMeta[dataDBDirs[0]].CommittedVersion
	for _, dbDir := range dataDBDirs {
		if v := s.localMeta[dbDir].CommittedVersion; v < version {
			version = v
		}
	}

	for _, dbDir := range dataDBDirs {
		if meta := s.localMeta[dbDir]; meta.CommittedVersion > version {
			logger.Warn("data DB versions disagree, catchup will replay from the lowest",
				"db", dbDir,
				"localVersion", meta.CommittedVersion,
				"storeVersion", version)
		}
	}

	s.committedVersion = version
	s.loadedHashes.BlockNumber = version
	s.loadedHashes.Global = lthash.SumDBHashes(dataDBDirs, s.loadedHashes.PerDB)
}

// startHashing builds the hash engine and the finalizer that consumes it, both seeded from what load
// read off disk. They are built together and only here, so neither outlives the stores it reads.
//
// A read-only store gets them too: it replays blocks to reach its target height, and each replayed block
// is hashed against the one before it exactly as a committed block is.
func (s *CommitStore) startHashing() error {
	// Called through a closure rather than passed directly, so the field stays the live source of truth
	// and a test can swap it on an open store.
	moduleParser := func(key []byte) (string, error) { return s.moduleOf(key) }

	engine, err := lthash.NewHashEngine(
		s.ctx, &s.config.HashEngineConfig, s.ltHashPool, dataDBDirs, moduleParser, s.loadedHashes)
	if err != nil {
		return fmt.Errorf("create hash engine: %w", err)
	}
	s.hashEngine = engine
	s.finalizer = newFinalizationManager(
		s.ctx,
		s.hashEngine.AwaitHash(),
		s.loadedHashes,
		s.config.FinalizationQueueSize,
		s.config.HashChanSize,
		s.hashLogger,
	)

	if s.readOnly {
		// Nothing consumes a read-only store's stream — HashChan reports it as closed — so it is drained
		// here. Left unread, replaying past the channel's depth would block on a hash no one wants. The
		// goroutine ends when the finalizer closes the stream.
		published := s.finalizer.HashChan()
		go func() {
			for range published { //nolint:revive // discarding is the point
			}
		}()
	}
	return nil
}

// stopHashing closes the hash engine and the finalizer, in that order.
//
// The order is load-bearing: the engine publishes the blocks it drains and the finalizer is its only
// reader, so stopping the finalizer first would leave the engine blocked forever.
func (s *CommitStore) stopHashing() error {
	var errs []error
	if s.hashEngine != nil {
		if err := s.hashEngine.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close hash engine: %w", err))
		}
		s.hashEngine = nil
	}
	if s.finalizer != nil {
		if err := s.finalizer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close finalization manager: %w", err))
		}
		s.finalizer = nil
	}
	return errors.Join(errs...)
}

// restartHashing rebuilds the hash engine and the finalizer from what the store has just loaded, for a
// caller that has replaced the databases underneath them.
//
// Discarding and rebuilding rather than reseeding in place: the caller has already quiesced the store,
// so there is nothing in flight to preserve, and the pair has to move together anyway — the finalizer
// captures the engine's stream when it is built.
func (s *CommitStore) restartHashing() error {
	if s.hashEngine == nil {
		return nil
	}
	if err := s.stopHashing(); err != nil {
		return err
	}
	return s.startHashing()
}

// reloadLocalMeta re-reads each data database's recorded metadata from disk, for a caller that needs
// what the finalizer has written rather than what load saw.
//
// It waits for both barriers first, because without them the read is meaningless: a block's metadata is
// written by the finalizer, on its own goroutine, into the same batch as the block's data — and that
// batch reaches disk asynchronously after that. Waiting here rather than at each caller is what stops
// the next one reading a database that has not caught up yet.
func (s *CommitStore) reloadLocalMeta() error {
	if err := s.FlushHashes(); err != nil {
		return fmt.Errorf("flush hashes before reading local meta: %w", err)
	}
	if err := s.flushLatestVersion(); err != nil {
		return fmt.Errorf("flush to disk before reading local meta: %w", err)
	}
	for _, dir := range dataDBDirs {
		meta, err := loadLocalMeta(s.rawDBFor(dir))
		if err != nil {
			return fmt.Errorf("reload %s local meta: %w", dir, err)
		}
		s.localMeta[dir] = meta
	}
	return nil
}

// requireAlignedDataDBs returns an error unless every data DB sits at the store's committed version.
// The condition holds once replay has run; before then the DBs may legally disagree, and
// rebuildIfAnyDataDBIsUnreachable has already removed the disagreements replay cannot close.
//
// This is what makes summing the per-DB roots into the store root sound: the sum only describes a real
// state if every DB contributed at the same version.
func (s *CommitStore) requireAlignedDataDBs() error {
	// s.localMeta describes what load saw, not what the replay above has since recorded.
	if err := s.reloadLocalMeta(); err != nil {
		return fmt.Errorf("flatkv: reload local meta before checking data DB alignment: %w", err)
	}

	misaligned := make([]string, 0, len(dataDBDirs))
	for _, dbDir := range dataDBDirs {
		if meta := s.localMeta[dbDir]; meta.CommittedVersion != s.committedVersion {
			misaligned = append(misaligned, fmt.Sprintf("%s at %d", dbDir, meta.CommittedVersion))
		}
	}
	if len(misaligned) == 0 {
		return nil
	}
	return fmt.Errorf("flatkv: store is at version %d after replay but %s; the write-ahead log did not "+
		"bring every data DB to the same version (restore from a snapshot, or re-sync)",
		s.committedVersion, strings.Join(misaligned, ", "))
}

func (s *CommitStore) Version() int64 {
	return s.committedVersion
}

// PendingVersion returns s.pendingBlockHeight: the height of the block currently
// buffered by ApplyChangeSets, or 0 when there are no buffered writes.
func (s *CommitStore) PendingVersion() int64 {
	return s.pendingBlockHeight
}

// PublishedHash returns the most recent block hash the store has published: its height, its lattice
// hash root, and each database's root.
//
// On a committing store this is whatever the pipeline has reached, which lags the committed version. On
// a store that has just been loaded, and on a read-only store, it is the height that was loaded. Use
// FlushHashes first to make it describe the version just committed.
func (s *CommitStore) PublishedHash() *lthash.BlockHash {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.finalizer != nil {
		return s.finalizer.PublishedHash()
	}
	return s.loadedHashes
}

// HashChan returns a channel producing the hash of each block. Exactly one hash per block committed, in
// block order, with no gaps or duplicates. It is closed once the store stops hashing.
//
// The channel has finite depth, so failure to dequeue hashes for long enough blocks commit. Every
// deployment therefore needs a consumer.
func (s *CommitStore) HashChan() <-chan *lthash.BlockHash {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.finalizer != nil {
		return s.finalizer.HashChan()
	}
	// A read-only store never commits and so never publishes. A closed channel lets a consumer range
	// over it and finish, rather than blocking forever on a stream that will never carry anything.
	empty := make(chan *lthash.BlockHash)
	close(empty)
	return empty
}

// FlushHashes blocks until the store has published a hash for every block committed so far, and
// recorded each one's metadata alongside the block it describes.
func (s *CommitStore) FlushHashes() error {
	s.mu.RLock()
	engine, finalizer := s.hashEngine, s.finalizer
	s.mu.RUnlock()

	if engine == nil {
		return nil
	}
	// The engine first: its output is the finalizer's input, so waiting on the finalizer alone would
	// return before blocks still inside the engine had reached it.
	if err := engine.Flush(); err != nil {
		return fmt.Errorf("flush hashes: %w", err)
	}
	if err := finalizer.Flush(); err != nil {
		return fmt.Errorf("flush hashes: %w", err)
	}
	return nil
}

// CommitPendingBlock commits the block currently being applied, if any, so that it has a hash. A no-op
// on a store with no pending writes, which is every store between blocks and every read-only store.
//
// A block that has not been committed has no hash — the hash is computed from the views a commit
// produces — so a caller wanting one mid-block is asking for the block to be committed. This is that
// request, made explicitly. Post-Cosmos nothing asks for a hash mid-block and this goes away.
func (s *CommitStore) CommitPendingBlock() error {
	if s.readOnly {
		return nil
	}
	pending := s.PendingVersion()
	if pending == 0 {
		return nil
	}
	if _, err := s.Commit(pending); err != nil {
		return fmt.Errorf("commit pending block %d: %w", pending, err)
	}
	return nil
}

func (s *CommitStore) Importer(version int64) (types.Importer, error) {
	if s.readOnly {
		return nil, errReadOnly
	}
	// rootmulti.Restore closes the store before creating an importer.
	// Close() cancels the context (killing pools), so recreate them
	// before reopening the DBs.
	if s.isClosed() {
		if s.ctx.Err() != nil {
			s.resetPools()
		}
		if err := s.open(); err != nil {
			return nil, fmt.Errorf("reopen store for import: %w", err)
		}
		// The store must be able to commit once the import finishes, so make sure it holds a live WAL: the
		// injected instance is normally closed along with the DBs.
		if err := s.reopenWAL(); err != nil {
			return nil, fmt.Errorf("reopen WAL for import: %w", err)
		}
	}
	if err := s.resetForImport(); err != nil {
		return nil, fmt.Errorf("reset store for import: %w", err)
	}
	return NewKVImporter(s, version, s.importDBs()), nil
}

// importDBs collects the raw databases an import writes into, taken from the view managers that
// currently own them.
//
// An import writes beneath the managers, so it needs the databases themselves. Gathering them in one place
// keeps that need visible as a single act rather than as a handle fetched per key.
func (s *CommitStore) importDBs() rawDBs {
	return rawDBs{
		account: s.rawDBFor(accountDBDir),
		code:    s.rawDBFor(codeDBDir),
		storage: s.rawDBFor(storageDBDir),
		misc:    s.rawDBFor(miscDBDir),
	}
}

// resetForImport purges all existing data so that a subsequent import
// produces a clean store containing only the snapshot being restored.
// Without this, keys that exist locally but were deleted in the remote
// snapshot would survive the import, producing a mixed stale state.
func (s *CommitStore) resetForImport() error {
	if err := s.closeDBsOnly(); err != nil {
		return fmt.Errorf("close before import reset: %w", err)
	}

	dir := s.flatkvDir()
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("create flatkv dir: %w", err)
	}

	// rootmulti.Restore calls Close() (which releases the file lock)
	// before calling Importer(). Re-acquire the lock before mutating
	// the data directory so no other process can interfere.
	if s.fileLock == nil {
		if err := s.acquireFileLock(dir); err != nil {
			return err
		}
	}

	if err := atomicRemoveDir(filepath.Join(dir, workingDirName)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("resetForImport: remove %s: %w", workingDirName, err)
	}

	if err := traverseSnapshots(dir, true, func(v int64) (bool, error) {
		p := filepath.Join(dir, snapshotName(v))
		if err := atomicRemoveDir(p); err != nil {
			return false, fmt.Errorf("remove snapshot %s: %w", p, err)
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("resetForImport: %w", err)
	}

	if err := os.Remove(currentPath(dir)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("resetForImport: remove %s: %w", currentLink, err)
	}

	// The WAL is deliberately left alone. Import bypasses it, so a pre-existing WAL is stale relative to the
	// imported version — but a state-sync restore is a manual procedure in which the operator stops the node
	// and removes its data directories, changelog included, so the restored node opens an empty WAL and its
	// first commit at the restored height is accepted. Skipping that wipe leaves a WAL ending below the
	// restored height: replay finds nothing to apply and the next commit is rejected by the state WAL's
	// contiguity rule, which fails loudly rather than diverging silently.

	// Reopen from a pristine empty state. open() will load metadata
	// from the empty DB (a no-op), then we reset in-memory state below.
	if err := s.open(); err != nil {
		return err
	}

	s.committedVersion = 0
	s.loadedHashes = lthash.NewBlockHash(dataDBDirs)

	return nil
}

// reopenWAL replaces this store's WAL instance with a freshly opened one on the same directory, preserving
// everything the directory holds. It is how a store that was closed and then reused (rootmulti.Restore closes
// before importing) gets back a WAL it can commit into; nothing here discards WAL data.
//
// When the store does not manage a WAL (constructed with nil — the outer context owns the pipeline) this is a
// no-op. On failure the store keeps the old instance rather than dropping to nil, so a later write fails
// loudly instead of being skipped.
func (s *CommitStore) reopenWAL() error {
	if s.wal == nil {
		return nil
	}
	// Close first. A closed store does not imply a closed WAL — closeDBsOnly deliberately leaves it open — and
	// the WAL directory admits a single owner, so a live instance would block the reopen below. Closing an
	// already-closed WAL is a no-op.
	if err := s.wal.Close(); err != nil {
		return fmt.Errorf("close state WAL: %w", err)
	}
	w, err := statewal.New(stateWALConfig(s.config.DataDir))
	if err != nil {
		return fmt.Errorf("open state WAL: %w", err)
	}
	s.wal = w
	return nil
}

func (s *CommitStore) GetPhaseTimer() *metrics.PhaseTimer {
	return s.phaseTimer
}
