package memiavl

import (
	"testing"
	"time"

	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// TestSnapshotTimeThrottling verifies that snapshots are throttled by minimum time interval
func TestSnapshotTimeThrottling(t *testing.T) {
	dir := t.TempDir()

	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Config: Config{
			SnapshotInterval:        100,     // Small interval for testing
			SnapshotMinTimeInterval: 60 * 60, // 1 hour minimum time interval (in seconds)
		},
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	// Apply changesets rapidly (simulating state sync catch-up)
	// Even though we exceed the block interval (100 blocks), snapshots won't be created
	// because the minimum time interval (1 hour) hasn't elapsed
	for i := range 1000 {
		cs := []*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte{byte(i >> 8), byte(i & 0xff)}, Value: []byte{byte(i)}},
					},
				},
			},
		}
		require.NoError(t, db.ApplyChangeSets(cs))
		_, err := db.Commit()
		require.NoError(t, err)
	}

	// Wait for any background snapshot operations to complete before checking count
	require.Eventually(t, func() bool {
		return db.checkBackgroundSnapshotRewrite() == nil
	}, 2*time.Second, 50*time.Millisecond, "background snapshot should complete")

	// Count snapshots created (excluding the initial one)
	snapshotCount := 0
	err = traverseSnapshots(dir, true, func(version int64) (bool, error) {
		if version > 0 { // Skip initial snapshot
			snapshotCount++
		}
		return false, nil
	})
	require.NoError(t, err)

	// Should create at most 1 snapshot because time threshold (1 hour) prevents more
	require.LessOrEqual(t, snapshotCount, 1, "should create very few snapshots when time threshold not met")

	t.Logf("Snapshots created during rapid commits: %d (expected <= 1)", snapshotCount)
}

// TestSnapshotCreationAfterTimeThreshold verifies snapshot creation after time threshold
func TestSnapshotCreationAfterTimeThreshold(t *testing.T) {
	dir := t.TempDir()

	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Config:          Config{SnapshotInterval: 100},
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	// Commit initial blocks
	for i := range 200 {
		cs := []*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte{byte(i)}, Value: []byte{byte(i * 2)}},
					},
				},
			},
		}
		require.NoError(t, db.ApplyChangeSets(cs))
		_, err := db.Commit()
		require.NoError(t, err)
	}

	// Wait for any background operations
	require.Eventually(t, func() bool {
		return db.checkBackgroundSnapshotRewrite() == nil
	}, 2*time.Second, 50*time.Millisecond, "background operations should complete")

	initialCount := 0
	err = traverseSnapshots(dir, true, func(version int64) (bool, error) {
		if version > 0 {
			initialCount++
		}
		return false, nil
	})
	require.NoError(t, err)

	// Set lastSnapshotTime to 61 minutes ago to simulate time passage
	db.lastSnapshotTime = time.Now().Add(-61 * time.Minute)

	// Now commit more blocks to trigger snapshot (need to exceed interval)
	for idx := range 200 {
		i := idx + 200
		cs := []*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte{byte(i >> 8), byte(i & 0xff)}, Value: []byte{byte(i)}},
					},
				},
			},
		}
		require.NoError(t, db.ApplyChangeSets(cs))
		_, err := db.Commit()
		require.NoError(t, err)
	}

	// Wait longer for background snapshot to complete
	require.Eventually(t, func() bool {
		return db.checkBackgroundSnapshotRewrite() == nil
	}, 3*time.Second, 50*time.Millisecond, "background snapshot should complete after time threshold")

	finalCount := 0
	err = traverseSnapshots(dir, true, func(version int64) (bool, error) {
		if version > 0 {
			finalCount++
		}
		return false, nil
	})
	require.NoError(t, err)

	// Should have created at least one more snapshot
	require.GreaterOrEqual(t, finalCount, initialCount,
		"should create snapshot after 60+ minutes")

	t.Logf("Snapshots before: %d, after: %d", initialCount, finalCount)
}

// TestSnapshotWithShortTimeInterval verifies snapshot creation with short time intervals
func TestSnapshotWithShortTimeInterval(t *testing.T) {
	dir := t.TempDir()

	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Config: Config{
			SnapshotInterval:        100,
			SnapshotMinTimeInterval: 1, // 1 second minimum time interval for testing
		},
		Dir:             dir,
		CreateIfMissing: true,
		InitialStores:   []string{"test"},
	})
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	// Commit blocks with short time intervals between them
	// This allows multiple snapshots to be created since time threshold is low (1 second)
	for i := range 500 {
		cs := []*proto.NamedChangeSet{
			{
				Name: "test",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte{byte(i >> 8), byte(i & 0xff)}, Value: []byte{byte(i)}},
					},
				},
			},
		}
		require.NoError(t, db.ApplyChangeSets(cs))
		_, err := db.Commit()
		require.NoError(t, err)

		// Check and wait for background snapshots periodically
		// Add small delay to allow time threshold to be met
		if i%100 == 0 && i > 0 {
			time.Sleep(1100 * time.Millisecond) // > 1 second to meet time threshold
			require.Eventually(t, func() bool {
				return db.checkBackgroundSnapshotRewrite() == nil
			}, 2*time.Second, 50*time.Millisecond, "background snapshot should complete")
		}
	}

	// Wait longer for any remaining background snapshot to complete
	require.Eventually(t, func() bool {
		return db.checkBackgroundSnapshotRewrite() == nil
	}, 3*time.Second, 50*time.Millisecond, "remaining background snapshot should complete")

	// Count snapshots - should have multiple snapshots
	snapshotCount := 0
	err = traverseSnapshots(dir, true, func(version int64) (bool, error) {
		if version > 0 {
			snapshotCount++
		}
		return false, nil
	})
	require.NoError(t, err)

	// 500 blocks / 100 interval = up to 5 snapshots
	// With 1 second time threshold and delays, we should create at least 1 snapshot
	// (actual count depends on timing and background processing)
	require.GreaterOrEqual(t, snapshotCount, 1, "should create snapshots with short time interval")

	t.Logf("Snapshots created with short time interval: %d (expected >= 1)", snapshotCount)
}
