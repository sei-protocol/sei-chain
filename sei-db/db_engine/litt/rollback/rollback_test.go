package rollback

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/stretchr/testify/require"
)

const rollbackTestTable = "rollback-test"

// newRollbackTestDB returns a littDB config (with a handful of storage roots) and an open database, sized
// so that even a modest number of writes produces many sealed segments. The caller must close the DB.
func newRollbackTestDB(t *testing.T) (*litt.Config, []string) {
	t.Helper()
	rand := util.NewTestRandom()
	testDirectory := t.TempDir()

	rootPathCount := rand.Uint64Range(1, 4)
	rootPaths := make([]string, rootPathCount)
	for i := uint64(0); i < rootPathCount; i++ {
		rootPaths[i] = filepath.Join(testDirectory, fmt.Sprintf("root-%d", i))
	}

	config, err := litt.DefaultConfig(rootPaths...)
	require.NoError(t, err)
	config.Fsync = false
	config.DoubleWriteProtection = true
	// A tiny target file size forces the data to be spread over many sealed segments, exercising both
	// whole-segment deletion and partial (truncating) rollback of a single segment.
	config.TargetSegmentFileSize = 100

	return config, rootPaths
}

// writeSequentialKeys writes count primary keys named "key-NNNNN" in index order, each with value
// "value-NNNNN", returning the value for every key index. The DB is flushed and closed before returning so
// the data is sealed on disk and ready for an offline rollback.
func writeSequentialKeys(t *testing.T, config *litt.Config, count int) map[int][]byte {
	t.Helper()

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)

	tableConfig := litt.DefaultTableConfig(rollbackTestTable)
	tableConfig.ShardingFactor = 3
	table, err := db.BuildTable(tableConfig)
	require.NoError(t, err)

	values := make(map[int][]byte, count)
	for i := 0; i < count; i++ {
		value := []byte(fmt.Sprintf("value-%05d", i))
		require.NoError(t, table.Put(keyForIndex(i), value))
		values[i] = value
	}

	require.NoError(t, table.Flush())
	require.NoError(t, db.Close())
	return values
}

func keyForIndex(i int) []byte {
	return []byte(fmt.Sprintf("key-%05d", i))
}

// indexFromKey parses the integer index encoded in a "key-NNNNN" key.
func indexFromKey(t *testing.T, key []byte) int {
	t.Helper()
	idx, err := strconv.Atoi(strings.TrimPrefix(string(key), "key-"))
	require.NoError(t, err)
	return idx
}

// openTable reopens the test table.
func openTable(t *testing.T, config *litt.Config) (litt.DB, litt.Table) {
	t.Helper()
	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)
	tableConfig := litt.DefaultTableConfig(rollbackTestTable)
	tableConfig.ShardingFactor = 3
	table, err := db.BuildTable(tableConfig)
	require.NoError(t, err)
	return db, table
}

// assertSequentialState verifies that exactly indices [0, keepThrough] are present (with their original
// values) and all higher indices are absent.
func assertSequentialState(t *testing.T, table litt.Table, count int, keepThrough int, values map[int][]byte) {
	t.Helper()
	for i := 0; i < count; i++ {
		got, ok, err := table.Get(keyForIndex(i))
		require.NoError(t, err)
		if i <= keepThrough {
			require.Truef(t, ok, "key %d should survive rollback", i)
			require.Equalf(t, values[i], got, "value mismatch for surviving key %d", i)
		} else {
			require.Falsef(t, ok, "key %d should have been rolled back", i)
		}
	}
	require.Equal(t, uint64(keepThrough+1), table.KeyCount())
}

// TestRollbackLittDB rolls back to a key in the middle of the write history and verifies that the surviving
// keys keep their values and the newer keys are gone.
func TestRollbackLittDB(t *testing.T) {
	t.Parallel()

	const count = 200
	const keepThrough = 137

	config, roots := newRollbackTestDB(t)
	values := writeSequentialKeys(t, config, count)

	err := RollbackLittDB(roots, func(key []byte, isPrimary bool) (bool, error) {
		require.True(t, isPrimary) // these are all standalone primary keys
		return indexFromKey(t, key) <= keepThrough, nil
	})
	require.NoError(t, err)

	// The rollback discards the keymap, so reopening rebuilds it from the truncated segment files. A correct
	// read of every surviving key therefore also confirms the segments were truncated consistently.
	db, table := openTable(t, config)
	assertSequentialState(t, table, count, keepThrough, values)
	require.NoError(t, db.Close())
}

// TestRollbackNoMatch verifies that a table for which the filter never returns true is left untouched.
func TestRollbackNoMatch(t *testing.T) {
	t.Parallel()

	const count = 50

	config, roots := newRollbackTestDB(t)
	values := writeSequentialKeys(t, config, count)

	err := RollbackLittDB(roots, func(key []byte, isPrimary bool) (bool, error) {
		return false, nil
	})
	require.NoError(t, err)

	db, table := openTable(t, config)
	assertSequentialState(t, table, count, count-1, values) // everything survives
	require.NoError(t, db.Close())
}

// TestRollbackKeepsEverything verifies that when the newest key matches, nothing is deleted.
func TestRollbackKeepsEverything(t *testing.T) {
	t.Parallel()

	const count = 50

	config, roots := newRollbackTestDB(t)
	values := writeSequentialKeys(t, config, count)

	err := RollbackLittDB(roots, func(key []byte, isPrimary bool) (bool, error) {
		return true, nil // the very first key visited (the newest) matches
	})
	require.NoError(t, err)

	db, table := openTable(t, config)
	assertSequentialState(t, table, count, count-1, values)
	require.NoError(t, db.Close())
}

// TestRollbackPropagatesFilterError verifies that an error from the filter aborts the rollback.
func TestRollbackPropagatesFilterError(t *testing.T) {
	t.Parallel()

	config, roots := newRollbackTestDB(t)
	writeSequentialKeys(t, config, 20)

	wantErr := fmt.Errorf("boom")
	err := RollbackLittDB(roots, func(key []byte, isPrimary bool) (bool, error) {
		return false, wantErr
	})
	require.ErrorIs(t, err, wantErr)
}

// TestRollbackWithSecondaryKeys verifies that secondary keys are handled correctly: the rollback point's
// whole group (its primary plus the secondaries written after it) is retained, isPrimary is reported
// correctly to the filter, and discarded groups lose both their primary and secondary keys.
func TestRollbackWithSecondaryKeys(t *testing.T) {
	t.Parallel()

	const count = 60
	const keepThrough = 28

	config, roots := newRollbackTestDB(t)

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
		// One secondary key aliasing the entire value.
		secondary := &types.SecondaryKey{Key: secondaryKey(i), Offset: 0, Length: uint32(len(value))}
		require.NoError(t, table.Put(primaryKey(i), value, secondary))
		values[i] = value
	}
	require.NoError(t, table.Flush())
	require.NoError(t, db.Close())

	err = RollbackLittDB(roots, func(key []byte, isPrimary bool) (bool, error) {
		// Only primary keys carry an index we want to stop on; secondaries are reported with isPrimary=false.
		if !isPrimary {
			require.True(t, strings.HasPrefix(string(key), "sk-"))
			return false, nil
		}
		require.True(t, strings.HasPrefix(string(key), "pk-"))
		idx, err := strconv.Atoi(strings.TrimPrefix(string(key), "pk-"))
		require.NoError(t, err)
		return idx <= keepThrough, nil
	})
	require.NoError(t, err)

	db, err = littbuilder.NewDB(config)
	require.NoError(t, err)
	table, err = db.BuildTable(tableConfig)
	require.NoError(t, err)

	for i := 0; i < count; i++ {
		gotPrimary, okPrimary, err := table.Get(primaryKey(i))
		require.NoError(t, err)
		gotSecondary, okSecondary, err := table.Get(secondaryKey(i))
		require.NoError(t, err)

		if i <= keepThrough {
			require.Truef(t, okPrimary, "primary %d should survive", i)
			require.Equal(t, values[i], gotPrimary)
			require.Truef(t, okSecondary, "secondary %d should survive (same group as its primary)", i)
			require.Equal(t, values[i], gotSecondary)
		} else {
			require.Falsef(t, okPrimary, "primary %d should be rolled back", i)
			require.Falsef(t, okSecondary, "secondary %d should be rolled back", i)
		}
	}
	require.NoError(t, db.Close())
}
