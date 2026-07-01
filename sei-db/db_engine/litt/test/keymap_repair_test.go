package test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/littbuilder"
	"github.com/stretchr/testify/require"
)

// buildKeymapRepairDB builds a LittDB instance rooted at directory using a (sync) PebbleDB keymap.
// The same directory is reused across restarts so that the on-disk keymap and segments persist.
func buildKeymapRepairDB(t *testing.T, directory string) litt.DB {
	config, err := litt.DefaultConfig(directory)
	require.NoError(t, err)
	config.KeymapType = keymap.PebbleDBKeymapType
	config.TargetSegmentFileSize = 100 // tiny, so the data spans many segments
	config.Fsync = false               // fsync is too slow for unit test workloads
	config.DoubleWriteProtection = true

	db, err := littbuilder.NewDB(config)
	require.NoError(t, err)
	return db
}

// TestKeymapRepairOnRestart verifies that LittDB repairs a keymap that has fallen behind the segment
// files. We write data, close cleanly, then reach into the keymap's PebbleDB out-of-band and delete the
// most-recently-written keys (the "tail"). On restart, those keys must be repaired back into the keymap
// from the segment key files and become readable again via Get.
func TestKeymapRepairOnRestart(t *testing.T) {
	directory := t.TempDir()
	tableName := "repair-test"

	tableConfig := litt.TableConfig{
		Name:           tableName,
		TTL:            0,
		ShardingFactor: 4,
		WriteCacheSize: 1000,
	}

	keyCount := 100
	values := make(map[string][]byte, keyCount)
	key := func(i int) []byte { return []byte(fmt.Sprintf("key-%04d", i)) }

	// Phase 1: write data, flush, close cleanly.
	db := buildKeymapRepairDB(t, directory)
	table, err := db.BuildTable(tableConfig)
	require.NoError(t, err)

	for i := 0; i < keyCount; i++ {
		value := []byte(fmt.Sprintf("value-%04d", i))
		require.NoError(t, table.Put(key(i), value))
		values[string(key(i))] = value
	}
	require.NoError(t, table.Flush())
	require.NoError(t, db.Close())

	// Phase 2: open the keymap's PebbleDB out-of-band and delete the newest keys (the repairable tail).
	deletedCount := 5
	deleted := make([][]byte, 0, deletedCount)
	for i := keyCount - deletedCount; i < keyCount; i++ {
		deleted = append(deleted, key(i))
	}

	keymapDataDir := filepath.Join(directory, tableName, keymap.KeymapDirectoryName, keymap.KeymapDataDirectoryName)
	pdb, err := pebble.Open(keymapDataDir, &pebble.Options{})
	require.NoError(t, err)
	for _, k := range deleted {
		require.NoError(t, pdb.Delete(k, pebble.Sync))
	}
	require.NoError(t, pdb.Close())

	// Phase 3: restart LittDB and observe that the deleted keys have been repaired into the keymap.
	db = buildKeymapRepairDB(t, directory)
	table, err = db.BuildTable(tableConfig)
	require.NoError(t, err)

	for _, k := range deleted {
		value, ok, err := table.Get(k)
		require.NoError(t, err)
		require.True(t, ok, "key %s should have been repaired into the keymap", k)
		require.Equal(t, values[string(k)], value)
	}

	require.NoError(t, db.Close())
}
