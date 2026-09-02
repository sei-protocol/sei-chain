//go:build mock_block_validation

package app

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/app/migration"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	tmproto "github.com/sei-protocol/sei-chain/sei-tendermint/proto/tendermint/types"
	"github.com/stretchr/testify/require"
)

func TestMockBlockValidationUsesLocalMigrationBatchSize(t *testing.T) {
	a := Setup(t, false, false, false)
	ctx := a.NewContext(false, tmproto.Header{Height: 1, ChainID: "sei-test", Time: time.Now()})

	a.migrationBatchSizeOverride = mockBlockValidationMigrationBatchSize(config.StateCommitConfig{
		KeysToMigratePerBlock: 1024,
	})

	subspace, ok := a.ParamsKeeper.GetSubspace(migration.SubspaceName)
	require.True(t, ok)
	subspace.Set(ctx, migration.KeyNumKeysToMigratePerBlock, uint64(500))

	a.applyMigrationBatchSize(ctx)
	got, ok := a.rootStore.GetMigrationBatchSize()
	require.True(t, ok)
	require.Equal(t, 1024, got)
}
