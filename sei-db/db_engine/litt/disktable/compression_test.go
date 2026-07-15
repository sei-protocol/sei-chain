package disktable

import (
	"bytes"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/stretchr/testify/require"
)

// buildCompressedMemKeyDiskTable builds a single-shard, mem-keymap disk table with the given compression
// algorithm. Single shard keeps value placement deterministic for size assertions.
func buildCompressedMemKeyDiskTable(
	t *testing.T,
	clock func() time.Time,
	name string,
	paths []string,
	algorithm types.CompressionAlgorithm,
) litt.ManagedTable {
	t.Helper()
	logger := slog.Default()
	keymapPath := filepath.Join(paths[0], keymap.KeymapDirectoryName)
	keymapTypeFile, err := setupKeymapTypeFile(keymapPath, keymap.MemKeymapType)
	require.NoError(t, err)
	keys, _, err := keymap.NewMemKeymap(logger, "", true)
	require.NoError(t, err)

	config, err := litt.DefaultConfig(paths...)
	require.NoError(t, err)
	config.GCPeriod = time.Hour
	config.Fsync = false
	config.TargetSegmentFileSize = 1 << 20

	tableConfig := litt.DefaultTableConfig(name)
	tableConfig.ShardingFactor = 1
	tableConfig.Compression = algorithm

	runtimeConfig := litt.DefaultRuntimeConfig()
	runtimeConfig.Clock = clock
	runtimeConfig.Logger = logger

	table, err := NewDiskTable(
		config,
		runtimeConfig,
		name,
		tableConfig,
		keys,
		keymapPath,
		keymapTypeFile,
		paths,
		true,
		nil,
	)
	require.NoError(t, err)
	return table
}

// compressiblePayload returns a repetitive (and therefore highly compressible) value.
func compressiblePayload() []byte {
	return bytes.Repeat([]byte("littDB compression makes value files smaller. "), 200)
}

func TestCompressionEndToEnd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	table := buildCompressedMemKeyDiskTable(t, time.Now, "compressed", []string{dir}, types.CompressionS2)
	defer func() { require.NoError(t, table.Close()) }()

	key := []byte("key")
	value := compressiblePayload()
	require.NoError(t, table.Put(key, value))

	// Before flush: served uncompressed from the write cache.
	got, ok, err := table.Get(key)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, value, got)

	// After flush: served from disk and decompressed.
	require.NoError(t, table.Flush())
	got, ok, err = table.Get(key)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, value, got)

	// The on-disk value file should be meaningfully smaller than the raw value.
	require.Less(t, int(table.Size()), len(value), "compressed segment should be smaller than the raw value")
}

func TestCompressionFullValueAliasSecondary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	table := buildCompressedMemKeyDiskTable(t, time.Now, "aliases", []string{dir}, types.CompressionS2)
	defer func() { require.NoError(t, table.Close()) }()

	primary := []byte("block-number-42")
	byHash := []byte("block-hash-deadbeef")
	value := compressiblePayload()

	// A full-value alias: Offset 0, Length == len(value). This is the block-by-number / block-by-hash case.
	alias := &types.SecondaryKey{Key: byHash, Offset: 0, Length: uint32(len(value))}
	require.NoError(t, table.Put(primary, value, alias))

	verify := func(stage string) {
		for _, k := range [][]byte{primary, byHash} {
			got, ok, err := table.Get(k)
			require.NoError(t, err, stage)
			require.True(t, ok, stage)
			require.Equal(t, value, got, stage)
		}
	}

	verify("before flush")
	require.NoError(t, table.Flush())
	verify("after flush")
}

func TestCompressionRejectsSubRangeSecondary(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	table := buildCompressedMemKeyDiskTable(t, time.Now, "subrange", []string{dir}, types.CompressionS2)
	defer func() { require.NoError(t, table.Close()) }()

	value := []byte("the quick brown fox")
	// A strict sub-range secondary is not supported on a compressed table.
	sub := &types.SecondaryKey{Key: []byte("quick"), Offset: 4, Length: 5}
	err := table.Put([]byte("primary"), value, sub)
	require.Error(t, err)
	require.Contains(t, err.Error(), "compressed table")
}

func TestCompressionFlushOrdering(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	table := buildCompressedMemKeyDiskTable(t, time.Now, "ordering", []string{dir}, types.CompressionS2)
	defer func() { require.NoError(t, table.Close()) }()

	// Interleave writes and flushes. Every key written before a Flush() must be durable and readable
	// afterwards, which only holds if the flush passes through the compression stage in order.
	const rounds = 20
	for i := 0; i < rounds; i++ {
		key := []byte(fmt.Sprintf("key-%03d", i))
		value := append(bytes.Repeat([]byte("payload "), 50), []byte(fmt.Sprintf("-%d", i))...)
		require.NoError(t, table.Put(key, value))
		require.NoError(t, table.Flush())

		got, ok, err := table.Get(key)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, value, got)
	}

	// Re-read every key from disk after all flushes.
	for i := 0; i < rounds; i++ {
		key := []byte(fmt.Sprintf("key-%03d", i))
		want := append(bytes.Repeat([]byte("payload "), 50), []byte(fmt.Sprintf("-%d", i))...)
		got, ok, err := table.Get(key)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, want, got)
	}
}

func TestCompressionIteration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	table := buildCompressedMemKeyDiskTable(t, time.Now, "iterate", []string{dir}, types.CompressionS2)
	defer func() { require.NoError(t, table.Close()) }()

	type record struct {
		key   string
		value []byte
	}
	records := make([]record, 0, 10)
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("k-%02d", i)
		value := append(compressiblePayload(), byte(i))
		records = append(records, record{key: key, value: value})
	}

	// Write each record with a full-value-alias secondary so iteration exercises grouped keys too.
	for _, r := range records {
		alias := &types.SecondaryKey{Key: []byte(r.key + "-alias"), Offset: 0, Length: uint32(len(r.value))}
		require.NoError(t, table.Put([]byte(r.key), r.value, alias))
	}
	require.NoError(t, table.Flush())

	// Forward iteration returns primary and alias, both decompressed to the full value.
	it, err := table.Iterator(false)
	require.NoError(t, err)
	seen := make(map[string][]byte)
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		key, _ := it.GetKey()
		value, err := it.GetValue()
		require.NoError(t, err)
		seen[string(key)] = append([]byte(nil), value...)
	}
	require.NoError(t, it.Close())

	for _, r := range records {
		require.Equal(t, r.value, seen[r.key], "primary %s", r.key)
		require.Equal(t, r.value, seen[r.key+"-alias"], "alias for %s", r.key)
	}
}
