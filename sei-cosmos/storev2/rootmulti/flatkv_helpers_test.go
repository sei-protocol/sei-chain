package rootmulti

// Shared fixtures, config factories, and assertion helpers for the flatkv_*_test.go suite.

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	errorutils "github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	seidbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
	scmemiavl "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

// withTestMemIAVL applies the standard memiavl tuning used by all integration
// tests (synchronous commits, per-block snapshots, no async buffer) so the
// test output is deterministic.
func withTestMemIAVL(cfg seidbconfig.StateCommitConfig) seidbconfig.StateCommitConfig {
	cfg.MemIAVLConfig.SnapshotInterval = 1
	cfg.MemIAVLConfig.SnapshotMinTimeInterval = 0
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0
	// Snapshotting every block (interval 1) combined with the default
	// keep-recent of 1 would prune all but the two newest snapshots. FlatKV,
	// which mirrors this cadence, needs a retained snapshot at-or-below the
	// rollback target to reconstruct that version, so aggressive pruning would
	// make the rollback/recovery tests unable to target older versions. In
	// production (interval 10000) a small rollback always lands within a
	// retained interval; here we retain all snapshots across the small test
	// version ranges to model that guarantee. Tests that specifically exercise
	// pruning override this explicitly.
	cfg.MemIAVLConfig.SnapshotKeepRecent = 1000
	cfg.HistoricalProofRateLimit = 0
	cfg.HistoricalProofMaxInFlight = 100
	return cfg
}

func dualWriteConfig() seidbconfig.StateCommitConfig {
	cfg := seidbconfig.DefaultStateCommitConfig()
	cfg.WriteMode = sctypes.TestOnlyDualWrite
	return withTestMemIAVL(cfg)
}

func evmMigratedConfig() seidbconfig.StateCommitConfig {
	cfg := seidbconfig.DefaultStateCommitConfig()
	cfg.WriteMode = sctypes.EVMMigrated
	return withTestMemIAVL(cfg)
}

func flatKVOnlyConfig() seidbconfig.StateCommitConfig {
	cfg := seidbconfig.DefaultStateCommitConfig()
	cfg.WriteMode = sctypes.FlatKVOnly
	return withTestMemIAVL(cfg)
}

// memiavlOnlyConfig is the v0 starting point for FlatKV EVM migrate tests:
// memiavl is the only backend, flatkv is not allocated. Phase 1 of the
// migration tests drives traffic in this mode before flipping to
// MigrateEVM at restart.
func memiavlOnlyConfig() seidbconfig.StateCommitConfig {
	cfg := seidbconfig.DefaultStateCommitConfig()
	cfg.WriteMode = sctypes.MemiavlOnly
	return withTestMemIAVL(cfg)
}

// migrateEVMConfig returns the MigrateEVM config used by phase 2 of the
// FlatKV EVM migrate tests. The per-block migration batch is no longer part
// of the config; callers set it after constructing the store via
// store.SetMigrationBatchSize so a small value (e.g. 4) keeps the migration
// in flight across the resume / determinism assertions, while a large value
// drains the boundary in one or two blocks.
func migrateEVMConfig() seidbconfig.StateCommitConfig {
	cfg := seidbconfig.DefaultStateCommitConfig()
	cfg.WriteMode = sctypes.MigrateEVM
	return withTestMemIAVL(cfg)
}

// restartRootMultiWithConfig closes the given store and reopens a fresh
// rootmulti store rooted at the same dir under newCfg. This is the
// shortest reliable simulation of the production "operator stops the
// node, edits app.toml, restarts" sequence: every backend is closed
// (so WALs are flushed and snapshots committed) before the new config
// is applied. Returns the new store and store-key map.
//
// Use this for the MemiavlOnly -> MigrateEVM flip and the MigrateEVM ->
// EVMMigrated migration; for an in-process Close/Open cycle under the
// same config (e.g. the resume path), call newTestRootMulti directly
// after store.Close() to keep intent obvious.
func restartRootMultiWithConfig(
	t *testing.T, store *Store, dir string, newCfg seidbconfig.StateCommitConfig,
) (*Store, map[string]*types.KVStoreKey) {
	t.Helper()
	require.NoError(t, store.Close())
	return newTestRootMulti(t, dir, newCfg)
}

// ---------------------------------------------------------------------------
// EVM test data and helpers
// ---------------------------------------------------------------------------

type evmTestData struct {
	storKey []byte // 0x03 + addr + slot
	nonKey  []byte // 0x0a + addr
	codeKey []byte // 0x07 + addr
}

func newEVMTestData(seed byte) evmTestData {
	var addr [20]byte
	addr[0] = seed
	addr[19] = 0xFF
	var slot [32]byte
	slot[0] = seed + 1
	slot[31] = 0xEE

	internal := make([]byte, 52)
	copy(internal[:20], addr[:])
	copy(internal[20:], slot[:])

	return evmTestData{
		storKey: keys.BuildEVMKey(keys.EVMKeyStorage, internal),
		nonKey:  keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]),
		codeKey: keys.BuildEVMKey(keys.EVMKeyCode, addr[:]),
	}
}

func newMultiEVMTestData(n int) []evmTestData {
	result := make([]evmTestData, n)
	for i := range result {
		result[i] = newEVMTestData(byte(i + 1))
	}
	return result
}

func makeNonce(n uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, n)
	return b
}

func makeSlot(prefix ...byte) []byte {
	var slot [32]byte
	copy(slot[:], prefix)
	return slot[:]
}

// storageMemIAVLKeys returns count distinct EVM storage memiavl keys for one
// address prefix (52-byte internal: 20 addr + 32 slot).
func storageMemIAVLKeys(addrSeed byte, count int) [][]byte {
	out := make([][]byte, count)
	var addr [20]byte
	addr[0] = addrSeed
	for i := 0; i < count; i++ {
		var slot [32]byte
		binary.BigEndian.PutUint64(slot[24:], uint64(i))
		internal := make([]byte, 52)
		copy(internal[:20], addr[:])
		copy(internal[20:], slot[:])
		out[i] = keys.BuildEVMKey(keys.EVMKeyStorage, internal)
	}
	return out
}

// ---------------------------------------------------------------------------
// Store fixtures and block simulators
// ---------------------------------------------------------------------------

type commitRecord struct {
	version     int64
	hash        []byte
	workingHash []byte
	infos       []types.StoreInfo
}

var storeNames = []string{"acc", "bank", "evm"}

func newTestRootMulti(t *testing.T, dir string, scCfg seidbconfig.StateCommitConfig) (*Store, map[string]*types.KVStoreKey) {
	t.Helper()
	return newTestRootMultiWithSS(t, dir, scCfg, seidbconfig.StateStoreConfig{})
}

// newTestRootMultiWithSS is identical to newTestRootMulti but lets the caller
// pass a non-default StateStoreConfig (e.g. to enable SS or tune the async
// write buffer). Used by the SS-path tests in flatkv_modes_test.go.
func newTestRootMultiWithSS(
	t *testing.T, dir string,
	scCfg seidbconfig.StateCommitConfig,
	ssCfg seidbconfig.StateStoreConfig,
) (*Store, map[string]*types.KVStoreKey) {
	t.Helper()
	store := NewStore(dir, scCfg, ssCfg, nil)
	storeKeys := make(map[string]*types.KVStoreKey)
	for _, name := range storeNames {
		sk := types.NewKVStoreKey(name)
		storeKeys[name] = sk
		store.MountStoreWithDB(sk, types.StoreTypeIAVL, nil)
	}
	require.NoError(t, store.LoadLatestVersion())
	return store, storeKeys
}

// finalizeBlock runs GetWorkingHash + Commit and snapshots the resulting
// CommitInfo into a commitRecord. Callers are responsible for driving the
// cache store (cms.Write) before calling this. The snapshot of StoreInfos is
// a defensive copy: rootmulti.Store reassigns lastCommitInfo on the next
// commit but we want the record to remain stable for later assertions.
func finalizeBlock(t *testing.T, store *Store) commitRecord {
	t.Helper()
	workingHash, err := store.GetWorkingHash()
	require.NoError(t, err)
	cid := store.Commit(true)
	infos := make([]types.StoreInfo, len(store.lastCommitInfo.StoreInfos))
	copy(infos, store.lastCommitInfo.StoreInfos)
	return commitRecord{version: cid.Version, hash: cid.Hash, workingHash: workingHash, infos: infos}
}

// simulateBlock writes a deterministic set of acc/bank/evm kvs for the given
// `block` and commits. The `block` parameter is used BOTH as a data seed
// (first byte of written values/nonces) AND as the natural sequence number
// for the block. The committer itself assigns the version based on the
// underlying store state, not on `block`. Callers that want to re-drive the
// chain with different data but the same committed version therefore pass a
// seed like `block+100` or `block+200`; the returned `rec.version` comes
// from the committer and is independent of the seed.
func simulateBlock(t *testing.T, store *Store, storeKeys map[string]*types.KVStoreKey, block int, evmData evmTestData) commitRecord {
	t.Helper()
	cms := store.CacheMultiStore()
	b := byte(block)
	evmKV := cms.GetKVStore(storeKeys["evm"])

	cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), []byte{b})
	cms.GetKVStore(storeKeys["bank"]).Set([]byte("supply"), []byte{b, b})
	evmKV.Set(evmData.storKey, makeSlot(b, 0xAA))
	evmKV.Set(evmData.nonKey, makeNonce(uint64(block)))
	if block == 1 {
		evmKV.Set(evmData.codeKey, []byte{0x60, 0x60, 0x60, b})
	}

	cms.Write()
	return finalizeBlock(t, store)
}

func simulateCosmosOnlyBlock(t *testing.T, store *Store, storeKeys map[string]*types.KVStoreKey, block int) commitRecord {
	t.Helper()
	cms := store.CacheMultiStore()
	b := byte(block)
	cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), []byte{b})
	cms.GetKVStore(storeKeys["bank"]).Set([]byte("supply"), []byte{b})
	cms.Write()
	return finalizeBlock(t, store)
}

// simulateBlockManyStorage writes one block with many EVM storage keys (same
// block height byte b) plus nonce/code for addrBase.
func simulateBlockManyStorage(
	t *testing.T, store *Store, storeKeys map[string]*types.KVStoreKey,
	block int, storageKeys [][]byte, addrBase evmTestData,
) commitRecord {
	t.Helper()
	cms := store.CacheMultiStore()
	b := byte(block)
	evmKV := cms.GetKVStore(storeKeys["evm"])
	cms.GetKVStore(storeKeys["acc"]).Set([]byte("acct1"), []byte{b})
	cms.GetKVStore(storeKeys["bank"]).Set([]byte("supply"), []byte{b, b})
	for i, sk := range storageKeys {
		evmKV.Set(sk, makeSlot(b, byte(i&0xFF)))
	}
	evmKV.Set(addrBase.nonKey, makeNonce(uint64(block)))
	if block == 1 {
		evmKV.Set(addrBase.codeKey, []byte{0x60, 0x60, 0x60, b})
	}
	cms.Write()
	return finalizeBlock(t, store)
}

// ---------------------------------------------------------------------------
// Assertions
// ---------------------------------------------------------------------------

func findStoreInfo(infos []types.StoreInfo, name string) *types.StoreInfo {
	for i := range infos {
		if infos[i].Name == name {
			return &infos[i]
		}
	}
	return nil
}

func verifyHistoricalHashes(t *testing.T, store *Store, records []commitRecord) {
	t.Helper()
	for _, rec := range records {
		scStore, err := store.scStore.LoadVersion(rec.version, true)
		require.NoError(t, err)

		commitInfo := convertCommitInfo(scStore.LastCommitInfo())
		commitInfo = amendCommitInfo(commitInfo, store.storesParams)

		require.Equalf(t, rec.hash, commitInfo.Hash(),
			"ROOT HASH MISMATCH at version %d", rec.version)

		_ = scStore.Close()
	}
}

// ---------------------------------------------------------------------------
// Low-level FlatKV / memiavl manipulation (used by recovery tests)
// ---------------------------------------------------------------------------

// rollbackFlatKV opens the FlatKV store at dir, loads latest, rolls back to
// the target version, and closes. Used to simulate a crash where FlatKV is
// behind cosmos.
func rollbackFlatKV(t *testing.T, dir string, cfg seidbconfig.StateCommitConfig, target int64) {
	t.Helper()
	flatkvCfg := cfg.FlatKVConfig
	flatkvCfg.DataDir = utils.GetFlatKVPath(dir)
	evmStore, err := flatkv.NewCommitStore(context.Background(), &flatkvCfg)
	require.NoError(t, err)
	_, err = evmStore.LoadVersion(0, false)
	require.NoError(t, err)
	require.NoError(t, evmStore.Rollback(target))
	require.NoError(t, evmStore.Close())
}

// rollbackMemiavl opens the memiavl commit store at dir, loads latest, rolls
// back to target, and closes. Used to simulate a crash where cosmos is behind
// FlatKV.
func rollbackMemiavl(t *testing.T, dir string, cfg seidbconfig.StateCommitConfig, target int64) {
	t.Helper()
	cs := scmemiavl.NewCommitStore(dir, cfg.MemIAVLConfig)
	_, err := cs.LoadVersion(0, false)
	require.NoError(t, err)
	require.NoError(t, cs.Rollback(target))
	require.NoError(t, cs.Close())
}

// openFlatKVReadOnly opens a readonly FlatKV store at the given rootmulti
// home directory at the specified version. Caller must Close() when done.
func openFlatKVReadOnly(t *testing.T, dir string, cfg seidbconfig.StateCommitConfig, version int64) flatkv.Store {
	t.Helper()
	flatkvCfg := cfg.FlatKVConfig
	flatkvCfg.DataDir = utils.GetFlatKVPath(dir)
	store, err := flatkv.NewCommitStore(context.Background(), &flatkvCfg)
	require.NoError(t, err)
	ro, err := store.LoadVersion(version, true)
	require.NoError(t, err)
	// Close the parent immediately: the returned read-only view owns an
	// independent context and resources, so it must survive the parent's
	// teardown. This doubles as a regression guard — if the clone is ever
	// re-rooted at the parent's context, cold reads on ro (those that miss the
	// warm cache and hit the async pebble loader) will fail with "context
	// canceled".
	require.NoError(t, store.Close())
	return ro
}

// verifyFlatKVSelfConsistent opens FlatKV readonly at the latest version and
// asserts that a full-scan LtHash recomputation matches the committed root.
// The rootmulti store at dir must already be closed.
func verifyFlatKVSelfConsistent(t *testing.T, dir string, cfg seidbconfig.StateCommitConfig) {
	t.Helper()
	ro := openFlatKVReadOnly(t, dir, cfg, 0)
	require.NoError(t, flatkv.VerifyLtHash(ro))
	require.NoError(t, ro.Close())
}

// ---------------------------------------------------------------------------
// TestOnlyDualWrite equivalence helpers
// ---------------------------------------------------------------------------
//
// These helpers power the memiavl<->flatkv differential oracle in
// flatkv_equivalence_test.go. TestOnlyDualWrite mirrors every EVM write into both
// backends; any divergence between them at the end of a workload points to a
// FlatKV bug (memiavl is treated as ground truth for this oracle).

// collectCommitKVStore iterates a memiavl child store in full and returns a
// snapshot map keyed by memiavl-format key.
func collectCommitKVStore(t *testing.T, s sctypes.CommitKVStore) map[string][]byte {
	t.Helper()
	out := make(map[string][]byte)
	it := s.Iterator(nil, nil, true)
	defer func() { require.NoError(t, it.Close()) }()
	for ; it.Valid(); it.Next() {
		out[string(bytes.Clone(it.Key()))] = bytes.Clone(it.Value())
	}
	require.NoError(t, it.Error())
	return out
}

// collectFlatKVEVM opens FlatKV at dir and drains its Exporter at the given
// version (0 = latest), returning a snapshot map in the same memiavl-format
// key/value shape as collectCommitKVStore. The rootmulti store at dir must
// already be closed.
//
// Post PR #3250 the FlatKV Exporter emits raw physical keys
// ("evm/" + prefixByte + strippedKey) and vtype-serialized values. This helper
// mirrors the conversion logic in convertFlatKVNodes (sei-db/state_db/ss/
// composite/store.go) to re-materialize the EVM rows in memiavl format so the
// downstream equivalence assertion can compare them byte-for-byte against the
// memiavl child store.
func collectFlatKVEVM(t *testing.T, dir string, cfg seidbconfig.StateCommitConfig, version int64) map[string][]byte {
	t.Helper()
	flatkvCfg := cfg.FlatKVConfig
	flatkvCfg.DataDir = utils.GetFlatKVPath(dir)

	s, err := flatkv.NewCommitStore(context.Background(), &flatkvCfg)
	require.NoError(t, err)
	defer func() { require.NoError(t, s.Close()) }()

	if _, err := s.LoadVersion(0, false); err != nil {
		require.NoError(t, err)
	}

	exp, err := s.Exporter(version)
	require.NoError(t, err)
	defer func() { require.NoError(t, exp.Close()) }()

	out := make(map[string][]byte)
	for {
		item, err := exp.Next()
		if err != nil {
			require.True(t, errors.Is(err, errorutils.ErrorExportDone),
				"unexpected exporter error: %v", err)
			break
		}
		// The self-describing exporter emits the keys.FlatKVStoreKey module
		// header (a string) before any node; skip it.
		if name, ok := item.(string); ok {
			require.Equal(t, keys.FlatKVStoreKey, name, "unexpected module header")
			continue
		}
		node, ok := item.(*sctypes.SnapshotNode)
		require.Truef(t, ok, "expected *SnapshotNode, got %T", item)
		addFlatKVNodeToMap(t, out, node)
	}
	return out
}

// addFlatKVNodeToMap converts a single raw-physical FlatKV exporter node into
// one or more memiavl-format (key, value) entries and inserts them into out.
// Non-EVM modules are unexpected in this test suite and fail the test.
func addFlatKVNodeToMap(t *testing.T, out map[string][]byte, node *sctypes.SnapshotNode) {
	t.Helper()

	moduleName, innerKey, err := ktype.StripModulePrefix(node.Key)
	require.NoError(t, err)
	require.Equalf(t, keys.EVMStoreKey, moduleName,
		"unexpected non-EVM module %q in FlatKV export (key=%x)", moduleName, node.Key)

	kind, strippedKey := keys.ParseEVMKey(innerKey)
	switch kind {
	case keys.EVMKeyNonce:
		acct, err := vtype.DeserializeAccountData(node.Value)
		require.NoErrorf(t, err, "deserialize AccountData for key %x", node.Key)
		if !acct.IsDelete() {
			nonceBuf := make([]byte, 8)
			binary.BigEndian.PutUint64(nonceBuf, acct.GetNonce())
			out[string(keys.BuildEVMKey(keys.EVMKeyNonce, strippedKey))] = nonceBuf
		}
		if codeHash := acct.GetCodeHash(); *codeHash != (vtype.CodeHash{}) {
			out[string(keys.BuildEVMKey(keys.EVMKeyCodeHash, strippedKey))] = bytes.Clone(codeHash[:])
		}

	case keys.EVMKeyStorage:
		sd, err := vtype.DeserializeStorageData(node.Value)
		require.NoErrorf(t, err, "deserialize StorageData for key %x", node.Key)
		v := sd.GetValue()
		out[string(bytes.Clone(innerKey))] = bytes.Clone(v[:])

	case keys.EVMKeyCode:
		cd, err := vtype.DeserializeCodeData(node.Value)
		require.NoErrorf(t, err, "deserialize CodeData for key %x", node.Key)
		out[string(bytes.Clone(innerKey))] = bytes.Clone(cd.GetBytecode())

	case keys.EVMKeyLegacy:
		ld, err := vtype.DeserializeLegacyData(node.Value)
		require.NoErrorf(t, err, "deserialize LegacyData for key %x", node.Key)
		out[string(bytes.Clone(innerKey))] = bytes.Clone(ld.GetValue())

	default:
		t.Fatalf("unexpected EVM key kind %v in FlatKV export (key=%x)", kind, node.Key)
	}
}

// requireSameEVMKVSet asserts that two snapshot maps are byte-for-byte equal
// in both directions — the forward check catches "FlatKV missing / stale",
// the reverse catches "FlatKV has extra rows memiavl never saw".
func requireSameEVMKVSet(t *testing.T, memMap, flatMap map[string][]byte) {
	t.Helper()
	require.Equalf(t, len(memMap), len(flatMap),
		"evm entry count mismatch: memiavl=%d flatkv=%d", len(memMap), len(flatMap))

	for k, mv := range memMap {
		fv, ok := flatMap[k]
		require.Truef(t, ok, "flatkv missing memiavl key %x", []byte(k))
		require.Equalf(t, mv, fv,
			"value mismatch for key %x:\n  memiavl: %x\n  flatkv:  %x",
			[]byte(k), mv, fv)
	}
	for k := range flatMap {
		_, ok := memMap[k]
		require.Truef(t, ok, "memiavl missing key %x (flatkv has it)", []byte(k))
	}
}
