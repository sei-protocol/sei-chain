package pebbledb

import (
	"bytes"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/ss/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestDB creates a temporary test database
func setupTestDB(t *testing.T) (*Database, string) {
	tempDir := t.TempDir()

	// Set up config with hash range enabled
	cfg := config.StateStoreConfig{
		HashRange:          10, // 10 blocks per hash range
		AsyncWriteBuffer:   100,
		KeepRecent:         100,
		KeepLastVersion:    true,
		ImportNumWorkers:   4,
		DedicatedChangelog: false,
	}

	db, err := New(tempDir, cfg)
	require.NoError(t, err)

	return db, tempDir
}

// cleanupTestDB closes the database and removes the temporary directory
func cleanupTestDB(t *testing.T, db *Database, tempDir string) {
	if db != nil {
		err := db.Close()
		require.NoError(t, err)
	}
	if tempDir != "" {
		err := os.RemoveAll(tempDir)
		require.NoError(t, err)
	}
}

// populateTestData populates the database with test data for multiple modules
func populateTestData(t *testing.T, db *Database, startVersion, endVersion int64) {
	// Define test modules
	modules := []string{"bank", "staking", "auth"}

	// Define test store keys for each module, each with multiple records
	for version := startVersion; version <= endVersion; version++ {
		for _, module := range modules {
			// Create a batch for this module and version
			batch, err := NewBatch(db.storage, version)
			require.NoError(t, err)

			// Add 5 keys per module per version
			for i := 1; i <= 5; i++ {
				key := []byte(fmt.Sprintf("key%d", i))
				val := []byte(fmt.Sprintf("val%d-%d", i, version))
				err := batch.Set(module, key, val)
				require.NoError(t, err)
			}

			// Write the batch
			err = batch.Write()
			require.NoError(t, err)

			// Mark store as dirty
			db.storeKeyDirty.Store(module, version)
		}

		// Set the latest version
		err := db.SetLatestVersion(version)
		require.NoError(t, err)
	}

	// Ensure util.Modules is populated with our test modules
	util.Modules = modules
}

// TestComputeMissingRanges tests the computeMissingRanges function
func TestComputeMissingRanges(t *testing.T) {
	db, tempDir := setupTestDB(t)
	defer cleanupTestDB(t, db, tempDir)

	// Populate test data for versions 1-30
	populateTestData(t, db, 1, 30)

	// Initial state: no ranges hashed yet
	lastHashed, err := db.GetLastRangeHashed()
	require.NoError(t, err)
	assert.Equal(t, int64(0), lastHashed)

	// Test computing missing ranges with the latest version at 30
	err = db.computeMissingRanges(30)
	require.NoError(t, err)

	// We should now have hashed up to version 30 (3 complete ranges of size 10)
	lastHashed, err = db.GetLastRangeHashed()
	require.NoError(t, err)
	assert.Equal(t, int64(30), lastHashed)

	// Verify that we have hashes for each range and module
	for _, module := range util.Modules {
		// Check for range hashes for blocks 1-10, 11-20, and 21-30
		for startBlock := int64(1); startBlock <= 21; startBlock += 10 {
			endBlock := startBlock + 9
			hashKey := []byte(fmt.Sprintf(HashTpl, module, startBlock, endBlock))

			hash, closer, err := db.storage.Get(hashKey)
			require.NoError(t, err)
			assert.NotNil(t, hash)
			assert.True(t, len(hash) > 0)
			closer.Close()
		}
	}

	// Test with partial range - add data for version 31-35
	populateTestData(t, db, 31, 35)

	// Compute ranges with latest version at 35
	err = db.computeMissingRanges(35)
	require.NoError(t, err)

	// LastRangeHashed should still be 30 since we don't have a complete range
	lastHashed, err = db.GetLastRangeHashed()
	require.NoError(t, err)
	assert.Equal(t, int64(30), lastHashed)

	// Now add data up to version 40 and compute ranges again
	populateTestData(t, db, 36, 40)
	err = db.computeMissingRanges(40)
	require.NoError(t, err)

	// We should now have hashed up to version 40
	lastHashed, err = db.GetLastRangeHashed()
	require.NoError(t, err)
	assert.Equal(t, int64(40), lastHashed)

	// Verify that we have a hash for the range 31-40
	for _, module := range util.Modules {
		hashKey := []byte(fmt.Sprintf(HashTpl, module, 31, 40))
		hash, closer, err := db.storage.Get(hashKey)
		require.NoError(t, err)
		assert.NotNil(t, hash)
		assert.True(t, len(hash) > 0)
		closer.Close()
	}
}

// TestComputeHashForRange tests the computeHashForRange function directly
func TestComputeHashForRange(t *testing.T) {
	db, tempDir := setupTestDB(t)
	defer cleanupTestDB(t, db, tempDir)

	// Populate test data for versions 1-20
	populateTestData(t, db, 1, 20)

	// Compute hash for range 1-10
	err := db.computeHashForRange(1, 10)
	require.NoError(t, err)

	// Check that hashes exist for all modules
	for _, module := range util.Modules {
		hashKey := []byte(fmt.Sprintf(HashTpl, module, 1, 10))
		hash1, closer, err := db.storage.Get(hashKey)
		require.NoError(t, err)
		assert.NotNil(t, hash1)
		assert.True(t, len(hash1) > 0)
		closer.Close()

		// Store the hash for later comparison
		hash1Copy := make([]byte, len(hash1))
		copy(hash1Copy, hash1)

		// Now compute hash for a different range 11-20
		err = db.computeHashForRange(11, 20)
		require.NoError(t, err)

		// Check that the hash for the new range exists
		hashKey = []byte(fmt.Sprintf(HashTpl, module, 11, 20))
		hash2, closer, err := db.storage.Get(hashKey)
		require.NoError(t, err)
		assert.NotNil(t, hash2)
		assert.True(t, len(hash2) > 0)
		closer.Close()

		// The hashes for different ranges should be different
		assert.False(t, bytes.Equal(hash1Copy, hash2))
	}
}

// TestComputeHashForEmptyRange tests computing hashes for ranges with no data
func TestComputeHashForEmptyRange(t *testing.T) {
	db, tempDir := setupTestDB(t)
	defer cleanupTestDB(t, db, tempDir)

	// Populate test data for versions 1-10 only
	populateTestData(t, db, 1, 10)

	// Try to compute hash for empty range 11-20
	err := db.computeHashForRange(11, 20)
	require.NoError(t, err)

	// Check if any hashes were created
	for _, module := range util.Modules {
		hashKey := []byte(fmt.Sprintf(HashTpl, module, 11, 20))
		_, closer, err := db.storage.Get(hashKey)
		if err == nil {
			closer.Close()
			// If hash exists, it should be because the code handles empty ranges gracefully
			continue
		}
		assert.Equal(t, pebble.ErrNotFound, err)
	}
}

// TestComputeHashForSingleBlockRange tests computing hashes for a single block
func TestComputeHashForSingleBlockRange(t *testing.T) {
	db, tempDir := setupTestDB(t)
	defer cleanupTestDB(t, db, tempDir)

	// Populate test data for versions 1-5
	populateTestData(t, db, 1, 5)

	// Compute hash for single block range 3-3
	err := db.computeHashForRange(3, 3)
	require.NoError(t, err)

	// Check that hashes exist for all modules
	for _, module := range util.Modules {
		hashKey := []byte(fmt.Sprintf(HashTpl, module, 3, 3))
		hash, closer, err := db.storage.Get(hashKey)
		require.NoError(t, err)
		assert.NotNil(t, hash)
		assert.True(t, len(hash) > 0)
		closer.Close()
	}
}

// TestComputeHashWithInvalidRange tests computing hashes with invalid ranges
func TestComputeHashWithInvalidRange(t *testing.T) {
	db, tempDir := setupTestDB(t)
	defer cleanupTestDB(t, db, tempDir)

	// Test with end block < begin block (invalid range)
	err := db.computeHashForRange(10, 5)
	require.NoError(t, err) // The function handles this gracefully

	// Test with zero size range
	err = db.computeHashForRange(0, 0)
	require.NoError(t, err) // The function handles this gracefully
}

// TestComputeHashWithDifferentModules tests computing hashes when different modules have different data
func TestComputeHashWithDifferentModules(t *testing.T) {
	db, tempDir := setupTestDB(t)
	defer cleanupTestDB(t, db, tempDir)

	// Define test modules
	util.Modules = []string{"bank", "staking", "auth"}

	// Add data for "bank" module only
	batch, err := NewBatch(db.storage, 1)
	require.NoError(t, err)
	err = batch.Set("bank", []byte("key1"), []byte("val1"))
	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)

	// Add data for "staking" module at a different version
	batch, err = NewBatch(db.storage, 2)
	require.NoError(t, err)
	err = batch.Set("staking", []byte("key1"), []byte("val1"))
	require.NoError(t, err)
	err = batch.Write()
	require.NoError(t, err)

	// Set latest version
	err = db.SetLatestVersion(2)
	require.NoError(t, err)

	// Compute hash for range 1-2
	err = db.computeHashForRange(1, 2)
	require.NoError(t, err)

	// Check that hashes exist for bank and staking modules
	for _, module := range []string{"bank", "staking"} {
		hashKey := []byte(fmt.Sprintf(HashTpl, module, 1, 2))
		hash, closer, err := db.storage.Get(hashKey)
		require.NoError(t, err)
		assert.NotNil(t, hash)
		assert.True(t, len(hash) > 0)
		closer.Close()
	}

	// Auth module shouldn't have any data, so might not have a hash
	hashKey := []byte(fmt.Sprintf(HashTpl, "auth", 1, 2))
	_, closer, err := db.storage.Get(hashKey)
	if err == nil {
		closer.Close()
		// If hash exists, it should be because the code handles empty modules gracefully
	} else {
		assert.Equal(t, pebble.ErrNotFound, err)
	}
}

// TestAsyncComputeMissingRanges tests the async compute of missing ranges
func TestAsyncComputeMissingRanges(t *testing.T) {
	db, tempDir := setupTestDB(t)
	defer cleanupTestDB(t, db, tempDir)

	// Populate test data for versions 1-30
	populateTestData(t, db, 1, 30)

	// Test the async method by applying a changeset which triggers range computation
	cs := &proto.NamedChangeSet{
		Name: "bank",
		Changeset: iavl.ChangeSet{
			Pairs: []*iavl.KVPair{
				{
					Key:   []byte("asyncKey"),
					Value: []byte("asyncValue"),
				},
			},
		},
	}

	changesets := []*proto.NamedChangeSet{cs}

	// Apply the changeset async at version 31
	err := db.ApplyChangesetAsync(31, changesets)
	require.NoError(t, err)

	// Wait a bit for the async computation to complete
	time.Sleep(200 * time.Millisecond)

	// We should now have hashed up to version 30 (3 complete ranges)
	lastHashed, err := db.GetLastRangeHashed()
	require.NoError(t, err)
	assert.Equal(t, int64(30), lastHashed)

	// Apply more changesets to get to version 40
	for v := int64(32); v <= 40; v++ {
		err := db.ApplyChangesetAsync(v, changesets)
		require.NoError(t, err)
	}

	// Wait a bit for async computation
	time.Sleep(500 * time.Millisecond)

	// We should now have hashed up to version 40
	lastHashed, err = db.GetLastRangeHashed()
	require.NoError(t, err)
	assert.Equal(t, int64(40), lastHashed)
}

// TestGeneratedHash tests that the same data produces consistent hashes
func TestGeneratedHash(t *testing.T) {
	db1, tempDir1 := setupTestDB(t)
	defer cleanupTestDB(t, db1, tempDir1)

	db2, tempDir2 := setupTestDB(t)
	defer cleanupTestDB(t, db2, tempDir2)

	// Populate identical test data in both databases
	for _, db := range []*Database{db1, db2} {
		// Create identical data
		batch, err := NewBatch(db.storage, 1)
		require.NoError(t, err)
		err = batch.Set("bank", []byte("key1"), []byte("val1"))
		require.NoError(t, err)
		err = batch.Set("bank", []byte("key2"), []byte("val2"))
		require.NoError(t, err)
		err = batch.Write()
		require.NoError(t, err)

		err = db.SetLatestVersion(1)
		require.NoError(t, err)
	}

	// Set up the util.Modules to include our test module
	util.Modules = []string{"bank"}

	// Compute hash for range 1-1 in both databases
	err := db1.computeHashForRange(1, 1)
	require.NoError(t, err)

	err = db2.computeHashForRange(1, 1)
	require.NoError(t, err)

	// Get the hash from the first database
	hashKey := []byte(fmt.Sprintf(HashTpl, "bank", 1, 1))
	hash1, closer, err := db1.storage.Get(hashKey)
	require.NoError(t, err)
	assert.NotNil(t, hash1)
	closer.Close()

	// Get the hash from the second database
	hash2, closer, err := db2.storage.Get(hashKey)
	require.NoError(t, err)
	assert.NotNil(t, hash2)
	closer.Close()

	// The hashes should be identical for the same data
	assert.True(t, bytes.Equal(hash1, hash2))
}
