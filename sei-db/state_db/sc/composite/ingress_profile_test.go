package composite

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

func ingressProfileConfig() config.StateCommitConfig {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.Auto
	cfg.IngressProfile = true
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0
	return cfg
}

// openStore constructs a store over dir and initializes its child stores, leaving it unloaded.
func openStore(t *testing.T, dir string, cfg config.StateCommitConfig) *CompositeCommitStore {
	t.Helper()
	store, err := NewCompositeCommitStore(t.Context(), dir, cfg)
	require.NoError(t, err)
	require.NoError(t, store.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	return store
}

func loadIngressStore(t *testing.T, dir string) *CompositeCommitStore {
	t.Helper()
	store := openStore(t, dir, ingressProfileConfig())
	require.NoError(t, store.LoadLatest())
	return store
}

// memiavlOnlyStoreDir returns the directory of a closed store holding one block of state that never
// started the EVM migration.
func memiavlOnlyStoreDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MemiavlOnly
	cfg.MemIAVLConfig.AsyncCommitBuffer = 0
	store := openStore(t, dir, cfg)
	require.NoError(t, store.LoadLatest())
	require.NoError(t, store.ApplyChangeSets(newMigrationWorkload(0x1041).generateBlock(5, 0, 0, 5, 0)))
	_, err := store.Commit(1)
	require.NoError(t, err)
	require.NoError(t, store.Close())
	return dir
}

// migrationInFlightStoreDir returns the directory of a closed store whose EVM migration has started
// and not finished.
func migrationInFlightStoreDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	driveMigrationWorkload(t, dir, newMigrationWorkload(0x1041), 20, 1, 5)
	return dir
}

// evmMigratedStoreDir returns the directory of a closed store that ran the EVM migration to
// completion, together with the workload that wrote it.
func evmMigratedStoreDir(t *testing.T) (string, *migrationWorkload) {
	t.Helper()
	const batch = 1000
	dir := t.TempDir()
	workload := newMigrationWorkload(0x1041)
	driveMigrationWorkload(t, dir, workload, 5, 1, batch)
	store := reopenInMigrateEVM(t, dir, batch)
	runUntilMigrationComplete(t, store, workload, 10)
	require.NoError(t, store.Close())
	return dir, workload
}

// exportLatest drains a snapshot of the store in dir at its latest version.
func exportLatest(t *testing.T, dir string, cfg config.StateCommitConfig) (int64, []exportedItem) {
	t.Helper()
	source := openStore(t, dir, cfg)
	require.NoError(t, source.LoadLatest())
	height := source.Version()
	exporter, err := source.Exporter(height)
	require.NoError(t, err)
	items := drainCompositeExporter(t, exporter)
	require.NoError(t, exporter.Close())
	require.NoError(t, source.Close())
	return height, items
}

// restoreInto restores items into a fresh ingress store the way rootmulti.Restore does: close the loaded
// store, import, then reload. It returns the reload's error alongside the store.
func restoreInto(t *testing.T, height int64, items []exportedItem) (*CompositeCommitStore, error) {
	t.Helper()
	target := loadIngressStore(t, t.TempDir())
	require.Zero(t, target.Version())
	require.NoError(t, target.Close())
	importer, err := target.Importer(height)
	require.NoError(t, err)
	replayImport(t, importer, items)
	require.NoError(t, importer.Close())
	return target, target.LoadLatest()
}

func TestIngressProfileLoadsEmptyStoreForStateSync(t *testing.T) {
	store := loadIngressStore(t, t.TempDir())
	require.Zero(t, store.Version())
	require.Equal(t, uint64(512*unit.MB), store.config.FlatKVConfig.StorageStoreConfig.MaxSize)
	require.NoError(t, store.Close())
}

func TestIngressProfileRefusesGenesis(t *testing.T) {
	t.Run("initial height 1", func(t *testing.T) {
		store := loadIngressStore(t, t.TempDir())
		require.NoError(t, store.ApplyChangeSets(newMigrationWorkload(1).generateBlock(0, 0, 0, 1, 0)))
		_, err := store.Commit(1)
		require.ErrorIs(t, err, errIngressGenesis)
		require.Zero(t, store.Version())
		require.NoError(t, store.Close())
	})
	t.Run("initial height above 1", func(t *testing.T) {
		store := loadIngressStore(t, t.TempDir())
		require.NoError(t, store.SetInitialVersion(5))
		_, err := store.Commit(5)
		require.ErrorIs(t, err, errIngressGenesis)
		require.NoError(t, store.Close())
	})
}

func TestIngressProfileRefusesStateBeforeEVMMigration(t *testing.T) {
	for name, tc := range map[string]struct {
		dir  func(t *testing.T) string
		mode types.WriteMode
	}{
		"never migrated":      {memiavlOnlyStoreDir, types.MemiavlOnly},
		"migration in flight": {migrationInFlightStoreDir, types.MigrateEVM},
	} {
		t.Run(name, func(t *testing.T) {
			store := openStore(t, tc.dir(t), ingressProfileConfig())
			err := store.LoadLatest()
			require.ErrorContains(t, err, "past the EVM migration")
			require.ErrorContains(t, err, string(tc.mode))
			require.NoError(t, store.Close())
		})
	}
}

func TestIngressProfileServesEVMMigratedStore(t *testing.T) {
	dir, workload := evmMigratedStoreDir(t)
	store := loadIngressStore(t, dir)
	defer func() { require.NoError(t, store.Close()) }()

	require.Equal(t, types.EVMMigrated, store.loadWriteMode())
	require.NotNil(t, store.memIAVL, "the Cosmos modules stay in memiavl until MigrateCosmos")
	requireOracleMatches(t, store, workload.snapshotOracle())

	height := store.Version()
	require.NoError(t, store.ApplyChangeSets(workload.generateBlock(2, 2, 1, 1, 1)))
	_, err := store.Commit(height + 1)
	require.NoError(t, err)
	requireOracleMatches(t, store, workload.snapshotOracle())

	_, err = store.LoadVersionReadOnly(height)
	require.ErrorContains(t, err, "opens only the current state-commit version")
	current, err := store.LoadVersionReadOnly(0)
	require.NoError(t, err)
	require.NoError(t, current.Close())
}

func TestIngressProfileBootstrapsFromEVMMigratedSnapshot(t *testing.T) {
	dir, workload := evmMigratedStoreDir(t)
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.Auto
	height, items := exportLatest(t, dir, cfg)

	target, err := restoreInto(t, height, items)
	require.NoError(t, err)
	require.Equal(t, height, target.Version())
	require.Equal(t, types.EVMMigrated, target.loadWriteMode())
	requireOracleMatches(t, target, workload.snapshotOracle())
	require.NoError(t, target.Close())
}

func TestIngressProfileRefusesSnapshotBeforeEVMMigration(t *testing.T) {
	cfg := config.DefaultStateCommitConfig()
	cfg.WriteMode = types.MemiavlOnly
	height, items := exportLatest(t, memiavlOnlyStoreDir(t), cfg)

	target, err := restoreInto(t, height, items)
	require.ErrorContains(t, err, "past the EVM migration")
	require.ErrorContains(t, err, string(types.MemiavlOnly))
	require.NoError(t, target.Close())
}
