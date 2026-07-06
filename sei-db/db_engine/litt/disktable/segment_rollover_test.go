package disktable

import (
	"bytes"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/segment"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/stretchr/testify/require"
)

// rolloverValueSize is the size of each value written by TestSegmentRollsOverAt2GiBBoundary.
// 2^32 is an exact multiple of this, so the boundary lands cleanly between values.
const rolloverValueSize = 256 * 1024 * 1024 // 256 MiB

// rolloverValueCount is chosen so the total written (count * 256 MiB = 5 GiB) comfortably exceeds the
// 2^32-byte (4 GiB) single-value-file addressable limit, forcing at least one segment rollover.
const rolloverValueCount = 20

// makeRolloverValue deterministically generates a value of rolloverValueSize bytes whose contents depend
// on index, so a mis-read (wrong segment/offset) is detectable without holding every value in memory.
func makeRolloverValue(index int) []byte {
	v := make([]byte, rolloverValueSize)
	seed := make([]byte, 4096)
	for i := range seed {
		seed[i] = byte(index*7 + i)
	}
	for off := 0; off < len(v); off += len(seed) {
		copy(v[off:], seed)
	}
	return v
}

// buildSingleShardDiskTableDefaultSegmentSize builds a single-shard, mem-keymap disk table using the
// DEFAULT (math.MaxUint32) target segment size, so segments only roll when the addressability limit is
// reached — the behavior under test. fsync is disabled to keep the multi-GiB write fast.
func buildSingleShardDiskTableDefaultSegmentSize(t *testing.T, root string) litt.ManagedTable {
	t.Helper()
	logger := slog.Default()

	keymapPath := filepath.Join(root, keymap.KeymapDirectoryName)
	keymapTypeFile, err := setupKeymapTypeFile(keymapPath, keymap.MemKeymapType)
	require.NoError(t, err)

	keys, _, err := keymap.NewMemKeymap(logger, "", true)
	require.NoError(t, err)

	config, err := litt.DefaultConfig(root)
	require.NoError(t, err)
	config.Fsync = false // default TargetSegmentFileSize (math.MaxUint32) is intentionally kept

	tableConfig := litt.DefaultTableConfig("rollover")
	tableConfig.ShardingFactor = 1 // one value file, so 4 GiB of writes crosses the 2^32 boundary

	runtimeConfig := litt.DefaultRuntimeConfig()
	runtimeConfig.Logger = logger

	table, err := NewDiskTable(
		config,
		runtimeConfig,
		"rollover",
		tableConfig,
		keys,
		keymapPath,
		keymapTypeFile,
		[]string{root},
		true,
		nil)
	require.NoError(t, err)
	return table
}

// TestSegmentRollsOverAt2GiBBoundary writes more than 2^32 bytes of values into a single-shard table and
// verifies that the value file never exceeds the 2^32-byte addressable limit: the control loop must roll
// to a new segment before a value would cross it (rather than panicking, the previous behavior). Every
// primary and secondary key must read back correctly across the boundary.
func TestSegmentRollsOverAt2GiBBoundary(t *testing.T) {
	root := t.TempDir()
	table := buildSingleShardDiskTableDefaultSegmentSize(t, root)
	defer func() { require.NoError(t, table.Close()) }()

	const secondaryOffset = uint32(1 * 1024 * 1024) // 1 MiB into the value
	const secondaryLength = uint32(64 * 1024)       // 64 KiB alias
	primaryKey := func(i int) []byte { return []byte(fmt.Sprintf("primary-%03d", i)) }
	secondaryKey := func(i int) []byte { return []byte(fmt.Sprintf("secondary-%03d", i)) }
	hasSecondary := func(i int) bool { return i%5 == 0 } // a subset carry a secondary sub-range alias

	for i := 0; i < rolloverValueCount; i++ {
		value := makeRolloverValue(i)
		if hasSecondary(i) {
			sk := &types.SecondaryKey{Key: secondaryKey(i), Offset: secondaryOffset, Length: secondaryLength}
			require.NoError(t, table.Put(primaryKey(i), value, sk))
		} else {
			require.NoError(t, table.Put(primaryKey(i), value))
		}
	}
	require.NoError(t, table.Flush())

	// The single shard's value files must have rolled: at least two segments exist, and none exceeds the
	// 2^32-byte addressable limit.
	valueFileSizes := collectValueFileSizes(t, root)
	require.GreaterOrEqual(t, len(valueFileSizes), 2,
		"expected the segment to roll over (>=2 value files) after writing >2^32 bytes")
	for path, size := range valueFileSizes {
		require.LessOrEqualf(t, size, int64(math.MaxUint32),
			"value file %s is %d bytes, exceeding the 2^32 addressable limit", path, size)
	}

	// Every primary (and secondary) key reads back correctly across the boundary.
	for i := 0; i < rolloverValueCount; i++ {
		expected := makeRolloverValue(i)

		got, exists, err := table.Get(primaryKey(i))
		require.NoError(t, err)
		require.Truef(t, exists, "primary key %d missing after rollover", i)
		require.Truef(t, bytes.Equal(expected, got), "primary value %d mismatch after rollover", i)

		if hasSecondary(i) {
			gotSecondary, exists, err := table.Get(secondaryKey(i))
			require.NoError(t, err)
			require.Truef(t, exists, "secondary key %d missing after rollover", i)
			wantSecondary := expected[secondaryOffset : secondaryOffset+secondaryLength]
			require.Truef(t, bytes.Equal(wantSecondary, gotSecondary),
				"secondary value %d mismatch after rollover", i)
		}
	}
}

// collectValueFileSizes walks root for segment value files (*.values) and returns their sizes by path.
func collectValueFileSizes(t *testing.T, root string) map[string]int64 {
	t.Helper()
	sizes := make(map[string]int64)
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != segment.ValuesFileExtension {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		sizes[path] = info.Size()
		return nil
	})
	require.NoError(t, err)
	return sizes
}
