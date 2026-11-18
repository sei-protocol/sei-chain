package audit

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

const (
	// storageDB is the name of the data database holding EVM storage slots.
	storageDB = "storage"

	// miscDB is the name of the data database holding every row no other database holds.
	miscDB = "misc"

	// auditTestSnapshotInterval is the snapshot interval of the test store, and so the version of the
	// snapshot it takes.
	auditTestSnapshotInterval = 2
)

// auditTestSnapshot is a closed store's root, holding a snapshot of rows in every data database and in more than
// one module.
type auditTestSnapshot struct {
	// root is the FlatKV root directory.
	root string

	// version is the version of the snapshot the current link names.
	version uint64

	// globalChecksum is the checksum of the store root the store reported at that version.
	globalChecksum []byte
}

// newAuditTestSnapshot commits a few blocks touching every data database, including a delete, snapshots the
// result, and closes the store.
func newAuditTestSnapshot(t *testing.T) auditTestSnapshot {
	t.Helper()
	cfg := config.DefaultTestConfig(t)
	cfg.SnapshotInterval = auditTestSnapshotInterval
	stateWAL, err := flatkv.OpenStateWAL(cfg)
	require.NoError(t, err)
	s, err := flatkv.NewCommitStore(t.Context(), cfg, stateWAL)
	require.NoError(t, err)
	require.NoError(t, s.LoadLatest())

	evmPairs := []*proto.KVPair{
		noncePair(addrN(1), 1),
		noncePair(addrN(2), 7),
		codeHashPair(addrN(1), codeHashN(1)),
		codePair(addrN(1), []byte{0x60, 0x80, 0x60, 0x40}),
		codePair(addrN(2), []byte{0x60, 0x00}),
	}
	for i := byte(1); i <= 20; i++ {
		evmPairs = append(evmPairs, storagePair(addrN(i%3+1), slotN(i), []byte{i}))
	}
	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: evmPairs}},
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("supply/usei"), Value: []byte("100")},
			{Key: []byte("balance/a"), Value: []byte("40")},
		}}},
		{Name: "gov", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("proposal/1"), Value: []byte("open")},
		}}},
	}))
	_, err = s.Commit(s.Version() + 1)
	require.NoError(t, err)

	require.NoError(t, s.ApplyChangeSets(s.Version()+1, []*proto.NamedChangeSet{
		{Name: "evm", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			storageDeletePair(addrN(2), slotN(1)),
			codeDeletePair(addrN(2)),
			storagePair(addrN(1), slotN(2), []byte{0xFF}),
		}}},
		{Name: "bank", Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("balance/a"), Value: []byte("41")},
		}}},
	}))
	_, err = s.Commit(s.Version() + 1)
	require.NoError(t, err)
	require.Equal(t, int64(auditTestSnapshotInterval), s.Version())
	require.NoError(t, s.FlushSnapshots())

	require.NoError(t, s.FlushHashes())
	// A nil listener reports the hash at the height the store stands at without subscribing to anything.
	current, err := s.RegisterHashListener(nil)
	require.NoError(t, err)
	checksum := current.Global.Checksum()
	snapshot := auditTestSnapshot{
		root:           cfg.DataDir,
		version:        auditTestSnapshotInterval,
		globalChecksum: checksum[:],
	}
	require.NoError(t, s.Close())
	return snapshot
}

// auditTestConfig returns a config that audits snapshot with the given number of hashers, in batches small
// enough that every hasher takes several.
func auditTestConfig(snapshot auditTestSnapshot, hashers uint32) *SnapshotAuditConfig {
	config := DefaultSnapshotAuditConfig()
	config.Root = snapshot.root
	config.Hashers = hashers
	config.BatchSize = 2
	return config
}

// openAuditTestSnapshotDB opens one database of the snapshot in place, read-write, so a test can forge a
// corruption into it.
func openAuditTestSnapshotDB(t *testing.T, snapshot auditTestSnapshot, dir string) types.KeyValueDB {
	t.Helper()
	cfg := pebbledb.DefaultTestConfig(t)
	cfg.DataDir = filepath.Join(snapshot.root, flatkv.SnapshotDirName(int64(snapshot.version)), dir) //nolint:gosec
	db, err := pebbledb.Open(t.Context(), &cfg)
	require.NoError(t, err)
	return db
}

// findDBAudit returns the report's entry for the named database.
func findDBAudit(t *testing.T, report *SnapshotAuditReport, name string) *DBAudit {
	t.Helper()
	for i := range report.DBs {
		if report.DBs[i].Name == name {
			return &report.DBs[i]
		}
	}
	require.FailNow(t, "no audit for database", name)
	return nil
}

// findModuleAudit returns the database entry's entry for the named module.
func findModuleAudit(t *testing.T, db *DBAudit, module string) *ModuleAudit {
	t.Helper()
	for i := range db.Modules {
		if db.Modules[i].Module == module {
			return &db.Modules[i]
		}
	}
	require.FailNow(t, "no audit for module", "%s/%s", db.Name, module)
	return nil
}

// requireOnlyFailures asserts that the report fails exactly the (database, module) pairs in failing, with a
// module of "" standing for the database's own version and root, and that every other comparison matches.
func requireOnlyFailures(t *testing.T, report *SnapshotAuditReport, failing map[string]map[string]bool) {
	t.Helper()
	for i := range report.DBs {
		db := &report.DBs[i]
		require.True(t, db.Version.Matches(), "version of %s", db.Name)
		require.Equal(t, !failing[db.Name][""], db.Root.Matches(), "root of %s", db.Name)
		for j := range db.Modules {
			module := &db.Modules[j]
			wantMatch := !failing[db.Name][module.Module]
			require.Equal(t, wantMatch, module.Matches(), "%s/%s", db.Name, module.Module)
		}
	}
	require.False(t, report.Passed())
}

func TestAuditSnapshotPassesOnACleanSnapshot(t *testing.T) {
	snapshot := newAuditTestSnapshot(t)

	single, err := AuditSnapshot(t.Context(), auditTestConfig(snapshot, 1))
	require.NoError(t, err)
	require.True(t, single.Passed())
	require.Equal(t, snapshot.version, single.Version)
	require.Equal(t, snapshot.globalChecksum, single.Global.Scanned[:])
	require.Len(t, single.DBs, len(flatkv.DataDBDirs()))
	for i, name := range flatkv.DataDBDirs() {
		require.Equal(t, name, single.DBs[i].Name)
		require.NotZero(t, single.DBs[i].ScannedStats().KeyCount, "every database holds rows: %s", name)
		require.NotZero(t, single.DBs[i].DiskBytes, name)
		require.Equal(t, single.DBs[i].RecordedStats(), single.DBs[i].ScannedStats(), name)
	}
	misc := findDBAudit(t, single, miscDB)
	require.Equal(t, int64(2), findModuleAudit(t, misc, "bank").Scanned.KeyCount)
	require.Equal(t, int64(1), findModuleAudit(t, misc, "gov").Scanned.KeyCount)
	require.Equal(t, single.ScannedStats().KeyCount, int64(single.Rows)) //nolint:gosec // small
	require.Equal(t, single.ScannedStats().Bytes, int64(single.Bytes))   //nolint:gosec // small
	require.Positive(t, single.LogicalToDiskRatio())

	// The partition of rows across hashers must not change anything the report computes.
	many, err := AuditSnapshot(t.Context(), auditTestConfig(snapshot, 8))
	require.NoError(t, err)
	require.Equal(t, single.DBs, many.DBs)
	require.Equal(t, single.Global, many.Global)
	require.Equal(t, single.Rows, many.Rows)
	require.Equal(t, single.Bytes, many.Bytes)

	explicit := auditTestConfig(snapshot, 4)
	explicit.Version = snapshot.version
	pinned, err := AuditSnapshot(t.Context(), explicit)
	require.NoError(t, err)
	require.Equal(t, single.DBs, pinned.DBs)

	requireNoAuditDirs(t, snapshot.root)
}

func TestAuditSnapshotReportsARowNoMetadataAccountsFor(t *testing.T) {
	snapshot := newAuditTestSnapshot(t)
	db := openAuditTestSnapshotDB(t, snapshot, storageDB)
	require.NoError(t, db.Set(storagePhysKey(addrN(9), slotN(9)), padLeft32(0x09), types.WriteOptions{Sync: true}))
	require.NoError(t, db.Close())

	report, err := AuditSnapshot(t.Context(), auditTestConfig(snapshot, 4))
	require.NoError(t, err)
	requireOnlyFailures(t, report, map[string]map[string]bool{storageDB: {"": true, "evm": true}})
	require.False(t, report.Global.Matches())

	module := findModuleAudit(t, findDBAudit(t, report, storageDB), "evm")
	require.False(t, module.Hash.Matches())
	require.Equal(t, module.Recorded.KeyCount+1, module.Scanned.KeyCount)
}

func TestAuditSnapshotReportsWrongModuleStats(t *testing.T) {
	snapshot := newAuditTestSnapshot(t)
	db := openAuditTestSnapshotDB(t, snapshot, miscDB)
	forged := lthash.ModuleStats{KeyCount: 99, Bytes: 1234}
	require.NoError(t, db.Set(ktype.ModuleStatsKey("bank"), forged.Marshal(), types.WriteOptions{Sync: true}))
	require.NoError(t, db.Close())

	report, err := AuditSnapshot(t.Context(), auditTestConfig(snapshot, 4))
	require.NoError(t, err)
	requireOnlyFailures(t, report, map[string]map[string]bool{miscDB: {"bank": true}})
	require.True(t, report.Global.Matches())

	module := findModuleAudit(t, findDBAudit(t, report, miscDB), "bank")
	require.True(t, module.Hash.Matches())
	require.Equal(t, forged, module.Recorded)
}

func TestAuditSnapshotReportsARecordedModuleWithNoRows(t *testing.T) {
	snapshot := newAuditTestSnapshot(t)
	db := openAuditTestSnapshotDB(t, snapshot, miscDB)
	ghost := lthash.ComputeLtHash(nil, []mutation.Mutation{{Key: []byte("ghost/k"), NewValue: []byte("v")}})
	require.NoError(t, db.Set(ktype.ModuleLtHashKey("ghost"), ghost.Marshal(), types.WriteOptions{Sync: true}))
	require.NoError(t, db.Close())

	report, err := AuditSnapshot(t.Context(), auditTestConfig(snapshot, 4))
	require.NoError(t, err)
	requireOnlyFailures(t, report, map[string]map[string]bool{miscDB: {"ghost": true}})

	module := findModuleAudit(t, findDBAudit(t, report, miscDB), "ghost")
	require.Equal(t, lthash.New().Checksum(), module.Hash.Scanned)
	require.Equal(t, ghost.Checksum(), module.Hash.Recorded)
}

func TestAuditSnapshotSurvivesPruningOfTheSource(t *testing.T) {
	snapshot := newAuditTestSnapshot(t)
	version := int64(snapshot.version) //nolint:gosec // small

	linked := filepath.Join(t.TempDir(), "linked")
	require.NoError(t, flatkv.HardLinkSnapshot(snapshot.root, version, linked))
	require.NoError(t, os.RemoveAll(filepath.Join(snapshot.root, flatkv.SnapshotDirName(version))))

	report, err := auditLinkedSnapshot(t.Context(), auditTestConfig(snapshot, 4), linked, snapshot.version)
	require.NoError(t, err)
	require.True(t, report.Passed())
	require.Equal(t, snapshot.globalChecksum, report.Global.Scanned[:])
}

func TestAuditSnapshotStopsWhenCancelled(t *testing.T) {
	snapshot := newAuditTestSnapshot(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := AuditSnapshot(ctx, auditTestConfig(snapshot, 4))
	require.ErrorIs(t, err, context.Canceled)
	requireNoAuditDirs(t, snapshot.root)
}

func TestAuditSnapshotRejectsAMissingSnapshot(t *testing.T) {
	snapshot := newAuditTestSnapshot(t)
	config := auditTestConfig(snapshot, 1)
	config.Version = snapshot.version + 100

	_, err := AuditSnapshot(t.Context(), config)
	require.ErrorIs(t, err, os.ErrNotExist)
	requireNoAuditDirs(t, snapshot.root)
}

func TestAuditSnapshotRemovesStaleAuditDirs(t *testing.T) {
	snapshot := newAuditTestSnapshot(t)
	stale := plantStaleAuditDir(t, snapshot)
	unrelated := filepath.Join(snapshot.root, "unrelated")
	require.NoError(t, os.Mkdir(unrelated, 0o750))

	report, err := AuditSnapshot(t.Context(), auditTestConfig(snapshot, 4))
	require.NoError(t, err)
	require.True(t, report.Passed())
	require.NoDirExists(t, stale)
	requireNoAuditDirs(t, snapshot.root)
	require.DirExists(t, unrelated)
	//nolint:gosec // small
	require.DirExists(t, filepath.Join(snapshot.root, flatkv.SnapshotDirName(int64(snapshot.version))))

	// Removing the stale links must leave the snapshot they pointed at intact.
	again, err := AuditSnapshot(t.Context(), auditTestConfig(snapshot, 4))
	require.NoError(t, err)
	require.True(t, again.Passed())
}

func TestAuditSnapshotKeepsStaleAuditDirsWhenAsked(t *testing.T) {
	snapshot := newAuditTestSnapshot(t)
	stale := plantStaleAuditDir(t, snapshot)
	config := auditTestConfig(snapshot, 4)
	config.KeepStaleAuditDirs = true

	report, err := AuditSnapshot(t.Context(), config)
	require.NoError(t, err)
	require.True(t, report.Passed())
	require.DirExists(t, stale)
}

func TestAuditSnapshotLogsItsLinkDirectory(t *testing.T) {
	snapshot := newAuditTestSnapshot(t)
	var log bytes.Buffer
	config := auditTestConfig(snapshot, 4)
	config.Log = &log

	_, err := AuditSnapshot(t.Context(), config)
	require.NoError(t, err)
	root, err := filepath.Abs(snapshot.root)
	require.NoError(t, err)
	require.Contains(t, log.String(), filepath.Join(root, snapshotAuditDirPrefix))
}

// plantStaleAuditDir leaves behind under the snapshot's root what an audit killed mid-scan leaves: its temporary
// directory, holding a hard-linked copy of the snapshot. It returns the directory's path.
func plantStaleAuditDir(t *testing.T, snapshot auditTestSnapshot) string {
	t.Helper()
	stale, err := os.MkdirTemp(snapshot.root, snapshotAuditDirPrefix+"*")
	require.NoError(t, err)
	version := int64(snapshot.version) //nolint:gosec // small
	require.NoError(t, flatkv.HardLinkSnapshot(snapshot.root, version, filepath.Join(stale, snapshotAuditLinkName)))
	return stale
}

// requireNoAuditDirs asserts that no audit left its temporary directory under root.
func requireNoAuditDirs(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	require.NoError(t, err)
	for _, entry := range entries {
		name := entry.Name()
		require.False(t, strings.HasPrefix(name, snapshotAuditDirPrefix), "left behind: %s", name)
	}
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

// padLeft32 returns a 32-byte big-endian value with the given bytes right-aligned.
func padLeft32(val ...byte) []byte {
	var b [32]byte
	copy(b[32-len(val):], val)
	return b[:]
}

func noncePair(addr ktype.Address, nonce uint64) *proto.KVPair {
	value := make([]byte, vtype.NonceLen)
	binary.BigEndian.PutUint64(value, nonce)
	return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyNonce, addr[:]), Value: value}
}

func codeHashPair(addr ktype.Address, ch vtype.CodeHash) *proto.KVPair {
	return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyCodeHash, addr[:]), Value: ch[:]}
}

func codePair(addr ktype.Address, bytecode []byte) *proto.KVPair {
	return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyCode, addr[:]), Value: bytecode}
}

func codeDeletePair(addr ktype.Address) *proto.KVPair {
	return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyCode, addr[:]), Delete: true}
}

func storagePair(addr ktype.Address, slot ktype.Slot, val []byte) *proto.KVPair {
	return &proto.KVPair{
		Key:   keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)),
		Value: padLeft32(val...),
	}
}

func storageDeletePair(addr ktype.Address, slot ktype.Slot) *proto.KVPair {
	return &proto.KVPair{Key: keys.BuildEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot)), Delete: true}
}

// storagePhysKey returns the key a storage slot is stored under in the storage database.
func storagePhysKey(addr ktype.Address, slot ktype.Slot) []byte {
	return ktype.BuildPhysicalEVMKey(keys.EVMKeyStorage, ktype.StorageKey(addr, slot))
}
