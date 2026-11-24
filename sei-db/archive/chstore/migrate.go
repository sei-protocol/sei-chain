package chstore

import (
	"context"
	"embed"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/clickhouse"
)

// migrations holds the archive's schema migrations.
//
//go:embed migrations/*.sql
var migrations embed.FS

// migrate creates the archive database when it is missing and applies the archive's migrations.
func migrate(ctx context.Context, cfg clickhouse.Config) error {
	return clickhouse.Migrate(ctx, cfg, migrations)
}
