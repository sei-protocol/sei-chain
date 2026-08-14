package flatkv

import (
	"encoding/binary"
	"maps"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/snapshot"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Helpers
// =============================================================================

// evmStorageKey builds a prefix-encoded storage key for the external Get/Has API.
func evmStorageKey(addr ktype.Address, slot ktype.Slot) []byte {
	internal := ktype.StorageKey(addr, slot)
	return keys.BuildEVMKey(keys.EVMKeyStorage, internal)
}

// accountPhysKey returns the physical DB key for an account address.
func accountPhysKey(addr ktype.Address) []byte {
	return ktype.EVMPhysicalKey(ktype.EVMKeyAccount, addr[:])
}

// storagePhysKey returns the physical DB key for a storage slot.
func storagePhysKey(addr ktype.Address, slot ktype.Slot) []byte {
	return ktype.EVMPhysicalKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
}

// padLeft32 returns a 32-byte big-endian value with the given bytes right-aligned.
func padLeft32(val ...byte) []byte {
	var b [32]byte
	copy(b[32-len(val):], val)
	return b[:]
}

// makeChangeSet creates a changeset
func makeChangeSet(key, value []byte, delete bool) *proto.NamedChangeSet {
	return &proto.NamedChangeSet{
		Name: "evm",
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{
				{Key: key, Value: value, Delete: delete},
			},
		},
	}
}

// setupTestDB creates a temporary PebbleDB for testing
func setupTestDB(t *testing.T) types.KeyValueDB {
	t.Helper()
	cfg := pebbledb.DefaultTestConfig(t)
	db, err := pebbledb.Open(t.Context(), &cfg)
	require.NoError(t, err)
	return db
}

// setupTestStore creates a minimal test store
func setupTestStore(t *testing.T) *CommitStore {
	t.Helper()
	s, err := newCommitStoreWithWAL(t.Context(), config.DefaultTestConfig(t))
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)
	return s
}

// setupTestStoreWithConfig creates a test store with custom config
func setupTestStoreWithConfig(t *testing.T, cfg *config.Config) *CommitStore {
	t.Helper()
	dir := t.TempDir()
	cfg.DataDir = filepath.Join(dir, flatkvRootDir)
	s, err := newCommitStoreWithWAL(t.Context(), cfg)
	require.NoError(t, err)
	err = s.LoadLatest()
	require.NoError(t, err)
	return s
}

// commitAndCheck commits the next block and waits for it to reach disk.
//
// The wait is what keeps the bulk of this suite meaningful: the stores flush asynchronously, so
// without it a test that commits and then reads a database directly is looking at a disk that lags the
// commit. It also matches how the Cosmos-era node drives the store, which forces a flush every block.
// A test specifically about asynchronous flushing should call s.Commit directly instead.
func commitAndCheck(t *testing.T, s *CommitStore) int64 {
	t.Helper()
	v, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)
	requireFlushedToDisk(t, s)
	return v
}

// rootHash returns the store's committed root hash, discarding the height it describes. Tests that
// care about the height assert on it directly rather than through this.
func rootHash(s Store) []byte {
	hash, _ := s.RootHash()
	return hash
}

// ---------- helpers to build prefix-encoded changeset pairs ----------
func nonceBytes(n uint64) []byte {
	b := make([]byte, vtype.NonceLen)
	binary.BigEndian.PutUint64(b, n)
	return b
}

func addrN(n byte) ktype.Address {
	var a ktype.Address
	a[19] = n
	return a
}

func slotN(n byte) ktype.Slot {
	var s ktype.Slot
	s[31] = n
	return s
}

func codeHashN(n byte) vtype.CodeHash {
	var h vtype.CodeHash
	for i := range h {
		h[i] = n
	}
	return h
}

func noncePair(addr ktype.Address, nonce uint64) *proto.KVPair {
	return &proto.KVPair{
		Key:   keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]),
		Value: nonceBytes(nonce),
	}
}

func codeHashPair(addr ktype.Address, ch vtype.CodeHash) *proto.KVPair {
	return &proto.KVPair{
		Key:   keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]),
		Value: ch[:],
	}
}

func codePair(addr ktype.Address, bytecode []byte) *proto.KVPair {
	return &proto.KVPair{
		Key:   keys.BuildEVMKey(keys.EVMKeyCode, addr[:]),
		Value: bytecode,
	}
}

func codeDeletePair(addr ktype.Address) *proto.KVPair {
	return &proto.KVPair{
		Key:    keys.BuildEVMKey(keys.EVMKeyCode, addr[:]),
		Delete: true,
	}
}

func storagePair(addr ktype.Address, slot ktype.Slot, val []byte) *proto.KVPair {
	return &proto.KVPair{
		Key:   keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)),
		Value: padLeft32(val...),
	}
}

func storageDeletePair(addr ktype.Address, slot ktype.Slot) *proto.KVPair {
	return &proto.KVPair{
		Key:    keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)),
		Delete: true,
	}
}

func nonceDeletePair(addr ktype.Address) *proto.KVPair {
	return &proto.KVPair{
		Key:    keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]),
		Delete: true,
	}
}

func codeHashDeletePair(addr ktype.Address) *proto.KVPair {
	return &proto.KVPair{
		Key:    keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]),
		Delete: true,
	}
}

func namedCS(pairs ...*proto.KVPair) *proto.NamedChangeSet {
	return &proto.NamedChangeSet{
		Name:      "evm",
		Changeset: proto.ChangeSet{Pairs: pairs},
	}
}

// CountKeys returns the total number of non-meta keys across all data DBs in s.
// It uses RawGlobalIterator, so pending (uncommitted) writes are not counted.
func CountKeys(s *CommitStore) (int64, error) {
	iter, err := s.RawGlobalIterator()
	if err != nil {
		return 0, err
	}
	defer func() { _ = iter.Close() }()
	var count int64
	for ; iter.Valid(); iter.Next() {
		count++
	}
	if err := iter.Error(); err != nil {
		return 0, err
	}
	return count, nil
}

// workingHashSnapshot captures the full working lattice state — global,
// per-DB, and per-module hashes plus per-module stats — so a failed
// ApplyChangeSets can assert none of it moved. Global equality alone is not
// enough: two different per-module maps can sum to the same root.
type workingHashSnapshot struct {
	global         *lthash.LtHash
	perDB          map[string]*lthash.LtHash
	perModule      map[string]map[string]*lthash.LtHash
	perModuleStats map[string]map[string]lthash.ModuleStats
}

func snapshotWorkingHashes(s *CommitStore) workingHashSnapshot {
	perDB := make(map[string]*lthash.LtHash, len(s.perDBWorkingLtHash))
	for dir, h := range s.perDBWorkingLtHash {
		perDB[dir] = h.Clone()
	}
	perModule := make(map[string]map[string]*lthash.LtHash, len(s.perDBModuleWorkingLtHash))
	for dir, mods := range s.perDBModuleWorkingLtHash {
		cloned := make(map[string]*lthash.LtHash, len(mods))
		for module, h := range mods {
			cloned[module] = h.Clone()
		}
		perModule[dir] = cloned
	}
	perModuleStats := make(map[string]map[string]lthash.ModuleStats, len(s.perDBModuleWorkingStats))
	for dir, mods := range s.perDBModuleWorkingStats {
		perModuleStats[dir] = maps.Clone(mods)
	}
	return workingHashSnapshot{
		global:         s.workingLtHash.Clone(),
		perDB:          perDB,
		perModule:      perModule,
		perModuleStats: perModuleStats,
	}
}

func requireWorkingHashesUnchanged(t *testing.T, s *CommitStore, before workingHashSnapshot) {
	t.Helper()
	// Compute clones prev* before folding; a regression that mutates those
	// clones in place or swaps them onto the store on the error path must
	// fail these checks. Global equality alone cannot catch a per-module rewrite.
	require.True(t, s.workingLtHash.Equal(before.global), "workingLtHash mutated on failed Apply")
	require.Equal(t, len(before.perDB), len(s.perDBWorkingLtHash), "perDBWorkingLtHash dir set changed")
	for dir, want := range before.perDB {
		got := s.perDBWorkingLtHash[dir]
		require.NotNil(t, got, "perDBWorkingLtHash[%s] missing", dir)
		require.True(t, got.Equal(want), "perDBWorkingLtHash[%s] mutated on failed Apply", dir)
	}
	require.Equal(t, len(before.perModule), len(s.perDBModuleWorkingLtHash), "perDBModuleWorkingLtHash dir set changed")
	for dir, wantMods := range before.perModule {
		gotMods := s.perDBModuleWorkingLtHash[dir]
		require.Equal(t, len(wantMods), len(gotMods), "perDBModuleWorkingLtHash[%s] module set changed", dir)
		for module, want := range wantMods {
			got := gotMods[module]
			require.NotNil(t, got, "perDBModuleWorkingLtHash[%s][%s] missing", dir, module)
			require.True(t, got.Equal(want), "perDBModuleWorkingLtHash[%s][%s] mutated on failed Apply", dir, module)
		}
	}
	require.Equal(t, before.perModuleStats, s.perDBModuleWorkingStats, "perDBModuleWorkingStats mutated on failed Apply")
}

// stagedRow reads a physical key back through its store and decodes it. The store reports whatever
// the block has staged so far merged over the on-disk row, so this is how a staged row is observed now
// that the pending-write maps are gone. A nil result means the key is absent — either never written, or
// deleted in this block, which the store deliberately does not distinguish.
func stagedRow[T vtype.VType](
	t *testing.T,
	store snapshot.SnapshotEngine,
	physKey []byte,
	decode func([]byte) (T, error),
) T {
	t.Helper()
	row, err := getAndParse(store, physKey, decode)
	require.NoError(t, err)
	return row
}

// requireStaged asserts physKey currently reads back a row from store. Presence needs no decoding, so
// it asks the store directly rather than going through a row type.
func requireStaged(t *testing.T, store snapshot.SnapshotEngine, physKey []byte, msgAndArgs ...any) {
	t.Helper()
	_, found, err := store.Get(physKey, true)
	require.NoError(t, err)
	require.True(t, found, msgAndArgs...)
}

// requireNotStaged asserts physKey reads back nothing from store.
func requireNotStaged(t *testing.T, store snapshot.SnapshotEngine, physKey []byte, msgAndArgs ...any) {
	t.Helper()
	_, found, err := store.Get(physKey, true)
	require.NoError(t, err)
	require.False(t, found, msgAndArgs...)
}

// requireFlushedToDisk waits until the most recently committed block has reached the databases.
//
// Any test that reads a database directly — a full scan for independent ground truth, or a check that
// a metadata key landed — needs this first. The stores flush asynchronously, so without it the test
// is looking at a disk that lags the committed version and the comparison means nothing.
func requireFlushedToDisk(t *testing.T, s *CommitStore) {
	t.Helper()
	require.NoError(t, s.flushLatestVersion())
}
