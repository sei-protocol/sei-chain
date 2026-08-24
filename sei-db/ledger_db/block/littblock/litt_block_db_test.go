package littblock_test

import (
	"path/filepath"
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

// TestTableDirectoryName pins the on-disk directory the store keeps its data in.
// The name is persisted layout, not an identifier: renaming it makes a reopen
// build a fresh empty table and leaves the data under the old name unreachable,
// which reads as a healthy empty store. The name is spelled out here rather than
// taken from the constant, so that renaming the constant fails this test.
func TestTableDirectoryName(t *testing.T) {
	dir := t.TempDir()

	db := openLitt(t, littConfig(t, dir))
	require.NoError(t, db.Close())

	require.DirExists(t, filepath.Join(dir, "blocks"))
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
