package flatkv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/zbiljic/go-filelock"
	"go.opentelemetry.io/otel/attribute"

	commonerrors "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/snapshot"
	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "state-db", "sc", "flatkv")

var _ Store = (*CommitStore)(nil)

// CommitStore implements flatkv.Store for EVM state.
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

	// The metadata each database had persisted when this store was opened, keyed by database directory
	// name. A LocalMeta records a database's committed height, its LtHash, and its per-module hashes and
	// stats.
	//
	// Read at open, and rewritten only by the paths that replace a database's contents wholesale — import,
	// rollback, and seeding an initial version. Committing a block does not update it: the block's metadata
	// is written by the hasher, in the same atomic batch as the data it describes, so this map goes stale as
	// soon as the first block commits. What consumes it runs before the stores exist — deciding where replay
	// must start, and seeding the hasher's accumulator.
	localMeta map[string]*ktype.LocalMeta

	// The height of the most recently committed block. The next Commit must be exactly this plus one.
	committedVersion int64

	// The root LtHash as of committedVersion — the value reported to anyone asking for the committed
	// hash. It does not move until a Commit has succeeded on all five stores.
	committedLtHash *lthash.LtHash

	// earliestVersion is the version this store's history begins at, as
	// recorded by SetInitialVersion (the seeded version). 0 when unknown:
	// genesis stores and stores created before the record existed. See
	// EarliestVersion.
	earliestVersion int64

	// The four data stores below mediate every read and write of their databases. The block being
	// applied accumulates its writes inside each store, so a read through a store already sees what
	// that same block staged, with no separate overlay to consult.
	//
	// They are constructed as the last step of open, after any replay or rollback has run, and are nil
	// until then — the bootstrap and import paths deliberately write raw pebble before they exist.

	// Mediates the account database.
	accountStore snapshot.SnapshotEngine

	// Mediates the code database.
	codeStore snapshot.SnapshotEngine

	// Mediates the storage database.
	storageStore snapshot.SnapshotEngine

	// Mediates the misc database.
	miscStore snapshot.SnapshotEngine

	// Holds raw bytes rather than vtype values, and never serves reads: its keys all live under the
	// reserved metadata prefix, so they are written only via Finalize and read only off pebble before
	// the stores exist.
	metadataStore snapshot.SnapshotEngine

	// All five stores, for the paths that treat them uniformly.
	stores []snapshot.SnapshotEngine

	// The snapshots produced by the most recent commit, one per store and keyed by its name, each still
	// holding the reservation Commit handed out. flushLatestVersion waits on them, and holding them
	// keeps any later block out of pebble until the next commit hands them back.
	lastSealed map[string]snapshot.Snapshot

	// The state WAL. Injected at construction: non-nil ⇒ FlatKV writes/replays/prunes it; nil ⇒ the outer
	// context owns the whole WAL pipeline and FlatKV no-ops every WAL operation. FlatKV owns Close of whatever
	// instance it currently holds (rollback/restore may replace it via close→prune/delete→reopen); its
	// lifecycle is decoupled from the DB open/close cycle, so closeDBsOnly does not touch it.
	wal statewal.StateWAL

	// Changes to feed into the WAL at the next commit.
	pendingChangeSets []*proto.NamedChangeSet

	// pendingBlockHeight is the version stamped by the current buffered
	// ApplyChangeSets. 0 means no pending apply. Further ApplyChangeSets
	// calls and Commit both require version to match when this is non-zero:
	// only one block may be buffered per commit.
	pendingBlockHeight int64

	// Computes each committed block's lattice hash off the execution thread, and owns the accumulated hash
	// state while doing so. Built by openStores once the stores exist and torn down by closeStores. Nil on a
	// read-only store, which never commits — such a store answers hash queries from what it loaded.
	hasher *blockHasher

	// The hash state the next hasher is built from, produced by loadGlobalMetadata before the stores exist.
	// Only meaningful between that load and the openStores that consumes it.
	hashSeed hasherSeed

	// Writes snapshots off the execution thread. Built by openStores once the stores exist and torn
	// down by closeStores, so its lifetime is exactly the window in which the databases it checkpoints
	// are open. Nil on a read-only store, which never commits.
	snapshotWriter *SnapshotWriter

	// File lock prevents multiple processes from opening the same DB.
	fileLock filelock.TryLockerSafe

	phaseTimer *metrics.PhaseTimer

	// classifyBucketSizes records how many pairs each EVM key kind held in the last applied block,
	// so the next block's buckets can be allocated up front. Guarded by mu.
	classifyBucketSizes [keys.EVMKeyKindCount]int

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

	// ltCalc encapsulates the lattice-hash pipeline (old-value reads, per-key
	// hashing, and worker-combine into final per-DB / per-module hashes) over
	// ltHashPool. The commit path is serialized by s.mu, so the calculator has
	// a single caller at a time.
	ltCalc *lthash.HashCalculator
}

// routePhysicalKey maps a physical DB key to its target database.
// Non-EVM modules are routed to miscDB; EVM keys are routed by kind.
func (s *CommitStore) routePhysicalKey(physicalKey []byte) (seidbtypes.KeyValueDB, error) {
	moduleName, innerKey, err := ktype.StripModulePrefix(physicalKey)
	if err != nil {
		return nil, err
	}
	if moduleName == "" {
		// An empty module name would fold into moduleLtHash[""]/moduleStats[""]
		// and later persist as the per-module meta key "_meta/x:/hash", which
		// ParseModuleLtHashKey rejects on the very next reopen — permanently
		// bricking the store (root != sum(modules) forever). Reject it here,
		// at the state-sync import dispatch boundary, mirroring the
		// classifyAndPrefix guard on the live-commit path (store_apply.go).
		return nil, fmt.Errorf("flatkv: empty module name in physical key %q", physicalKey)
	}
	if moduleName != keys.EVMStoreKey {
		return s.rawDBFor(miscDBDir), nil
	}
	kind, _ := keys.ParseEVMKey(innerKey)
	switch kind {
	case ktype.EVMKeyAccount, keys.EVMKeyCodeHash:
		return s.rawDBFor(accountDBDir), nil
	case keys.EVMKeyCode:
		return s.rawDBFor(codeDBDir), nil
	case keys.EVMKeyStorage:
		return s.rawDBFor(storageDBDir), nil
	default:
		return s.rawDBFor(miscDBDir), nil
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
) (*CommitStore, error) {

	initializeDataDirectories(cfg)

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
	ltCalc := lthash.NewHashCalculator(ltHashPool, dataDBDirs, moduleOfKey)

	return &CommitStore{
		ctx:               ctx,
		cancel:            cancel,
		config:            *cfg,
		localMeta:         make(map[string]*ktype.LocalMeta),
		pendingChangeSets: make([]*proto.NamedChangeSet, 0),
		committedLtHash:   lthash.New(),
		phaseTimer:        metrics.NewPhaseTimer(flatkvMeter, "seidb_main_thread"),
		readPool:          readPool,
		miscPool:          miscPool,
		ltHashPool:        ltHashPool,
		ltCalc:            ltCalc,
		wal:               stateWAL,
	}, nil
}

// lthashWorkerCount computes the fixed lattice-hash pool worker count from
// config, clamped to at least 1 (LtHash computation always needs a worker).
// initializeDataDirectories sets the DataDir for each nested PebbleDB config
// that does not already have one, using DataDir as the base path. The DBs live
// under the working directory: <DataDir>/working/<subdir>.
func initializeDataDirectories(c *config.Config) {
	workDir := filepath.Join(c.DataDir, workingDirName)
	if c.AccountDBConfig.DataDir == "" {
		c.AccountDBConfig.DataDir = filepath.Join(workDir, accountDBDir)
	}
	if c.CodeDBConfig.DataDir == "" {
		c.CodeDBConfig.DataDir = filepath.Join(workDir, codeDBDir)
	}
	if c.StorageDBConfig.DataDir == "" {
		c.StorageDBConfig.DataDir = filepath.Join(workDir, storageDBDir)
	}
	if c.MiscDBConfig.DataDir == "" {
		c.MiscDBConfig.DataDir = filepath.Join(workDir, miscDBDir)
	}
	if c.MetadataDBConfig.DataDir == "" {
		c.MetadataDBConfig.DataDir = filepath.Join(workDir, metadataDir)
	}
	applyPebbleMetricsConfig(c)
}

func applyPebbleMetricsConfig(c *config.Config) {
	// Keep a single FlatKV-level knob for Pebble internal metrics. Per-DB
	// EnableMetrics values are intentionally overwritten here.
	c.AccountDBConfig.EnableMetrics = c.EnablePebbleMetrics
	c.CodeDBConfig.EnableMetrics = c.EnablePebbleMetrics
	c.StorageDBConfig.EnableMetrics = c.EnablePebbleMetrics
	c.MiscDBConfig.EnableMetrics = c.EnablePebbleMetrics
	c.MetadataDBConfig.EnableMetrics = c.EnablePebbleMetrics

	c.AccountDBConfig.EnableReadWriteMetrics = c.EnableReadWriteMetrics
	c.CodeDBConfig.EnableReadWriteMetrics = c.EnableReadWriteMetrics
	c.StorageDBConfig.EnableReadWriteMetrics = c.EnableReadWriteMetrics
	c.MiscDBConfig.EnableReadWriteMetrics = c.EnableReadWriteMetrics
	c.MetadataDBConfig.EnableReadWriteMetrics = c.EnableReadWriteMetrics
}

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
	s.ltCalc = lthash.NewHashCalculator(s.ltHashPool, dataDBDirs, moduleOfKey)
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
func (s *CommitStore) LoadVersionReadOnly(targetVersion int64) (opened Store, retErr error) {
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
	ro, err := NewCommitStore(context.Background(), &s.config, nil)
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
	ro.config.MetadataDBConfig.DataDir = filepath.Join(workDir, metadataDir)

	// Engine metrics are labelled by engine name, which the view shares with this store, so leaving them
	// enabled would publish two conflicting values for every series.
	ro.config.AccountStoreConfig.MetricsEnabled = false
	ro.config.CodeStoreConfig.MetricsEnabled = false
	ro.config.StorageStoreConfig.MetricsEnabled = false
	ro.config.MiscStoreConfig.MetricsEnabled = false
	ro.config.MetadataStoreConfig.MetricsEnabled = false

	// Transfer the lazily-acquired lock to the view so that ro.Close()
	// releases it, preventing a leak when this store is never closed.
	if lazyLock && s.fileLock != nil {
		ro.fileLock = s.fileLock
		s.fileLock = nil
	}

	if err := ro.openReadOnly(targetVersion); err != nil {
		return nil, fmt.Errorf("readonly open: %w", err)
	}

	// The clone is open at a snapshot boundary with a nil WAL. Replay this (primary) store's WAL into it up
	// to targetVersion so it reflects the exact requested height. The clone is not yet marked read-only, so
	// the replay's ApplyChangeSets calls are permitted; mark it read-only only once replay succeeds.
	if err := s.replayIntoReadOnlyCopy(ro, targetVersion); err != nil {
		return nil, err
	}

	if targetVersion > 0 && ro.committedVersion != targetVersion {
		return nil, fmt.Errorf("readonly version mismatch: requested %d, reached %d",
			targetVersion, ro.committedVersion)
	}

	ro.readOnly = true

	logger.Info("FlatKV readonly store opened", "version", ro.committedVersion, "dir", ro.readOnlyWorkDir)
	return ro, nil
}

// openReadOnly opens PebbleDBs in readOnlyWorkDir at the snapshot boundary at or below targetVersion,
// leaving committedVersion at that snapshot version. It never modifies the global "current" symlink.
//
// This clone has a nil WAL of its own, so it does NOT replay: advancing from the snapshot boundary up to
// targetVersion — and marking the store read-only — is driven by the primary via LoadVersionReadOnly /
// replayIntoReadOnlyCopy, which feeds the primary's WAL into this clone.
func (s *CommitStore) openReadOnly(targetVersion int64) (retErr error) {
	s.clearPendingBlock()

	dir := s.flatkvDir()

	var snapDir string
	if targetVersion > 0 {
		baseVer, err := seekSnapshot(dir, targetVersion)
		if err != nil {
			return fmt.Errorf("seek snapshot for readonly: %w", err)
		}
		snapDir = filepath.Join(dir, snapshotName(baseVer))
	} else {
		var err error
		snapDir, _, err = currentSnapshotDir(dir)
		if err != nil {
			return fmt.Errorf("resolve current snapshot for readonly: %w", err)
		}
	}

	if err := createWorkingDir(snapDir, s.readOnlyWorkDir); err != nil {
		return fmt.Errorf("create readonly working dir: %w", err)
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

	if err := s.loadGlobalMetadata(dbs.metadata); err != nil {
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

// openTo opens all DBs and catches up via WAL to the given version.
//   - 0  -> replay to end of WAL (latest).
//   - >0 -> replay up to (and including) that version.
func (s *CommitStore) openTo(catchupTarget int64) error {
	if err := s.open(); err != nil {
		return err
	}
	return s.replayIntoMutableStore(catchupTarget)
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
// The baseline snapshot is cloned into working/ on every open so that
// PebbleDB writes never mutate snapshot directories. On first run,
// existing flat DB directories are migrated into a snapshot.
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

	if err := s.loadGlobalMetadata(dbs.metadata); err != nil {
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

// rawDBs holds the five raw pebble handles between opening them and handing them to the stores.
type rawDBs struct {
	account  seidbtypes.KeyValueDB
	code     seidbtypes.KeyValueDB
	storage  seidbtypes.KeyValueDB
	misc     seidbtypes.KeyValueDB
	metadata seidbtypes.KeyValueDB
}

// close closes every handle, joining whatever errors come back.
func (d rawDBs) close() error {
	return errors.Join(
		closeDB(accountDBDir, d.account),
		closeDB(codeDBDir, d.code),
		closeDB(storageDBDir, d.storage),
		closeDB(miscDBDir, d.misc),
		closeDB(metadataDir, d.metadata),
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

// openRawDBs opens the five pebble instances. The caller owns them until the stores take over; on
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
	if dbs.metadata, err = s.openPebbleDB(&s.config.MetadataDBConfig); err != nil {
		return dbs, fmt.Errorf("failed to open metadata DB: %w", err)
	}
	return dbs, nil
}

// loadLocalMeta reads each data database's persisted metadata into localMeta.
func (s *CommitStore) loadLocalMeta(dbs rawDBs) error {
	s.localMeta = make(map[string]*ktype.LocalMeta)
	for dir, db := range map[string]seidbtypes.KeyValueDB{
		accountDBDir: dbs.account,
		codeDBDir:    dbs.code,
		storageDBDir: dbs.storage,
		miscDBDir:    dbs.misc,
	} {
		meta, err := loadLocalMeta(db)
		if err != nil {
			return fmt.Errorf("failed to load %s local meta: %w", dir, err)
		}
		s.localMeta[dir] = meta
	}
	return nil
}

// openStores wraps the five already-open PebbleDBs in snapshot engines. It is the last step of opening a
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
	open := func(cfg *snapshot.SnapshotEngineConfig, db seidbtypes.KeyValueDB) (snapshot.SnapshotEngine, error) {
		store, storeErr := snapshot.NewSnapshotEngine(cfg, db, s.readPool, s.miscPool)
		if storeErr != nil {
			return nil, fmt.Errorf("failed to create %s snapshot store: %w", cfg.Name, storeErr)
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

	s.metadataStore, err = open(&s.config.MetadataStoreConfig, dbs.metadata)
	if err != nil {
		return err
	}

	s.stores = []snapshot.SnapshotEngine{
		s.accountStore, s.codeStore, s.storageStore, s.miscStore, s.metadataStore,
	}

	if !s.readOnly {
		if err := s.sealBaseline(); err != nil {
			return err
		}
		// Both are built here and only here, so neither outlives the stores it reads. closeStores drains them
		// before those stores go away.
		s.hasher = newBlockHasher(
			s.ctx,
			s.hashSeed,
			s.ltCalc,
			s.miscPool,
			s.config.HashQueueSize,
			s.config.HashChanSize,
		)
		s.snapshotWriter = newSnapshotWriter(
			s.ctx,
			s.snapshotLayout(),
			s.config.SnapshotInterval,
			s.config.MaxSnapshotLagBlocks,
			s.checkpointables(),
		)
	}

	return nil
}

// checkpointables returns the handle each database is checkpointed through, keyed by database
// directory name. Captured once while the stores exist, so a snapshot being written off-thread never
// has to reach back into the store for a handle that teardown may have cleared.
func (s *CommitStore) checkpointables() map[string]seidbtypes.Checkpointable {
	dbs := make(map[string]seidbtypes.Checkpointable, len(snapshotDBDirs))
	for _, name := range snapshotDBDirs {
		if db, ok := s.rawDBFor(name).(seidbtypes.Checkpointable); ok {
			dbs[name] = db
		}
	}
	return dbs
}

// rawDBFor returns the raw database behind the named store, bypassing every guarantee the store
// provides. Apply intense scrutiny at every call site.
//
// It exists for the operations that must address a database as a file rather than as a key-value store —
// taking a Pebble checkpoint — and for the bootstrap writes that seed a fresh store's metadata before it
// has committed anything. Reading data through it is a bug: it sees only what the flusher has written,
// missing both staged and finalized-but-unflushed rows, silently. Use Get/BatchGet/Iterator instead.
//
// The handles live on CommitStore only until the stores exist: openStores gives each one to its store,
// which owns and closes it from then on, and clears the field. A raw access after that is a nil
// dereference rather than a silent read of a version nobody asked for, which is the point of routing
// every such access through here.
//
// Returns nil before the stores exist; callers in that window hold the handles directly.
func (s *CommitStore) rawDBFor(name string) seidbtypes.KeyValueDB {
	switch name {
	case accountDBDir:
		if s.accountStore != nil {
			return s.accountStore.EscapeHatchUnderlyingDB()
		}
	case codeDBDir:
		if s.codeStore != nil {
			return s.codeStore.EscapeHatchUnderlyingDB()
		}
	case storageDBDir:
		if s.storageStore != nil {
			return s.storageStore.EscapeHatchUnderlyingDB()
		}
	case miscDBDir:
		if s.miscStore != nil {
			return s.miscStore.EscapeHatchUnderlyingDB()
		}
	case metadataDir:
		if s.metadataStore != nil {
			return s.metadataStore.EscapeHatchUnderlyingDB()
		}
	}
	return nil
}

// closeStores tears down whichever stores exist and clears them, so a store that is being reopened
// (rollback, restore) does not keep stores pointed at closed databases. Errors are joined rather
// than short-circuited: every store must be given its chance to stop.
func (s *CommitStore) closeStores() error {
	var errs []error

	// The hasher stops before the writer, because the writer can be waiting for a block to flush and only
	// finalization by the hasher makes that possible. Draining the other way round hangs teardown.
	if s.hasher != nil {
		if err := s.hasher.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close block hasher: %w", err))
		}
		s.hasher = nil
	}

	// The writer must stop before anything below runs: closing a store closes the database it owns, and
	// a checkpoint in progress would then be reading a closed handle. This is the choke point every
	// teardown path reaches — Close directly, Rollback and resetForImport through closeDBsOnly — so the
	// guard lives here rather than at each of them.
	if s.snapshotWriter != nil {
		if err := s.snapshotWriter.Close(); err != nil {
			errs = append(errs, fmt.Errorf("close snapshot writer: %w", err))
		}
		s.snapshotWriter = nil
	}

	// Hand back the reservations on the last sealed block and forget the handles. They belong to the
	// stores being torn down here, so keeping them would leave a reopened store (rollback, restore)
	// awaiting a flush on snapshots whose store is already gone.
	if err := s.releaseLastSealed(); err != nil {
		errs = append(errs, fmt.Errorf("release sealed snapshots: %w", err))
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
	s.metadataStore = nil
	s.stores = nil
	return errors.Join(errs...)
}

// computeStoreHeights reports the height each database actually reached on disk, plus the lowest of
// them — the block replay has to start from. The heights are nil when all five agree, since then there
// is nothing to catch up.
//
// Must run after each database's LocalMeta and the metadata database's committed version have been
// read off pebble, and before the stores exist.
func (s *CommitStore) computeStoreHeights() (map[string]int64, int64) {
	heights := make(map[string]int64, len(dataDBDirs)+1)
	lowest := s.committedVersion
	for _, dir := range dataDBDirs {
		height := int64(0)
		if meta := s.localMeta[dir]; meta != nil {
			height = meta.CommittedVersion
		}
		heights[dir] = height
		if height < lowest {
			lowest = height
		}
	}
	// The metadata database records the store-wide committed version, so that is its own height.
	heights[metadataDir] = s.committedVersion

	// The heights are worth recording whenever any two disagree, in either direction: a database can be
	// behind the store-wide version (its flush never landed) or ahead of it (its flush landed and the
	// metadata database's did not). Only when all five agree is there nothing to catch up.
	for _, height := range heights {
		if height != lowest {
			logger.Info("FlatKV stores are at different heights; replay will catch them up",
				"lowest", lowest, "storeWide", s.committedVersion, "perDB", heights)
			return heights, lowest
		}
	}
	return nil, lowest
}

// loadGlobalMetadata reads the store-wide records out of the metadata database: committed version, root
// LtHash, and the height this store's history begins at.
//
// The version and LtHash read here can disagree, and only do so when a previous startup recovery was
// interrupted. No hash reads either value, and the first seal replaces both. See changedValuesByStore
// for the invariant this depends on.
func (s *CommitStore) loadGlobalMetadata(metaDB seidbtypes.KeyValueDB) error {
	globalVersion, err := loadGlobalVersion(metaDB)
	if err != nil {
		return fmt.Errorf("failed to load global version: %w", err)
	}
	s.committedVersion = globalVersion

	earliestVersion, err := loadGlobalEarliestVersion(metaDB)
	if err != nil {
		return fmt.Errorf("failed to load global earliest version: %w", err)
	}
	s.earliestVersion = earliestVersion

	globalLtHash, err := loadGlobalLtHash(metaDB)
	if err != nil {
		return fmt.Errorf("failed to load global LtHash: %w", err)
	}
	if globalLtHash != nil {
		s.committedLtHash = globalLtHash
	} else {
		s.committedLtHash = lthash.New()
	}
	rootChecksum := s.committedLtHash.Checksum()
	s.hashSeed = hasherSeed{
		perDBLtHash:       make(map[string]*lthash.LtHash, len(dataDBDirs)),
		perDBModuleLtHash: newPerDBModuleLtHashMap(),
		perDBModuleStats:  newPerDBModuleStatsMap(),
	}

	// Load per-DB LtHashes from each DB's LocalMeta (already loaded by loadLocalMeta).
	// If any DB's version is behind the global version (partial commit or
	// corruption), lower committedVersion so catchup replays from there.
	for _, dbDir := range dataDBDirs {
		meta := s.localMeta[dbDir]
		if err := validatePerModuleMetadata(dbDir, meta); err != nil {
			return err
		}
		if meta != nil && meta.LtHash != nil {
			s.hashSeed.perDBLtHash[dbDir] = meta.LtHash.Clone()
		} else {
			s.hashSeed.perDBLtHash[dbDir] = lthash.New()
		}
		if meta != nil {
			s.hashSeed.perDBModuleLtHash[dbDir] = cloneModuleHashes(meta.ModuleLtHashes)
			s.hashSeed.perDBModuleStats[dbDir] = cloneModuleStats(meta.ModuleStats)
		} else {
			s.hashSeed.perDBModuleLtHash[dbDir] = make(map[string]*lthash.LtHash)
			s.hashSeed.perDBModuleStats[dbDir] = make(map[string]lthash.ModuleStats)
		}
		if meta != nil && meta.CommittedVersion < s.committedVersion {
			logger.Warn("DB LocalMeta version behind global version, will catchup",
				"db", dbDir,
				"localVersion", meta.CommittedVersion,
				"globalVersion", s.committedVersion)
			s.committedVersion = meta.CommittedVersion
		}
	}

	// Published before the first block is hashed, so a reader has an answer at the height the store loaded.
	s.hashSeed.committed = BlockHash{Hash: rootChecksum[:], BlockHeight: s.committedVersion}

	return nil
}

func (s *CommitStore) Version() int64 {
	return s.committedVersion
}

// PendingVersion returns s.pendingBlockHeight: the height of the block currently
// buffered by ApplyChangeSets, or 0 when there are no buffered writes.
func (s *CommitStore) PendingVersion() int64 {
	return s.pendingBlockHeight
}

// commitPendingBlock commits the block currently being applied, if any. It is a no-op on a store with
// no pending writes, which is every store between blocks and every read-only store.
func (s *CommitStore) commitPendingBlock() error {
	s.mu.RLock()
	pending := s.pendingBlockHeight
	s.mu.RUnlock()

	if pending == 0 || s.readOnly {
		return nil
	}
	_, err := s.Commit(pending)
	return err
}

// CommitPendingBlock commits the block currently being applied, if any, so that it has a hash. It is a no-op
// on a store with no pending writes, which is every store between blocks and every read-only store.
//
// This exists for Cosmos, which asks for a block's hash before it calls Commit. A block that has not been
// committed has no hash — the hash is computed from the snapshots a commit produces — so a caller wanting one
// mid-block is asking for the block to be committed, and this is that request made explicitly. Committing
// early is safe there because every one of the block's writes has already arrived: rootmulti's
// GetWorkingHash flushes every buffered changeset into this store before it reads the hash, and the Commit
// that follows finds the block already committed and does nothing.
//
// Post-Cosmos this goes away along with rootmulti: a single call will supply a block's writes and commit
// them, and nothing will ask for a hash mid-block.
func (s *CommitStore) CommitPendingBlock() error {
	return s.commitPendingBlock()
}

// HashChan implements Store.
func (s *CommitStore) HashChan() <-chan BlockHash {
	if s.hasher == nil {
		// A read-only store never commits, so it never produces a hash. A closed channel reports that
		// immediately rather than leaving a consumer waiting for a block that will not come.
		empty := make(chan BlockHash)
		close(empty)
		return empty
	}
	return s.hasher.HashChan()
}

// PublishedHash implements Store.
func (s *CommitStore) PublishedHash() BlockHash {
	if s.hasher != nil {
		return s.hasher.Published()
	}
	checksum := s.committedLtHash.Checksum()
	return BlockHash{Hash: checksum[:], BlockHeight: s.committedVersion}
}

// EarliestVersion implements Store.
func (s *CommitStore) EarliestVersion() int64 {
	return s.earliestVersion
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
	// The importer's workers accumulate on top of the hashes the hasher is carrying, so read them here where
	// the error can be returned.
	seed, err := s.hasher.Seed()
	if err != nil {
		return nil, fmt.Errorf("read hash state for import: %w", err)
	}
	return NewKVImporter(s, version, seed), nil
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
	s.committedLtHash = lthash.New()

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
	w, err := statewal.New(stateWALConfig(&s.config))
	if err != nil {
		return fmt.Errorf("open state WAL: %w", err)
	}
	s.wal = w
	return nil
}

func (s *CommitStore) GetPhaseTimer() *metrics.PhaseTimer {
	return s.phaseTimer
}
