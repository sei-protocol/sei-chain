package littblock_test

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/blocktest"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/littblock"
	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

// littConfig builds a littblock config rooted at dir with a tiny retention, so
// the prune watermark is the sole gate on reclamation rather than the TTL.
func littConfig(t *testing.T, dir string) *littblock.BlockDBConfig {
	t.Helper()
	cfg, err := littblock.DefaultConfig(dir)
	require.NoError(t, err)
	cfg.RetentionTime = time.Nanosecond
	return cfg
}

// openLitt opens a store at cfg's paths, failing the test if it cannot.
func openLitt(t *testing.T, cfg *littblock.BlockDBConfig) blocktypes.BlockDB {
	t.Helper()
	db, err := littblock.NewBlockDB(cfg)
	require.NoError(t, err)
	return db
}

// TestBlockDBContract runs the shared storage contract against littblock. One
// backing directory serves every reopen, so a "restart" reloads persisted state
// from disk.
func TestBlockDBContract(t *testing.T) {
	blocktest.RunContract(t, func(t *testing.T) blocktest.Open {
		dir := t.TempDir()
		return func() (blocktypes.BlockDB, error) {
			return littblock.NewBlockDB(littConfig(t, dir))
		}
	})
}
