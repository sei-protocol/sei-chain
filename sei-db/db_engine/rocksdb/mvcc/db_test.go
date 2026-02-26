//go:build rocksdbBackend
// +build rocksdbBackend

package mvcc

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/test"
)

func TestStorageTestSuite(t *testing.T) {
	rocksConfig := config.DefaultStateStoreConfig()
	rocksConfig.Backend = "rocksdb"
	s := &sstest.StorageTestSuite{
		BaseStorageTestSuite: sstest.BaseStorageTestSuite{
			NewDB: func(dir string, config config.StateStoreConfig) (db_engine.MvccDB, error) {
				return OpenDB(dir, config)
			},
			Config:         rocksConfig,
			EmptyBatchSize: 12,
		},
	}

	suite.Run(t, s)
}
