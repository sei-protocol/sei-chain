//go:build rocksdbBackend
// +build rocksdbBackend

package rocksdb

import (
	"testing"

	"github.com/sei-protocol/sei-db/config"
	sstest "github.com/sei-protocol/sei-db/ss/test"
	"github.com/sei-protocol/sei-db/ss/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestStorageTestSuite(t *testing.T) {
	rocksConfig := config.DefaultStateStoreConfig()
	rocksConfig.Backend = "rocksdb"
	s := &sstest.StorageTestSuite{
		NewDB: func(dir string, config config.StateStoreConfig) (types.StateStore, error) {
			return New(dir, config)
		},
		Config:         rocksConfig,
		EmptyBatchSize: 12,
	}

	suite.Run(t, s)
}

// TestPruneAfterClose verifies that calling Prune() after Close() returns an error
// instead of causing a panic due to nil pointer dereference.
// This is a regression test for the nil pointer panic during node shutdown.
func TestPruneAfterClose(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = "rocksdb"

	db, err := New(dir, cfg)
	require.NoError(t, err)

	// Write some data
	err = db.SetLatestVersion(1)
	require.NoError(t, err)

	// Close the database
	err = db.Close()
	require.NoError(t, err)

	// Prune should return error, not panic
	err = db.Prune(1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "database is closed")
}
