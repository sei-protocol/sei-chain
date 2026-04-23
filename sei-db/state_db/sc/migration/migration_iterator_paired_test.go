package migration

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/stretchr/testify/require"
)

// Tests in this file exercise both the mock and memiavl iterators in lockstep,
// applying the same mutations to both and asserting identical results.

func TestWriteBeforeBoundaryIgnored(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("v1"), "b": []byte("v2"), "c": []byte("v3"), "d": []byte("v4")},
	}

	mockIter := NewMockMigrationIterator(copyData(data), false)
	db, memiavlIter := openMemiavlDB(t, data)

	// Migrate first two keys.
	mockBatch, mockBound, err := mockIter.NextBatch(2)
	require.NoError(t, err)
	memiavlBatch, memiavlBound, err := memiavlIter.NextBatch(2)
	require.NoError(t, err)
	requireBatchesEqual(t, mockBatch, memiavlBatch)
	require.True(t, mockBound.Equals(memiavlBound))

	// Write to a key before the boundary ("a") — should be invisible.
	mockIter.Data["bank"]["a"] = []byte("UPDATED")
	mockIter.Rebuild()
	require.NoError(t, db.ApplyChangeSet("bank", proto.ChangeSet{
		Pairs: []*proto.KVPair{{Key: []byte("a"), Value: []byte("UPDATED")}},
	}))
	_, err = db.Commit()
	require.NoError(t, err)

	// Drain remaining — should see c, d with original values only.
	mockBatch, mockBound, err = mockIter.NextBatch(10)
	require.NoError(t, err)
	memiavlBatch, memiavlBound, err = memiavlIter.NextBatch(10)
	require.NoError(t, err)
	requireBatchesEqual(t, mockBatch, memiavlBatch)
	require.True(t, mockBound.Equals(memiavlBound))
	require.Len(t, mockBatch, 2)
	requireEntry(t, mockBatch[0], "bank", "c", "v3")
	requireEntry(t, mockBatch[1], "bank", "d", "v4")
}

func TestWriteAfterBoundaryVisible(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("v1"), "b": []byte("v2"), "d": []byte("v4")},
	}

	mockIter := NewMockMigrationIterator(copyData(data), false)
	db, memiavlIter := openMemiavlDB(t, data)

	// Migrate first two keys (a, b).
	mockBatch, _, err := mockIter.NextBatch(2)
	require.NoError(t, err)
	memiavlBatch, _, err := memiavlIter.NextBatch(2)
	require.NoError(t, err)
	requireBatchesEqual(t, mockBatch, memiavlBatch)

	// Insert "c" after the boundary — should appear in next batch.
	mockIter.Data["bank"]["c"] = []byte("NEW")
	mockIter.Rebuild()
	require.NoError(t, db.ApplyChangeSet("bank", proto.ChangeSet{
		Pairs: []*proto.KVPair{{Key: []byte("c"), Value: []byte("NEW")}},
	}))
	_, err = db.Commit()
	require.NoError(t, err)

	// Drain remaining — should see c (new) and d.
	mockBatch, mockBound, err := mockIter.NextBatch(10)
	require.NoError(t, err)
	memiavlBatch, memiavlBound, err := memiavlIter.NextBatch(10)
	require.NoError(t, err)
	requireBatchesEqual(t, mockBatch, memiavlBatch)
	require.True(t, mockBound.Equals(memiavlBound))
	require.Len(t, mockBatch, 2)
	requireEntry(t, mockBatch[0], "bank", "c", "NEW")
	requireEntry(t, mockBatch[1], "bank", "d", "v4")
}

func TestDeleteAfterBoundaryVisible(t *testing.T) {
	data := map[string]map[string][]byte{
		"bank": {"a": []byte("v1"), "b": []byte("v2"), "c": []byte("v3"), "d": []byte("v4")},
	}

	mockIter := NewMockMigrationIterator(copyData(data), false)
	db, memiavlIter := openMemiavlDB(t, data)

	// Migrate first two keys (a, b).
	mockBatch, _, err := mockIter.NextBatch(2)
	require.NoError(t, err)
	memiavlBatch, _, err := memiavlIter.NextBatch(2)
	require.NoError(t, err)
	requireBatchesEqual(t, mockBatch, memiavlBatch)

	// Delete "c" which is after the boundary.
	delete(mockIter.Data["bank"], "c")
	mockIter.Rebuild()
	require.NoError(t, db.ApplyChangeSet("bank", proto.ChangeSet{
		Pairs: []*proto.KVPair{{Key: []byte("c"), Delete: true}},
	}))
	_, err = db.Commit()
	require.NoError(t, err)

	// Drain remaining — should see only d.
	mockBatch, mockBound, err := mockIter.NextBatch(10)
	require.NoError(t, err)
	memiavlBatch, memiavlBound, err := memiavlIter.NextBatch(10)
	require.NoError(t, err)
	requireBatchesEqual(t, mockBatch, memiavlBatch)
	require.True(t, mockBound.Equals(memiavlBound))
	require.Len(t, mockBatch, 1)
	requireEntry(t, mockBatch[0], "bank", "d", "v4")
}

const (
	numStores         = 10
	keysPerStore      = 1000
	batchSize         = 200
	mutationsPerRound = 15
)

// TestMigrationIteratorRandomized creates 10 stores each with 1000 keys,
// then iterates both a MapMigrationIterator and a MemiavlMigrationIterator
// in lockstep. Between each NextBatch call a small number of random mutations
// (inserts, updates, deletes) are applied to both. The test asserts that every
// batch returned by the two iterators is identical.
func TestMigrationIteratorRandomized(t *testing.T) {
	rng := rand.New(rand.NewSource(42)) //nolint:gosec // deterministic seed for reproducibility

	storeNames := make([]string, numStores)
	for i := range numStores {
		storeNames[i] = fmt.Sprintf("store_%02d", i)
	}
	sort.Strings(storeNames)

	data := make(map[string]map[string][]byte, numStores)
	for _, name := range storeNames {
		kvs := make(map[string][]byte, keysPerStore)
		for j := range keysPerStore {
			k := fmt.Sprintf("key_%04d", j)
			kvs[k] = randValue(rng)
		}
		data[name] = kvs
	}

	mockIter := NewMockMigrationIterator(copyData(data), false)
	db, memiavlIter := openMemiavlDB(t, data)

	round := 0
	for {
		mockBatch, mockBound, err := mockIter.NextBatch(batchSize)
		require.NoError(t, err)
		memiavlBatch, memiavlBound, err := memiavlIter.NextBatch(batchSize)
		require.NoError(t, err)

		require.Equal(t, len(mockBatch), len(memiavlBatch),
			"batch length mismatch at round %d", round)
		for i := range mockBatch {
			require.Equal(t, mockBatch[i].ModuleName, memiavlBatch[i].ModuleName,
				"ModuleName mismatch at round %d entry %d", round, i)
			require.Equal(t, mockBatch[i].Key, memiavlBatch[i].Key,
				"Key mismatch at round %d entry %d", round, i)
			require.Equal(t, mockBatch[i].Value, memiavlBatch[i].Value,
				"Value mismatch at round %d entry %d", round, i)
		}
		require.True(t, mockBound.Equals(memiavlBound), "boundary mismatch at round %d", round)

		if len(mockBatch) == 0 {
			break
		}

		applyRandomMutations(t, rng, storeNames, mockIter, db)
		round++
	}

	t.Logf("completed migration in %d rounds", round)
}

// applyRandomMutations applies mutationsPerRound random inserts, updates, or
// deletes to both the mock iterator's Data map and the memiavl DB.
func applyRandomMutations(
	t *testing.T,
	rng *rand.Rand,
	storeNames []string,
	mockIter *MockMigrationIterator,
	db *memiavl.DB,
) {
	t.Helper()

	changesByStore := make(map[string][]*proto.KVPair)

	for range mutationsPerRound {
		store := storeNames[rng.Intn(len(storeNames))]
		if mockIter.Data[store] == nil {
			mockIter.Data[store] = make(map[string][]byte)
		}
		kvs := mockIter.Data[store]

		op := rng.Intn(3) // 0=insert, 1=update, 2=delete
		switch op {
		case 0:
			k := fmt.Sprintf("rnd_%08d", rng.Intn(100_000))
			v := randValue(rng)
			kvs[k] = v
			changesByStore[store] = append(changesByStore[store], &proto.KVPair{
				Key: []byte(k), Value: v,
			})
		case 1:
			k := randomExistingKey(rng, kvs)
			if k == "" {
				continue
			}
			v := randValue(rng)
			kvs[k] = v
			changesByStore[store] = append(changesByStore[store], &proto.KVPair{
				Key: []byte(k), Value: v,
			})
		case 2:
			k := randomExistingKey(rng, kvs)
			if k == "" {
				continue
			}
			delete(kvs, k)
			changesByStore[store] = append(changesByStore[store], &proto.KVPair{
				Key: []byte(k), Delete: true,
			})
		}
	}

	mockIter.Rebuild()

	sortedStores := make([]string, 0, len(changesByStore))
	for store := range changesByStore {
		sortedStores = append(sortedStores, store)
	}
	sort.Strings(sortedStores)
	for _, store := range sortedStores {
		require.NoError(t, db.ApplyChangeSet(store, proto.ChangeSet{Pairs: changesByStore[store]}))
	}
	_, err := db.Commit()
	require.NoError(t, err)
}

func randValue(rng *rand.Rand) []byte {
	n := rng.Intn(32) + 1
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Intn(256)) //nolint:gosec // test-only
	}
	return b
}

func randomExistingKey(rng *rand.Rand, kvs map[string][]byte) string {
	if len(kvs) == 0 {
		return ""
	}
	keys := make([]string, 0, len(kvs))
	for k := range kvs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys[rng.Intn(len(keys))]
}

// TestMemiavlIteratorSurvivesSnapshotRewrite exercises the Issue 10 fix:
// a snapshot rewrite between NextBatch calls replaces the memiavl internal
// tree state, and the iterator (which re-resolves *Tree by name per call)
// must continue producing batches matching the reference MapMigrationIterator.
func TestMemiavlIteratorSurvivesSnapshotRewrite(t *testing.T) {
	const (
		keysPerStore   = 50
		iterBatchSize  = 7
		storesPerTable = 3
	)
	storeNames := []string{"auth", "bank", "staking"}

	data := make(map[string]map[string][]byte, storesPerTable)
	for _, name := range storeNames {
		kvs := make(map[string][]byte, keysPerStore)
		for j := 0; j < keysPerStore; j++ {
			k := fmt.Sprintf("key_%04d", j)
			kvs[k] = []byte(fmt.Sprintf("%s-%s", name, k))
		}
		data[name] = kvs
	}

	db, err := memiavl.OpenDB(0, memiavl.Options{
		Config: memiavl.Config{
			SnapshotInterval:        1,
			SnapshotMinTimeInterval: 0,
		},
		Dir:             t.TempDir(),
		CreateIfMissing: true,
		InitialStores:   storeNames,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	var changeSets []*proto.NamedChangeSet
	for name, kvs := range data {
		pairs := make([]*proto.KVPair, 0, len(kvs))
		for k, v := range kvs {
			pairs = append(pairs, &proto.KVPair{Key: []byte(k), Value: v})
		}
		changeSets = append(changeSets, &proto.NamedChangeSet{
			Name:      name,
			Changeset: proto.ChangeSet{Pairs: pairs},
		})
	}
	require.NoError(t, db.ApplyChangeSets(changeSets))
	_, err = db.Commit()
	require.NoError(t, err)

	mockIter := NewMockMigrationIterator(copyData(data), false)
	memiavlIter := NewMemiavlMigrationIterator(db, nil)

	// Pull a couple of batches before forcing a rewrite.
	for i := 0; i < 2; i++ {
		mockBatch, mockBound, err := mockIter.NextBatch(iterBatchSize)
		require.NoError(t, err)
		memiavlBatch, memiavlBound, err := memiavlIter.NextBatch(iterBatchSize)
		require.NoError(t, err)
		requireBatchesEqual(t, mockBatch, memiavlBatch)
		require.True(t, mockBound.Equals(memiavlBound))
	}

	// Force a background snapshot rewrite and wait for it to complete.
	// The next Commit call will pick up the result via checkAsyncTasks
	// and swap in the new MultiTree (which ReplaceWith's each tree).
	require.NoError(t, db.RewriteSnapshotBackground())
	time.Sleep(500 * time.Millisecond)
	_, err = db.Commit()
	require.NoError(t, err)

	// Drain to completion in lockstep and assert every batch still matches.
	for {
		mockBatch, mockBound, err := mockIter.NextBatch(iterBatchSize)
		require.NoError(t, err)
		memiavlBatch, memiavlBound, err := memiavlIter.NextBatch(iterBatchSize)
		require.NoError(t, err, "memiavl iterator must survive snapshot rewrite")
		requireBatchesEqual(t, mockBatch, memiavlBatch)
		require.True(t, mockBound.Equals(memiavlBound))
		if len(mockBatch) == 0 {
			break
		}
	}
}
