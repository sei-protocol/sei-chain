package flatkv

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
	"github.com/zbiljic/go-filelock"
)

const (
	// Top-level directory names
	flatkvRootDir = "flatkv"
	changelogDir  = "changelog"
	lockFileName  = "LOCK"

	// DB subdirectories (inside each snapshot)
	accountDBDir = "account"
	codeDBDir    = "code"
	storageDBDir = "storage"
	legacyDBDir  = "legacy"
	metadataDir  = "metadata"

	// Suffixes for atomic directory operations
	tmpSuffix      = "-tmp"
	removingSuffix = "-removing"

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
	metadataDB seidbtypes.KeyValueDB // Global version + LtHash watermark
	accountDB  seidbtypes.KeyValueDB // addr(20) → AccountValue (40 or 72 bytes)
	codeDB     seidbtypes.KeyValueDB // addr(20) → bytecode
	storageDB  seidbtypes.KeyValueDB // addr(20)||slot(32) → value(32)
	legacyDB   seidbtypes.KeyValueDB // Legacy data for backward compatibility

	// Per-DB committed version, keyed by DB dir name (e.g. accountDBDir).
	localMeta map[string]*LocalMeta

	// LtHash state for integrity checking
	committedVersion int64
	committedLtHash  *lthash.LtHash
	workingLtHash    *lthash.LtHash

	// Pending writes buffer
	// accountWrites: key = address string (20 bytes), value = AccountValue
	// codeWrites/storageWrites/legacyWrites: key = internal DB key string, value = raw bytes
	accountWrites map[string]*pendingAccountWrite
	codeWrites    map[string]*pendingKVWrite
	storageWrites map[string]*pendingKVWrite
	legacyWrites  map[string]*pendingKVWrite

	changelog         wal.ChangelogWAL
	pendingChangeSets []*proto.NamedChangeSet

	lastSnapshotTime time.Time

	// File lock prevents multiple processes from opening the same DB.
	fileLock filelock.TryLockerSafe
}

var _ Store = (*CommitStore)(nil)

// NewCommitStore creates a new (unopened) FlatKV commit store.
// Call LoadVersion to open and initialize.
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
		legacyWrites:      make(map[string]*pendingKVWrite),
		pendingChangeSets: make([]*proto.NamedChangeSet, 0),
		committedLtHash:   lthash.New(),
		workingLtHash:     lthash.New(),
	}
}

func (s *CommitStore) flatkvDir() string {
	return filepath.Join(s.homeDir, flatkvRootDir)
}

// LoadVersion loads the specified version of the database.
//
//   - targetVersion == 0: open latest (follow current symlink + catchup to end of WAL).
//   - targetVersion > 0: seek the best snapshot <= targetVersion, open it, then
//     catchup via WAL to reach targetVersion exactly.
func (s *CommitStore) LoadVersion(targetVersion int64) (_ Store, retErr error) {
	s.log.Info("FlatKV LoadVersion", "targetVersion", targetVersion)

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
			s.log.Debug("no snapshot found, will open current", "target", targetVersion, "err", err)
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

	workDir := filepath.Join(dir, workingDirName)
	if err := createWorkingDir(snapDir, workDir); err != nil {
		return fmt.Errorf("create working dir: %w", err)
	}

	if err := s.openAllDBs(workDir, dir); err != nil {
		return err
	}

	if err := s.loadGlobalMetadata(); err != nil {
		return err
	}

	s.log.Info("FlatKV store opened", "dir", dir, "version", s.committedVersion)
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
		return fmt.Errorf("acquire file lock: %w", err)
	}
	if !locked {
		return fmt.Errorf("acquire file lock: already held by another process (%s)", lockPath)
	}
	s.fileLock = fl
	return nil
}

// openAllDBs opens the 5 PebbleDBs from the snapshot directory, the changelog
// WAL from the flatkv root, and loads per-DB local metadata. On failure all
// already-opened handles are closed.
func (s *CommitStore) openAllDBs(snapDir, flatkvRoot string) (retErr error) {
	type namedPath struct {
		name string
		path string
	}
	dbPaths := []namedPath{
		{accountDBDir, filepath.Join(snapDir, accountDBDir)},
		{codeDBDir, filepath.Join(snapDir, codeDBDir)},
		{storageDBDir, filepath.Join(snapDir, storageDBDir)},
		{legacyDBDir, filepath.Join(snapDir, legacyDBDir)},
		{metadataDir, filepath.Join(snapDir, metadataDir)},
	}

	for _, np := range dbPaths {
		if err := os.MkdirAll(np.path, 0750); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", np.path, err)
		}
	}

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
		}
	}()

	openDB := func(np namedPath) (seidbtypes.KeyValueDB, error) {
		db, err := pebbledb.Open(np.path, seidbtypes.OpenOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to open %s: %w", np.name, err)
		}
		toClose = append(toClose, db)
		return db, nil
	}

	var err error
	if s.accountDB, err = openDB(dbPaths[0]); err != nil {
		return err
	}
	if s.codeDB, err = openDB(dbPaths[1]); err != nil {
		return err
	}
	if s.storageDB, err = openDB(dbPaths[2]); err != nil {
		return err
	}
	if s.legacyDB, err = openDB(dbPaths[3]); err != nil {
		return err
	}
	if s.metadataDB, err = openDB(dbPaths[4]); err != nil {
		return err
	}

	changelogPath := filepath.Join(flatkvRoot, changelogDir)
	s.changelog, err = wal.NewChangelogWAL(s.log, changelogPath, wal.Config{
		WriteBufferSize: 0,
		KeepRecent:      0,
		PruneInterval:   0,
	})
	if err != nil {
		return fmt.Errorf("failed to open changelog: %w", err)
	}
	toClose = append(toClose, s.changelog)

	// Load per-DB local metadata (or initialize if not present)
	dataDBs := map[string]seidbtypes.KeyValueDB{
		accountDBDir: s.accountDB,
		codeDBDir:    s.codeDB,
		storageDBDir: s.storageDB,
		legacyDBDir:  s.legacyDB,
	}
	for name, db := range dataDBs {
		meta, err := loadLocalMeta(db)
		if err != nil {
			return fmt.Errorf("failed to load %s local meta: %w", name, err)
		}
		s.localMeta[name] = meta
	}

	return nil
}

func (s *CommitStore) loadGlobalMetadata() error {
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
	return nil
}

// clearChangelog closes the WAL, removes its directory, and reopens an empty
// WAL. Used by Rollback when the target version predates all WAL entries and
// the entire log must be discarded to prevent re-application on restart.
func (s *CommitStore) clearChangelog() error {
	if s.changelog == nil {
		return nil
	}
	dir := filepath.Join(s.flatkvDir(), changelogDir)
	if err := s.changelog.Close(); err != nil {
		return fmt.Errorf("close changelog: %w", err)
	}
	s.changelog = nil
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove changelog dir: %w", err)
	}
	var err error
	s.changelog, err = wal.NewChangelogWAL(s.log, dir, wal.Config{})
	if err != nil {
		return fmt.Errorf("reopen changelog: %w", err)
	}
	return nil
}

func (s *CommitStore) Version() int64 {
	return s.committedVersion
}

// RootHash returns the Blake3-256 digest of the working LtHash.
func (s *CommitStore) RootHash() []byte {
	checksum := s.workingLtHash.Checksum()
	return checksum[:]
}

func (s *CommitStore) Importer(version int64) (types.Importer, error) {
	return NewKVImporter(s, version), nil
}
