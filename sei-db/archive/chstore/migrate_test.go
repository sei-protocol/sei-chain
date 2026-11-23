package chstore

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/clickhouse"
)

func TestMigrationsAreWellFormed(t *testing.T) {
	require.NoError(t, clickhouse.CheckMigrations(migrations))
}
