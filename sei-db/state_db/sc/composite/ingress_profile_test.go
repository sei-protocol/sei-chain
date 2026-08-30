package composite

import (
	"encoding/binary"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

func ingressProfileConfig() config.StateCommitConfig {
	cfg := config.DefaultStateCommitConfig()
	cfg.IngressProfile = true
	return cfg
}

func seedFlatKVMigrationVersion(t *testing.T, dir string, migrationVersion uint64) {
	t.Helper()
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.FlatKVOnly
	cfg.WriteModeEnableAuto = false
	store, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, store.LoadLatest())

	version := make([]byte, 8)
	binary.BigEndian.PutUint64(version, migrationVersion)
	require.NoError(t, store.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: migration.MigrationStore,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{
			Key:   []byte(migration.MigrationVersionKey),
			Value: version,
		}}},
	}}))
	_, err = store.Commit(1)
	require.NoError(t, err)
	require.NoError(t, store.Close())
}

// An empty store must load, because that is the state a node is in when state
// sync is about to restore into it. Refusing here would kill the process before
// Tendermint could offer a snapshot.
func TestIngressProfileLoadsEmptyStoreForStateSync(t *testing.T) {
	store, err := NewCompositeCommitStore(t.Context(), t.TempDir(), ingressProfileConfig())
	require.NoError(t, err)
	require.NoError(t, store.LoadLatest())
	require.Zero(t, store.Version())
	require.NoError(t, store.Close())
}

// The one route out of an empty store that must stay closed: building state
// from genesis, which would produce a pre-migration store no ingress node can
// serve. The error has to name state sync, since that is the way forward.
func TestIngressProfileRefusesGenesisInitialization(t *testing.T) {
	store, err := NewCompositeCommitStore(t.Context(), t.TempDir(), ingressProfileConfig())
	require.NoError(t, err)
	require.NoError(t, store.LoadLatest())

	err = store.SetInitialVersion(1)
	require.ErrorContains(t, err, "cannot build state from genesis")
	require.ErrorContains(t, err, "state sync")
	require.NoError(t, store.Close())
}

// A flatkv-only store fed a cosmos-module section has no backend that can
// accept it. The flatkv importer would take the module name and then drop every
// node as a wrong-version leaf, so an unguarded restore reports success and
// leaves an empty store.
func TestIngressProfileRejectsMemIAVLSnapshotSection(t *testing.T) {
	dir := t.TempDir()
	seedFlatKVMigrationVersion(t, dir, migration.Version3_FlatKVOnly)

	store, err := NewCompositeCommitStore(t.Context(), dir, ingressProfileConfig())
	require.NoError(t, err)
	require.NoError(t, store.LoadLatest())

	importer, err := store.Importer(2)
	require.NoError(t, err)
	require.ErrorContains(t, importer.AddModule(keys.BankStoreKey), "no memiavl backend")
	require.NoError(t, importer.AddModule(keys.FlatKVStoreKey))
	_ = importer.Close()
	require.NoError(t, store.Close())
}

func TestIngressProfileAcceptsMigrationVersionThree(t *testing.T) {
	dir := t.TempDir()
	seedFlatKVMigrationVersion(t, dir, migration.Version3_FlatKVOnly)

	store, err := NewCompositeCommitStore(t.Context(), dir, ingressProfileConfig())
	require.NoError(t, err)
	require.NoError(t, store.LoadLatest())
	require.Nil(t, store.memIAVL)
	require.True(t, store.config.FlatKVConfig.LowMemory)
	require.ErrorContains(t, store.SetInitialVersion(10), "cannot build state from genesis")
	require.NoError(t, store.ApplyChangeSets(nil))
	_, err = store.Commit(2)
	require.NoError(t, err)
	_, err = store.LoadVersionReadOnly(1)
	require.ErrorContains(t, err, "only supports the current state version")
	require.NoError(t, store.Close())
}

// The bootstrap path end to end: an empty ingress store loads, accepts a
// restore from a post-migration FlatKV peer, and comes back up serving the
// restored version. The migration version rides along in the snapshot as an
// ordinary "migration" module row, which is what lets the post-restore load
// derive flatkv_only and pass validation.
func TestIngressProfileBootstrapsFromStateSyncRestore(t *testing.T) {
	sourceDir := t.TempDir()
	seedFlatKVMigrationVersion(t, sourceDir, migration.Version3_FlatKVOnly)

	sourceCfg := config.DefaultStateCommitConfig()
	sourceCfg.WriteMode = types.FlatKVOnly
	sourceCfg.WriteModeEnableAuto = false
	source, err := NewCompositeCommitStore(t.Context(), sourceDir, sourceCfg)
	require.NoError(t, err)
	require.NoError(t, source.LoadLatest())
	require.NoError(t, source.ApplyChangeSets([]*proto.NamedChangeSet{{
		Name: keys.BankStoreKey,
		Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{
			Key:   []byte("ingress-key"),
			Value: []byte("ingress-value"),
		}}},
	}}))
	restoreVersion, err := source.Commit(2)
	require.NoError(t, err)

	exporter, err := source.Exporter(restoreVersion)
	require.NoError(t, err)
	items := drainCompositeExporter(t, exporter)
	require.NoError(t, exporter.Close())
	require.NoError(t, source.Close())

	targetDir := t.TempDir()
	target, err := NewCompositeCommitStore(t.Context(), targetDir, ingressProfileConfig())
	require.NoError(t, err)
	require.NoError(t, target.LoadLatest())
	require.Zero(t, target.Version())

	importer, err := target.Importer(restoreVersion)
	require.NoError(t, err)
	replayImport(t, importer, items)
	require.NoError(t, importer.Close())

	// rootmulti reloads the store after a restore; that reload is where the
	// ingress validation runs against the freshly imported data.
	require.NoError(t, target.LoadLatest())
	require.Equal(t, restoreVersion, target.Version())

	value, found, err := target.Get(keys.BankStoreKey, []byte("ingress-key"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("ingress-value"), value)
	require.NoError(t, target.Close())
}

func TestIngressProfileRejectsPreMigrationFlatKV(t *testing.T) {
	dir := t.TempDir()
	seedFlatKVMigrationVersion(t, dir, migration.Version1_MigrateEVM)

	store, err := NewCompositeCommitStore(t.Context(), dir, ingressProfileConfig())
	require.NoError(t, err)
	err = store.LoadLatest()
	require.ErrorContains(t, err, "requires migration version 3")
	require.NoError(t, store.Close())
}
