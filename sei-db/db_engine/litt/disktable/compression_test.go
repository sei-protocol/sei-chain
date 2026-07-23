package disktable

import (
	"bytes"
	"fmt"
	"log/slog"
	"math"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/segment"
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

// incompressiblePayload returns deterministic pseudo-random bytes that S2 cannot shrink, so the
// store-smaller path keeps them raw (tagged CompressionNone) even on a compressed segment.
func incompressiblePayload() []byte {
	payload := make([]byte, 4096)
	rng := rand.New(rand.NewSource(1)) //nolint:gosec // test fixture, not security-sensitive
	_, _ = rng.Read(payload)
	return payload
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

// TestCompressionMixedValues writes a compressible and an incompressible value into the same compressed
// segment and confirms both survive a flush. The incompressible value exercises the per-value store-raw
// tag (CompressionNone) alongside the compressible value's S2 tag on one segment.
func TestCompressionMixedValues(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	table := buildCompressedMemKeyDiskTable(t, time.Now, "mixed", []string{dir}, types.CompressionS2)
	defer func() { require.NoError(t, table.Close()) }()

	compressible := compressiblePayload()
	incompressible := incompressiblePayload()
	require.NoError(t, table.Put([]byte("compressible"), compressible))
	require.NoError(t, table.Put([]byte("incompressible"), incompressible))
	require.NoError(t, table.Flush())

	got, ok, err := table.Get([]byte("compressible"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, compressible, got)

	got, ok, err = table.Get([]byte("incompressible"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, incompressible, got)
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

// TestCompressionToggleAcrossRestarts writes into the same table under alternating compression settings
// (off -> on -> off -> on), restarting between each phase, then reads everything back. It exercises the
// core guarantee that each segment is decoded with the algorithm it was written with, independent of the
// table's current configuration.
func TestCompressionToggleAcrossRestarts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	name := "toggle"

	phases := []struct {
		label string
		algo  types.CompressionAlgorithm
	}{
		{"off1", types.CompressionNone},
		{"on1", types.CompressionS2},
		{"off2", types.CompressionNone},
		{"on2", types.CompressionS2},
	}

	written := make(map[string][]byte)

	for _, phase := range phases {
		table := buildCompressedMemKeyDiskTable(t, time.Now, name, []string{dir}, phase.algo)

		// Every key written in a prior phase (under a possibly different algorithm) must still read back
		// correctly after this restart.
		for k, v := range written {
			got, ok, err := table.Get([]byte(k))
			require.NoError(t, err, "reading %q in phase %s", k, phase.label)
			require.True(t, ok, "missing %q in phase %s", k, phase.label)
			require.Equal(t, v, got, "value mismatch for %q in phase %s", k, phase.label)
		}

		// Write this phase's keys into a fresh segment created under this phase's algorithm.
		for i := 0; i < 5; i++ {
			key := fmt.Sprintf("%s-%d", phase.label, i)
			value := append(compressiblePayload(), []byte(key)...)
			require.NoError(t, table.Put([]byte(key), value))
			written[key] = value
		}
		require.NoError(t, table.Flush())
		require.NoError(t, table.Close())
	}

	// Final restart: read everything back, spanning segments written under both algorithms.
	final := buildCompressedMemKeyDiskTable(t, time.Now, name, []string{dir}, types.CompressionNone)
	defer func() { require.NoError(t, final.Close()) }()
	for k, v := range written {
		got, ok, err := final.Get([]byte(k))
		require.NoError(t, err)
		require.True(t, ok, "missing %q after final restart", k)
		require.Equal(t, v, got, "value mismatch for %q after final restart", k)
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

// TestCompressionReverseIteration verifies that reverse iteration over a compressed segment returns the
// correct decompressed value for each key (reverse iteration reads through Segment.Read).
func TestCompressionReverseIteration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	table := buildCompressedMemKeyDiskTable(t, time.Now, "reverse", []string{dir}, types.CompressionS2)
	defer func() { require.NoError(t, table.Close()) }()

	want := make(map[string][]byte)
	for i := 0; i < 10; i++ {
		key := fmt.Sprintf("k-%02d", i)
		value := append(compressiblePayload(), byte(i))
		require.NoError(t, table.Put([]byte(key), value))
		want[key] = value
	}
	require.NoError(t, table.Flush())

	it, err := table.Iterator(true)
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
	require.Equal(t, want, seen)
}

// TestCompressionRecoveryUnsealedSegment exercises the crash-recovery path (Segment.sealLoadedSegment)
// over on-disk *compressed* blobs. Every other compression test does a clean Flush+Close (which seals)
// before reopening, so this is the only coverage of recovery re-sealing a segment and validating
// value-file completeness against compressed-blob addresses.
func TestCompressionRecoveryUnsealedSegment(t *testing.T) {
	t.Parallel()

	// unsealDataSegment flips the sealed byte of the data-bearing segment back to 0, simulating a crash
	// that happened before the segment was sealed. This is what makes LoadSegment run recovery on reopen.
	// It returns the segments directory and the segment index so callers can also corrupt the value file.
	unsealDataSegment := func(t *testing.T, dir string, name string) (string, uint32) {
		t.Helper()
		segmentsDir := findLatestSegmentDir(t, dir, name)
		idx := segmentIndexWithLargestValueFile(t, segmentsDir)
		metaPath := path.Join(segmentsDir, fmt.Sprintf("%d%s", idx, segment.MetadataFileExtension))
		mdBytes, err := os.ReadFile(metaPath)
		require.NoError(t, err)
		require.Equal(t, segment.V4MetadataSize, len(mdBytes)) // a compressed table writes v4 metadata
		mdBytes[segment.MetadataSealedByteOffset] = 0
		require.NoError(t, os.WriteFile(metaPath, mdBytes, 0600))
		return segmentsDir, idx
	}

	t.Run("clean crash before seal, all groups survive", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		name := "recover-clean"

		table := buildCompressedMemKeyDiskTable(t, time.Now, name, []string{dir}, types.CompressionS2)

		written := make(map[string][]byte)
		for i := 0; i < 6; i++ {
			key := fmt.Sprintf("key-%02d", i)
			value := append(compressiblePayload(), byte(i))
			require.NoError(t, table.Put([]byte(key), value))
			written[key] = value
		}
		// One group carries a full-value-alias secondary so recovery re-seals a multi-record group too.
		aliasValue := append(compressiblePayload(), []byte("-alias")...)
		alias := &types.SecondaryKey{Key: []byte("alias-secondary"), Offset: 0, Length: uint32(len(aliasValue))}
		require.NoError(t, table.Put([]byte("aliased-primary"), aliasValue, alias))
		written["aliased-primary"] = aliasValue

		require.NoError(t, table.Flush())
		require.NoError(t, table.Close())

		unsealDataSegment(t, dir, name)

		table = buildCompressedMemKeyDiskTable(t, time.Now, name, []string{dir}, types.CompressionS2)
		defer func() { require.NoError(t, table.Close()) }()

		for key, want := range written {
			got, ok, err := table.Get([]byte(key))
			require.NoError(t, err)
			require.True(t, ok, "expected %q to survive recovery", key)
			require.Equal(t, want, got, "value for %q must decompress correctly after recovery", key)
		}
		// The full-value-alias secondary resolves to the whole decompressed value.
		got, ok, err := table.Get([]byte("alias-secondary"))
		require.NoError(t, err)
		require.True(t, ok, "alias secondary must survive recovery")
		require.Equal(t, aliasValue, got)
	})

	t.Run("torn compressed value, last group dropped", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		name := "recover-torn"

		table := buildCompressedMemKeyDiskTable(t, time.Now, name, []string{dir}, types.CompressionS2)

		survivors := make(map[string][]byte)
		for i := 0; i < 4; i++ {
			key := fmt.Sprintf("survivor-%02d", i)
			value := append(compressiblePayload(), byte(i))
			require.NoError(t, table.Put([]byte(key), value))
			survivors[key] = value
		}
		// The last-written group lands at the tail of the single-shard value file. It carries a
		// full-value alias so we can confirm the whole group is dropped — primary and secondary alike.
		tornValue := append(compressiblePayload(), []byte("-torn")...)
		tornAlias := &types.SecondaryKey{
			Key:    []byte("torn-secondary"),
			Offset: 0,
			Length: uint32(len(tornValue)),
		}
		require.NoError(t, table.Put([]byte("torn-primary"), tornValue, tornAlias))

		require.NoError(t, table.Flush())
		require.NoError(t, table.Close())

		segmentsDir := findLatestSegmentDir(t, dir, name)
		idx := segmentIndexWithLargestValueFile(t, segmentsDir)

		// Lop a few bytes off the tail of the value file so the last group's blob is incomplete: its
		// address end now exceeds the flushed size, so group-atomic recovery discards the whole group.
		valPath := path.Join(segmentsDir, fmt.Sprintf("%d-0%s", idx, segment.ValuesFileExtension))
		data, err := os.ReadFile(valPath)
		require.NoError(t, err)
		require.GreaterOrEqual(t, len(data), 4)
		require.NoError(t, os.WriteFile(valPath, data[:len(data)-3], 0600))

		metaPath := path.Join(segmentsDir, fmt.Sprintf("%d%s", idx, segment.MetadataFileExtension))
		mdBytes, err := os.ReadFile(metaPath)
		require.NoError(t, err)
		mdBytes[segment.MetadataSealedByteOffset] = 0
		require.NoError(t, os.WriteFile(metaPath, mdBytes, 0600))

		table = buildCompressedMemKeyDiskTable(t, time.Now, name, []string{dir}, types.CompressionS2)
		defer func() { require.NoError(t, table.Close()) }()

		for key, want := range survivors {
			got, ok, err := table.Get([]byte(key))
			require.NoError(t, err)
			require.True(t, ok, "expected %q to survive recovery", key)
			require.Equal(t, want, got)
		}
		// The torn group is gone in its entirety.
		for _, key := range []string{"torn-primary", "torn-secondary"} {
			_, ok, err := table.Get([]byte(key))
			require.NoError(t, err)
			require.False(t, ok, "expected torn group member %q to be dropped by recovery", key)
		}
	})
}

// buildCompressedGCTable builds a single-shard, mem-keymap, S2-compressed table wired for deterministic
// GC testing: size-based sealing is disabled so segments seal exactly every maxSegmentKeyCount keys,
// background GC is effectively disabled (driven explicitly via RunGC), and the clock is injectable so a
// test can advance time past a segment's TTL.
func buildCompressedGCTable(
	t *testing.T,
	clock func() time.Time,
	name string,
	dir string,
	maxSegmentKeyCount uint32,
) litt.ManagedTable {
	t.Helper()
	logger := slog.Default()
	keymapPath := filepath.Join(dir, keymap.KeymapDirectoryName)
	keymapTypeFile, err := setupKeymapTypeFile(keymapPath, keymap.MemKeymapType)
	require.NoError(t, err)
	keys, _, err := keymap.NewMemKeymap(logger, "", true)
	require.NoError(t, err)

	config, err := litt.DefaultConfig(dir)
	require.NoError(t, err)
	config.TargetSegmentFileSize = math.MaxUint32
	config.MaxSegmentKeyCount = maxSegmentKeyCount
	config.GCPeriod = time.Hour
	config.Fsync = false

	tableConfig := litt.DefaultTableConfig(name)
	tableConfig.ShardingFactor = 1
	tableConfig.Compression = types.CompressionS2

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
		[]string{dir},
		true,
		nil,
	)
	require.NoError(t, err)
	return table
}

// TestCompressionGarbageCollection collects TTL-expired *compressed* segments and confirms that the
// surviving compressed data still reads back (and decompresses) correctly, both via Get and via a
// forward iterator.
func TestCompressionGarbageCollection(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	start := time.Unix(1_700_000_000, 0)
	var fakeTime atomic.Pointer[time.Time]
	fakeTime.Store(&start)
	clock := func() time.Time { return *fakeTime.Load() }

	// Seal after every 2 keys so both batches below land in sealed (hence collectable) segments rather
	// than the always-live mutable segment.
	table := buildCompressedGCTable(t, clock, "gc", dir, 2)
	defer func() { require.NoError(t, table.Close()) }()

	ttl := time.Hour
	require.NoError(t, table.SetTTL(ttl))

	// Batch A, written at t0.
	oldKeys := make(map[string][]byte)
	for i := 0; i < 4; i++ {
		key := fmt.Sprintf("old-%02d", i)
		value := append(compressiblePayload(), byte(i))
		require.NoError(t, table.Put([]byte(key), value))
		oldKeys[key] = value
	}
	require.NoError(t, table.Flush())

	// Batch B, written far enough after batch A that its segments are much younger.
	tB := start.Add(10 * ttl)
	fakeTime.Store(&tB)
	newKeys := make(map[string][]byte)
	for i := 0; i < 4; i++ {
		key := fmt.Sprintf("new-%02d", i)
		value := append(compressiblePayload(), []byte(fmt.Sprintf("-new-%d", i))...)
		require.NoError(t, table.Put([]byte(key), value))
		newKeys[key] = value
	}
	require.NoError(t, table.Flush())

	// Advance just past batch A's TTL: batch A's segments are now expired, batch B's are still young.
	now := tB.Add(ttl / 2)
	fakeTime.Store(&now)
	require.NoError(t, table.RunGC())

	// Batch A is gone; batch B survives and decompresses to the original bytes.
	for key := range oldKeys {
		requireKeyAbsent(t, table, key)
	}
	for key, want := range newKeys {
		got, ok, err := table.Get([]byte(key))
		require.NoError(t, err)
		require.True(t, ok, "expected %q to survive GC", key)
		require.Equal(t, want, got, "survivor %q must decompress correctly", key)
	}

	// A forward iterator sees exactly the survivors, each decompressed correctly.
	it, err := table.Iterator(false)
	require.NoError(t, err)
	entries := drainIterator(t, it)
	require.NoError(t, it.Close())

	expectedKeys := make([]string, 0, len(newKeys))
	for key := range newKeys {
		expectedKeys = append(expectedKeys, key)
	}
	require.ElementsMatch(t, expectedKeys, entryKeys(entries))
	for _, e := range entries {
		require.Equal(t, newKeys[e.key], []byte(e.value),
			"iterated survivor %q must decompress correctly", e.key)
	}
}
