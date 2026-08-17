package flatkv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

	// Suffixes for atomic directory operations
	tmpSuffix      = "-tmp"
	removingSuffix = "-removing"

	readOnlyDirPrefix = "readonly-"

	flatkvMeterName = "seidb_flatkv"
)

// dataDBDirs lists all data DB directory names (used for per-DB LtHash iteration).
var dataDBDirs = []string{accountDBDir, codeDBDir, storageDBDir, miscDBDir}

var _ Store = (*CommitStore)(nil)

// CommitStore implements flatkv.Store for EVM state storage.
//
// Concurrency: writes (ApplyChangeSets, Commit) and the reads that touch the
// pending-writes maps (Get, Has, GetBlockHeightModified) and iterator
// construction (Iterator, RawGlobalIterator) are guarded by mu. Iterators
// snapshot their data at construction time (pending writes are cloned and the
// Pebble view is pinned), so once built they may be used and Closed without
// holding mu and may safely outlive a subsequent ApplyChangeSets/Commit. All
// other lifecycle operations (LoadLatest, Rollback, snapshot/import/export,
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

	// Four separate PebbleDB instances.
	// Physical key format: "module/" + type_prefix + stripped_key.
	accountDB seidbtypes.KeyValueDB // "evm/"+0x0a+addr(20) → vtype.AccountData
	codeDB    seidbtypes.KeyValueDB // "evm/"+0x07+addr(20) → vtype.CodeData
	storageDB seidbtypes.KeyValueDB // "evm/"+0x03+addr(20)||slot(32) → vtype.StorageData
	miscDB    seidbtypes.KeyValueDB // "module/"+key → vtype.MiscData

	// Per-DB committed version, keyed by DB dir name (e.g. accountDBDir).
	localMeta map[string]*ktype.LocalMeta

	// LtHash state for integrity checking
	committedVersion int64
	committedLtHash  *lthash.LtHash
	workingLtHash    *lthash.LtHash

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

	// Changes to feed into the WAL at the next commit.
	pendingChangeSets []*proto.NamedChangeSet

	// pendingBlockHeight is the version stamped by the current buffered
	// ApplyChangeSets. 0 means no pending apply. Further ApplyChangeSets
	// calls and Commit both require version to match when this is non-zero:
	// only one block may be buffered per commit.
	pendingBlockHeight int64

	lastSnapshotTime time.Time

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

	// ltCalc encapsulates the lattice-hash pipeline (old-value reads, per-key
	// hashing, and worker-combine into final per-DB / per-module hashes) over
	// ltHashPool. The commit path is serialized by s.mu, so the calculator has
	// a single caller at a time.
	ltCalc *lthash.HashCalculator
}

// dataDBs returns the four data PebbleDB instances in fixed iteration order:
// accountDB, codeDB, storageDB, miscDB.
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

	workDir, err := os.MkdirTemp(ro.flatkvDir(), readOnlyDirPrefix)
	if err != nil {
		return nil, fmt.Errorf("create readonly temp dir: %w", err)
	}
	ro.readOnlyWorkDir = workDir

	ro.config.AccountDBConfig.DataDir = filepath.Join(workDir, accountDBDir)
	ro.config.CodeDBConfig.DataDir = filepath.Join(workDir, codeDBDir)
	ro.config.StorageDBConfig.DataDir = filepath.Join(workDir, storageDBDir)
	ro.config.MiscDBConfig.DataDir = filepath.Join(workDir, miscDBDir)

	// Transfer the lazily-acquired lock to the view so that ro.Close()
	// releases it, preventing a leak when this store is never closed.
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
//
// A store left mid-initialization by a crash is discarded and reopened once, so the caller can seed
// or restore it again rather than facing a store that can never open.
func (s *CommitStore) openTo(catchupTarget int64) error {
	if err := s.open(); err != nil {
		return err
	}
	interrupted, err := s.replayIntoMutableStore(catchupTarget)
	if err == nil {
		return nil
	}
	if !interrupted {
		return err
	}

	// Losing the working directory costs nothing here: checkDataDBAlignment established that no data DB
	// holds a key, so the working DBs contain nothing but the derived _meta/ cache (see ktype.IsMetaKey),
	// and rebuilding that over an empty DB is free.
	logger.Warn("FlatKV discarding an interrupted initialization", "reason", err)
	if err := s.discardInterruptedInitialization(); err != nil {
		return err
	}
	// The retry is deliberately not a loop. A discard that leaves the same state behind — a skew baked
	// into the snapshot the working dir is re-cloned from — must surface, not spin.
	_, err = s.replayIntoMutableStore(catchupTarget)
	return err
}

// discardInterruptedInitialization throws away the working directory and reopens from the snapshot the
// current symlink names, leaving the store as if it had never been initialized. Everything in the
// working directory is destroyed, so callers must first establish that no data DB holds a key.
// Snapshots are left in place.
func (s *CommitStore) discardInterruptedInitialization() error {
	if err := s.closeDBsOnly(); err != nil {
		return fmt.Errorf("flatkv: close before discarding interrupted initialization: %w", err)
	}
	// The snapshots can stay: an initialization that got far enough to write one had already finished
	// stamping every watermark, so a store reaching here has none of its own to discard.
	workDir := filepath.Join(s.flatkvDir(), workingDirName)
	if err := atomicRemoveDir(workDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("flatkv: remove %s while discarding interrupted initialization: %w",
			workingDirName, err)
	}
	if err := s.open(); err != nil {
		return fmt.Errorf("flatkv: reopen after discarding interrupted initialization: %w", err)
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

	for _, ndb := range s.namedDataDBs() {
		meta, err := loadLocalMeta(ndb.db)
		if err != nil {
			return fmt.Errorf("failed to load %s local meta: %w", ndb.dir, err)
		}
		s.localMeta[ndb.dir] = meta
	}

	return nil
}

// loadGlobalMetadata rebuilds the store's in-memory global state from the data
// DBs' metadata.
func (s *CommitStore) loadGlobalMetadata() error {
	if err := s.hydratePerDBState(); err != nil {
		return err
	}
	s.deriveGlobalState()
	return nil
}

// hydratePerDBState populates the working per-DB and per-module hash state from
// each data DB's LocalMeta. It rejects a DB whose per-module hashes do not sum
// to its recorded root.
func (s *CommitStore) hydratePerDBState() error {
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
	}
	return nil
}

// deriveGlobalState sets the committed version to the lowest version any data DB
// reached and the committed LtHash to the homomorphic sum of their roots.
func (s *CommitStore) deriveGlobalState() {
	version := int64(math.MaxInt64)
	global := lthash.New()
	for _, dbDir := range dataDBDirs {
		global.MixIn(s.perDBWorkingLtHash[dbDir])
		if meta := s.localMeta[dbDir]; meta != nil && meta.CommittedVersion < version {
			version = meta.CommittedVersion
		}
	}
	if version == math.MaxInt64 {
		version = 0
	}

	for _, dbDir := range dataDBDirs {
		if meta := s.localMeta[dbDir]; meta != nil && meta.CommittedVersion > version {
			logger.Warn("data DB versions disagree, catchup will replay from the lowest",
				"db", dbDir,
				"localVersion", meta.CommittedVersion,
				"storeVersion", version)
		}
	}

	s.committedVersion = version
	s.committedLtHash = global
	s.workingLtHash = global.Clone()
}

// checkDataDBAlignment reports whether every data DB sits at the store's committed version. The
// condition holds once catchup has run; before then the DBs may legally disagree.
//
// The three outcomes are distinct:
//   - (false, nil) — aligned.
//   - (true, err) — the DBs disagree and none of them holds data, so an initialization was
//     interrupted. SetInitialVersion and FinalizeImport each stamp the four watermarks
//     non-atomically, and those versions never entered the WAL, so no replay can reconcile them.
//     The caller may discard the store instead of surfacing err.
//   - (false, err) — the DBs disagree while data is present, or the check itself failed. err is for
//     the operator.
func (s *CommitStore) checkDataDBAlignment() (interruptedInitialization bool, err error) {
	misaligned := make([]string, 0, len(dataDBDirs))
	for _, dbDir := range dataDBDirs {
		meta := s.localMeta[dbDir]
		if meta == nil {
			return false, fmt.Errorf("flatkv: %s has no local metadata after load", dbDir)
		}
		if meta.CommittedVersion != s.committedVersion {
			misaligned = append(misaligned, fmt.Sprintf("%s at %d", dbDir, meta.CommittedVersion))
		}
	}
	if len(misaligned) == 0 {
		return false, nil
	}

	populated, err := s.populatedDataDBs()
	if err != nil {
		return false, err
	}
	if len(populated) == 0 {
		return true, fmt.Errorf("flatkv: store is at %d but %s, and no data DB holds any data",
			s.committedVersion, strings.Join(misaligned, ", "))
	}
	return false, fmt.Errorf(
		"flatkv: store is at version %d but %s, while %s hold data; no replay can reconcile this, "+
			"because either a block is committed that the write-ahead log no longer holds or a DB lost "+
			"its metadata (restore from a snapshot, or re-sync)",
		s.committedVersion, strings.Join(misaligned, ", "), strings.Join(populated, ", "),
	)
}

// populatedDataDBs returns the directory names of the data DBs that hold at least one key outside the
// _meta/ namespace, in dataDBDirs order.
func (s *CommitStore) populatedDataDBs() ([]string, error) {
	populated := make([]string, 0, len(dataDBDirs))
	for _, ndb := range s.namedDataDBs() {
		holdsData, err := dbHoldsData(ndb.db)
		if err != nil {
			return nil, fmt.Errorf("flatkv: probe %s for data: %w", ndb.dir, err)
		}
		if holdsData {
			populated = append(populated, ndb.dir)
		}
	}
	return populated, nil
}

// dbHoldsData reports whether db holds any key outside the _meta/ namespace.
//
// The _meta/ keys occupy one contiguous range, so the two keyspaces surrounding it are probed rather
// than scanned past: each probe is a single seek, and the cost does not grow with the number of
// modules whose metadata the DB carries.
func dbHoldsData(db seidbtypes.KeyValueDB) (bool, error) {
	for _, bounds := range []*seidbtypes.IterOptions{
		{UpperBound: ktype.MetaKeyPrefixBytes},
		{LowerBound: ktype.PrefixEnd(ktype.MetaKeyPrefixBytes)},
	} {
		iter, err := db.NewIter(bounds)
		if err != nil {
			return false, fmt.Errorf("open data probe iterator: %w", err)
		}
		holdsData := iter.Valid()
		err = iter.Error()
		if closeErr := iter.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			return false, fmt.Errorf("data probe iterator: %w", err)
		}
		if holdsData {
			return true, nil
		}
	}
	return false, nil
}

func (s *CommitStore) Version() int64 {
	return s.committedVersion
}

// PendingVersion returns s.pendingBlockHeight: the height of the block currently
// buffered by ApplyChangeSets, or 0 when there are no buffered writes.
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
	s.workingLtHash = lthash.New()
	s.perDBWorkingLtHash = newPerDBLtHashMap()
	s.perDBModuleWorkingLtHash = newPerDBModuleLtHashMap()
	s.perDBModuleWorkingStats = newPerDBModuleStatsMap()

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
	applyPebbleMetricsConfig(c)
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
