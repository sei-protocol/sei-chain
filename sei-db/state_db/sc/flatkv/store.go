package flatkv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/zbiljic/go-filelock"
	"go.opentelemetry.io/otel/attribute"

	commonerrors "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/common/threading"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/dbcache"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
	"github.com/sei-protocol/seilog"
)

var logger = seilog.NewLogger("db", "state-db", "sc", "flatkv")

const (
	// Top-level directory names
	flatkvRootDir = "flatkv"
	changelogDir  = "changelog"
	lockFileName  = "LOCK"

	// DB subdirectories (inside each snapshot)
	accountDBDir = "account"
	codeDBDir    = "code"
	storageDBDir = "storage"
	miscDBDir    = "misc"
	metadataDir  = "metadata"

	// Suffixes for atomic directory operations
	tmpSuffix      = "-tmp"
	removingSuffix = "-removing"

	readOnlyDirPrefix = "readonly-"

	flatkvMeterName = "seidb_flatkv"
)

// dataDBDirs lists all data DB directory names (used for per-DB LtHash iteration).
var dataDBDirs = []string{accountDBDir, codeDBDir, storageDBDir, miscDBDir}

// InitializeDataDirectories sets the DataDir for each nested PebbleDB config
// that does not already have one, using DataDir as the base path. The DBs live
// under the working directory: <DataDir>/working/<subdir>.
func InitializeDataDirectories(c *config.Config) {
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

// CommitStore implements flatkv.Store for EVM state storage.
//
// Concurrency: writes (ApplyChangeSets, Commit) and the reads that touch the
// pending-writes maps (Get, Has, GetBlockHeightModified) and iterator
// construction (Iterator, RawGlobalIterator) are guarded by mu. Iterators
// snapshot their data at construction time (pending writes are cloned and the
// Pebble view is pinned), so once built they may be used and Closed without
// holding mu and may safely outlive a subsequent ApplyChangeSets/Commit. All
// other lifecycle operations (LoadVersion, Rollback, snapshot/import/export,
// Close) must still be serialized by the caller.
type CommitStore struct {
	// mu guards the pending-writes maps against concurrent iterator
	// construction / reads while ApplyChangeSets and Commit mutate them.
	//
	// TODO(concurrency): this is a coarse lock taken at the exported entry
	// points. Commit in particular holds the write lock across its WAL fsync
	// and periodic auto-snapshot. That is acceptable while commits are not
	// pipelined with reads; revisit with a finer-grained scheme (guarding only
	// the in-memory maps) if/when pipelining is introduced.
	mu sync.RWMutex

	ctx    context.Context
	cancel context.CancelFunc
	config config.Config
	dbDir  string

	// Five separate PebbleDB instances.
	// Physical key format: "module/" + type_prefix + stripped_key.
	metadataDB seidbtypes.KeyValueDB // Global version + LtHash watermark
	accountDB  seidbtypes.KeyValueDB // "evm/"+0x0a+addr(20) → vtype.AccountData
	codeDB     seidbtypes.KeyValueDB // "evm/"+0x07+addr(20) → vtype.CodeData
	storageDB  seidbtypes.KeyValueDB // "evm/"+0x03+addr(20)||slot(32) → vtype.StorageData
	miscDB     seidbtypes.KeyValueDB // "module/"+key → vtype.MiscData

	// Per-DB committed version, keyed by DB dir name (e.g. accountDBDir).
	localMeta map[string]*ktype.LocalMeta

	// LtHash state for integrity checking
	committedVersion int64
	committedLtHash  *lthash.LtHash
	workingLtHash    *lthash.LtHash

	// earliestVersion is the version this store's history begins at, as
	// recorded by SetInitialVersion (the seeded version). 0 when unknown:
	// genesis stores and stores created before the record existed. See
	// EarliestVersion.
	earliestVersion int64

	// Per-DB working LTHash tracking. Authoritative copies live in each
	// DB's LocalMeta (atomically committed with data). On startup the
	// working hashes are loaded from LocalMeta.
	perDBWorkingLtHash map[string]*lthash.LtHash

	// Per-DB, per-module working LtHash: dbDir -> module name -> hash.
	// The per-DB root (perDBWorkingLtHash[dir]) is the homomorphic sum of
	// the module hashes here. account/code/storage DBs only ever carry the
	// "evm" module; miscDB may carry several (evm plus cosmos modules).
	// Persisted alongside the per-DB root in each DB's LocalMeta and reloaded
	// on startup. This is bookkeeping metadata only: it does not feed the
	// global evm_lattice/AppHash.
	perDBModuleWorkingLtHash map[string]map[string]*lthash.LtHash

	// Per-DB, per-module working stats: dbDir -> module name -> key-count /
	// byte totals. Accumulated alongside perDBModuleWorkingLtHash using the
	// same key-membership rule, persisted in each DB's LocalMeta, and reloaded
	// on startup. Consensus-irrelevant bookkeeping; per-DB / global totals are
	// derived on demand.
	perDBModuleWorkingStats map[string]map[string]lthash.ModuleStats

	// Pending writes buffer
	accountWrites map[string]*vtype.AccountData
	codeWrites    map[string]*vtype.CodeData
	storageWrites map[string]*vtype.StorageData
	miscWrites    map[string]*vtype.MiscData

	// The state WAL. Injected at construction: non-nil ⇒ FlatKV writes/replays/prunes it; nil ⇒ the outer
	// context owns the whole WAL pipeline and FlatKV no-ops every WAL operation. FlatKV owns Close of whatever
	// instance it currently holds (rollback/restore may replace it via close→prune/delete→reopen); its
	// lifecycle is decoupled from the DB open/close cycle, so closeDBsOnly does not touch it.
	wal statewal.StateWAL

	// Whether FlatKV manages a WAL (a non-nil instance was injected at construction). It records the intent
	// even across a Close that nil-es the live instance, so import reset can reconstruct the WAL rather than
	// mistaking a closed-but-owned WAL for the nil "outer context owns it" case.
	manageWAL bool

	// Changes to feed into the WAL at the next commit.
	pendingChangeSets []*proto.NamedChangeSet

	// pendingBlockHeight is the version stamped by the current buffered
	// ApplyChangeSets. 0 means no pending apply. Commit requires version to
	// match when this is non-zero.
	pendingBlockHeight int64

	lastSnapshotTime time.Time

	// File lock prevents multiple processes from opening the same DB.
	fileLock filelock.TryLockerSafe

	phaseTimer *metrics.PhaseTimer

	// readOnly marks stores opened via LoadVersion(..., true).
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

var _ Store = (*CommitStore)(nil)

// dataDBs returns the four data PebbleDB instances in fixed iteration order:
// accountDB, codeDB, storageDB, miscDB. metadataDB is excluded.
func (s *CommitStore) dataDBs() []seidbtypes.KeyValueDB {
	return []seidbtypes.KeyValueDB{s.accountDB, s.codeDB, s.storageDB, s.miscDB}
}

type namedDB struct {
	dir string
	db  seidbtypes.KeyValueDB
}

// namedDataDBs returns the four data DBs paired with their directory names.
func (s *CommitStore) namedDataDBs() []namedDB {
	return []namedDB{
		{accountDBDir, s.accountDB},
		{codeDBDir, s.codeDB},
		{storageDBDir, s.storageDB},
		{miscDBDir, s.miscDB},
	}
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
		return s.miscDB, nil
	}
	kind, _ := keys.ParseEVMKey(innerKey)
	switch kind {
	case ktype.EVMKeyAccount, keys.EVMKeyCodeHash:
		return s.accountDB, nil
	case keys.EVMKeyCode:
		return s.codeDB, nil
	case keys.EVMKeyStorage:
		return s.storageDB, nil
	default:
		return s.miscDB, nil
	}
}

// NewCommitStore creates a new (unopened) FlatKV commit store.
// Call LoadVersion to open and initialize.
func NewCommitStore(
	ctx context.Context,
	cfg *config.Config,
	stateWAL statewal.StateWAL,
) (*CommitStore, error) {

	InitializeDataDirectories(cfg)

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
		ctx:                      ctx,
		cancel:                   cancel,
		config:                   *cfg,
		localMeta:                make(map[string]*ktype.LocalMeta),
		accountWrites:            make(map[string]*vtype.AccountData),
		codeWrites:               make(map[string]*vtype.CodeData),
		storageWrites:            make(map[string]*vtype.StorageData),
		miscWrites:               make(map[string]*vtype.MiscData),
		pendingChangeSets:        make([]*proto.NamedChangeSet, 0),
		committedLtHash:          lthash.New(),
		workingLtHash:            lthash.New(),
		perDBWorkingLtHash:       make(map[string]*lthash.LtHash),
		perDBModuleWorkingLtHash: newPerDBModuleLtHashMap(),
		perDBModuleWorkingStats:  newPerDBModuleStatsMap(),
		phaseTimer:               metrics.NewPhaseTimer(flatkvMeter, "seidb_main_thread"),
		readPool:                 readPool,
		miscPool:                 miscPool,
		ltHashPool:               ltHashPool,
		ltCalc:                   ltCalc,
		wal:                      stateWAL,
		manageWAL:                stateWAL != nil,
	}, nil
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
	s.ltCalc = lthash.NewHashCalculator(s.ltHashPool, dataDBDirs, moduleOfKey)
}

func (s *CommitStore) flatkvDir() string {
	return s.config.DataDir
}

var errReadOnly = errors.New("flatkv: store is read-only")

// LoadVersion opens the database at the given version (0 = latest).
// When readOnly is true an isolated, read-only CommitStore is returned;
// the caller must Close it when done.
func (s *CommitStore) LoadVersion(targetVersion int64, readOnly bool) (opened Store, retErr error) {
	logger.Info("FlatKV LoadVersion", "targetVersion", targetVersion, "readOnly", readOnly)
	obs := s.observeOp("LoadVersion", otelMetrics.OpenLatency,
		"targetVersion", targetVersion, "readOnly", readOnly).
		withAttrs(attribute.Bool("read_only", readOnly))
	defer obs.done(&retErr, func() {
		version := s.committedVersion
		if opened != nil {
			version = opened.Version()
		}
		if !readOnly {
			otelMetrics.CurrentVersion.Record(s.ctx, s.committedVersion)
		}
		logger.Info("FlatKV LoadVersion complete",
			"targetVersion", targetVersion,
			"readOnly", readOnly,
			"version", version,
			"elapsed", obs.elapsed())
	})

	if readOnly {
		if s.readOnly {
			return nil, errReadOnly
		}
		return s.loadVersionReadOnly(targetVersion)
	}

	_ = s.closeDBsOnly()

	dir := s.flatkvDir()

	// Track whether we acquire the lock in this call so we can release it
	// on any error path (open() won't track a pre-held lock).
	lockHeldBefore := s.fileLock != nil
	defer func() {
		if retErr != nil && !lockHeldBefore && s.fileLock != nil {
			_ = s.fileLock.Unlock()
			s.fileLock = nil
		}
	}()

	if targetVersion > 0 {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return nil, fmt.Errorf("create flatkv dir: %w", err)
		}
		// Acquire lock before mutating the current symlink to prevent
		// a race with another process observing an unintended baseline.
		if s.fileLock == nil {
			if err := s.acquireFileLock(dir); err != nil {
				return nil, err
			}
		}
		if baseVer, err := seekSnapshot(dir, targetVersion); err == nil {
			if err := updateCurrentSymlink(dir, snapshotName(baseVer)); err != nil {
				return nil, fmt.Errorf("update current symlink for target version %d: %w", targetVersion, err)
			}
		} else {
			logger.Debug("no snapshot found, will open current", "target", targetVersion, "err", err)
		}
		// Force a fresh working dir clone: the working dir may contain data
		// beyond targetVersion from a previous open-to-latest.
		_ = os.Remove(filepath.Join(dir, workingDirName, snapshotBaseFile))
	}

	if err := s.openTo(targetVersion); err != nil {
		return nil, fmt.Errorf("failed to open FlatKV store: %w", err)
	}

	if targetVersion > 0 && s.committedVersion != targetVersion {
		_ = s.closeDBsOnly()
		return nil, fmt.Errorf("FlatKV version mismatch: requested %d, reached %d",
			targetVersion, s.committedVersion)
	}

	return s, nil
}

// loadVersionReadOnly creates an isolated, read-only CommitStore at the
// requested version. If the writer lock has not yet been acquired (e.g. the
// store was freshly constructed), CleanupOrphanedReadOnlyDirs is called
// lazily to acquire it and clean up any leftover directories. When the lock
// is acquired lazily, ownership is transferred to the returned clone so that
// closing the clone releases it; this prevents leaking the lock when the
// caller never explicitly closes the parent store.
func (s *CommitStore) loadVersionReadOnly(targetVersion int64) (_ Store, retErr error) {
	lazyLock := s.fileLock == nil
	if lazyLock {
		if err := s.CleanupOrphanedReadOnlyDirs(); err != nil {
			return nil, fmt.Errorf("loadVersionReadOnly: pre-init cleanup: %w", err)
		}
	}

	ro, err := NewCommitStore(context.Background(), &s.config, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create readonly store: %w", err)
	}

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

	// Transfer the lazily-acquired lock to the clone so that ro.Close()
	// releases it, preventing a leak when the parent is never closed.
	if lazyLock && s.fileLock != nil {
		ro.fileLock = s.fileLock
		s.fileLock = nil
	}

	defer func() {
		if retErr != nil {
			if closeErr := ro.Close(); closeErr != nil {
				logger.Error("failed to close readonly store during error cleanup", "err", closeErr)
			}
		}
	}()

	if err := ro.openReadOnly(targetVersion); err != nil {
		return nil, fmt.Errorf("readonly open: %w", err)
	}

	// The clone is open at a snapshot boundary with a nil WAL. Replay this (primary) store's WAL into it up
	// to targetVersion so it reflects the exact requested height. The clone is not yet marked read-only, so
	// the replay's ApplyChangeSets calls are permitted; mark it read-only only once replay succeeds.
	if err := s.replayInto(ro, targetVersion); err != nil {
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

// replayInto replays this store's WAL into a read-only clone, advancing the clone from the snapshot
// boundary it opened at up to targetVersion (or this store's latest WAL block when targetVersion <= 0). It
// exists because the clone has a nil WAL: the primary owns the WAL, so the primary reads it and feeds each
// block into the clone via applyAndCommit (which never touches a WAL).
//
// The WAL must still reach back to the block after the clone's snapshot boundary. If it begins later than
// that, the blocks the clone needs are gone and this fails rather than serving a clone with a hole in it.
//
// Concurrency: export runs in a background goroutine while this (primary) store may still be committing.
// The iterator is constructed under s.mu, serializing against a concurrent Commit's WAL-wrapper access;
// iteration then proceeds lock-free, because a seiwal iterator reads a consistent point-in-time (hard-link)
// snapshot that concurrent appends/prunes cannot disturb. A concurrent Commit is the only overlap this
// tolerates: closing the primary while an export is in flight is not permitted (see Close).
func (s *CommitStore) replayInto(clone *CommitStore, targetVersion int64) (retErr error) {
	if s.wal == nil {
		// nil WAL: the outer context owns the pipeline, so no between-snapshot replay is available here. The
		// clone can only serve the snapshot boundary it opened at.
		if targetVersion > 0 && clone.committedVersion != targetVersion {
			return fmt.Errorf("readonly: nil WAL cannot replay to version %d (opened at %d)",
				targetVersion, clone.committedVersion)
		}
		return nil
	}

	s.mu.Lock()
	ok, first, last, err := s.wal.GetStoredRange()
	if err != nil {
		s.mu.Unlock()
		return fmt.Errorf("readonly: WAL range: %w", err)
	}
	if !ok {
		s.mu.Unlock()
		return nil // empty WAL: nothing to replay
	}

	// Replay from the block after the snapshot boundary the clone opened at. A clone at version 0 has no
	// history behind it, so the WAL's first block is where this store's history begins rather than a gap —
	// the same reasoning as catchup, which this must match or a store whose first block is above 1 (chain
	// initial_height > 1) would be rejected as gapped.
	start := first
	if clone.committedVersion > 0 {
		start = uint64(clone.committedVersion) + 1 //nolint:gosec // committedVersion > 0 checked above
	}
	end := last
	if targetVersion > 0 && uint64(targetVersion) < end {
		end = uint64(targetVersion)
	}
	if end < start {
		s.mu.Unlock()
		return nil // clone already at or beyond target
	}
	if first > start {
		// We are about to replay, but the primary's WAL no longer reaches back to this clone's snapshot, so
		// the blocks the clone needs are gone. Only the export fails — the primary's own state is untouched.
		s.mu.Unlock()
		return fmt.Errorf("readonly: WAL starts at block %d but the clone needs block %d: "+
			"blocks %d-%d are missing (data loss or corruption)", first, start, start, first-1)
	}
	it, err := s.wal.Iterator(start, end)
	s.mu.Unlock()
	if err != nil {
		return fmt.Errorf("readonly: WAL iterator [%d,%d]: %w", start, end, err)
	}
	defer func() {
		if cerr := it.Close(); cerr != nil && retErr == nil {
			retErr = fmt.Errorf("readonly: close WAL iterator: %w", cerr)
		}
	}()

	for {
		hasNext, nErr := it.Next()
		if nErr != nil {
			return fmt.Errorf("readonly: WAL iterate: %w", nErr)
		}
		if !hasNext {
			break
		}
		block, changesets := it.Entry()
		if err := clone.applyAndCommit(int64(block), changesets); err != nil { //nolint:gosec // block <= end
			return fmt.Errorf("readonly: replay block %d: %w", block, err)
		}
	}
	return nil
}

// openReadOnly opens PebbleDBs in readOnlyWorkDir at the snapshot boundary at or below targetVersion,
// leaving committedVersion at that snapshot version. It never modifies the global "current" symlink.
//
// This clone has a nil WAL of its own, so it does NOT replay: advancing from the snapshot boundary up to
// targetVersion — and marking the store read-only — is driven by the primary via loadVersionReadOnly /
// replayInto, which feeds the primary's WAL into this clone.
func (s *CommitStore) openReadOnly(targetVersion int64) error {
	s.clearPendingWrites()

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

	if err := s.openDBs(s.readOnlyWorkDir); err != nil {
		return err
	}

	if err := s.loadGlobalMetadata(); err != nil {
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
	return s.catchup(catchupTarget)
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
	s.clearPendingWrites()

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

	if err := s.openDBs(workDir); err != nil {
		return err
	}

	if err := s.loadGlobalMetadata(); err != nil {
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

// openPebbleDB creates the directory at cfg.DataDir and opens a PebbleDB instance.
func (s *CommitStore) openPebbleDB(cfg *pebbledb.PebbleDBConfig, cacheCfg *dbcache.CacheConfig) (seidbtypes.KeyValueDB, error) {
	if err := os.MkdirAll(cfg.DataDir, 0750); err != nil {
		return nil, fmt.Errorf("create directory %s: %w", cfg.DataDir, err)
	}
	db, err := pebbledb.OpenWithCache(s.ctx, cfg, cacheCfg, s.readPool, s.miscPool)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", cfg.DataDir, err)
	}
	return db, nil
}

// openDBs opens all PebbleDBs from dbDir. On failure all already-opened handles are closed.
//
// It does not touch the WAL: the WAL is injected at construction and its lifecycle is decoupled from the
// DB open/close cycle (it must survive LoadVersion/Rollback DB reopens), so it is neither opened nor
// cleared here.
func (s *CommitStore) openDBs(dbDir string) (retErr error) {

	var toClose []io.Closer
	defer func() {
		if retErr != nil {
			for _, c := range toClose {
				_ = c.Close()
			}
			s.metadataDB = nil
			s.accountDB = nil
			s.codeDB = nil
			s.storageDB = nil
			s.miscDB = nil
			s.localMeta = make(map[string]*ktype.LocalMeta)
		}
	}()

	var err error
	s.accountDB, err = s.openPebbleDB(&s.config.AccountDBConfig, &s.config.AccountCacheConfig)
	if err != nil {
		return fmt.Errorf("failed to open account DB: %w", err)
	}
	toClose = append(toClose, s.accountDB)

	s.codeDB, err = s.openPebbleDB(&s.config.CodeDBConfig, &s.config.CodeCacheConfig)
	if err != nil {
		return fmt.Errorf("failed to open code DB: %w", err)
	}
	toClose = append(toClose, s.codeDB)

	s.storageDB, err = s.openPebbleDB(&s.config.StorageDBConfig, &s.config.StorageCacheConfig)
	if err != nil {
		return fmt.Errorf("failed to open storage DB: %w", err)
	}
	toClose = append(toClose, s.storageDB)

	s.miscDB, err = s.openPebbleDB(&s.config.MiscDBConfig, &s.config.MiscCacheConfig)
	if err != nil {
		return fmt.Errorf("failed to open misc DB: %w", err)
	}
	toClose = append(toClose, s.miscDB)

	s.metadataDB, err = s.openPebbleDB(&s.config.MetadataDBConfig, &s.config.MetadataCacheConfig)
	if err != nil {
		return fmt.Errorf("failed to open metadata DB: %w", err)
	}
	toClose = append(toClose, s.metadataDB)

	for _, ndb := range s.namedDataDBs() {
		meta, err := loadLocalMeta(ndb.db)
		if err != nil {
			return fmt.Errorf("failed to load %s local meta: %w", ndb.dir, err)
		}
		s.localMeta[ndb.dir] = meta
	}

	return nil
}

func (s *CommitStore) loadGlobalMetadata() error {
	globalVersion, err := s.loadGlobalVersion()
	if err != nil {
		return fmt.Errorf("failed to load global version: %w", err)
	}
	s.committedVersion = globalVersion

	earliestVersion, err := s.loadGlobalEarliestVersion()
	if err != nil {
		return fmt.Errorf("failed to load global earliest version: %w", err)
	}
	s.earliestVersion = earliestVersion

	globalLtHash, err := s.loadGlobalLtHash()
	if err != nil {
		return fmt.Errorf("failed to load global LtHash: %w", err)
	}
	if globalLtHash != nil {
		s.committedLtHash = globalLtHash
		s.workingLtHash = globalLtHash.Clone()
	} else {
		s.committedLtHash = lthash.New()
		s.workingLtHash = lthash.New()
	}

	// Load per-DB LtHashes from each DB's LocalMeta (already loaded in openDBs).
	// If any DB's version is behind the global version (partial commit or
	// corruption), lower committedVersion so catchup replays from there.
	for _, dbDir := range dataDBDirs {
		meta := s.localMeta[dbDir]
		if err := validatePerModuleMetadata(dbDir, meta); err != nil {
			return err
		}
		if meta != nil && meta.LtHash != nil {
			s.perDBWorkingLtHash[dbDir] = meta.LtHash.Clone()
		} else {
			s.perDBWorkingLtHash[dbDir] = lthash.New()
		}
		if meta != nil {
			s.perDBModuleWorkingLtHash[dbDir] = cloneModuleHashes(meta.ModuleLtHashes)
			s.perDBModuleWorkingStats[dbDir] = cloneModuleStats(meta.ModuleStats)
		} else {
			s.perDBModuleWorkingLtHash[dbDir] = make(map[string]*lthash.LtHash)
			s.perDBModuleWorkingStats[dbDir] = make(map[string]lthash.ModuleStats)
		}
		if meta != nil && meta.CommittedVersion < s.committedVersion {
			logger.Warn("DB LocalMeta version behind global version, will catchup",
				"db", dbDir,
				"localVersion", meta.CommittedVersion,
				"globalVersion", s.committedVersion)
			s.committedVersion = meta.CommittedVersion
		}
	}

	return nil
}

func (s *CommitStore) Version() int64 {
	return s.committedVersion
}

// PendingVersion returns s.pendingBlockHeight: the height stamped by the most
// recent ApplyChangeSets call since the last Commit, or 0 when there are no
// buffered writes.
func (s *CommitStore) PendingVersion() int64 {
	return s.pendingBlockHeight
}

// RootHash returns the Blake3-256 digest of the working LtHash.
func (s *CommitStore) RootHash() []byte {
	checksum := s.workingLtHash.Checksum()
	return checksum[:]
}

// CommittedRootHash returns the Blake3-256 digest of the last committed LtHash.
func (s *CommitStore) CommittedRootHash() []byte {
	checksum := s.committedLtHash.Checksum()
	return checksum[:]
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
	}
	if err := s.resetForImport(); err != nil {
		return nil, fmt.Errorf("reset store for import: %w", err)
	}
	return NewKVImporter(s, version), nil
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

	// Reset the WAL through the injected instance rather than removing its directory directly: the instance
	// is still open here, so a raw removal would strand it (and break its Close). resetWAL wipes it to a
	// clean empty log aligned with the import; it is a no-op when the outer context owns the WAL (nil).
	if err := s.resetWAL(); err != nil {
		return fmt.Errorf("resetForImport: reset WAL: %w", err)
	}

	// Reopen from a pristine empty state. open() will load metadata
	// from the empty DB (a no-op), then we reset in-memory state below.
	if err := s.open(); err != nil {
		return err
	}

	s.committedVersion = 0
	s.committedLtHash = lthash.New()
	s.workingLtHash = lthash.New()
	s.perDBWorkingLtHash = newPerDBLtHashMap()
	s.perDBModuleWorkingLtHash = newPerDBModuleLtHashMap()
	s.perDBModuleWorkingStats = newPerDBModuleStatsMap()

	return nil
}

// resetWAL discards this store's WAL and reopens an empty one, so a restore starts from a clean WAL aligned
// with the imported snapshot (called by resetForImport). Import bypasses the WAL, so any pre-existing WAL is
// stale relative to the imported version; under statewal's contiguity a stale non-empty WAL (a re-syncing
// node's old-chain entries) would reject the next commit. Wiping is a no-op on a fresh node and the fix on
// a re-sync.
//
// When the store does not manage a WAL (constructed with nil — the outer context owns the pipeline) this is
// a no-op. The instance may already be closed here, since rootmulti.Restore closes the store before
// importing; Config() stays readable across Close, and closing twice is a no-op. On failure the store keeps
// the closed instance rather than dropping to nil, so a later write fails loudly instead of being skipped.
func (s *CommitStore) resetWAL() error {
	if !s.manageWAL {
		return nil
	}
	cfg := s.wal.Config()
	if err := s.wal.Close(); err != nil {
		return fmt.Errorf("close WAL for reset: %w", err)
	}
	if err := statewal.Delete(cfg); err != nil {
		return fmt.Errorf("delete WAL for reset: %w", err)
	}
	w, err := statewal.New(cfg)
	if err != nil {
		return fmt.Errorf("reopen WAL after reset: %w", err)
	}
	s.wal = w
	return nil
}

func (s *CommitStore) GetPhaseTimer() *metrics.PhaseTimer {
	return s.phaseTimer
}
