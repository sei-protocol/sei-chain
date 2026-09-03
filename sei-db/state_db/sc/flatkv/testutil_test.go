package flatkv

import (
	"encoding/binary"
	"fmt"
	"maps"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/view"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Helpers
// =============================================================================

// namedDB pairs a data DB's directory name with the raw handle beneath its view manager.
type namedDB struct {
	dir string
	db  types.KeyValueDB
}

// rewindVersionRecords rewrites every data DB's version record, lowering the
// store's watermark the way a torn commit does. Pass one dir to skew a single DB.
func rewindVersionRecords(t *testing.T, s *CommitStore, version int64, dirs ...string) {
	t.Helper()
	for _, ndb := range selectDataDBs(t, s, dirs) {
		require.NoError(t, ndb.db.Set(ktype.MetaVersionKey, versionToBytes(version),
			types.WriteOptions{Sync: true}))
	}
}

// stampSeedRecords writes the records SetInitialVersion would leave — a version
// and the identity root — straight into the named data DBs. Stamping a subset
// reproduces a seed that crashed partway through its four writes, a state the
// public API cannot produce because a successful seed also writes a snapshot.
func stampSeedRecords(t *testing.T, s *CommitStore, version int64, dirs ...string) {
	t.Helper()
	opts := types.WriteOptions{Sync: true}
	for _, ndb := range selectDataDBs(t, s, dirs) {
		require.NoError(t, ndb.db.Set(ktype.MetaVersionKey, versionToBytes(version), opts))
		require.NoError(t, ndb.db.Set(ktype.MetaLtHashKey, lthash.New().Marshal(), opts))
	}
}

// stripMetaRecords deletes the named data DBs' version and root records, leaving
// them as DBs that never had metadata written. Their data, if any, survives.
func stripMetaRecords(t *testing.T, s *CommitStore, dirs ...string) {
	t.Helper()
	opts := types.WriteOptions{Sync: true}
	for _, ndb := range selectDataDBs(t, s, dirs) {
		require.NoError(t, ndb.db.Delete(ktype.MetaVersionKey, opts))
		require.NoError(t, ndb.db.Delete(ktype.MetaLtHashKey, opts))
	}
}

// writeRawDataKey puts a key outside the _meta/ namespace straight into one data
// DB, bypassing the commit path so the DB holds data no metadata accounts for.
func writeRawDataKey(t *testing.T, s *CommitStore, dir string, key, value []byte) {
	t.Helper()
	for _, ndb := range selectDataDBs(t, s, []string{dir}) {
		require.NoError(t, ndb.db.Set(key, value, types.WriteOptions{Sync: true}))
	}
}

// selectDataDBs returns the raw databases named by dirs, or all four when dirs is empty.
//
// It waits for the view managers to flush first, which is what makes a forgery written through the returned
// handles stick: the managers write asynchronously, so a record already staged would otherwise land on
// top of whatever the caller writes next.
func selectDataDBs(t *testing.T, s *CommitStore, dirs []string) []namedDB {
	t.Helper()
	requireFlushedToDisk(t, s)

	if len(dirs) == 0 {
		dirs = dataDBDirs
	}
	selected := make([]namedDB, 0, len(dirs))
	for _, dir := range dirs {
		db := s.rawDBFor(dir)
		require.NotNil(t, db, "no view manager for %s", dir)
		selected = append(selected, namedDB{dir: dir, db: db})
	}
	return selected
}

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

// setupTestStoreWithHashLogger creates a test store that reports each finalized block's hashes to hl.
func setupTestStoreWithHashLogger(t *testing.T, cfg *config.Config, hl hashlog.HashLogger) *CommitStore {
	t.Helper()
	stateWAL, err := OpenStateWAL(cfg)
	require.NoError(t, err)
	s, err := NewCommitStore(t.Context(), cfg, stateWAL, hl)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())
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
//
// Snapshots are written off the execution thread for the same reason, so the wait covers them too: a
// test that commits past SnapshotInterval and then looks at the snapshot tree would otherwise be
// racing the writer.
func commitAndCheck(t *testing.T, s *CommitStore) int64 {
	t.Helper()
	v, err := s.Commit(s.Version() + 1)
	require.NoError(t, err)
	requireFlushedToDisk(t, s)
	require.NoError(t, s.FlushSnapshots())
	return v
}

// rootHash returns the store's root hash once hashing has caught up with what was committed.
//
// Hashing is asynchronous, so nearly every assertion about a hash needs that barrier first; putting it
// here rather than at each call site is what keeps the suite from racing the pipeline.
func rootHash(s giga.LiveStateStore) []byte {
	if err := s.FlushHashes(); err != nil {
		panic(fmt.Sprintf("flatkv: flush hashes before reading the root: %v", err))
	}
	checksum := s.PublishedHash().Global.Checksum()
	return checksum[:]
}

// rootHashAndVersion is rootHash paired with the height it describes, for the tests that assert on
// both. Reading them after the same flush is what makes them describe one moment.
func rootHashAndVersion(s giga.LiveStateStore) ([]byte, int64) {
	return rootHash(s), s.Version()
}

// maintainedHashes returns the hash state the store maintains, with the pipeline caught up first.
//
// This is what the store's synchronous accumulator fields used to be, and it is a method on the store
// so a test can read it the same way.
func (s *CommitStore) maintainedHashes() *lthash.BlockHash {
	if err := s.FlushHashes(); err != nil {
		panic(fmt.Sprintf("flatkv: flush hashes before reading maintained state: %v", err))
	}
	return s.PublishedHash()
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

// workingHashes captures the full working lattice state — global,
// per-DB, and per-module hashes plus per-module stats — so a failed
// ApplyChangeSets can assert none of it moved. Global equality alone is not
// enough: two different per-module maps can sum to the same root.
type workingHashes struct {
	global         *lthash.LtHash
	perDB          map[string]*lthash.LtHash
	perModule      map[string]map[string]*lthash.LtHash
	perModuleStats map[string]map[string]lthash.ModuleStats
}

func captureWorkingHashes(s *CommitStore) workingHashes {
	perDB := make(map[string]*lthash.LtHash, len(s.maintainedHashes().PerDB))
	for dir, h := range s.maintainedHashes().PerDB {
		perDB[dir] = h.Clone()
	}
	perModule := make(map[string]map[string]*lthash.LtHash, len(s.maintainedHashes().PerModule))
	for dir, mods := range s.maintainedHashes().PerModule {
		cloned := make(map[string]*lthash.LtHash, len(mods))
		for module, h := range mods {
			cloned[module] = h.Clone()
		}
		perModule[dir] = cloned
	}
	perModuleStats := make(map[string]map[string]lthash.ModuleStats, len(s.maintainedHashes().PerModuleStats))
	for dir, mods := range s.maintainedHashes().PerModuleStats {
		perModuleStats[dir] = maps.Clone(mods)
	}
	return workingHashes{
		global:         s.maintainedHashes().Global.Clone(),
		perDB:          perDB,
		perModule:      perModule,
		perModuleStats: perModuleStats,
	}
}

func requireWorkingHashesUnchanged(t *testing.T, s *CommitStore, before workingHashes) {
	t.Helper()
	// Compute clones prev* before folding; a regression that mutates those
	// clones in place or swaps them onto the store on the error path must
	// fail these checks. Global equality alone cannot catch a per-module rewrite.
	require.True(t, s.maintainedHashes().Global.Equal(before.global), "workingLtHash mutated on failed Apply")
	require.Equal(t, len(before.perDB), len(s.maintainedHashes().PerDB), "perDBWorkingLtHash dir set changed")
	for dir, want := range before.perDB {
		got := s.maintainedHashes().PerDB[dir]
		require.NotNil(t, got, "perDBWorkingLtHash[%s] missing", dir)
		require.True(t, got.Equal(want), "perDBWorkingLtHash[%s] mutated on failed Apply", dir)
	}
	require.Equal(t, len(before.perModule), len(s.maintainedHashes().PerModule),
		"maintained per-module dir set changed")
	for dir, wantMods := range before.perModule {
		gotMods := s.maintainedHashes().PerModule[dir]
		require.Equal(t, len(wantMods), len(gotMods), "perDBModuleWorkingLtHash[%s] module set changed", dir)
		for module, want := range wantMods {
			got := gotMods[module]
			require.NotNil(t, got, "perDBModuleWorkingLtHash[%s][%s] missing", dir, module)
			require.True(t, got.Equal(want), "perDBModuleWorkingLtHash[%s][%s] mutated on failed Apply", dir, module)
		}
	}
	require.Equal(t, before.perModuleStats, s.maintainedHashes().PerModuleStats,
		"maintained per-module stats mutated on failed Apply")
}

// stagedRow reads a physical key back through its store and decodes it. The store reports whatever
// the block has staged so far merged over the on-disk row, so this is how a staged row is observed now
// that the pending-write maps are gone. A nil result means the key is absent — either never written, or
// deleted in this block, which the store deliberately does not distinguish.
func stagedRow[T vtype.VType](
	t *testing.T,
	store view.ViewManager,
	physKey []byte,
	decode func([]byte) (T, error),
) T {
	t.Helper()
	raw, found, err := store.Get(physKey, true)
	require.NoError(t, err)
	row, err := parseRow(raw, found, decode)
	require.NoError(t, err)
	return row
}

// requireStaged asserts physKey currently reads back a row from store. Presence needs no decoding, so
// it asks the store directly rather than going through a row type.
func requireStaged(t *testing.T, store view.ViewManager, physKey []byte, msgAndArgs ...any) {
	t.Helper()
	_, found, err := store.Get(physKey, true)
	require.NoError(t, err)
	require.True(t, found, msgAndArgs...)
}

// requireNotStaged asserts physKey reads back nothing from store.
func requireNotStaged(t *testing.T, store view.ViewManager, physKey []byte, msgAndArgs ...any) {
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
