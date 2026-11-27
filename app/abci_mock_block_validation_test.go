//go:build mock_block_validation

package app

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app/migration"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

func TestMockBlockValidationUsesLocalMigrationBatchSize(t *testing.T) {
	require.Equal(t, 1024, mockBlockValidationMigrationBatchSize(mapAppOpts{FlagSCKeysToMigratePerBlock: 1024}))
	require.Zero(t, mockBlockValidationMigrationBatchSize(mapAppOpts{FlagSCKeysToMigratePerBlock: -1}))
	require.Zero(t, mockBlockValidationMigrationBatchSize(mapAppOpts{}))

	a := Setup(t, false, false, false)
	ctx := a.NewContext(false, tmproto.Header{Height: 1, ChainID: "sei-test", Time: time.Now()})
	subspace, ok := a.ParamsKeeper.GetSubspace(migration.SubspaceName)
	require.True(t, ok)

	// pacific-1 keeps the governance rate at 0; the local rate still applies.
	a.migrationBatchSizeOverride = 1024
	subspace.Set(ctx, migration.KeyNumKeysToMigratePerBlock, uint64(0))
	a.applyMigrationBatchSize(ctx)
	got, ok := a.rootStore.GetMigrationBatchSize()
	require.True(t, ok)
	require.Equal(t, 1024, got)

	subspace.Set(ctx, migration.KeyNumKeysToMigratePerBlock, uint64(500))
	a.applyMigrationBatchSize(ctx)
	got, _ = a.rootStore.GetMigrationBatchSize()
	require.Equal(t, 1024, got)

	// Without a local rate the node follows governance, so a local 0 pauses it.
	a.migrationBatchSizeOverride = 0
	subspace.Set(ctx, migration.KeyNumKeysToMigratePerBlock, uint64(0))
	a.applyMigrationBatchSize(ctx)
	got, _ = a.rootStore.GetMigrationBatchSize()
	require.Zero(t, got)
}
