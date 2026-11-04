package blockstore_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/blockstore/internal/blocktest"
	"github.com/stretchr/testify/require"
)

// littConfig builds a LittDB config rooted at dir with a tiny retention, so
// the prune watermark is the sole gate on reclamation rather than the TTL.
func littConfig(t *testing.T, dir string) *blockstore.Config {
	t.Helper()
	cfg, err := blockstore.DefaultConfig(dir)
	require.NoError(t, err)
	cfg.RetentionTime = time.Nanosecond
	return cfg
}

// openLitt opens a store at cfg's paths, failing the test if it cannot.
func openLitt(t *testing.T, cfg *blockstore.Config) blocktest.DB {
	t.Helper()
	db, err := blockstore.OpenRecords(cfg)
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

// TestBlockDBContract runs the shared storage contract against blockstore. One
// backing directory serves every reopen, so a "restart" reloads persisted state
// from disk.
func TestBlockDBContract(t *testing.T) {
	blocktest.RunContract(t, func(t *testing.T) blocktest.Open {
		dir := t.TempDir()
		return func() (blocktest.DB, error) {
			return blockstore.OpenRecords(littConfig(t, dir))
		}
	})
}
