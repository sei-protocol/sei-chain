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

// TestStorageTestSuiteDefaultComparer runs the storage test suite with Pebble's DefaultComparer
// instead of MVCCComparer. This is useful for new databases that don't need backwards compatibility.
// Note: Iterator tests are skipped because DefaultComparer doesn't have the Split function
// configured for MVCC key encoding, so NextPrefix/SeekLT operations won't work correctly.
func TestStorageTestSuiteDefaultComparer(t *testing.T) {
	pebbleConfig := config.DefaultStateStoreConfig()
	pebbleConfig.Backend = "pebbledb"
	pebbleConfig.UseDefaultComparer = true

	// Skip all iterator-related tests since DefaultComparer doesn't support MVCC iteration properly
	iteratorTests := []string{
		"TestStorageTestSuiteDefaultComparer/TestDatabaseIterator",
		"TestStorageTestSuiteDefaultComparer/TestDatabaseIteratorClose",
		"TestStorageTestSuiteDefaultComparer/TestDatabaseIteratorDomain",
		"TestStorageTestSuiteDefaultComparer/TestDatabaseIteratorEmptyDomain",
		"TestStorageTestSuiteDefaultComparer/TestDatabaseIteratorDeletes",
		"TestStorageTestSuiteDefaultComparer/TestDatabaseIteratorMultiVersion",
		"TestStorageTestSuiteDefaultComparer/TestDatabaseIteratorNoDomain",
		"TestStorageTestSuiteDefaultComparer/TestDatabaseIteratorRangedDeletes",
		"TestStorageTestSuiteDefaultComparer/TestDatabaseReverseIterator",
		"TestStorageTestSuiteDefaultComparer/TestDatabaseReverseIteratorPrefixIsolation",
		"TestStorageTestSuiteDefaultComparer/TestDatabaseBugInitialForwardIteration",
		"TestStorageTestSuiteDefaultComparer/TestDatabaseBugInitialForwardIterationHigher",
		"TestStorageTestSuiteDefaultComparer/TestDatabaseBugInitialReverseIteration",
		"TestStorageTestSuiteDefaultComparer/TestDatabaseBugInitialReverseIterationHigher",
		"TestStorageTestSuiteDefaultComparer/TestDatabaseParallelDeleteIteration",
		"TestStorageTestSuiteDefaultComparer/TestDatabaseParallelIterationVersions",
		"TestStorageTestSuiteDefaultComparer/TestParallelIterationAndPruning",
		// Prune tests also use iteration internally
		"TestStorageTestSuiteDefaultComparer/TestDatabasePrune",
		"TestStorageTestSuiteDefaultComparer/TestDatabasePruneKeepRecent",
		"TestStorageTestSuiteDefaultComparer/TestParallelWriteAndPruning",
	}

	s := &sstest.StorageTestSuite{
		NewDB: func(dir string, config config.StateStoreConfig) (types.StateStore, error) {
			return OpenDB(dir, config)
		},
		Config:         pebbleConfig,
		EmptyBatchSize: 12,
		SkipTests:      iteratorTests,
	}

	suite.Run(t, s)
}
