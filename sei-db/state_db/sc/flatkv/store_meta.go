package flatkv

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"

	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/statewal"
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

// loadLocalMeta loads per-DB metadata by reading separate keys. A DB missing its version record is
// reported as one that has never been written, and rejected if it carries any other metadata.
func loadLocalMeta(db types.KeyValueDB) (*ktype.LocalMeta, error) {
	meta := &ktype.LocalMeta{}

	versionData, err := db.Get(ktype.MetaVersionKey)
	if err != nil {
		if errorutils.IsNotFound(err) {
			// Metadata is written as a set, so a DB missing its version must hold no metadata at
			// all. Reporting one that holds some as never-written would hide a lost root behind a
			// replay that cannot restore it: the hashes are maintained by unmixing the old value
			// and mixing the new, so re-applying a block whose rows are already on disk cancels
			// out and leaves those rows uncounted, silently changing the store root.
			//
			// Rebuilding the root here instead would mean a full scan of the DB, and unlike an
			// import or an empty DB this path is not already making one.
			if err := requireNoMetadata(db); err != nil {
				return nil, err
			}
			return &ktype.LocalMeta{CommittedVersion: 0}, nil
		}
		return nil, fmt.Errorf("could not read meta version: %w", err)
	}
	meta.CommittedVersion, err = decodeVersion(ktype.MetaVersionKey, versionData)
	if err != nil {
		return nil, err
	}

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

// requireNoMetadata returns an error if db holds any key in the _meta/ namespace.
func requireNoMetadata(db types.KeyValueDB) error {
	iter, err := db.NewIter(&types.IterOptions{
		LowerBound: ktype.MetaKeyPrefixBytes,
		UpperBound: ktype.PrefixEnd(ktype.MetaKeyPrefixBytes),
	})
	if err != nil {
		return fmt.Errorf("open metadata iterator: %w", err)
	}
	defer func() { _ = iter.Close() }()

	if iter.Valid() {
		return fmt.Errorf("flatkv: %s is absent but %s is present; this DB lost its version record, "+
			"and no replay can rebuild the root it should have",
			ktype.MetaVersionKey, iter.Key())
	}
	return iter.Error()
}

// loadModuleLtHashes reads every per-module LtHash key ("_meta/x:<module>/hash")
// from db and returns them keyed by module name. Returns an empty map when the
// DB carries none, meaning it has never been written or holds no module's keys.
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
// DB carries none, meaning it has never been written or holds no module's keys.
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
// per-module LtHashes + per-module stats) as separate keys. It rejects a nil
// ltHash.
func writeLocalMetaToBatch(
	batch types.Batch,
	version int64,
	ltHash *lthash.LtHash,
	moduleHashes map[string]*lthash.LtHash,
	moduleStats map[string]lthash.ModuleStats,
) error {
	pairs, err := encodeLocalMeta(version, ltHash, moduleHashes, moduleStats)
	if err != nil {
		return err
	}
	for _, pair := range pairs {
		if err := batch.Set(pair.Key, pair.Value); err != nil {
			return fmt.Errorf("set %q: %w", pair.Key, err)
		}
	}
	return nil
}

// encodeLocalMeta encodes one data database's LocalMeta as reserved-prefix key-value pairs, for a
// caller to hand to View.Finalize so they land in the same atomic batch as that version's diff.
//
// The pairs are the committed version, the per-DB root LtHash, and a hash plus a stats entry per
// module. It rejects a nil ltHash.
func encodeLocalMeta(
	version int64,
	ltHash *lthash.LtHash,
	moduleHashes map[string]*lthash.LtHash,
	moduleStats map[string]lthash.ModuleStats,
) ([]*proto.KVPair, error) {
	if ltHash == nil {
		// The version and the root must be co-present on disk. deriveGlobalState sums the persisted
		// per-DB roots into the store root, and hydratePerDBState substitutes the identity for a DB
		// that records none — so a DB carrying a version without a root would contribute nothing to
		// the store root while holding data, silently omitting its keys from the AppHash.
		return nil, fmt.Errorf("flatkv: refusing to write metadata for version %d with no root hash", version)
	}
	pairs := make([]*proto.KVPair, 0, 2+len(moduleHashes)+len(moduleStats))
	pairs = append(pairs, &proto.KVPair{Key: ktype.MetaVersionKey, Value: versionToBytes(version)})
	pairs = append(pairs, &proto.KVPair{Key: ktype.MetaLtHashKey, Value: ltHash.Marshal()})
	for module, h := range moduleHashes {
		if h == nil {
			continue
		}
		pairs = append(pairs, &proto.KVPair{Key: ktype.ModuleLtHashKey(module), Value: h.Marshal()})
	}
	for module, st := range moduleStats {
		pairs = append(pairs, &proto.KVPair{Key: ktype.ModuleStatsKey(module), Value: st.Marshal()})
	}
	return pairs, nil
}

// validatePerModuleMetadata enforces the load-time invariant that a persisted
// per-DB root is consistent with its per-module decomposition. FlatKV writes
// the per-DB root and its per-module hashes in the same atomic batch (see
// writeLocalMetaToBatch / commitBatches), so a correctly written store always
// satisfies SumModuleHashes(ModuleLtHashes) == LtHash.
//
// Two failure modes are rejected here:
//  1. Non-identity root with an empty module map — the store predates
//     per-module hashing; migration is intentionally unsupported.
//  2. Module map present but its homomorphic sum does not equal the root —
//     incomplete / drifted bookkeeping (e.g. a subset of module /hash keys
//     lost while the root survived). On the next write,
//     HashCalculator.Compute rebuilds the root as SumModuleHashes over that
//     incomplete map, silently dropping a module's contribution to the
//     per-DB root and thus the global store hash / AppHash.
//
// Fail loudly at load instead of corrupting consensus-critical state.
func validatePerModuleMetadata(dbDir string, meta *ktype.LocalMeta) error {
	if meta == nil || meta.LtHash == nil {
		return nil
	}
	if len(meta.ModuleLtHashes) == 0 {
		if meta.LtHash.IsZero() {
			return nil
		}
		return fmt.Errorf(
			"flatkv: %s carries a non-identity per-DB LtHash root but no "+
				"per-module metadata; this store predates per-module hashing "+
				"and cannot be opened (migration is intentionally unsupported — "+
				"recreate the store from a snapshot or genesis)",
			dbDir,
		)
	}
	sum := lthash.SumModuleHashes(meta.ModuleLtHashes)
	if !meta.LtHash.Equal(sum) {
		return fmt.Errorf(
			"flatkv: %s per-module hashes do not sum to per-DB root (root=%x sum=%x)",
			dbDir, meta.LtHash.Checksum(), sum.Checksum(),
		)
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

// decodeVersion parses an 8-byte big-endian version record; key names it in
// error messages.
func decodeVersion(key []byte, data []byte) (int64, error) {
	if len(data) != 8 {
		return 0, fmt.Errorf("invalid %s length: got %d, want 8", key, len(data))
	}
	v := binary.BigEndian.Uint64(data)
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("%s overflow: %d exceeds max int64", key, v)
	}
	return int64(v), nil //nolint:gosec // overflow checked above
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

// SetInitialVersion seeds the store so that Commit(initialVersion) is
// accepted as the first commit. Mirrors memiavl.DB.SetInitialVersion: only
// valid on a truly fresh store (committedVersion == 0 and no prior commits),
// rejected on read-only stores, and persists durably across restart.
//
// It records initialVersion-1 as the height every database has reached, so Commit(initialVersion) is
// the next contiguous block. LtHashes stay at their zero values: a freshly seeded store holds no data.
//
// The per-database writes are atomic individually but not with one another, so a crash partway through
// leaves a torn seed. The open path discards one, which makes a retry with any initialVersion valid and
// not only the one that was interrupted.
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
	if s.isClosed() {
		return fmt.Errorf("flatkv: SetInitialVersion called before LoadLatest")
	}

	seededVersion := initialVersion - 1

	for _, dir := range dataDBDirs {
		if s.perDBWorkingLtHash[dir] == nil {
			s.perDBWorkingLtHash[dir] = lthash.New()
		}
	}

	if err := s.sealSeededVersion(seededVersion); err != nil {
		return fmt.Errorf("flatkv: SetInitialVersion: seal seeded version: %w", err)
	}

	for _, dir := range dataDBDirs {
		s.localMeta[dir] = &ktype.LocalMeta{
			CommittedVersion: seededVersion,
			LtHash:           s.perDBWorkingLtHash[dir].Clone(),
			ModuleLtHashes:   cloneModuleHashes(s.perDBModuleWorkingLtHash[dir]),
			ModuleStats:      cloneModuleStats(s.perDBModuleWorkingStats[dir]),
		}
	}

	s.committedVersion = seededVersion

	// The seal only stages the records; the view managers flush asynchronously. Wait for them so the seed is
	// durable across a restart, as this method promises. For a non-genesis seed the snapshot below supplies
	// the same barrier, but it does not run at genesis.
	if err := s.flushLatestVersion(); err != nil {
		return fmt.Errorf("flatkv: SetInitialVersion: flush seeded version: %w", err)
	}

	if seededVersion > 0 {
		if err := s.outOfBandSnapshot(); err != nil {
			return fmt.Errorf("flatkv: SetInitialVersion: write seeded snapshot: %w", err)
		}
	}
	logger.Info("FlatKV SetInitialVersion", "initialVersion", initialVersion, "seededVersion", seededVersion)
	return nil
}

// GetLatestVersion returns the version a store opened on dir will report once LoadLatest has run.
// A directory that has never been opened reads as 0.
//
// It must not be called while a CommitStore holds dir's WAL open; such a caller should use
// CommitStore.GetLatestVersion instead.
func GetLatestVersion(dir string) (int64, error) {
	return latestVersion(dir, nil)
}

// latestVersion resolves the version a store on dir will open at, reading the WAL range through wal
// when that is non-nil and out-of-band otherwise.
func latestVersion(dir string, wal statewal.StateWAL) (int64, error) {
	// An open loads the working directory from the current snapshot and then replays the WAL forward
	// over it, so the higher of those two is where it lands. A per-DB watermark would not do: those
	// are written after the WAL record and trail it by a block whenever a commit is interrupted.
	snapshotVersion, err := currentSnapshotVersion(dir)
	if err != nil {
		return 0, err
	}
	walVersion, err := walTailVersion(dir, wal)
	if err != nil {
		return 0, err
	}
	return max(snapshotVersion, walVersion), nil
}

// currentSnapshotVersion returns the version of the snapshot the current symlink names, or 0 when
// there is none.
func currentSnapshotVersion(dir string) (int64, error) {
	_, version, err := currentSnapshotDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("flatkv: resolve current snapshot under %q: %w", dir, err)
	}
	return version, nil
}

// walTailVersion returns the last block the state WAL under dir holds, or 0 when it holds none. It
// reads through wal when that is non-nil and out-of-band otherwise.
func walTailVersion(dir string, wal statewal.StateWAL) (int64, error) {
	readRange := func() (bool, uint64, uint64, error) {
		if wal != nil {
			return wal.GetStoredRange()
		}
		// Reading out-of-band takes the changelog's exclusive lock, so it fails against a store that
		// holds the WAL — which is why a caller with a handle passes it rather than relying on dir.
		return statewal.GetRange(StateWALConfig(dir))
	}
	stored, _, last, err := readRange()
	if err != nil {
		return 0, fmt.Errorf("flatkv: read state WAL range under %q: %w", dir, err)
	}
	if !stored {
		return 0, nil
	}
	if last > math.MaxInt64 {
		return 0, fmt.Errorf("flatkv: state WAL last block %d exceeds max int64", last)
	}
	return int64(last), nil //nolint:gosec // bounds checked above
}

// GetLatestVersion returns the version this store is at, or will be at once loaded. A fresh store
// returns 0.
func (s *CommitStore) GetLatestVersion() (int64, error) {
	if !s.isClosed() {
		return s.committedVersion, nil
	}
	return latestVersion(s.flatkvDir(), s.wal)
}
