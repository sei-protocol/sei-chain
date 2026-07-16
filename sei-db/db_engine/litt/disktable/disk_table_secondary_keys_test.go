package disktable

import (
	"fmt"
	"log/slog"
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
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/stretchr/testify/require"
)

// buildOneShardMemKeyDiskTable creates a disk table with sharding factor 1 so every value lands in
// the same value file. This makes torn-write recovery tests deterministic — we know exactly which
// file to truncate.
func buildOneShardMemKeyDiskTable(
	clock func() time.Time,
	name string,
	paths []string,
) (litt.ManagedTable, error) {
	logger := slog.Default()
	keymapPath := filepath.Join(paths[0], keymap.KeymapDirectoryName)
	keymapTypeFile, err := setupKeymapTypeFile(keymapPath, keymap.MemKeymapType)
	if err != nil {
		return nil, fmt.Errorf("failed to load keymap type file: %w", err)
	}
	keys, _, err := keymap.NewMemKeymap(logger, "", true)
	if err != nil {
		return nil, fmt.Errorf("failed to create keymap: %w", err)
	}
	config, err := litt.DefaultConfig(paths...)
	if err != nil {
		return nil, fmt.Errorf("failed to create config: %w", err)
	}
	config.GCPeriod = time.Millisecond
	config.Fsync = false
	// Pick a target file size large enough that several Puts can co-exist in one segment without
	// rotation; the recovery test specifically wants the torn group to share a segment with the
	// surviving groups so the all-or-nothing behavior is observable.
	config.TargetSegmentFileSize = 1 << 20

	tableConfig := litt.DefaultTableConfig(name)
	tableConfig.ShardingFactor = 1

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
	if err != nil {
		return nil, fmt.Errorf("failed to create disk table: %w", err)
	}
	return table, nil
}

// This file collects tests specific to the secondary-key API of DiskTable:
//
//   * basic reads/exists/key-count semantics for secondaries, both before and after flush,
//   * input validation in PutBatch (oversized / nil / out-of-range / duplicate),
//   * aliasing the whole value to another key,
//   * TTL/GC reaping the primary and its secondaries together,
//   * end-to-end recovery proving that a torn final Put loses every key in its group while every
//     completed group survives.
//
// The validation, aliasing, KeyCount, restart and recovery tests run against every disk-table
// implementation in tableBuilders. The cached-write-cache test only makes sense for the cached
// variants, so the test bodies skip the other implementations.

// putBatchSingle is a tiny helper to PutBatch a single PutRequest, which is otherwise verbose.
func putBatchSingle(t *testing.T, table litt.ManagedTable, req *types.PutRequest) {
	t.Helper()
	require.NoError(t, table.PutBatch([]*types.PutRequest{req}))
}

// TestSecondaryKeyReadsBeforeAndAfterFlush proves that a secondary key behaves like any other key
// at every read-side boundary. The same Get/Exists call works pre-flush (served from the
// unflushed data cache) and post-flush (served from the keymap + segment Read).
func TestSecondaryKeyReadsBeforeAndAfterFlush(t *testing.T) {
	t.Parallel()
	for _, tb := range tableBuilders {
		tb := tb
		t.Run(tb.name, func(t *testing.T) {
			t.Parallel()
			rand := util.NewTestRandom()
			directory := t.TempDir()
			tableName := rand.String(8)
			table, err := tb.builder(time.Now, tableName, []string{directory})
			require.NoError(t, err)

			value := []byte("the quick brown fox jumps over the lazy dog")
			primary := []byte("primary")
			sk1 := &types.SecondaryKey{Key: []byte("quick"), Offset: 4, Length: 5}
			sk2 := &types.SecondaryKey{Key: []byte("brown"), Offset: 10, Length: 5}
			sk3 := &types.SecondaryKey{Key: []byte("alias"), Offset: 0, Length: uint32(len(value))}

			require.NoError(t, table.Put(primary, value, sk1, sk2, sk3))

			verify := func(stage string) {
				t.Helper()
				got, ok, err := table.Get(primary)
				require.NoError(t, err, stage)
				require.True(t, ok, stage)
				require.Equal(t, value, got, stage)

				for _, sk := range []*types.SecondaryKey{sk1, sk2, sk3} {
					ok, err := table.Exists(sk.Key)
					require.NoError(t, err, stage)
					require.True(t, ok, stage)

					got, ok, err := table.Get(sk.Key)
					require.NoError(t, err, stage)
					require.True(t, ok, stage)
					require.Equal(t, value[sk.Offset:sk.Offset+sk.Length], got, stage)
				}

				require.EqualValues(t, 4, table.KeyCount(), stage)
			}

			verify("before flush")
			require.NoError(t, table.Flush())
			verify("after flush")

			require.NoError(t, table.Drop())
		})
	}
}

// TestSecondaryKeyValidationErrors verifies that every documented validation rule is enforced and
// that a rejected Put leaves no observable side-effect (KeyCount unchanged).
func TestSecondaryKeyValidationErrors(t *testing.T) {
	t.Parallel()
	for _, tb := range tableBuilders {
		tb := tb
		t.Run(tb.name, func(t *testing.T) {
			t.Parallel()
			rand := util.NewTestRandom()
			directory := t.TempDir()
			tableName := rand.String(8)
			table, err := tb.builder(time.Now, tableName, []string{directory})
			require.NoError(t, err)

			require.Zero(t, table.KeyCount())

			value := []byte("hello world")

			// Offset+Length exceeds the value.
			err = table.Put([]byte("p1"), value, &types.SecondaryKey{Key: []byte("s1"), Offset: 6, Length: 100})
			require.Error(t, err)

			// nil secondary key bytes.
			err = table.Put([]byte("p2"), value, &types.SecondaryKey{Key: nil, Offset: 0, Length: 1})
			require.Error(t, err)

			// secondary key collides with the primary.
			err = table.Put([]byte("p3"), value, &types.SecondaryKey{Key: []byte("p3"), Offset: 0, Length: 1})
			require.Error(t, err)

			// two secondaries collide with each other in the same Put.
			err = table.Put([]byte("p4"), value,
				&types.SecondaryKey{Key: []byte("dup"), Offset: 0, Length: 1},
				&types.SecondaryKey{Key: []byte("dup"), Offset: 1, Length: 1},
			)
			require.Error(t, err)

			// primary key too long.
			oversized := make([]byte, 1<<16)
			err = table.Put(oversized, value)
			require.Error(t, err)

			// secondary key too long.
			err = table.Put([]byte("p5"), value, &types.SecondaryKey{Key: oversized, Offset: 0, Length: 1})
			require.Error(t, err)

			// No successful Put happened, so the table must report zero keys.
			require.Zero(t, table.KeyCount())

			require.NoError(t, table.Drop())
		})
	}
}

// TestSecondaryKeyAliasing covers the alias-the-whole-value pattern: Put(K, V, {A, 0, len(V)}) →
// Get(K) and Get(A) both return V with KeyCount==2.
func TestSecondaryKeyAliasing(t *testing.T) {
	t.Parallel()
	for _, tb := range tableBuilders {
		tb := tb
		t.Run(tb.name, func(t *testing.T) {
			t.Parallel()
			rand := util.NewTestRandom()
			directory := t.TempDir()
			tableName := rand.String(8)
			table, err := tb.builder(time.Now, tableName, []string{directory})
			require.NoError(t, err)

			primary := []byte("primary")
			alias := []byte("alias")
			value := []byte("payload")
			require.NoError(t, table.Put(primary, value,
				&types.SecondaryKey{Key: alias, Offset: 0, Length: uint32(len(value))}))
			require.NoError(t, table.Flush())

			got, ok, err := table.Get(primary)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, value, got)

			got, ok, err = table.Get(alias)
			require.NoError(t, err)
			require.True(t, ok)
			require.Equal(t, value, got)

			require.EqualValues(t, 2, table.KeyCount())

			require.NoError(t, table.Drop())
		})
	}
}

// TestSecondaryKeyTTLGroupExpiration verifies that a primary and all of its secondaries expire
// together: once the TTL window passes for the primary, every secondary becomes unreachable on
// the same GC pass. The buildMemKeyDiskTableSingleShard test config uses TargetSegmentFileSize=100,
// so writing a few hundred bytes of cycler data is enough to rotate past the segment that holds
// our group; once the old segment is sealed and its lastValueTimestamp is older than the TTL, GC
// reaps it.
func TestSecondaryKeyTTLGroupExpiration(t *testing.T) {
	t.Parallel()

	rand := util.NewTestRandom()
	directory := t.TempDir()

	startTime := rand.Time()
	var fakeTime atomic.Pointer[time.Time]
	fakeTime.Store(&startTime)
	clock := func() time.Time { return *fakeTime.Load() }

	tableName := rand.String(8)
	table, err := buildMemKeyDiskTableSingleShard(clock, tableName, []string{directory})
	require.NoError(t, err)

	ttl := 30 * time.Second
	require.NoError(t, table.SetTTL(ttl))

	value := []byte("hello world")
	require.NoError(t, table.Put([]byte("primary"), value,
		&types.SecondaryKey{Key: []byte("hello"), Offset: 0, Length: 5},
		&types.SecondaryKey{Key: []byte("world"), Offset: 6, Length: 5},
	))
	require.NoError(t, table.Flush())
	require.EqualValues(t, 3, table.KeyCount())

	// Write enough additional data to push the group's segment past TargetSegmentFileSize (100)
	// and force a rotation, so the group's segment becomes sealed and therefore reapable.
	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("filler-%03d", i))
		require.NoError(t, table.Put(key, make([]byte, 16)))
	}
	require.NoError(t, table.Flush())

	// Advance the clock past the TTL and trigger GC.
	advanced := startTime.Add(2 * ttl)
	fakeTime.Store(&advanced)

	// One more Put + Flush after the clock advance so the GC pass sees the new lastValueTimestamp
	// on any active segment.
	require.NoError(t, table.Put([]byte("post-advance"), []byte("trigger")))
	require.NoError(t, table.Flush())

	require.NoError(t, table.(*DiskTable).RunGC())

	// Wait for the GC pass to reap the expired segment.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ok, err := table.Exists([]byte("primary"))
		require.NoError(t, err)
		if !ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
		require.NoError(t, table.(*DiskTable).RunGC())
	}

	// Primary and all secondaries are reaped together.
	for _, key := range [][]byte{[]byte("primary"), []byte("hello"), []byte("world")} {
		ok, err := table.Exists(key)
		require.NoError(t, err)
		require.False(t, ok, "expected expired key %q to be gone", key)
	}

	require.NoError(t, table.Drop())
}

// restartWithSecondariesTest exercises the table-restart code path with a workload that mixes
// 0-3 secondaries per Put. After restart every primary AND every secondary must still be
// readable. This is the disk-table-level analogue of the existing TestRestart, and pins down the
// keymap-reload behavior for the new ScopedKey.Kind field.
func restartWithSecondariesTest(t *testing.T, tableBuilder *tableBuilder) {
	rand := util.NewTestRandom()
	directory := t.TempDir()
	tableName := rand.String(8)
	table, err := tableBuilder.builder(time.Now, tableName, []string{directory})
	require.NoError(t, err)

	// keyToValue holds the expected bytes for each surviving key (primary OR secondary).
	keyToValue := make(map[string][]byte)

	iterations := 200
	restartIteration := iterations / 2
	for i := 0; i < iterations; i++ {
		if i == restartIteration {
			require.NoError(t, table.Close())
			table, err = tableBuilder.builder(time.Now, tableName, []string{directory})
			require.NoError(t, err)
			for k, v := range keyToValue {
				got, ok, err := table.Get([]byte(k))
				require.NoError(t, err)
				require.True(t, ok, "key %q lost across restart", k)
				require.Equal(t, v, got)
			}
		}

		primary := rand.PrintableVariableBytes(16, 32)
		value := rand.PrintableVariableBytes(8, 64)

		// 0-3 secondaries; offsets/lengths chosen to span both strict sub-ranges and the whole
		// value.
		nSecondaries := int(rand.Int32Range(0, 4))
		secondaries := make([]*types.SecondaryKey, 0, nSecondaries)
		for s := 0; s < nSecondaries; s++ {
			offset := uint32(rand.Int32Range(0, int32(len(value))))
			maxLen := uint32(len(value)) - offset
			if maxLen == 0 {
				continue
			}
			length := uint32(rand.Int32Range(1, int32(maxLen+1)))
			skKey := rand.PrintableVariableBytes(16, 32)
			if _, exists := keyToValue[string(skKey)]; exists {
				continue
			}
			secondaries = append(secondaries, &types.SecondaryKey{Key: skKey, Offset: offset, Length: length})
		}

		require.NoError(t, table.Put(primary, value, secondaries...))
		keyToValue[string(primary)] = value
		for _, sk := range secondaries {
			keyToValue[string(sk.Key)] = value[sk.Offset : sk.Offset+sk.Length]
		}

		if rand.BoolWithProbability(0.1) {
			require.NoError(t, table.Flush())
		}
	}

	require.NoError(t, table.Flush())
	for k, v := range keyToValue {
		got, ok, err := table.Get([]byte(k))
		require.NoError(t, err)
		require.True(t, ok, "key %q missing at end of test", k)
		require.Equal(t, v, got)
	}

	require.NoError(t, table.Drop())
}

func TestRestartWithSecondaries(t *testing.T) {
	t.Parallel()
	for _, tb := range tableBuilders {
		tb := tb
		t.Run(tb.name, func(t *testing.T) {
			t.Parallel()
			restartWithSecondariesTest(t, tb)
		})
	}
}

// TestGroupAtomicRecoveryEndToEnd is the high-level analogue of the segment-level
// TestSealLoadedSegmentGroupAtomicity: a torn final Put loses every key in its group while every
// completed Put survives. We drive this through DiskTable's public API to make sure the group
// atomicity invariant survives the keymap reload that happens at startup.
//
// The test runs only against MemKeyDiskTableSingleShard, where we know the segment layout exactly
// so we can corrupt it deterministically. The recovery contract is the same for the other disk
// table flavors (they share the same segment package) but corrupting a multi-shard layout would
// require figuring out which shard hosted the torn write.
func TestGroupAtomicRecoveryEndToEnd(t *testing.T) {
	t.Parallel()

	rand := util.NewTestRandom()
	directory := t.TempDir()
	tableName := rand.String(8)

	table, err := buildOneShardMemKeyDiskTable(time.Now, tableName, []string{directory})
	require.NoError(t, err)

	// Two completed Puts, then a third Put whose value we will truncate. We Flush after each to
	// move the writes through the value file's flushedSize boundary.
	require.NoError(t, table.Put([]byte("survivor-1"), []byte("v1")))
	require.NoError(t, table.Flush())

	require.NoError(t, table.Put([]byte("survivor-primary"), []byte("hello"),
		&types.SecondaryKey{Key: []byte("survivor-secondary"), Offset: 0, Length: 5},
	))
	require.NoError(t, table.Flush())

	require.NoError(t, table.Put([]byte("torn-primary"), []byte("worldwide"),
		&types.SecondaryKey{Key: []byte("torn-secondary"), Offset: 0, Length: 5},
	))
	require.NoError(t, table.Flush())

	require.NoError(t, table.Close())

	// Find the segment that holds the torn write: it's the highest-indexed segment whose value
	// file is non-empty. (Disk table may rotate to a fresh segment after each flush, so the very
	// latest metadata may belong to an empty rollover segment.)
	segmentDir := findLatestSegmentDir(t, directory, tableName)
	require.NotEmpty(t, segmentDir)
	segIdx := segmentIndexWithLargestValueFile(t, segmentDir)

	// Truncate the value file so the primary's tail goes missing. The secondary at the front
	// would individually fit, but group-atomic recovery drops it anyway.
	valPath := path.Join(segmentDir, fmt.Sprintf("%d-0%s", segIdx, segment.ValuesFileExtension))
	data, err := os.ReadFile(valPath)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(data), 3)
	require.NoError(t, os.WriteFile(valPath, data[:len(data)-3], 0600))

	// Flip the metadata's sealed byte from 1 back to 0 to simulate a crash before sealing. This
	// is what makes LoadSegment run the recovery path on reopen.
	metaPath := path.Join(segmentDir, fmt.Sprintf("%d%s", segIdx, segment.MetadataFileExtension))
	mdBytes, err := os.ReadFile(metaPath)
	require.NoError(t, err)
	require.Equal(t, segment.V3MetadataSize, len(mdBytes))
	mdBytes[segment.V3MetadataSize-1] = 0
	require.NoError(t, os.WriteFile(metaPath, mdBytes, 0600))

	// Reopen.
	table, err = buildOneShardMemKeyDiskTable(time.Now, tableName, []string{directory})
	require.NoError(t, err)

	// Survivors remain.
	for _, key := range [][]byte{
		[]byte("survivor-1"),
		[]byte("survivor-primary"),
		[]byte("survivor-secondary"),
	} {
		ok, err := table.Exists(key)
		require.NoError(t, err)
		require.True(t, ok, "expected %q to survive recovery", key)
	}

	// Torn group is gone, both primary and secondary.
	for _, key := range [][]byte{[]byte("torn-primary"), []byte("torn-secondary")} {
		ok, err := table.Exists(key)
		require.NoError(t, err)
		require.False(t, ok, "expected %q to be discarded by recovery", key)
	}

	require.NoError(t, table.Drop())
}

// findLatestSegmentDir locates the segment directory created by the single-shard mem-keymap disk
// table at the given root. The directory layout is
// <root>/<tableName>/segments/, with each segment occupying a triple of files prefixed by its
// segment index. We return the segments directory itself; the test then walks its files to find
// the highest-indexed segment.
func findLatestSegmentDir(t *testing.T, root, tableName string) string {
	t.Helper()
	segmentsDir := filepath.Join(root, tableName, "segments")
	info, err := os.Stat(segmentsDir)
	require.NoError(t, err)
	require.True(t, info.IsDir())
	return segmentsDir
}

// segmentIndexWithLargestValueFile walks the segments directory and returns the index of the
// segment whose value file is the largest. The torn write we corrupt in the recovery test always
// lives in the segment with the most value bytes (the one we wrote into most recently); ignoring
// rollover-only segments (which may exist after a Close) makes the test robust to whatever the
// disk table happens to do at shutdown.
func segmentIndexWithLargestValueFile(t *testing.T, segmentsDir string) uint32 {
	t.Helper()
	entries, err := os.ReadDir(segmentsDir)
	require.NoError(t, err)

	var bestIdx uint32
	var bestSize int64 = -1
	for _, e := range entries {
		name := e.Name()
		const suffix = segment.ValuesFileExtension
		if len(name) < len(suffix) || name[len(name)-len(suffix):] != suffix {
			continue
		}
		// value files are named "<index>-<shard>.values"; we always corrupt shard 0 so we only
		// consider files whose shard portion is "0".
		stripped := name[:len(name)-len(suffix)]
		dash := -1
		for i := len(stripped) - 1; i >= 0; i-- {
			if stripped[i] == '-' {
				dash = i
				break
			}
		}
		require.GreaterOrEqual(t, dash, 0)
		if stripped[dash+1:] != "0" {
			continue
		}
		idx, err := parseUint32(stripped[:dash])
		require.NoError(t, err)

		info, err := e.Info()
		require.NoError(t, err)
		if info.Size() > bestSize {
			bestSize = info.Size()
			bestIdx = idx
		}
	}
	require.GreaterOrEqual(t, bestSize, int64(0), "no value files found in %s", segmentsDir)
	return bestIdx
}

func parseUint32(s string) (uint32, error) {
	var n uint32
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a number: %q", s)
		}
		n = n*10 + uint32(r-'0')
	}
	return n, nil
}
