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
func (s *CommitStore) LoadVersion(targetVersion int64, readOnly bool) (Store, error) {
	s.log.Info("FlatKV LoadVersion", "targetVersion", targetVersion, "readOnly", readOnly)

	if readOnly {
		// Read-only mode requires snapshot support (not yet implemented).
		// Return sentinel error so callers can fall back to Cosmos-only mode.
		return nil, ErrReadOnlyNotSupported
	}

	// Close existing resources if already open
	if s.metadataDB != nil {
		_ = s.Close()
	}

	// Open the store
	if err := s.open(); err != nil {
		return nil, fmt.Errorf("failed to open FlatKV store: %w", err)
	}

	// Verify version if specified
	if targetVersion > 0 && s.committedVersion != targetVersion {
		return nil, fmt.Errorf("FlatKV version mismatch: requested %d, current %d",
			targetVersion, s.committedVersion)
	}

	return s, nil
}

// open opens all database instances. Called by LoadVersion.
// On failure, all already-opened resources are closed via deferred cleanup.
func (s *CommitStore) open() (retErr error) {
	dir := filepath.Join(s.homeDir, "flatkv")

	// Create directory structure
	if err := os.MkdirAll(dir, 0750); err != nil {
		return fmt.Errorf("failed to create base directory: %w", err)
	}

	accountPath := filepath.Join(dir, accountDBDir)
	codePath := filepath.Join(dir, codeDBDir)
	storagePath := filepath.Join(dir, storageDBDir)
	legacyPath := filepath.Join(dir, legacyDBDir)
	metadataPath := filepath.Join(dir, metadataDir)

	for _, path := range []string{accountPath, codePath, storagePath, legacyPath, metadataPath} {
		if err := os.MkdirAll(path, 0750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", path, err)
		}
	}

	// Track opened resources for cleanup on failure
	var toClose []io.Closer
	defer func() {
		if retErr != nil {
			for _, c := range toClose {
				_ = c.Close()
			}
			// Clear fields to avoid dangling references to closed handles
			s.metadataDB = nil
			s.accountDB = nil
			s.codeDB = nil
			s.storageDB = nil
			s.legacyDB = nil
			s.changelog = nil
			s.localMeta = make(map[string]*LocalMeta)
		}
	}()

	// Open metadata DB first (needed for catchup)
	metaDB, err := pebbledb.Open(metadataPath, db_engine.OpenOptions{})
	if err != nil {
		return fmt.Errorf("failed to open metadata DB: %w", err)
	}
	toClose = append(toClose, metaDB)

	// Open PebbleDB instances
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

	// Open changelog WAL
	changelogPath := filepath.Join(dir, "changelog")
	changelog, err := wal.NewChangelogWAL(s.log, changelogPath, wal.Config{
		WriteBufferSize: 0, // Synchronous writes for Phase 1
		KeepRecent:      0, // No pruning for Phase 1
		PruneInterval:   0,
	})
	if err != nil {
		return fmt.Errorf("failed to open changelog: %w", err)
	}
	toClose = append(toClose, changelog)

	// Load per-DB local metadata (or initialize if not present)
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

	// TODO: Run catchup to recover from any incomplete commits
	// Catchup will be added in a future PR with state-sync support.

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
