package disktable

import (
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
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

// iterEntry is a single record observed while walking an iterator.
type iterEntry struct {
	key       string
	isPrimary bool
	value     string
}

// shardConfig parameterizes iterator tests across sharding factors so that value reads, which must
// route to the correct shard, are exercised both single- and multi-shard.
type shardConfig struct {
	name           string
	shardingFactor uint8
}

var iterShardConfigs = []shardConfig{
	{name: "SingleShard", shardingFactor: 1},
	{name: "MultiShard", shardingFactor: 4},
}

// buildIterTable builds a mem-keymap disk table for iterator tests. Like buildGCFilterTable, it seals a
// segment after exactly maxSegmentKeyCount keys (size-based sealing disabled) so segment boundaries are
// deterministic, and it disables background GC (GCPeriod is huge) so GC only runs via explicit RunGC.
func buildIterTable(
	t *testing.T,
	clock func() time.Time,
	name string,
	path string,
	maxSegmentKeyCount uint32,
	shardingFactor uint8,
) litt.ManagedTable {
	t.Helper()

	logger := slog.Default()

	keymapPath := filepath.Join(path, keymap.KeymapDirectoryName)
	keymapTypeFile, err := setupKeymapTypeFile(keymapPath, keymap.MemKeymapType)
	require.NoError(t, err)

	keys, _, err := keymap.NewMemKeymap(logger, "", true)
	require.NoError(t, err)

	config, err := litt.DefaultConfig(path)
	require.NoError(t, err)

	config.TargetSegmentFileSize = math.MaxUint32
	config.MaxSegmentKeyCount = maxSegmentKeyCount
	config.GCPeriod = time.Hour
	config.Fsync = false

	tableConfig := litt.DefaultTableConfig(name)
	tableConfig.ShardingFactor = shardingFactor

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
		[]string{path},
		true,
		nil)
	require.NoError(t, err)

	return table
}

// drainIterator walks the iterator to completion, collecting every record. It also reads the value of
// every record so that value-read correctness is exercised on a full forward scan.
func drainIterator(t *testing.T, it litt.Iterator) []iterEntry {
	t.Helper()
	var entries []iterEntry
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
		key, isPrimary := it.GetKey()
		value, err := it.GetValue()
		require.NoError(t, err)
		entries = append(entries, iterEntry{
			key:       string(key),
			isPrimary: isPrimary,
			value:     string(value),
		})
	}
	return entries
}

func entryKeys(entries []iterEntry) []string {
	keys := make([]string, len(entries))
	for i, e := range entries {
		keys[i] = e.key
	}
	return keys
}

// makeLargeValue builds a deterministic byte slice of the given size with a per-seed pattern, used to
// exercise values larger than the value reader's buffer.
func makeLargeValue(seed byte, size int) []byte {
	v := make([]byte, size)
	for i := range v {
		v[i] = byte(int(seed)*7 + i)
	}
	return v
}

// TestIteratorForwardOrder verifies that a forward iterator yields all keys in insertion order, with
// correct values and isPrimary flags.
func TestIteratorForwardOrder(t *testing.T) {
	t.Parallel()

	for _, sc := range iterShardConfigs {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()

			table := buildIterTable(t, time.Now, "fwd", directory, 2, sc.shardingFactor)
			defer func() { require.NoError(t, table.Close()) }()

			keyOrder := []string{"k0", "k1", "k2", "k3", "k4"}
			for _, k := range keyOrder {
				require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
			}

			it, err := table.Iterator(false)
			require.NoError(t, err)
			defer func() { require.NoError(t, it.Close()) }()

			entries := drainIterator(t, it)
			require.Equal(t, keyOrder, entryKeys(entries))
			for _, e := range entries {
				require.True(t, e.isPrimary)
				require.Equal(t, "value-"+e.key, e.value)
			}
		})
	}
}

// TestIteratorReverseOrder verifies that a reverse iterator yields all keys in reverse insertion order.
func TestIteratorReverseOrder(t *testing.T) {
	t.Parallel()

	for _, sc := range iterShardConfigs {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()

			table := buildIterTable(t, time.Now, "rev", directory, 2, sc.shardingFactor)
			defer func() { require.NoError(t, table.Close()) }()

			keyOrder := []string{"k0", "k1", "k2", "k3", "k4"}
			for _, k := range keyOrder {
				require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
			}

			it, err := table.Iterator(true)
			require.NoError(t, err)
			defer func() { require.NoError(t, it.Close()) }()

			entries := drainIterator(t, it)
			require.Equal(t, []string{"k4", "k3", "k2", "k1", "k0"}, entryKeys(entries))
			for _, e := range entries {
				require.True(t, e.isPrimary)
				require.Equal(t, "value-"+e.key, e.value)
			}
		})
	}
}

// TestIteratorSecondaryKeys verifies that secondary keys appear contiguously right after their primary
// in forward iteration, are flagged isPrimary=false, and that GetValue returns the correct sub-slice of
// the primary's value.
func TestIteratorSecondaryKeys(t *testing.T) {
	t.Parallel()

	for _, sc := range iterShardConfigs {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()

			table := buildIterTable(t, time.Now, "sec", directory, 64, sc.shardingFactor)
			defer func() { require.NoError(t, table.Close()) }()

			// Primary "p0" with value "abcdef" and two secondaries aliasing sub-ranges.
			require.NoError(t, table.Put(
				[]byte("p0"),
				[]byte("abcdef"),
				&types.SecondaryKey{Key: []byte("s0a"), Offset: 1, Length: 3}, // "bcd"
				&types.SecondaryKey{Key: []byte("s0b"), Offset: 0, Length: 6}, // "abcdef"
			))
			// A standalone primary.
			require.NoError(t, table.Put([]byte("p1"), []byte("xyz")))

			it, err := table.Iterator(false)
			require.NoError(t, err)
			defer func() { require.NoError(t, it.Close()) }()

			entries := drainIterator(t, it)

			expected := []iterEntry{
				{key: "p0", isPrimary: true, value: "abcdef"},
				{key: "s0a", isPrimary: false, value: "bcd"},
				{key: "s0b", isPrimary: false, value: "abcdef"},
				{key: "p1", isPrimary: true, value: "xyz"},
			}
			require.Equal(t, expected, entries)
		})
	}
}

// TestIteratorExcludesConcurrentWrites verifies that the iterator captures a snapshot at creation time:
// keys written after the iterator is created are not observed.
func TestIteratorExcludesConcurrentWrites(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	table := buildIterTable(t, time.Now, "snapshot", directory, 2, 1)
	defer func() { require.NoError(t, table.Close()) }()

	for _, k := range []string{"k0", "k1", "k2"} {
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}

	it, err := table.Iterator(false)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	// These writes happen after the iterator is created and must not be observed.
	for _, k := range []string{"k3", "k4"} {
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}

	entries := drainIterator(t, it)
	require.Equal(t, []string{"k0", "k1", "k2"}, entryKeys(entries))
}

// TestIteratorEmptyTable verifies that iterating an empty table immediately completes.
func TestIteratorEmptyTable(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	table := buildIterTable(t, time.Now, "empty", directory, 2, 1)
	defer func() { require.NoError(t, table.Close()) }()

	it, err := table.Iterator(false)
	require.NoError(t, err)
	defer func() { require.NoError(t, it.Close()) }()

	ok, err := it.Next()
	require.NoError(t, err)
	require.False(t, ok)
}

// TestGetOldestNewestKeyEmpty verifies that oldest/newest report exists=false on an empty table.
func TestGetOldestNewestKeyEmpty(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	table := buildIterTable(t, time.Now, "boundsempty", directory, 2, 1)
	defer func() { require.NoError(t, table.Close()) }()

	_, exists, err := table.GetOldestKey()
	require.NoError(t, err)
	require.False(t, exists)

	_, exists, err = table.GetNewestKey()
	require.NoError(t, err)
	require.False(t, exists)
}

// TestGetOldestNewestKey verifies that oldest/newest report the first/last primary key. The final Put
// includes a secondary key, confirming that GetNewestKey returns the primary, not the trailing secondary.
func TestGetOldestNewestKey(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	table := buildIterTable(t, time.Now, "bounds", directory, 2, 1)
	defer func() { require.NoError(t, table.Close()) }()

	require.NoError(t, table.Put([]byte("p0"), []byte("v0")))
	require.NoError(t, table.Put([]byte("p1"), []byte("v1")))
	require.NoError(t, table.Put(
		[]byte("plast"),
		[]byte("abc"),
		&types.SecondaryKey{Key: []byte("slast"), Offset: 0, Length: 3},
	))

	oldest, exists, err := table.GetOldestKey()
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "p0", string(oldest))

	newest, exists, err := table.GetNewestKey()
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "plast", string(newest))
}

// TestGetNewestKeyAfterReopen verifies that GetNewestKey returns the correct key after the table is
// closed and reopened over existing on-disk data, before any new write has occurred. On reopen, keyCount
// is reconstructed from the segments but the in-memory newest-key tracking starts empty, so the newest
// key must be recovered from the segments rather than reported as a populated-but-nil value.
func TestGetNewestKeyAfterReopen(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	table := buildIterTable(t, time.Now, "reopen", directory, 2, 1)

	require.NoError(t, table.Put([]byte("p0"), []byte("v0")))
	require.NoError(t, table.Put([]byte("p1"), []byte("v1")))
	require.NoError(t, table.Put(
		[]byte("plast"),
		[]byte("abc"),
		&types.SecondaryKey{Key: []byte("slast"), Offset: 0, Length: 3},
	))
	require.NoError(t, table.Flush())
	require.NoError(t, table.Close())

	// Reopen over the same directory. keyCount is reconstructed from the on-disk segments, but no write
	// has occurred this session, so the in-memory newest-key tracking is empty.
	table = buildIterTable(t, time.Now, "reopen", directory, 2, 1)
	defer func() { require.NoError(t, table.Close()) }()

	oldest, exists, err := table.GetOldestKey()
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "p0", string(oldest))

	newest, exists, err := table.GetNewestKey()
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "plast", string(newest))
}

// TestGetOldestKeyAdvancesAfterGC verifies that the oldest key advances once GC deletes the segment(s)
// that contained the previous oldest key.
func TestGetOldestKeyAdvancesAfterGC(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	start := time.Unix(1_700_000_000, 0)
	var fakeTime atomic.Pointer[time.Time]
	fakeTime.Store(&start)
	clock := func() time.Time { return *fakeTime.Load() }

	// Seal after every 2 keys: seg0[k0,k1], seg1[k2,k3], seg2(mutable)[k4].
	table := buildGCFilterTable(t, clock, "oldestgc", directory, 2, nil)
	defer func() { require.NoError(t, table.Close()) }()

	require.NoError(t, table.SetTTL(10*time.Second))
	for _, k := range []string{"k0", "k1", "k2", "k3", "k4"} {
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}
	require.NoError(t, table.Flush())

	oldest, exists, err := table.GetOldestKey()
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "k0", string(oldest))

	// Expire and collect the sealed segments (seg0, seg1). The mutable segment (k4) survives.
	expired := start.Add(time.Hour)
	fakeTime.Store(&expired)
	require.NoError(t, table.RunGC())

	requireKeyAbsent(t, table, "k0")
	requireKeyPresent(t, table, "k4")

	oldest, exists, err = table.GetOldestKey()
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "k4", string(oldest))
}

// countSegmentsOnDisk returns the number of segments with files present under dir, counted by their metadata
// files (exactly one per segment). Snapshotting is disabled in these tests, so every .metadata file is a live
// segment's.
func countSegmentsOnDisk(t *testing.T, dir string) int {
	t.Helper()
	count := 0
	require.NoError(t, filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // files may be deleted out from under the walk
		}
		if !info.IsDir() && strings.HasSuffix(path, segment.MetadataFileExtension) {
			count++
		}
		return nil
	}))
	return count
}

// TestIteratorRetainsSnapshotDespiteGC verifies that an open iterator does NOT pause garbage collection: while
// the iterator is open, GC collects its snapshot segments (so the keys vanish from Get), yet the iterator still
// yields its complete snapshot because it holds a reservation on each segment, keeping the files on disk until
// it closes. Once it closes, those reservations drop and the collected segments' files are reclaimed.
func TestIteratorRetainsSnapshotDespiteGC(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	start := time.Unix(1_700_000_000, 0)
	var fakeTime atomic.Pointer[time.Time]
	fakeTime.Store(&start)
	clock := func() time.Time { return *fakeTime.Load() }

	table := buildGCFilterTable(t, clock, "gciter", directory, 2, nil)
	defer func() { require.NoError(t, table.Close()) }()

	require.NoError(t, table.SetTTL(10*time.Second))
	keyOrder := []string{"k0", "k1", "k2", "k3", "k4"}
	for _, k := range keyOrder {
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}
	require.NoError(t, table.Flush())

	// Opening the iterator seals the previously-mutable segment, so all keys now live in its snapshot of
	// sealed segments.
	it, err := table.Iterator(false)
	require.NoError(t, err)

	// Expire everything, then run GC. GC is NOT paused by the open iterator: the snapshot segments are
	// collected and the keys disappear from Get.
	expired := start.Add(time.Hour)
	fakeTime.Store(&expired)
	require.NoError(t, table.RunGC())
	for _, k := range keyOrder {
		requireKeyAbsent(t, table, k)
	}

	// The open iterator nonetheless still observes its full snapshot: its reservations kept the files alive.
	require.Equal(t, keyOrder, entryKeys(drainIterator(t, it)))

	// Closing the iterator releases the reservations; the collected segments' files are then reclaimed,
	// leaving only the (empty) mutable segment on disk.
	require.NoError(t, it.Close())
	util.AssertEventuallyTrue(t, func() bool {
		return countSegmentsOnDisk(t, directory) == 1
	}, 5*time.Second, "collected segment files should be reclaimed after the iterator closes")
}

// TestConcurrentIteratorsRetainSnapshots verifies that segments collected while iterators are open are retained
// until the LAST iterator referencing them closes: each iterator's reservation independently keeps the files
// alive, so closing one iterator does not reclaim segments the other still holds.
func TestConcurrentIteratorsRetainSnapshots(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	start := time.Unix(1_700_000_000, 0)
	var fakeTime atomic.Pointer[time.Time]
	fakeTime.Store(&start)
	clock := func() time.Time { return *fakeTime.Load() }

	table := buildGCFilterTable(t, clock, "gcitermulti", directory, 2, nil)
	defer func() { require.NoError(t, table.Close()) }()

	require.NoError(t, table.SetTTL(10*time.Second))
	keyOrder := []string{"k0", "k1", "k2", "k3", "k4"}
	for _, k := range keyOrder {
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}
	require.NoError(t, table.Flush())

	// Two iterators capture the same snapshot; each reserves its segments. The first open seals the mutable
	// segment, so both snapshots cover the same sealed segments.
	it1, err := table.Iterator(false)
	require.NoError(t, err)
	it2, err := table.Iterator(false)
	require.NoError(t, err)
	segmentsBefore := countSegmentsOnDisk(t, directory)

	// Collect everything. The keys disappear from Get, but both iterators still observe the full snapshot.
	expired := start.Add(time.Hour)
	fakeTime.Store(&expired)
	require.NoError(t, table.RunGC())
	for _, k := range keyOrder {
		requireKeyAbsent(t, table, k)
	}
	require.Equal(t, keyOrder, entryKeys(drainIterator(t, it1)))
	require.Equal(t, keyOrder, entryKeys(drainIterator(t, it2)))

	// Closing the first iterator must not reclaim anything: it2 still reserves every segment.
	require.NoError(t, it1.Close())
	require.Equal(t, segmentsBefore, countSegmentsOnDisk(t, directory))

	// Closing the last iterator drops the final reservations; the collected segments are reclaimed.
	require.NoError(t, it2.Close())
	util.AssertEventuallyTrue(t, func() bool {
		return countSegmentsOnDisk(t, directory) == 1
	}, 5*time.Second, "collected segment files should be reclaimed after the last iterator closes")
}

// TestIteratorSkipValues verifies value correctness when GetValue is called for only a subset of keys,
// which forces the buffered value reader to seek (reposition) rather than read purely sequentially.
// Values are intentionally variable-length so the skipped ranges are non-uniform.
func TestIteratorSkipValues(t *testing.T) {
	t.Parallel()

	for _, sc := range iterShardConfigs {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()

			table := buildIterTable(t, time.Now, "skip", directory, 3, sc.shardingFactor)
			defer func() { require.NoError(t, table.Close()) }()

			const count = 30
			values := make(map[string]string)
			for i := 0; i < count; i++ {
				key := fmt.Sprintf("key-%03d", i)
				value := fmt.Sprintf("val-%03d-%s", i, strings.Repeat("x", i%7))
				require.NoError(t, table.Put([]byte(key), []byte(value)))
				values[key] = value
			}

			it, err := table.Iterator(false)
			require.NoError(t, err)
			defer func() { require.NoError(t, it.Close()) }()

			seen := 0
			for {
				ok, err := it.Next()
				require.NoError(t, err)
				if !ok {
					break
				}
				key, _ := it.GetKey()
				// Read the value for only every third key, forcing the reader to skip the others.
				if seen%3 == 0 {
					value, err := it.GetValue()
					require.NoError(t, err)
					require.Equal(t, values[string(key)], string(value))
				}
				seen++
			}
			require.Equal(t, count, seen)
		})
	}
}

// TestIteratorValuesAcrossSegments verifies value correctness when keys span many segments (and, in the
// multi-shard case, many shard value files).
func TestIteratorValuesAcrossSegments(t *testing.T) {
	t.Parallel()

	for _, sc := range iterShardConfigs {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()

			table := buildIterTable(t, time.Now, "spanseg", directory, 3, sc.shardingFactor)
			defer func() { require.NoError(t, table.Close()) }()

			const count = 50
			expected := make([]string, count)
			for i := 0; i < count; i++ {
				key := fmt.Sprintf("key-%03d", i)
				value := fmt.Sprintf("value-for-key-%03d", i)
				require.NoError(t, table.Put([]byte(key), []byte(value)))
				expected[i] = key
			}

			it, err := table.Iterator(false)
			require.NoError(t, err)
			defer func() { require.NoError(t, it.Close()) }()

			entries := drainIterator(t, it)
			require.Equal(t, expected, entryKeys(entries))
			for i, e := range entries {
				require.Equal(t, fmt.Sprintf("value-for-key-%03d", i), e.value)
			}
		})
	}
}

// TestIteratorReverseSecondaryKeys verifies reverse iteration over a group containing secondary keys:
// records appear in reverse insertion order, isPrimary flags are correct, and secondary values (which in
// reverse are read directly, since the forward sub-slice optimization does not apply) are correct.
func TestIteratorReverseSecondaryKeys(t *testing.T) {
	t.Parallel()

	for _, sc := range iterShardConfigs {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()

			table := buildIterTable(t, time.Now, "revsec", directory, 64, sc.shardingFactor)
			defer func() { require.NoError(t, table.Close()) }()

			require.NoError(t, table.Put(
				[]byte("p0"),
				[]byte("abcdef"),
				&types.SecondaryKey{Key: []byte("s0a"), Offset: 1, Length: 3}, // "bcd"
				&types.SecondaryKey{Key: []byte("s0b"), Offset: 0, Length: 6}, // "abcdef"
			))
			require.NoError(t, table.Put([]byte("p1"), []byte("xyz")))

			it, err := table.Iterator(true)
			require.NoError(t, err)
			defer func() { require.NoError(t, it.Close()) }()

			// Insertion order of records is [p0, s0a, s0b, p1]; reverse yields [p1, s0b, s0a, p0].
			expected := []iterEntry{
				{key: "p1", isPrimary: true, value: "xyz"},
				{key: "s0b", isPrimary: false, value: "abcdef"},
				{key: "s0a", isPrimary: false, value: "bcd"},
				{key: "p0", isPrimary: true, value: "abcdef"},
			}
			require.Equal(t, expected, drainIterator(t, it))
		})
	}
}

// TestIteratorReverseValuesAcrossSegments verifies reverse value correctness when keys span many
// segments (and, multi-shard, many shard value files) — the reverse direct-read path at scale.
func TestIteratorReverseValuesAcrossSegments(t *testing.T) {
	t.Parallel()

	for _, sc := range iterShardConfigs {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()

			table := buildIterTable(t, time.Now, "revspan", directory, 3, sc.shardingFactor)
			defer func() { require.NoError(t, table.Close()) }()

			const count = 50
			forward := make([]string, count)
			for i := 0; i < count; i++ {
				key := fmt.Sprintf("key-%03d", i)
				require.NoError(t, table.Put([]byte(key), []byte("value-for-"+key)))
				forward[i] = key
			}

			it, err := table.Iterator(true)
			require.NoError(t, err)
			defer func() { require.NoError(t, it.Close()) }()

			entries := drainIterator(t, it)
			require.Len(t, entries, count)
			for i, e := range entries {
				expectedKey := forward[count-1-i]
				require.Equal(t, expectedKey, e.key)
				require.True(t, e.isPrimary)
				require.Equal(t, "value-for-"+expectedKey, e.value)
			}
		})
	}
}

// TestIteratorLargeValues verifies that values larger than the value reader's buffer are read correctly
// in a forward scan (exercising the buffer-bypass path), single- and multi-shard.
func TestIteratorLargeValues(t *testing.T) {
	t.Parallel()

	for _, sc := range iterShardConfigs {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()

			table := buildIterTable(t, time.Now, "large", directory, 2, sc.shardingFactor)
			defer func() { require.NoError(t, table.Close()) }()

			const count = 5
			keys := make([]string, count)
			expected := make(map[string][]byte)
			for i := 0; i < count; i++ {
				key := fmt.Sprintf("key-%02d", i)
				// Each value exceeds the 64 KiB reader buffer, with a per-key length and pattern.
				value := makeLargeValue(byte(i+1), 70*1024+i*1024)
				require.NoError(t, table.Put([]byte(key), value))
				keys[i] = key
				expected[key] = value
			}

			it, err := table.Iterator(false)
			require.NoError(t, err)
			defer func() { require.NoError(t, it.Close()) }()

			var order []string
			for {
				ok, err := it.Next()
				require.NoError(t, err)
				if !ok {
					break
				}
				key, _ := it.GetKey()
				value, err := it.GetValue()
				require.NoError(t, err)
				require.Equal(t, expected[string(key)], value)
				order = append(order, string(key))
			}
			require.Equal(t, keys, order)
		})
	}
}

// TestIteratorEmptyValues verifies that zero-length values are iterated and read correctly, interleaved
// with non-empty values.
func TestIteratorEmptyValues(t *testing.T) {
	t.Parallel()

	for _, sc := range iterShardConfigs {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			t.Parallel()
			directory := t.TempDir()

			table := buildIterTable(t, time.Now, "emptyval", directory, 8, sc.shardingFactor)
			defer func() { require.NoError(t, table.Close()) }()

			require.NoError(t, table.Put([]byte("e0"), []byte{}))
			require.NoError(t, table.Put([]byte("v0"), []byte("nonempty")))
			require.NoError(t, table.Put([]byte("e1"), []byte{}))

			it, err := table.Iterator(false)
			require.NoError(t, err)
			defer func() { require.NoError(t, it.Close()) }()

			expected := []iterEntry{
				{key: "e0", isPrimary: true, value: ""},
				{key: "v0", isPrimary: true, value: "nonempty"},
				{key: "e1", isPrimary: true, value: ""},
			}
			require.Equal(t, expected, drainIterator(t, it))
		})
	}
}

// TestIteratorLifecycle verifies repeated GetValue on the same key, Next after exhaustion, and double
// Close.
func TestIteratorLifecycle(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	table := buildIterTable(t, time.Now, "lifecycle", directory, 2, 1)
	defer func() { require.NoError(t, table.Close()) }()

	for _, k := range []string{"k0", "k1", "k2"} {
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}

	it, err := table.Iterator(false)
	require.NoError(t, err)

	// First key: repeated GetValue must return the same value.
	ok, err := it.Next()
	require.NoError(t, err)
	require.True(t, ok)
	v1, err := it.GetValue()
	require.NoError(t, err)
	v2, err := it.GetValue()
	require.NoError(t, err)
	require.Equal(t, "value-k0", string(v1))
	require.Equal(t, v1, v2)

	// Drain the rest.
	for {
		ok, err := it.Next()
		require.NoError(t, err)
		if !ok {
			break
		}
	}

	// Next after exhaustion stays false with no error.
	ok, err = it.Next()
	require.NoError(t, err)
	require.False(t, ok)

	// Close is idempotent.
	require.NoError(t, it.Close())
	require.NoError(t, it.Close())
}

// TestReadsAndWritesDuringOpenIterator verifies that ordinary reads and writes work while an iterator is
// open, that the iterator's snapshot excludes post-open writes, and that a subsequently created iterator
// observes those writes.
func TestReadsAndWritesDuringOpenIterator(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	table := buildIterTable(t, time.Now, "concurrentrw", directory, 2, 1)
	defer func() { require.NoError(t, table.Close()) }()

	for _, k := range []string{"k0", "k1", "k2"} {
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}

	it, err := table.Iterator(false)
	require.NoError(t, err)

	// Writes succeed while an iterator is open...
	for _, k := range []string{"k3", "k4"} {
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}
	// ...and are immediately readable.
	for _, k := range []string{"k0", "k1", "k2", "k3", "k4"} {
		v, ok, err := table.Get([]byte(k))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, "value-"+k, string(v))
	}

	// The open iterator's snapshot excludes the post-open writes.
	require.Equal(t, []string{"k0", "k1", "k2"}, entryKeys(drainIterator(t, it)))
	require.NoError(t, it.Close())

	// A new iterator observes everything.
	it2, err := table.Iterator(false)
	require.NoError(t, err)
	defer func() { require.NoError(t, it2.Close()) }()
	require.Equal(t, []string{"k0", "k1", "k2", "k3", "k4"}, entryKeys(drainIterator(t, it2)))
}

// TestGetNewestKeyAfterPartialGC verifies that after GC deletes some but not all segments, the oldest
// key advances while the newest key (in the most-recent, not-yet-collected segment) remains correct.
func TestGetNewestKeyAfterPartialGC(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	start := time.Unix(1_700_000_000, 0)
	var fakeTime atomic.Pointer[time.Time]
	fakeTime.Store(&start)
	clock := func() time.Time { return *fakeTime.Load() }

	// Seal after every 2 keys. We advance the clock between segments so they have staggered seal times.
	table := buildGCFilterTable(t, clock, "partialgc", directory, 2, nil)
	defer func() { require.NoError(t, table.Close()) }()

	require.NoError(t, table.SetTTL(150*time.Second))

	// Writes are processed asynchronously by the control loop, so we Flush after each group (a barrier
	// that drains the pending writes) before advancing the clock. This pins each segment's seal time to
	// the intended clock value.

	// seg0 [k0,k1] sealed at t0.
	require.NoError(t, table.Put([]byte("k0"), []byte("v0")))
	require.NoError(t, table.Put([]byte("k1"), []byte("v1")))
	require.NoError(t, table.Flush())

	// seg1 [k2,k3] sealed at t0+100s.
	t100 := start.Add(100 * time.Second)
	fakeTime.Store(&t100)
	require.NoError(t, table.Put([]byte("k2"), []byte("v2")))
	require.NoError(t, table.Put([]byte("k3"), []byte("v3")))
	require.NoError(t, table.Flush())

	// seg2 [k4,k5] sealed at t0+200s; seg3 (mutable) holds k6.
	t200 := start.Add(200 * time.Second)
	fakeTime.Store(&t200)
	require.NoError(t, table.Put([]byte("k4"), []byte("v4")))
	require.NoError(t, table.Put([]byte("k5"), []byte("v5")))
	require.NoError(t, table.Put([]byte("k6"), []byte("v6")))
	require.NoError(t, table.Flush())

	// At t0+220s with TTL=150s, only seg0 (age 220) is past TTL; seg1 (age 120) is not, so GC stops
	// after deleting seg0.
	t220 := start.Add(220 * time.Second)
	fakeTime.Store(&t220)
	require.NoError(t, table.RunGC())

	requireKeyAbsent(t, table, "k0")
	requireKeyAbsent(t, table, "k1")
	requireKeyPresent(t, table, "k2")
	requireKeyPresent(t, table, "k6")

	// Oldest advanced to the first key of the now-lowest segment; newest is unchanged.
	oldest, exists, err := table.GetOldestKey()
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "k2", string(oldest))

	newest, exists, err := table.GetNewestKey()
	require.NoError(t, err)
	require.True(t, exists)
	require.Equal(t, "k6", string(newest))
}
