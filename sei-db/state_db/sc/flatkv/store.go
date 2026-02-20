package flatkv

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	db_engine "github.com/sei-protocol/sei-chain/sei-db/db_engine"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	"github.com/zbiljic/go-filelock"
)

const (
	// DB subdirectories
	accountDBDir = "account"
	codeDBDir    = "code"
	storageDBDir = "storage"
	legacyDBDir  = "legacy"
	metadataDir  = "metadata"

	// Metadata DB keys
	MetaGlobalVersion = "_meta/version" // Global committed version watermark (8 bytes)
	MetaGlobalLtHash  = "_meta/hash"    // Global LtHash (2048 bytes)
)

// pendingKVWrite tracks a buffered key-value write for code/storage DBs.
type pendingKVWrite struct {
	key      []byte // Internal DB key
	value    []byte
	isDelete bool
}

// pendingAccountWrite tracks a buffered account write.
// Uses AccountValue structure: balance(32) || nonce(8) || codehash(32)
type pendingAccountWrite struct {
	addr     Address
	value    AccountValue
	isDelete bool
}

// CommitStore implements flatkv.Store for EVM state storage.
// NOT thread-safe; callers must serialize all operations.
type CommitStore struct {
	log     logger.Logger
	config  Config
	homeDir string

	// Five separate PebbleDB instances
	metadataDB db_engine.DB // Global version + LtHash watermark
	accountDB  db_engine.DB // addr(20) → AccountValue (40 or 72 bytes)
	codeDB     db_engine.DB // addr(20) → bytecode
	storageDB  db_engine.DB // addr(20)||slot(32) → value(32)
	legacyDB   db_engine.DB // Legacy data for backward compatibility

	// Per-DB local metadata (stored inside each DB at 0x00)
	// Tracks committed version for recovery and consistency checks.
	// Keyed by DB directory name (accountDBDir, codeDBDir, storageDBDir).
	localMeta map[string]*LocalMeta

	// LtHash state for integrity checking
	committedVersion int64
	committedLtHash  *lthash.LtHash
	workingLtHash    *lthash.LtHash

	// Pending writes buffer
	// accountWrites: key = address string (20 bytes), value = AccountValue
	// codeWrites/storageWrites: key = internal DB key string, value = raw bytes
	accountWrites map[string]*pendingAccountWrite
	codeWrites    map[string]*pendingKVWrite
	storageWrites map[string]*pendingKVWrite

	// Changelog WAL for atomic writes and replay
	changelog wal.ChangelogWAL

	// Pending changesets (for changelog)
	pendingChangeSets []*proto.NamedChangeSet

	// File lock prevents multiple processes from opening the same DB.
	fileLock filelock.TryLockerSafe
}

// Compile-time check: CommitStore implements Store interface
var _ Store = (*CommitStore)(nil)

// NewCommitStore creates a new FlatKV commit store.
// Note: The store is NOT opened yet. Call LoadVersion to open and initialize the DB.
// This matches the memiavl.NewCommitStore pattern.
func NewCommitStore(homeDir string, log logger.Logger, cfg Config) *CommitStore {
	if log == nil {
		log = logger.NewNopLogger()
	}

	return &CommitStore{
		log:               log,
		config:            cfg,
		homeDir:           homeDir,
		localMeta:         make(map[string]*LocalMeta),
		accountWrites:     make(map[string]*pendingAccountWrite),
		codeWrites:        make(map[string]*pendingKVWrite),
		storageWrites:     make(map[string]*pendingKVWrite),
		pendingChangeSets: make([]*proto.NamedChangeSet, 0),
		committedLtHash:   lthash.New(),
		workingLtHash:     lthash.New(),
	}
}

// LoadVersion loads the specified version of the database.
//
//   - targetVersion == 0: open latest (follow current symlink + catchup to end of WAL).
//   - targetVersion > 0: seek the best snapshot <= targetVersion, open it, then
//     catchup via WAL to reach targetVersion exactly.
func (s *CommitStore) LoadVersion(targetVersion int64) (Store, error) {
	s.log.Info("FlatKV LoadVersion", "targetVersion", targetVersion)

	// Close existing resources if already open
	if s.metadataDB != nil {
		_ = s.Close()
	}

	flatkvDir := filepath.Join(s.homeDir, "flatkv")

	// If a specific version is requested and a better baseline snapshot exists,
	// point current at it before opening.
	if targetVersion > 0 {
		if err := os.MkdirAll(flatkvDir, 0750); err == nil {
			if baseVer, err := seekSnapshot(flatkvDir, targetVersion); err == nil {
				_ = updateCurrentSymlink(flatkvDir, snapshotName(baseVer))
			}
		}
	}

	if err := s.openTo(targetVersion); err != nil {
		return nil, fmt.Errorf("failed to open FlatKV store: %w", err)
	}

	if targetVersion > 0 && s.committedVersion != targetVersion {
		return nil, fmt.Errorf("FlatKV version mismatch: requested %d, reached %d",
			targetVersion, s.committedVersion)
	}

	return s, nil
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
// Directory layout:
//
//	flatkv/
//	  current -> snapshot-NNNNN   (symlink to active snapshot dir)
//	  snapshot-NNNNN/{account,code,storage,legacy,metadata}/
//	  changelog/                   (WAL, shared across snapshots)
//
// On first run (or migration from the pre-snapshot flat layout), the existing
// DB directories are moved into a snapshot directory and the current symlink
// is created.
//
// On failure, all already-opened resources are closed via deferred cleanup.
func (s *CommitStore) open() (retErr error) {
	s.clearPendingWrites()

	dir := filepath.Join(s.homeDir, "flatkv")

	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create base directory: %w", err)
	}

	// Acquire exclusive file lock to prevent concurrent access.
	lockPath, err := filepath.Abs(filepath.Join(dir, "LOCK"))
	if err != nil {
		return fmt.Errorf("abs lock path: %w", err)
	}
	fl, err := filelock.New(lockPath)
	if err != nil {
		return fmt.Errorf("create file lock: %w", err)
	}
	if _, err := fl.TryLock(); err != nil {
		return fmt.Errorf("acquire file lock (another process may be using this DB): %w", err)
	}
	s.fileLock = fl

	// Clean up any stale tmp directories left by a previous crash.
	if err := removeTmpDirs(dir); err != nil {
		return fmt.Errorf("cleanup tmp dirs: %w", err)
	}

	// Resolve (or create) the active snapshot directory.
	snapDir, err := s.ensureSnapshotDir(dir)
	if err != nil {
		return fmt.Errorf("resolve snapshot dir: %w", err)
	}

	// Paths inside the active snapshot directory.
	accountPath := filepath.Join(snapDir, accountDBDir)
	codePath := filepath.Join(snapDir, codeDBDir)
	storagePath := filepath.Join(snapDir, storageDBDir)
	legacyPath := filepath.Join(snapDir, legacyDBDir)
	metadataPath := filepath.Join(snapDir, metadataDir)

	// Changelog lives at the flatkv root (outside snapshots).
	changelogPath := filepath.Join(dir, "changelog")

	for _, p := range []string{accountPath, codePath, storagePath, metadataPath, legacyPath} {
		if err := os.MkdirAll(p, 0750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", p, err)
		}
	}

	// Track opened resources for cleanup on failure
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
			s.legacyDB = nil
			s.changelog = nil
			s.localMeta = make(map[string]*LocalMeta)
			if s.fileLock != nil {
				_ = s.fileLock.Unlock()
				s.fileLock = nil
			}
		}
	}()

	metaDB, err := pebbledb.Open(metadataPath, db_engine.OpenOptions{})
	if err != nil {
		return fmt.Errorf("failed to open metadata DB: %w", err)
	}
	toClose = append(toClose, metaDB)

	accountDB, err := pebbledb.Open(accountPath, db_engine.OpenOptions{})
	if err != nil {
		return fmt.Errorf("failed to open accountDB: %w", err)
	}
	toClose = append(toClose, accountDB)

	codeDB, err := pebbledb.Open(codePath, db_engine.OpenOptions{})
	if err != nil {
		return fmt.Errorf("failed to open codeDB: %w", err)
	}
	toClose = append(toClose, codeDB)

	storageDB, err := pebbledb.Open(storagePath, db_engine.OpenOptions{})
	if err != nil {
		return fmt.Errorf("failed to open storageDB: %w", err)
	}
	toClose = append(toClose, storageDB)

	legacyDB, err := pebbledb.Open(legacyPath, db_engine.OpenOptions{})
	if err != nil {
		return fmt.Errorf("failed to open legacyDB: %w", err)
	}
	toClose = append(toClose, legacyDB)

	changelog, err := wal.NewChangelogWAL(s.log, changelogPath, wal.Config{
		WriteBufferSize: 0,
		KeepRecent:      0,
		PruneInterval:   0,
	})
	if err != nil {
		return fmt.Errorf("failed to open changelog: %w", err)
	}
	toClose = append(toClose, changelog)

	// Load per-DB local metadata
	dataDBs := map[string]db_engine.DB{
		accountDBDir: accountDB,
		codeDBDir:    codeDB,
		storageDBDir: storageDB,
	}
	for name, db := range dataDBs {
		meta, err := loadLocalMeta(db)
		if err != nil {
			return fmt.Errorf("failed to load %s local meta: %w", name, err)
		}
		s.localMeta[name] = meta
	}

	// Assign to store fields
	s.metadataDB = metaDB
	s.accountDB = accountDB
	s.codeDB = codeDB
	s.storageDB = storageDB
	s.legacyDB = legacyDB
	s.changelog = changelog

	// Load committed state from metadataDB
	globalVersion, err := s.loadGlobalVersion()
	if err != nil {
		return fmt.Errorf("failed to load global version: %w", err)
	}
	s.committedVersion = globalVersion

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

	s.log.Info("FlatKV store opened", "dir", dir, "version", s.committedVersion)
	return nil
}

// Version returns the latest committed version.
func (s *CommitStore) Version() int64 {
	return s.committedVersion
}

// RootHash returns the Blake3-256 digest of the working LtHash.
func (s *CommitStore) RootHash() []byte {
	checksum := s.workingLtHash.Checksum()
	return checksum[:]
}
