package composite

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/migration"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

const migrationVersionInstrument = "seidb_migration_version"

// installManualMeterProvider routes the global OTel meter to a ManualReader
// for the duration of the test.
func installManualMeterProvider(t *testing.T) *sdkmetric.ManualReader {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(provider)
	t.Cleanup(func() {
		otel.SetMeterProvider(prev)
		require.NoError(t, provider.Shutdown(context.Background()))
	})
	return reader
}

// collectMigrationVersions collects once from reader and returns every
// seidb_migration_version data point value.
func collectMigrationVersions(t *testing.T, reader *sdkmetric.ManualReader) []int64 {
	t.Helper()
	var collected metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &collected))

	var values []int64
	for _, scope := range collected.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != migrationVersionInstrument {
				continue
			}
			gauge, ok := m.Data.(metricdata.Gauge[int64])
			require.True(t, ok, "unexpected data type %T", m.Data)
			for _, point := range gauge.DataPoints {
				values = append(values, point.Value)
			}
		}
	}
	return values
}

// TestLoadVersionReadOnlyDoesNotReportMigrationVersion pins that a read-only
// handle opened at a mid-migration height leaves the process-wide migration
// version gauge at the live manager's value. State-sync snapshot exports open
// such handles after the migration completes; they must not report the
// pre-completion version they observe.
func TestLoadVersionReadOnlyDoesNotReportMigrationVersion(t *testing.T) {
	dir := t.TempDir()

	// Phase 1: seed evm/ keys in MemiavlOnly so the migration spans several blocks.
	v0Cfg := config.DefaultStateCommitConfig()
	v0Cfg.WriteMode = types.MemiavlOnly
	cs1, err := NewCompositeCommitStore(t.Context(), dir, v0Cfg)
	require.NoError(t, err)
	require.NoError(t, cs1.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	require.NoError(t, cs1.LoadLatest())
	pairs := make([]*proto.KVPair, 0, 8)
	for i := 0; i < 8; i++ {
		pairs = append(pairs, &proto.KVPair{Key: []byte(fmt.Sprintf("evm_key_%d", i)), Value: []byte("v")})
	}
	require.NoError(t, cs1.ApplyChangeSets([]*proto.NamedChangeSet{
		{Name: keys.EVMStoreKey, Changeset: proto.ChangeSet{Pairs: pairs}},
	}))
	_, err = cs1.Commit(cs1.Version() + 1)
	require.NoError(t, err)
	require.NoError(t, cs1.Close())

	reader := installManualMeterProvider(t)

	// Phase 2: migrate one key per block until the completion block lands.
	migrateCfg := config.DefaultStateCommitConfig()
	migrateCfg.WriteMode = types.MigrateEVM
	cs2, err := NewCompositeCommitStore(t.Context(), dir, migrateCfg)
	require.NoError(t, err)
	require.NoError(t, cs2.SetMigrationBatchSize(1))
	require.NoError(t, cs2.Initialize([]string{keys.BankStoreKey, keys.EVMStoreKey}))
	require.NoError(t, cs2.LoadLatest())
	defer cs2.Close()

	var midMigrationVersion int64
	for block := 0; block < 32; block++ {
		require.NoError(t, cs2.ApplyChangeSets([]*proto.NamedChangeSet{
			{Name: keys.BankStoreKey, Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte(fmt.Sprintf("bank_%d", block)), Value: []byte("v")},
			}}},
		}))
		_, err = cs2.Commit(cs2.Version() + 1)
		require.NoError(t, err)
		if _, done := cs2.loadFlatKV().Get(migration.MigrationStore, []byte(migration.MigrationVersionKey)); done {
			break
		}
		midMigrationVersion = cs2.Version()
	}
	require.NotZero(t, midMigrationVersion, "migration must span more than one block")
	require.Equal(t, []int64{int64(migration.Version1_MigrateEVM)}, collectMigrationVersions(t, reader),
		"precondition: live manager reports the completed migration version")

	ro, err := cs2.LoadVersionReadOnly(midMigrationVersion)
	require.NoError(t, err)
	defer func() { _ = ro.Close() }()

	require.Equal(t, []int64{int64(migration.Version1_MigrateEVM)}, collectMigrationVersions(t, reader),
		"read-only handle at a mid-migration height must not overwrite the live migration version")
}
