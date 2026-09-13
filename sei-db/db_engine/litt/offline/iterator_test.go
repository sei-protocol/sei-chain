package offline

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/stretchr/testify/require"
)

// TestOfflineIteratorForwardOrder verifies that a forward offline iterator returns keys in insertion
// order with correct values, matching the live forward iterator's contract.
func TestOfflineIteratorForwardOrder(t *testing.T) {
	t.Parallel()
	const count = 20

	config, _ := newRollbackTestDB(t)
	values := writeSequentialKeys(t, config, count)

	it, err := NewIterator(config, rollbackTestTable, false)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	for i := 0; i < count; i++ {
		hasNext, err := it.Next()
		require.NoError(t, err)
		require.True(t, hasNext)

		key, isPrimary, err := it.GetKey()
		require.NoError(t, err)
		require.True(t, isPrimary)
		require.Equal(t, i, indexFromKey(t, key))

		value, err := it.GetValue()
		require.NoError(t, err)
		require.Equal(t, values[i], value)
	}

	hasNext, err := it.Next()
	require.NoError(t, err)
	require.False(t, hasNext)
}

// TestOfflineIteratorReverseOrder verifies that a reverse offline iterator returns keys in exact
// reverse insertion order with correct values.
func TestOfflineIteratorReverseOrder(t *testing.T) {
	t.Parallel()
	const count = 20

	config, _ := newRollbackTestDB(t)
	values := writeSequentialKeys(t, config, count)

	it, err := NewIterator(config, rollbackTestTable, true)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	for i := count - 1; i >= 0; i-- {
		hasNext, err := it.Next()
		require.NoError(t, err)
		require.True(t, hasNext)

		key, isPrimary, err := it.GetKey()
		require.NoError(t, err)
		require.True(t, isPrimary)
		require.Equal(t, i, indexFromKey(t, key))

		value, err := it.GetValue()
		require.NoError(t, err)
		require.Equal(t, values[i], value)
	}

	hasNext, err := it.Next()
	require.NoError(t, err)
	require.False(t, hasNext)
}

// TestOfflineIteratorEmptyTable verifies that an offline iterator over an existing but empty table
// reports no keys, rather than erroring.
func TestOfflineIteratorEmptyTable(t *testing.T) {
	t.Parallel()
	config, _ := newRollbackTestDB(t)

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)
	_, err = db.BuildTable(litt.DefaultTableConfig(rollbackTestTable))
	require.NoError(t, err)
	require.NoError(t, db.Close())

	it, err := NewIterator(config, rollbackTestTable, false)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	hasNext, err := it.Next()
	require.NoError(t, err)
	require.False(t, hasNext)
}

// TestOfflineIteratorSecondaryKeys verifies group handling on both directions: forward iteration
// visits a primary immediately followed by its secondary (both serving the same value), and reverse
// iteration visits the secondary first, reading its value directly since the group optimization does
// not apply in reverse.
func TestOfflineIteratorSecondaryKeys(t *testing.T) {
	t.Parallel()
	const count = 30

	config, _ := newRollbackTestDB(t)

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)
	tableConfig := litt.DefaultTableConfig(rollbackTestTable)
	tableConfig.ShardingFactor = 2
	table, err := db.BuildTable(tableConfig)
	require.NoError(t, err)

	primaryKey := func(i int) []byte { return []byte(fmt.Sprintf("pk-%05d", i)) }
	secondaryKey := func(i int) []byte { return []byte(fmt.Sprintf("sk-%05d", i)) }

	values := make(map[int][]byte, count)
	for i := 0; i < count; i++ {
		value := []byte(fmt.Sprintf("value-%05d", i))
		secondary := &types.SecondaryKey{Key: secondaryKey(i), Offset: 0, Length: uint32(len(value))}
		require.NoError(t, table.Put(primaryKey(i), value, secondary))
		values[i] = value
	}
	require.NoError(t, table.Flush())
	require.NoError(t, db.Close())

	forward, err := NewIterator(config, rollbackTestTable, false)
	require.NoError(t, err)
	for i := 0; i < count; i++ {
		hasNext, err := forward.Next()
		require.NoError(t, err)
		require.True(t, hasNext)
		key, isPrimary, err := forward.GetKey()
		require.NoError(t, err)
		require.True(t, isPrimary)
		require.Equal(t, primaryKey(i), key)
		value, err := forward.GetValue()
		require.NoError(t, err)
		require.Equal(t, values[i], value)

		hasNext, err = forward.Next()
		require.NoError(t, err)
		require.True(t, hasNext)
		key, isPrimary, err = forward.GetKey()
		require.NoError(t, err)
		require.False(t, isPrimary)
		require.Equal(t, secondaryKey(i), key)
		value, err = forward.GetValue()
		require.NoError(t, err)
		require.Equal(t, values[i], value)
	}
	hasNext, err := forward.Next()
	require.NoError(t, err)
	require.False(t, hasNext)
	require.NoError(t, forward.Close())

	reverse, err := NewIterator(config, rollbackTestTable, true)
	require.NoError(t, err)
	for i := count - 1; i >= 0; i-- {
		hasNext, err := reverse.Next()
		require.NoError(t, err)
		require.True(t, hasNext)
		key, isPrimary, err := reverse.GetKey()
		require.NoError(t, err)
		require.False(t, isPrimary)
		require.Equal(t, secondaryKey(i), key)
		value, err := reverse.GetValue()
		require.NoError(t, err)
		require.Equal(t, values[i], value)

		hasNext, err = reverse.Next()
		require.NoError(t, err)
		require.True(t, hasNext)
		key, isPrimary, err = reverse.GetKey()
		require.NoError(t, err)
		require.True(t, isPrimary)
		require.Equal(t, primaryKey(i), key)
		value, err = reverse.GetValue()
		require.NoError(t, err)
		require.Equal(t, values[i], value)
	}
	hasNext, err = reverse.Next()
	require.NoError(t, err)
	require.False(t, hasNext)
	require.NoError(t, reverse.Close())
}

// TestOfflineIteratorExcludesBelowGCWatermark verifies that segments already logically garbage
// collected are excluded from an offline iterator, matching what a live table's own reads would see.
func TestOfflineIteratorExcludesBelowGCWatermark(t *testing.T) {
	t.Parallel()
	const count = 50

	config, roots := newRollbackTestDB(t)
	writeSequentialKeys(t, config, count)

	segsBefore, err := gatherOrderedSegments(slog.Default(), roots, rollbackTestTable, config.Fsync)
	require.NoError(t, err)
	require.Greaterf(t, len(segsBefore), 1, "test requires multiple segments to be meaningful")
	highestIndex := segsBefore[len(segsBefore)-1].SegmentIndex()
	expectedKeys, err := segsBefore[len(segsBefore)-1].GetKeys()
	require.NoError(t, err)

	tableDir := filepath.Join(roots[0], rollbackTestTable)
	watermark, err := disktable.LoadGCWatermarkFile(tableDir)
	require.NoError(t, err)
	require.NoError(t, watermark.Update(highestIndex))

	it, err := NewIterator(config, rollbackTestTable, false)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	gotKeys := 0
	for {
		hasNext, err := it.Next()
		require.NoError(t, err)
		if !hasNext {
			break
		}
		gotKeys++
	}
	require.Equal(t, len(expectedKeys), gotKeys, "only the segment at/above the watermark should be visible")
}

// TestOfflineIteratorFailsWhileLive verifies that opening an offline iterator against a table a live
// database still has open fails, rather than reading alongside it.
func TestOfflineIteratorFailsWhileLive(t *testing.T) {
	t.Parallel()
	config, _ := newRollbackTestDB(t)

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()
	_, err = db.BuildTable(litt.DefaultTableConfig(rollbackTestTable))
	require.NoError(t, err)

	_, err = NewIterator(config, rollbackTestTable, false)
	require.Error(t, err)
}
