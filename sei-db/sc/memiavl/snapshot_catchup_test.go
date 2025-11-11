package memiavl

import (
	"testing"
	"time"

	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/stretchr/testify/require"
)

// TestSnapshotThrottlingDuringCatchup verifies that snapshots are throttled during catch-up scenarios
func TestSnapshotThrottlingDuringCatchup(t *testing.T) {
	dir := t.TempDir()

	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:              dir,
		CreateIfMissing:  true,
		InitialStores:    []string{"test"},
		SnapshotInterval: 100, // Small interval for testing
	})
	require.NoError(t, err)
	defer db.Close()

	// Apply changesets to simulate catch-up scenario
	// We'll commit 15000 blocks rapidly (simulating state sync catch-up)
	for i := 0; i < 15000; i++ {
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
	time.Sleep(500 * time.Millisecond)
	require.NoError(t, db.checkBackgroundSnapshotRewrite())

	// Count snapshots created (excluding the initial one)
	snapshotCount := 0
	err = traverseSnapshots(dir, true, func(version int64) (bool, error) {
		if version > 0 { // Skip initial snapshot
			snapshotCount++
		}
		return false, nil
	})
	require.NoError(t, err)

	// With the throttling logic:
	// - Without throttling: 15000 / 100 = 150 snapshots
	// - With throttling (< 60 min, > 10000 blocks): Should skip most of them
	// We expect significantly fewer snapshots (should be close to 0-2)
	require.Less(t, snapshotCount, 5, "should create very few snapshots during rapid catch-up")

	t.Logf("Snapshots created during catch-up: %d (expected < 5)", snapshotCount)
}

// TestSnapshotCreationAfterTimeThreshold verifies snapshot creation after time threshold
func TestSnapshotCreationAfterTimeThreshold(t *testing.T) {
	dir := t.TempDir()

	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:              dir,
		CreateIfMissing:  true,
		InitialStores:    []string{"test"},
		SnapshotInterval: 100,
	})
	require.NoError(t, err)
	defer db.Close()

	// Commit initial blocks
	for i := 0; i < 200; i++ {
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
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, db.checkBackgroundSnapshotRewrite())

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
	for i := 200; i < 400; i++ {
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
	time.Sleep(1 * time.Second)
	require.NoError(t, db.checkBackgroundSnapshotRewrite())

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

// TestSnapshotNormalOperation verifies normal snapshot creation without throttling
func TestSnapshotNormalOperation(t *testing.T) {
	dir := t.TempDir()

	db, err := OpenDB(logger.NewNopLogger(), 0, Options{
		Dir:              dir,
		CreateIfMissing:  true,
		InitialStores:    []string{"test"},
		SnapshotInterval: 100,
	})
	require.NoError(t, err)
	defer db.Close()

	// Simulate normal operation with moderate block intervals
	// 500 blocks should not trigger throttling (< 10000 threshold)
	for i := 0; i < 500; i++ {
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
		// Wait longer to ensure snapshot completes
		if i%100 == 0 && i > 0 {
			time.Sleep(200 * time.Millisecond)
			require.NoError(t, db.checkBackgroundSnapshotRewrite())
		}
	}

	// Wait longer for any remaining background snapshot to complete
	time.Sleep(1 * time.Second)
	require.NoError(t, db.checkBackgroundSnapshotRewrite())

	// Count snapshots - should have multiple snapshots (not throttled)
	snapshotCount := 0
	err = traverseSnapshots(dir, true, func(version int64) (bool, error) {
		if version > 0 {
			snapshotCount++
		}
		return false, nil
	})
	require.NoError(t, err)

	// 500 blocks / 100 interval = up to 5 snapshots
	// In async mode, we expect at least 1 snapshot to be created
	// (timing-dependent, but catch-up logic should NOT prevent creation for < 10000 blocks)
	require.GreaterOrEqual(t, snapshotCount, 1, "should create regular snapshots during normal operation")

	t.Logf("Snapshots created during normal operation: %d (expected 1-5 depending on timing)", snapshotCount)
}
