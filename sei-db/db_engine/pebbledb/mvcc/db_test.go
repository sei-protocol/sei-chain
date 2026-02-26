package mvcc

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/test"
)

func TestStorageTestSuite(t *testing.T) {
	pebbleConfig := config.DefaultStateStoreConfig()
	pebbleConfig.Backend = "pebbledb"
	s := &sstest.StorageTestSuite{
		BaseStorageTestSuite: sstest.BaseStorageTestSuite{
			NewDB: func(dir string, config config.StateStoreConfig) (db_engine.MvccDB, error) {
				return OpenDB(dir, config)
			},
			Config:         pebbleConfig,
			EmptyBatchSize: 12,
		},
	}

	suite.Run(t, s)
}

// TestStorageTestSuiteDefaultComparer runs the base storage test suite with Pebble's DefaultComparer
// instead of MVCCComparer. This is useful for new databases that don't need backwards compatibility.
// Note: Iterator tests are not included because DefaultComparer doesn't have the Split function
// configured for MVCC key encoding, so NextPrefix/SeekLT operations won't work correctly.
// BaseStorageTestSuite contains only tests that work with both comparers.
func TestStorageTestSuiteDefaultComparer(t *testing.T) {
	pebbleConfig := config.DefaultStateStoreConfig()
	pebbleConfig.Backend = "pebbledb"
	pebbleConfig.UseDefaultComparer = true

	s := &sstest.BaseStorageTestSuite{
		NewDB: func(dir string, config config.StateStoreConfig) (db_engine.MvccDB, error) {
			return OpenDB(dir, config)
		},
		Config:         pebbleConfig,
		EmptyBatchSize: 12,
	}

	suite.Run(t, s)
}
