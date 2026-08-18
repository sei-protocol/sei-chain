package composite

import (
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	dbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/lthash"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

// TestAuto_TornFlatKVSeedRecoversAndReseeds is the end-to-end guard for the migration-entry seed.
//
// Materializing flatkv stamps a starting version into each of its four data DBs, one non-atomic write
// at a time, so a crash partway through leaves them disagreeing about a version that never entered the
// WAL. That state used to be fatal and unrecoverable: flatKV.LoadLatest failed, and because it is
// called before the seeding branch below it, the level-triggered retry re-entered the same failure
// every time and the node could not start at all. Nothing has been written to flatkv at that point, so
// the store discards itself and the composite seeds it again.
func TestAuto_TornFlatKVSeedRecoversAndReseeds(t *testing.T) {
	dir := t.TempDir()
	cfg := autoExportConfig()

	cs := openAutoStoreWithConfig(t, dir, cfg, 100)
	for i := 1; i <= 3; i++ {
		require.NoError(t, cs.ApplyChangeSets([]*proto.NamedChangeSet{
			{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("k"), Value: []byte{byte(0x10 + i)}},
			}}},
		}))
		_, err := cs.Commit()
		require.NoError(t, err)
	}
	require.Equal(t, int64(3), cs.memIAVL.Version())
	require.Nil(t, cs.flatKV, "fixture precondition: flatkv must not be materialized yet")
	require.NoError(t, cs.Close())

	// Bring the flatkv directory into existence in a never-seeded state, the way materializeFlatKV
	// does before it seeds, then leave a seed half-written across its data DBs.
	flatkvDir := utils.GetFlatKVPath(dir)
	initializeUnseededFlatKV(t, cfg, flatkvDir)
	stampSeedRecords(t, flatkvDir, 99, "account", "code")

	reopened, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	defer func() { _ = reopened.Close() }()
	require.NoError(t, reopened.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))

	require.NoError(t, reopened.LoadLatest(), "an interrupted flatkv seed must not stop the node")
	require.Equal(t, int64(3), reopened.memIAVL.Version())
	require.Nil(t, reopened.flatKV,
		"an auto store keeps flatkv closed until a migration starts, even once it is seeded")

	// The re-seed is durable: materializing flatkv again finds it at memiavl's height rather than at
	// 0, so the seeding branch does not run a second time.
	require.NoError(t, reopened.SetWriteMode(types.MigrateEVM))
	require.NotNil(t, reopened.flatKV)
	require.Equal(t, int64(3), reopened.flatKV.Version(),
		"the composite must have re-seeded flatkv to memiavl's height")

	// The chain keeps going from there, with both backends in lockstep.
	require.NoError(t, reopened.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
			{Key: []byte("k"), Value: []byte{0xFF}},
		}}},
	}))
	_, err = reopened.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(4), reopened.memIAVL.Version())
	require.Equal(t, int64(4), reopened.flatKV.Version())
}

// initializeUnseededFlatKV creates the flatkv directory layout — a baseline snapshot and a working
// clone whose four data DBs carry no metadata — without seeding it to any version.
func initializeUnseededFlatKV(t *testing.T, cfg config.StateCommitConfig, flatkvDir string) {
	t.Helper()
	flatkvCfg := cfg.FlatKVConfig
	flatkvCfg.DataDir = flatkvDir

	wal, err := flatkv.OpenStateWAL(&flatkvCfg)
	require.NoError(t, err)
	store, err := flatkv.NewCommitStore(t.Context(), &flatkvCfg, wal)
	require.NoError(t, err)
	require.NoError(t, store.LoadLatest())
	require.Equal(t, int64(0), store.Version())
	require.NoError(t, store.Close())
}

// stampSeedRecords writes a version and the identity root straight into the named data DBs of the
// flatkv working directory, reproducing a seed that crashed between its per-DB writes. The DBs are
// opened directly because a completed seed also writes a snapshot, so the public API cannot leave a
// store in this state.
func stampSeedRecords(t *testing.T, flatkvDir string, version int64, dbDirs ...string) {
	t.Helper()
	versionBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(versionBytes, uint64(version))

	for _, dbDir := range dbDirs {
		pcfg := pebbledb.DefaultConfig()
		pcfg.DataDir = filepath.Join(flatkvDir, "working", dbDir)
		pcfg.EnableMetrics = false
		db, err := pebbledb.Open(t.Context(), &pcfg)
		require.NoError(t, err)
		opts := dbtypes.WriteOptions{Sync: true}
		require.NoError(t, db.Set(ktype.MetaVersionKey, versionBytes, opts))
		require.NoError(t, db.Set(ktype.MetaLtHashKey, lthash.New().Marshal(), opts))
		require.NoError(t, db.Close())
	}
}
