package flatkv

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/common/evm"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/config"
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
	metadataDir  = "metadata"

	// Metadata DB keys
	MetaGlobalVersion = "v" // Global committed version watermark (8 bytes)
	MetaGlobalLtHash  = "h" // Global LtHash (2048 bytes)
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
	config  config.FlatKVConfig
	homeDir string

	// Four separate PebbleDB instances
	metadataDB db_engine.DB // Global version + LtHash watermark
	accountDB  db_engine.DB // addr(20) → AccountValue (40 or 72 bytes)
	codeDB     db_engine.DB // addr(20) → bytecode
	storageDB  db_engine.DB // addr(20)||slot(32) → value(32)

	// Per-DB local metadata (stored inside each DB at 0x00)
	// Tracks committed version for recovery and consistency checks
	storageLocalMeta *LocalMeta
	accountLocalMeta *LocalMeta
	codeLocalMeta    *LocalMeta

	// LtHash state for integrity checking
	committedVersion   int64
	committedLtHash    *lthash.LtHash
	workingLtHash      *lthash.LtHash
	lastFlushedVersion int64 // Last version that was flushed to disk (for AsyncWrites)

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
func NewCommitStore(homeDir string, log logger.Logger, cfg config.FlatKVConfig) *CommitStore {
	// Apply defaults: if all write toggles are false (zero value), enable all
	if !cfg.EnableStorageWrites && !cfg.EnableAccountWrites && !cfg.EnableCodeWrites {
		cfg.EnableStorageWrites = true
		cfg.EnableAccountWrites = true
		cfg.EnableCodeWrites = true
	}

	// Default FlushInterval to 100 if not set
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 100
	}

	if log == nil {
		log = logger.NewNopLogger()
	}

	return &CommitStore{
		log:               log,
		config:            cfg,
		homeDir:           homeDir,
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

// open opens all database instances. Called by NewCommitStore.
func (s *CommitStore) open() error {
	dir := filepath.Join(s.homeDir, "flatkv")

	// Create directory structure
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create base directory: %w", err)
	}

	accountPath := filepath.Join(dir, accountDBDir)
	codePath := filepath.Join(dir, codeDBDir)
	storagePath := filepath.Join(dir, storageDBDir)
	metadataPath := filepath.Join(dir, metadataDir)

	for _, path := range []string{accountPath, codePath, storagePath, metadataPath} {
		if err := os.MkdirAll(path, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", path, err)
		}
	}

	// Open metadata DB first (needed for catchup)
	metaDB, err := pebbledb.Open(metadataPath, db_engine.OpenOptions{})
	if err != nil {
		return fmt.Errorf("failed to open metadata DB: %w", err)
	}

	// Open PebbleDB instances
	accountDB, err := pebbledb.Open(accountPath, db_engine.OpenOptions{})
	if err != nil {
		metaDB.Close()
		return fmt.Errorf("failed to open accountDB: %w", err)
	}

	codeDB, err := pebbledb.Open(codePath, db_engine.OpenOptions{})
	if err != nil {
		metaDB.Close()
		accountDB.Close()
		return fmt.Errorf("failed to open codeDB: %w", err)
	}

	storageDB, err := pebbledb.Open(storagePath, db_engine.OpenOptions{})
	if err != nil {
		metaDB.Close()
		accountDB.Close()
		codeDB.Close()
		return fmt.Errorf("failed to open storageDB: %w", err)
	}

	// Open changelog WAL
	changelogPath := filepath.Join(dir, "changelog")
	changelog, err := wal.NewChangelogWAL(s.log, changelogPath, wal.Config{
		WriteBufferSize: 0, // Synchronous writes for Phase 1
		KeepRecent:      0, // No pruning for Phase 1
		PruneInterval:   0,
	})
	if err != nil {
		metaDB.Close()
		accountDB.Close()
		codeDB.Close()
		storageDB.Close()
		return fmt.Errorf("failed to open changelog: %w", err)
	}

	// Load per-DB local metadata (or initialize if not present)
	storageLocalMeta, err := loadLocalMeta(storageDB)
	if err != nil {
		metaDB.Close()
		accountDB.Close()
		codeDB.Close()
		storageDB.Close()
		changelog.Close()
		return fmt.Errorf("failed to load storageDB local meta: %w", err)
	}
	accountLocalMeta, err := loadLocalMeta(accountDB)
	if err != nil {
		metaDB.Close()
		accountDB.Close()
		codeDB.Close()
		storageDB.Close()
		changelog.Close()
		return fmt.Errorf("failed to load accountDB local meta: %w", err)
	}
	codeLocalMeta, err := loadLocalMeta(codeDB)
	if err != nil {
		metaDB.Close()
		accountDB.Close()
		codeDB.Close()
		storageDB.Close()
		changelog.Close()
		return fmt.Errorf("failed to load codeDB local meta: %w", err)
	}

	s.metadataDB = metaDB
	s.accountDB = accountDB
	s.codeDB = codeDB
	s.storageDB = storageDB
	s.storageLocalMeta = storageLocalMeta
	s.accountLocalMeta = accountLocalMeta
	s.codeLocalMeta = codeLocalMeta
	s.changelog = changelog

	// Load committed state from metadataDB
	globalVersion, err := s.loadGlobalVersion()
	if err != nil {
		s.Close()
		return fmt.Errorf("failed to load global version: %w", err)
	}
	s.committedVersion = globalVersion

	globalLtHash, err := s.loadGlobalLtHash()
	if err != nil {
		s.Close()
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
	// if err := s.catchup(); err != nil {
	// 	s.Close()
	// 	return fmt.Errorf("catchup failed: %w", err)
	// }

	s.log.Info("FlatKV store opened", "dir", dir, "version", s.committedVersion)
	return nil
}

// ApplyChangeSets buffers EVM changesets and updates LtHash.
// Respects EnableStorageWrites/EnableAccountWrites/EnableCodeWrites toggles.
//
// LtHash is computed based on actual storage format (internal keys):
// - storageDB: key=addr||slot, value=storage_value
// - accountDB: key=addr, value=AccountValue (balance(32)||nonce(8)||codehash(32)
// - codeDB: key=addr, value=bytecode
func (s *CommitStore) ApplyChangeSets(cs []*proto.NamedChangeSet) error {
	// Save original changesets for changelog
	s.pendingChangeSets = append(s.pendingChangeSets, cs...)

	// Collect LtHash pairs per DB (using internal key format)
	var storagePairs []lthash.KVPairWithLastValue
	var codePairs []lthash.KVPairWithLastValue
	// Account pairs are collected at the end after all account changes are processed

	// Track which accounts were modified (for LtHash computation)
	modifiedAccounts := make(map[string]bool)

	for _, namedCS := range cs {
		if namedCS.Changeset.Pairs == nil {
			continue
		}

		for _, pair := range namedCS.Changeset.Pairs {
			// Parse memiavl key to determine type
			kind, keyBytes := evm.ParseEVMKey(pair.Key)
			if kind == evm.EVMKeyUnknown {
				// Skip non-EVM keys silently
				continue
			}

			// Route to appropriate DB based on key type
			switch kind {
			case evm.EVMKeyStorage:
				if s.config.EnableStorageWrites {
					// Get old value for LtHash
					oldValue, err := s.getStorageValue(keyBytes)
					if err != nil {
						return fmt.Errorf("failed to get storage value: %w", err)
					}

					// Storage: keyBytes = addr(20) || slot(32)
					keyStr := string(keyBytes)
					if pair.Delete {
						s.storageWrites[keyStr] = &pendingKVWrite{
							key:      keyBytes,
							isDelete: true,
						}
					} else {
						s.storageWrites[keyStr] = &pendingKVWrite{
							key:   keyBytes,
							value: pair.Value,
						}
					}

					// LtHash pair: internal key directly
					storagePairs = append(storagePairs, lthash.KVPairWithLastValue{
						Key:       keyBytes,
						Value:     pair.Value,
						LastValue: oldValue,
						Delete:    pair.Delete,
					})
				}

			case evm.EVMKeyNonce, evm.EVMKeyCodeHash:
				if s.config.EnableAccountWrites {
					// Account data: keyBytes = addr(20)
					addr, ok := AddressFromBytes(keyBytes)
					if !ok {
						return fmt.Errorf("invalid address length %d for key kind %d", len(keyBytes), kind)
					}
					addrStr := string(addr[:])

					// Track this account as modified for LtHash
					modifiedAccounts[addrStr] = true
					// Get or create pending account write
					paw := s.accountWrites[addrStr]
					if paw == nil {
						// Load existing value from DB
						existingValue, err := s.getAccountValue(addr)
						if err != nil {
							return fmt.Errorf("failed to load existing account value: %w", err)
						}
						paw = &pendingAccountWrite{
							addr:  addr,
							value: existingValue,
						}
						s.accountWrites[addrStr] = paw
					}

					if pair.Delete {
						if kind == evm.EVMKeyNonce {
							paw.value.Nonce = 0
						} else {
							paw.value.CodeHash = CodeHash{}
						}
					} else {
						if kind == evm.EVMKeyNonce {
							if len(pair.Value) != NonceLen {
								return fmt.Errorf("invalid nonce value length: got %d, expected %d", len(pair.Value), NonceLen)
							}
							paw.value.Nonce = binary.BigEndian.Uint64(pair.Value)
						} else {
							if len(pair.Value) != CodeHashLen {
								return fmt.Errorf("invalid codehash value length: got %d, expected %d", len(pair.Value), CodeHashLen)
							}
							copy(paw.value.CodeHash[:], pair.Value)
						}
					}
				}

			case evm.EVMKeyCode:
				if s.config.EnableCodeWrites {
					// Get old value for LtHash
					oldValue, err := s.getCodeValue(keyBytes)
					if err != nil {
						return fmt.Errorf("failed to get code value: %w", err)
					}

					// Code: keyBytes = addr(20) - per x/evm/types/keys.go
					keyStr := string(keyBytes)
					if pair.Delete {
						s.codeWrites[keyStr] = &pendingKVWrite{
							key:      keyBytes,
							isDelete: true,
						}
					} else {
						s.codeWrites[keyStr] = &pendingKVWrite{
							key:   keyBytes,
							value: pair.Value,
						}
					}

					// LtHash pair: internal key directly
					codePairs = append(codePairs, lthash.KVPairWithLastValue{
						Key:       keyBytes,
						Value:     pair.Value,
						LastValue: oldValue,
						Delete:    pair.Delete,
					})
				}

			case evm.EVMKeyCodeSize:
				// CodeSize is computed from len(Code), not stored in FlatKV - skip
				continue
			}
		}
	}

	// Build account LtHash pairs based on full AccountValue changes
	var accountPairs []lthash.KVPairWithLastValue
	for addrStr := range modifiedAccounts {
		addr, ok := AddressFromBytes([]byte(addrStr))
		if !ok {
			return fmt.Errorf("invalid address in modifiedAccounts: %x", addrStr)
		}

		// Get old AccountValue from DB (committed state)
		oldAV, err := s.getAccountValueFromDB(addr)
		if err != nil {
			return fmt.Errorf("failed to get old account value for addr %x: %w", addr, err)
		}
		oldValue := oldAV.Encode()

		// Get new AccountValue (from pending writes or DB)
		var newValue []byte
		var isDelete bool
		if paw, ok := s.accountWrites[addrStr]; ok {
			newValue = paw.value.Encode()
			isDelete = paw.isDelete
		} else {
			// No pending write means no change (shouldn't happen, but be safe)
			continue
		}

		accountPairs = append(accountPairs, lthash.KVPairWithLastValue{
			Key:       addr[:],
			Value:     newValue,
			LastValue: oldValue,
			Delete:    isDelete,
		})
	}

	// Combine all pairs and update working LtHash
	allPairs := append(storagePairs, accountPairs...)
	allPairs = append(allPairs, codePairs...)

	if len(allPairs) > 0 {
		newLtHash, _ := lthash.ComputeLtHash(s.workingLtHash, allPairs)
		s.workingLtHash = newLtHash
	}

	return nil
}

// Commit persists buffered writes and advances the version.
// Protocol: WAL → per-DB batch (with LocalMeta) → flush at interval → update metaDB.
// On crash, catchup replays WAL to recover incomplete commits.
func (s *CommitStore) Commit() (int64, error) {
	// Auto-increment version
	version := s.committedVersion + 1

	// Step 1: Write Changelog (WAL) - source of truth (always sync)
	changelogEntry := proto.ChangelogEntry{
		Version:    version,
		Changesets: s.pendingChangeSets,
	}
	if err := s.changelog.Write(changelogEntry); err != nil {
		return 0, fmt.Errorf("changelog write: %w", err)
	}

	// Step 2: Commit to each DB (data + LocalMeta.CommittedVersion atomically)
	if err := s.commitBatches(version); err != nil {
		return 0, fmt.Errorf("db commit: %w", err)
	}

	// Step 3: Update in-memory committed state
	s.committedVersion = version
	s.committedLtHash = s.workingLtHash.Clone()

	// Step 4: Flush and update metaDB based on flush interval
	// - Sync writes: always flush (implicit) and update metaDB
	// - Async writes: only flush and update metaDB at FlushInterval
	shouldFlush := !s.config.AsyncWrites || // Sync mode: always "flush"
		s.config.FlushInterval <= 1 || // FlushInterval=1: flush every block
		(version-s.lastFlushedVersion) >= int64(s.config.FlushInterval) // Interval reached

	if shouldFlush {
		// Flush data DBs if using async writes
		if s.config.AsyncWrites {
			if err := s.flushAllDBs(); err != nil {
				return 0, fmt.Errorf("flush: %w", err)
			}
		}

		// Persist global metadata to metadata DB (watermark)
		if err := s.commitGlobalMetadata(version, s.committedLtHash); err != nil {
			return 0, fmt.Errorf("metadata DB commit: %w", err)
		}

		s.lastFlushedVersion = version
	}

	// Step 5: Clear pending buffers
	s.clearPendingWrites()

	s.log.Info("Committed version", "version", version, "flushed", shouldFlush)
	return version, nil
}

// flushAllDBs flushes all data DBs to ensure data is on disk.
func (s *CommitStore) flushAllDBs() error {
	if err := s.accountDB.Flush(); err != nil {
		return fmt.Errorf("accountDB flush: %w", err)
	}
	if err := s.codeDB.Flush(); err != nil {
		return fmt.Errorf("codeDB flush: %w", err)
	}
	if err := s.storageDB.Flush(); err != nil {
		return fmt.Errorf("storageDB flush: %w", err)
	}
	return nil
}

// clearPendingWrites clears all pending write buffers
func (s *CommitStore) clearPendingWrites() {
	s.accountWrites = make(map[string]*pendingAccountWrite)
	s.codeWrites = make(map[string]*pendingKVWrite)
	s.storageWrites = make(map[string]*pendingKVWrite)
	s.pendingChangeSets = make([]*proto.NamedChangeSet, 0)
}

// commitBatches commits pending writes to their respective DBs atomically.
// Each DB batch includes LocalMeta update for crash recovery.
// Also called by catchup to replay WAL without re-writing changelog.
func (s *CommitStore) commitBatches(version int64) error {
	// Sync option: false for async (faster), true for sync (safer)
	syncOpt := db_engine.WriteOptions{Sync: !s.config.AsyncWrites}

	// Commit to accountDB (only if writes are enabled)
	// accountDB uses AccountValue structure: key=addr(20), value=balance(32)||nonce(8)||codehash(32)
	// When EnableAccountWrites=false, skip entirely (don't update LocalMeta to avoid false "synced" state)
	if s.config.EnableAccountWrites && (len(s.accountWrites) > 0 || version > s.accountLocalMeta.CommittedVersion) {
		batch := s.accountDB.NewBatch()
		defer batch.Close()

		for _, paw := range s.accountWrites {
			if paw.isDelete {
				if err := batch.Delete(paw.addr[:]); err != nil {
					return fmt.Errorf("accountDB delete: %w", err)
				}
			} else {
				// Encode AccountValue and store with addr as key
				encoded := EncodeAccountValue(paw.value)
				if err := batch.Set(paw.addr[:], encoded); err != nil {
					return fmt.Errorf("accountDB set: %w", err)
				}
			}
		}

		// Update local meta atomically with data (same batch)
		newLocalMeta := &LocalMeta{
			CommittedVersion: version,
		}
		if err := batch.Set(DBLocalMetaKey, MarshalLocalMeta(newLocalMeta)); err != nil {
			return fmt.Errorf("accountDB local meta set: %w", err)
		}

		if err := batch.Commit(syncOpt); err != nil {
			return fmt.Errorf("accountDB commit: %w", err)
		}

		// Update in-memory local meta after successful commit
		s.accountLocalMeta = newLocalMeta
	}

	// Commit to codeDB (only if writes are enabled)
	// When EnableCodeWrites=false, skip entirely (don't update LocalMeta)
	if s.config.EnableCodeWrites && (len(s.codeWrites) > 0 || version > s.codeLocalMeta.CommittedVersion) {
		batch := s.codeDB.NewBatch()
		defer batch.Close()

		for _, pw := range s.codeWrites {
			if pw.isDelete {
				if err := batch.Delete(pw.key); err != nil {
					return fmt.Errorf("codeDB delete: %w", err)
				}
			} else {
				if err := batch.Set(pw.key, pw.value); err != nil {
					return fmt.Errorf("codeDB set: %w", err)
				}
			}
		}

		// Update local meta atomically with data (same batch)
		newLocalMeta := &LocalMeta{
			CommittedVersion: version,
		}
		if err := batch.Set(DBLocalMetaKey, MarshalLocalMeta(newLocalMeta)); err != nil {
			return fmt.Errorf("codeDB local meta set: %w", err)
		}

		if err := batch.Commit(syncOpt); err != nil {
			return fmt.Errorf("codeDB commit: %w", err)
		}

		// Update in-memory local meta after successful commit
		s.codeLocalMeta = newLocalMeta
	}

	// Commit to storageDB (only if writes are enabled)
	// When EnableStorageWrites=false, skip entirely (don't update LocalMeta)
	if s.config.EnableStorageWrites && (len(s.storageWrites) > 0 || version > s.storageLocalMeta.CommittedVersion) {
		batch := s.storageDB.NewBatch()
		defer batch.Close()

		for _, pw := range s.storageWrites {
			if pw.isDelete {
				if err := batch.Delete(pw.key); err != nil {
					return fmt.Errorf("storageDB delete: %w", err)
				}
			} else {
				if err := batch.Set(pw.key, pw.value); err != nil {
					return fmt.Errorf("storageDB set: %w", err)
				}
			}
		}

		// Update local meta atomically with data (same batch)
		newLocalMeta := &LocalMeta{
			CommittedVersion: version,
		}
		if err := batch.Set(DBLocalMetaKey, MarshalLocalMeta(newLocalMeta)); err != nil {
			return fmt.Errorf("storageDB local meta set: %w", err)
		}

		if err := batch.Commit(syncOpt); err != nil {
			return fmt.Errorf("storageDB commit: %w", err)
		}

		// Update in-memory local meta after successful commit
		s.storageLocalMeta = newLocalMeta
	}

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
