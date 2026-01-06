package mvcc

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	sstest "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/test"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
	"github.com/stretchr/testify/suite"
)

func TestStorageTestSuite(t *testing.T) {
	pebbleConfig := config.DefaultStateStoreConfig()
	pebbleConfig.Backend = "pebbledb"
	s := &sstest.StorageTestSuite{
		NewDB: func(dir string, config config.StateStoreConfig) (types.StateStore, error) {
			return OpenDB(dir, config)
		},
		Config:         pebbleConfig,
		EmptyBatchSize: 12,
	}

	suite.Run(t, s)
}

func TestOpenDBCacheLifecycle(t *testing.T) {
	dir := t.TempDir()
	cfg := config.DefaultStateStoreConfig()
	cfg.Backend = "pebbledb"
	cfg.AsyncWriteBuffer = 1
	cfg.KeepRecent = 0
	cfg.PruneIntervalSeconds = 1

	db, err := OpenDB(dir, cfg)
	if err != nil {
		t.Fatalf("OpenDB: %v", err)
	}
	if db.cache == nil {
		t.Fatalf("expected cache to be non-nil after OpenDB")
	}
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if db.cache != nil {
		t.Fatalf("expected cache to be nil after Close")
	}
}
