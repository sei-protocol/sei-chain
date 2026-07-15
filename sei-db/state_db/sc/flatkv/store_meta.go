package flatkv

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
)

// versionToBytes encodes a non-negative version as 8-byte big-endian.
// Panics on negative input to catch programming errors early.
// Only called from internal commit/test paths — never with untrusted input.
func versionToBytes(v int64) []byte {
	if v < 0 {
		panic(fmt.Sprintf("flatkv: negative version %d", v))
	}
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, uint64(v)) //nolint:gosec // guarded above
	return b
}

// loadLocalMeta loads per-DB metadata by reading separate keys.
func loadLocalMeta(db types.KeyValueDB) (*ktype.LocalMeta, error) {
	meta := &ktype.LocalMeta{}

	versionData, err := db.Get(ktype.MetaVersionKey)
	if err != nil {
		if errorutils.IsNotFound(err) {
			return &ktype.LocalMeta{CommittedVersion: 0}, nil
		}
		return nil, fmt.Errorf("could not read meta version: %w", err)
	}
	if len(versionData) != 8 {
		return nil, fmt.Errorf("invalid meta version length: got %d, want 8", len(versionData))
	}
	meta.CommittedVersion = int64(binary.BigEndian.Uint64(versionData)) //nolint:gosec // version won't exceed int64 max

	hashData, err := db.Get(ktype.MetaLtHashKey)
	if err != nil && !errorutils.IsNotFound(err) {
		return nil, fmt.Errorf("could not read meta hash: %w", err)
	}
	if err == nil && hashData != nil {
		h, err := lthash.Unmarshal(hashData)
		if err != nil {
			return nil, fmt.Errorf("unmarshal meta hash: %w", err)
		}
		meta.LtHash = h
	}

	moduleHashes, err := loadModuleLtHashes(db)
	if err != nil {
		return nil, err
	}
	meta.ModuleLtHashes = moduleHashes

	moduleStats, err := loadModuleStats(db)
	if err != nil {
		return nil, err
	}
	meta.ModuleStats = moduleStats

	return meta, nil
}

// loadModuleLtHashes reads every per-module LtHash key ("_meta/x:<module>/hash")
// from db and returns them keyed by module name. Returns an empty map when the
// DB carries none (fresh store or a store written before per-module tracking).
func loadModuleLtHashes(db types.KeyValueDB) (map[string]*lthash.LtHash, error) {
	iter, err := db.NewIter(&types.IterOptions{
		LowerBound: ktype.ModuleLtHashPrefixBytes,
		UpperBound: ktype.PrefixEnd(ktype.ModuleLtHashPrefixBytes),
	})
	if err != nil {
		return nil, fmt.Errorf("open module lthash iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

	out := make(map[string]*lthash.LtHash)
	for ; iter.Valid(); iter.Next() {
		module, ok := ktype.ParseModuleLtHashKey(iter.Key())
		if !ok {
			continue
		}
		h, err := lthash.Unmarshal(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("unmarshal module %q meta hash: %w", module, err)
		}
		out[module] = h
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterate module lthash keys: %w", err)
	}
	return out, nil
}

// loadModuleStats reads every per-module stats key ("_meta/x:<module>/stats")
// from db and returns them keyed by module name. Returns an empty map when the
// DB carries none (fresh store or a store written before per-module stats
// tracking).
func loadModuleStats(db types.KeyValueDB) (map[string]lthash.ModuleStats, error) {
	iter, err := db.NewIter(&types.IterOptions{
		LowerBound: ktype.ModuleLtHashPrefixBytes,
		UpperBound: ktype.PrefixEnd(ktype.ModuleLtHashPrefixBytes),
	})
	if err != nil {
		return nil, fmt.Errorf("open module stats iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

	out := make(map[string]lthash.ModuleStats)
	for ; iter.Valid(); iter.Next() {
		module, ok := ktype.ParseModuleStatsKey(iter.Key())
		if !ok {
			continue
		}
		st, err := lthash.UnmarshalModuleStats(iter.Value())
		if err != nil {
			return nil, fmt.Errorf("unmarshal module %q stats: %w", module, err)
		}
		out[module] = st
	}
	if err := iter.Error(); err != nil {
		return nil, fmt.Errorf("iterate module stats keys: %w", err)
	}
	return out, nil
}

// writeLocalMetaToBatch writes per-DB metadata (version + per-DB root LtHash +
// per-module LtHashes + per-module stats) as separate keys.
func writeLocalMetaToBatch(
	batch types.Batch,
	version int64,
	ltHash *lthash.LtHash,
	moduleHashes map[string]*lthash.LtHash,
	moduleStats map[string]lthash.ModuleStats,
) error {
	if err := batch.Set(ktype.MetaVersionKey, versionToBytes(version)); err != nil {
		return fmt.Errorf("set meta version: %w", err)
	}
	if ltHash != nil {
		if err := batch.Set(ktype.MetaLtHashKey, ltHash.Marshal()); err != nil {
			return fmt.Errorf("set meta hash: %w", err)
		}
	}
	for module, h := range moduleHashes {
		if h == nil {
			continue
		}
		if err := batch.Set(ktype.ModuleLtHashKey(module), h.Marshal()); err != nil {
			return fmt.Errorf("set module %q meta hash: %w", module, err)
		}
	}
	for module, st := range moduleStats {
		if err := batch.Set(ktype.ModuleStatsKey(module), st.Marshal()); err != nil {
			return fmt.Errorf("set module %q stats: %w", module, err)
		}
	}
	return nil
}

// cloneModuleHashes returns a deep copy of a per-module hash map (cloning each
// LtHash). A nil or empty source yields a fresh empty map.
func cloneModuleHashes(src map[string]*lthash.LtHash) map[string]*lthash.LtHash {
	dst := make(map[string]*lthash.LtHash, len(src))
	for module, h := range src {
		if h != nil {
			dst[module] = h.Clone()
		}
	}
	return dst
}

// cloneModuleStats returns a copy of a per-module stats map. ModuleStats is a
// value type, so a per-entry copy is a full copy. A nil or empty source yields
// a fresh empty map.
func cloneModuleStats(src map[string]lthash.ModuleStats) map[string]lthash.ModuleStats {
	dst := make(map[string]lthash.ModuleStats, len(src))
	for module, st := range src {
		dst[module] = st
	}
	return dst
}

// loadGlobalVersion reads the global committed version from metadata DB.
// Returns 0 if not found (fresh start).
func (s *CommitStore) loadGlobalVersion() (int64, error) {
	data, err := s.metadataDB.Get(ktype.MetaVersionKey)
	if errorutils.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to read global version: %w", err)
	}
	if len(data) != 8 {
		return 0, fmt.Errorf("invalid global version length: got %d, want 8", len(data))
	}
	v := binary.BigEndian.Uint64(data)
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("global version overflow: %d exceeds max int64", v)
	}
	return int64(v), nil //nolint:gosec // overflow checked above
}

// loadGlobalEarliestVersion reads the earliest-history version recorded by
// SetInitialVersion. Returns 0 if not found (genesis stores, or stores
// created before this record existed).
func (s *CommitStore) loadGlobalEarliestVersion() (int64, error) {
	data, err := s.metadataDB.Get(ktype.MetaEarliestVersionKey)
	if errorutils.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to read global earliest version: %w", err)
	}
	if len(data) != 8 {
		return 0, fmt.Errorf("invalid global earliest version length: got %d, want 8", len(data))
	}
	v := binary.BigEndian.Uint64(data)
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("global earliest version overflow: %d exceeds max int64", v)
	}
	return int64(v), nil //nolint:gosec // overflow checked above
}

// loadGlobalLtHash reads the global committed LtHash from metadata DB.
// Returns nil if not found (fresh start).
func (s *CommitStore) loadGlobalLtHash() (*lthash.LtHash, error) {
	data, err := s.metadataDB.Get(ktype.MetaLtHashKey)
	if errorutils.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read global lthash: %w", err)
	}
	return lthash.Unmarshal(data)
}

// commitGlobalMetadata atomically commits global version and global LtHash
// to metadata DB. Per-DB LtHashes are stored in each DB's LocalMeta
// (committed atomically with data in commitBatches).
func (s *CommitStore) commitGlobalMetadata(version int64, hash *lthash.LtHash) error {
	batch := s.metadataDB.NewBatch()
	defer func() { _ = batch.Close() }()

	if err := batch.Set(ktype.MetaVersionKey, versionToBytes(version)); err != nil {
		return fmt.Errorf("failed to set global version: %w", err)
	}
	if err := batch.Set(ktype.MetaLtHashKey, hash.Marshal()); err != nil {
		return fmt.Errorf("failed to set global lthash: %w", err)
	}

	return batch.Commit(types.WriteOptions{Sync: s.config.Fsync})
}

// newPerDBLtHashMap returns a map with a fresh zero LtHash for each data DB.
func newPerDBLtHashMap() map[string]*lthash.LtHash {
	m := make(map[string]*lthash.LtHash, len(dataDBDirs))
	for _, dbDir := range dataDBDirs {
		m[dbDir] = lthash.New()
	}
	return m
}

// newPerDBModuleLtHashMap returns a map with a fresh empty per-module hash map
// for each data DB. Modules are added lazily as their keys are first written.
func newPerDBModuleLtHashMap() map[string]map[string]*lthash.LtHash {
	m := make(map[string]map[string]*lthash.LtHash, len(dataDBDirs))
	for _, dbDir := range dataDBDirs {
		m[dbDir] = make(map[string]*lthash.LtHash)
	}
	return m
}

// newPerDBModuleStatsMap returns a map with a fresh empty per-module stats map
// for each data DB. Modules are added lazily as their keys are first written.
func newPerDBModuleStatsMap() map[string]map[string]lthash.ModuleStats {
	m := make(map[string]map[string]lthash.ModuleStats, len(dataDBDirs))
	for _, dbDir := range dataDBDirs {
		m[dbDir] = make(map[string]lthash.ModuleStats)
	}
	return m
}

// SetInitialVersion seeds the store so that the next Commit produces
// initialVersion. Mirrors memiavl.DB.SetInitialVersion: only valid on a
// truly fresh store (committedVersion == 0 and no prior commits), rejected
// on read-only stores, and persists durably across restart.
//
// Implementation notes:
//   - We persist version = initialVersion - 1 to both the global metadata DB
//     and every per-DB LocalMeta. Commit() does `version := committedVersion + 1`,
//     so the next commit will return initialVersion.
//   - Write order is "global first, per-DB second" so that any partial-write
//     crash recovers as "fresh store" (loadGlobalMetadata lowers the global
//     watermark to the minimum per-DB watermark; per-DB at 0 forces global
//     back to 0). A retry with the same initialVersion is idempotent.
//   - LtHashes stay at their zero values (lthash.New()) — a freshly seeded
//     store has no data, so committed/working LtHashes remain the identity.
func (s *CommitStore) SetInitialVersion(initialVersion int64) error {
	if s.readOnly {
		return errReadOnly
	}
	if initialVersion <= 0 {
		return fmt.Errorf("flatkv: initial version must be positive, got %d", initialVersion)
	}
	if s.committedVersion != 0 {
		return fmt.Errorf("flatkv: SetInitialVersion can only be called on a fresh store; committedVersion=%d",
			s.committedVersion)
	}
	if s.metadataDB == nil {
		return fmt.Errorf("flatkv: SetInitialVersion called before LoadVersion")
	}

	seededVersion := initialVersion - 1

	if err := s.commitGlobalMetadata(seededVersion, s.committedLtHash); err != nil {
		return fmt.Errorf("flatkv: SetInitialVersion: persist global metadata: %w", err)
	}

	// Record where this store's history begins. Versions below this mark
	// predate the store entirely (the chain ran without flatkv), which is
	// distinct from pruned or corrupt in-history versions; the composite
	// store's era-aware read-only path keys on it.
	{
		batch := s.metadataDB.NewBatch()
		if err := batch.Set(ktype.MetaEarliestVersionKey, versionToBytes(seededVersion)); err != nil {
			_ = batch.Close()
			return fmt.Errorf("flatkv: SetInitialVersion: set earliest version: %w", err)
		}
		if err := batch.Commit(types.WriteOptions{Sync: s.config.Fsync}); err != nil {
			_ = batch.Close()
			return fmt.Errorf("flatkv: SetInitialVersion: persist earliest version: %w", err)
		}
		_ = batch.Close()
		s.earliestVersion = seededVersion
	}

	syncOpt := types.WriteOptions{Sync: s.config.Fsync}
	for _, ndb := range s.namedDataDBs() {
		ltHash := s.perDBWorkingLtHash[ndb.dir]
		if ltHash == nil {
			ltHash = lthash.New()
			s.perDBWorkingLtHash[ndb.dir] = ltHash
		}
		moduleHashes := s.perDBModuleWorkingLtHash[ndb.dir]
		moduleStats := s.perDBModuleWorkingStats[ndb.dir]
		batch := ndb.db.NewBatch()
		if err := writeLocalMetaToBatch(batch, seededVersion, ltHash, moduleHashes, moduleStats); err != nil {
			_ = batch.Close()
			return fmt.Errorf("flatkv: SetInitialVersion: prepare %s local meta: %w", ndb.dir, err)
		}
		if err := batch.Commit(syncOpt); err != nil {
			_ = batch.Close()
			return fmt.Errorf("flatkv: SetInitialVersion: commit %s local meta: %w", ndb.dir, err)
		}
		_ = batch.Close()
		s.localMeta[ndb.dir] = &ktype.LocalMeta{
			CommittedVersion: seededVersion,
			LtHash:           ltHash.Clone(),
			ModuleLtHashes:   cloneModuleHashes(moduleHashes),
			ModuleStats:      cloneModuleStats(moduleStats),
		}
	}

	s.committedVersion = seededVersion
	if seededVersion > 0 {
		if err := s.WriteSnapshot(""); err != nil {
			return fmt.Errorf("flatkv: SetInitialVersion: write seeded snapshot: %w", err)
		}
	}
	logger.Info("FlatKV SetInitialVersion", "initialVersion", initialVersion, "seededVersion", seededVersion)
	return nil
}

// GetLatestVersion returns the latest committed version persisted under
// dir without holding an open *CommitStore. Mirrors memiavl.GetLatestVersion
// in role: a side-channel for callers that need the on-disk watermark
// before LoadVersion has run (e.g. the rootmulti sanity check at
// process startup). Returns 0 when the store has never been opened or
// has no commits yet.
//
// The truth source is MetaVersionKey in working/metadata. The working
// dir survives across restarts and is updated on every Commit, so this
// matches the precision of memiavl.GetLatestVersion (which reads the
// WAL tail). It must not be called concurrently with a running
// CommitStore on dir, because the underlying PebbleDB takes an
// exclusive file lock.
func GetLatestVersion(dir string) (int64, error) {
	metaDir := filepath.Join(dir, workingDirName, metadataDir)
	if _, err := os.Stat(metaDir); err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("flatkv: stat working metadata dir %q: %w", metaDir, err)
	}

	cfg := pebbledb.DefaultConfig()
	cfg.DataDir = metaDir
	cfg.EnableMetrics = false
	db, err := pebbledb.Open(context.Background(), &cfg)
	if err != nil {
		return 0, fmt.Errorf("flatkv: open working metadata at %q: %w", cfg.DataDir, err)
	}
	defer func() { _ = db.Close() }()

	data, err := db.Get(ktype.MetaVersionKey)
	if errorutils.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("flatkv: read MetaVersionKey: %w", err)
	}
	if len(data) != 8 {
		return 0, fmt.Errorf("flatkv: invalid metadata version length: got %d, want 8", len(data))
	}
	v := binary.BigEndian.Uint64(data)
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("flatkv: metadata version overflow: %d exceeds max int64", v)
	}
	return int64(v), nil //nolint:gosec // overflow checked above
}

// GetLatestVersion returns the latest committed version. When the store
// is open, the in-memory committed watermark is authoritative; before
// LoadVersion has run, it falls back to the free-standing on-disk
// helper. Either path returns 0 on a fresh store.
func (s *CommitStore) GetLatestVersion() (int64, error) {
	if s.metadataDB != nil {
		return s.committedVersion, nil
	}
	return GetLatestVersion(s.flatkvDir())
}
