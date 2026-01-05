//go:build rocksdbBackend
// +build rocksdbBackend

package mvcc

import (
	"testing"

	"github.com/sei-protocol/sei-db/config"
	sstest "github.com/sei-protocol/sei-db/state_db/ss/test"
	"github.com/sei-protocol/sei-db/state_db/ss/types"
	"github.com/stretchr/testify/suite"
)

func TestStorageTestSuite(t *testing.T) {
	rocksConfig := config.DefaultStateStoreConfig()
	rocksConfig.Backend = "rocksdb"
	s := &sstest.StorageTestSuite{
		NewDB: func(dir string, config config.StateStoreConfig) (types.StateStore, error) {
			return OpenDB(dir, config)
		},
		Config:         rocksConfig,
		EmptyBatchSize: 12,
	}

	suite.Run(t, s)
}
