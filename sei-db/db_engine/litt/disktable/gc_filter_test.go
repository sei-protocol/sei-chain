package disktable

import (
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/segment"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/stretchr/testify/require"
)

// recordedGCCall captures a single invocation of a GCFilter, so that tests can assert both which keys
// were examined and the isPrimaryKey flag that was passed.
type recordedGCCall struct {
	key       string
	isPrimary bool
}

// gcFilterRecorder is a test helper that implements a litt.GCFilter. It records every invocation and
// allows individual keys to be "blocked" (filter returns false) or "unblocked" at will. All access is
// mutex-guarded because the filter is invoked from the control-loop goroutine while the test goroutine
// mutates the blocked set / drains recorded calls between (synchronous) RunGC invocations.
type gcFilterRecorder struct {
	mu      sync.Mutex
	blocked map[string]bool
	calls   []recordedGCCall
}

func newGCFilterRecorder() *gcFilterRecorder {
	return &gcFilterRecorder{blocked: make(map[string]bool)}
}

func (r *gcFilterRecorder) filter(key []byte, isPrimaryKey bool) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, recordedGCCall{key: string(key), isPrimary: isPrimaryKey})
	return !r.blocked[string(key)], nil
}

func (r *gcFilterRecorder) block(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blocked[key] = true
}

func (r *gcFilterRecorder) unblock(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.blocked, key)
}

// takeCalls returns the calls recorded since the last takeCalls (or since creation) and resets the buffer.
func (r *gcFilterRecorder) takeCalls() []recordedGCCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	calls := r.calls
	r.calls = nil
	return calls
}

// takeCallKeys is like takeCalls but returns just the key strings, in order.
func (r *gcFilterRecorder) takeCallKeys() []string {
	calls := r.takeCalls()
	keys := make([]string, len(calls))
	for i, c := range calls {
		keys[i] = c.key
	}
	return keys
}

// buildGCFilterTable builds a single-shard mem-keymap disk table wired with the supplied GCFilter. The
// segment is configured to seal after exactly maxSegmentKeyCount keys (size-based sealing is disabled),
// which lets the caller deterministically control how keys are distributed across segments. GC only runs
// when RunGC is called explicitly (GCPeriod is set very large), so tests can inspect filter calls without
// racing a background GC pass.
func buildGCFilterTable(
	t *testing.T,
	clock func() time.Time,
	name string,
	path string,
	maxSegmentKeyCount uint32,
	gcFilter litt.GCFilter,
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

	// Disable size-based sealing so MaxSegmentKeyCount is the only thing that seals a segment.
	config.TargetSegmentFileSize = math.MaxUint32
	config.MaxSegmentKeyCount = maxSegmentKeyCount
	// Effectively disable background GC; tests drive GC explicitly via RunGC.
	config.GCPeriod = time.Hour
	config.Fsync = false

	tableConfig := litt.DefaultTableConfig(name)
	tableConfig.ShardingFactor = 1
	tableConfig.GCFilter = gcFilter

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

// requireKeyPresent asserts that the key is still readable from the table.
func requireKeyPresent(t *testing.T, table litt.ManagedTable, key string) {
	t.Helper()
	_, ok, err := table.Get([]byte(key))
	require.NoError(t, err)
	require.True(t, ok, "expected key %q to be present", key)
}

// requireKeyAbsent asserts that the key has been removed from the table.
func requireKeyAbsent(t *testing.T, table litt.ManagedTable, key string) {
	t.Helper()
	_, ok, err := table.Get([]byte(key))
	require.NoError(t, err)
	require.False(t, ok, "expected key %q to be absent", key)
}

// TestGCFilterBlocksAndResumes verifies that:
//   - A TTL-expired segment is NOT deleted while the GCFilter blocks any of its keys.
//   - The scan resumes where it left off: keys already known to pass are not re-checked on the next pass.
//   - Once the blocking key is unblocked, the segment (and subsequent eligible segments) are deleted.
func TestGCFilterBlocksAndResumes(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	start := time.Unix(1_700_000_000, 0)
	var fakeTime atomic.Pointer[time.Time]
	fakeTime.Store(&start)
	clock := func() time.Time { return *fakeTime.Load() }

	recorder := newGCFilterRecorder()

	// Seal after every 2 keys. Writing 5 keys yields:
	//   segment 0 (sealed):  k0, k1
	//   segment 1 (sealed):  k2, k3
	//   segment 2 (mutable): k4
	table := buildGCFilterTable(t, clock, "gcfilter", directory, 2, recorder.filter)
	defer func() { require.NoError(t, table.Close()) }()

	ttl := 10 * time.Second
	require.NoError(t, table.SetTTL(ttl))

	keyOrder := []string{"k0", "k1", "k2", "k3", "k4"}
	for _, k := range keyOrder {
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}
	require.NoError(t, table.Flush())

	// Block k1 (the second key of segment 0) before any GC runs.
	recorder.block("k1")

	// Advance the clock well past the TTL so every sealed segment is eligible by age.
	expired := start.Add(time.Hour)
	fakeTime.Store(&expired)

	// First GC pass: walks segment 0 from the start, hits the block at k1, and stops. Segment 0 must
	// survive, and so must everything behind it (segments are deleted strictly in order).
	require.NoError(t, table.RunGC())
	require.Equal(t, []string{"k0", "k1"}, recorder.takeCallKeys(),
		"first pass should examine k0 then block at k1")
	for _, k := range keyOrder {
		requireKeyPresent(t, table, k)
	}

	// Second GC pass while still blocked: the cursor must resume at k1, NOT re-examine k0.
	require.NoError(t, table.RunGC())
	require.Equal(t, []string{"k1"}, recorder.takeCallKeys(),
		"second pass should resume at the blocked key without re-checking k0")
	for _, k := range keyOrder {
		requireKeyPresent(t, table, k)
	}

	// Unblock k1 and run GC again. Segment 0 now fully passes and is deleted; GC then advances to
	// segment 1 (k2, k3), which also passes and is deleted. Segment 2 is the mutable segment and is
	// never deletable, so k4 survives.
	recorder.unblock("k1")
	require.NoError(t, table.RunGC())
	require.Equal(t, []string{"k1", "k2", "k3"}, recorder.takeCallKeys(),
		"final pass should resume at k1, then scan segment 1's keys")

	requireKeyAbsent(t, table, "k0")
	requireKeyAbsent(t, table, "k1")
	requireKeyAbsent(t, table, "k2")
	requireKeyAbsent(t, table, "k3")
	requireKeyPresent(t, table, "k4")
}

// TestGCFilterIsPrimaryKey verifies that the GCFilter receives isPrimaryKey=true for primary keys
// (standalone primaries and primaries with secondaries) and isPrimaryKey=false for secondary keys.
func TestGCFilterIsPrimaryKey(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	start := time.Unix(1_700_000_000, 0)
	var fakeTime atomic.Pointer[time.Time]
	fakeTime.Store(&start)
	clock := func() time.Time { return *fakeTime.Load() }

	recorder := newGCFilterRecorder()

	// Capture the isPrimaryKey flag for every key without ever blocking deletion.
	seen := make(map[string]bool)
	var seenMu sync.Mutex
	filter := func(key []byte, isPrimaryKey bool) (bool, error) {
		seenMu.Lock()
		seen[string(key)] = isPrimaryKey
		seenMu.Unlock()
		return recorder.filter(key, isPrimaryKey)
	}

	// A high MaxSegmentKeyCount keeps everything in one segment until we force a seal by writing more
	// data into a fresh mutable segment.
	table := buildGCFilterTable(t, clock, "gcfilterprimary", directory, 64, filter)
	defer func() { require.NoError(t, table.Close()) }()

	require.NoError(t, table.SetTTL(10*time.Second))

	// A single Put with one secondary key. The primary is written as KeyKindPrimary and the secondary
	// as KeyKindFinalSecondary.
	value := []byte("the-quick-brown-fox")
	require.NoError(t, table.Put(
		[]byte("primary"),
		value,
		&types.SecondaryKey{Key: []byte("secondary"), Offset: 0, Length: 3},
	))
	// A standalone primary in the same segment.
	require.NoError(t, table.Put([]byte("standalone"), []byte("v")))

	// Write enough filler keys to push past MaxSegmentKeyCount, forcing the segment holding our keys to
	// seal so that GC is able to scan its key file.
	for i := 0; i < 64; i++ {
		require.NoError(t, table.Put([]byte(fmt.Sprintf("filler-%d", i)), []byte("x")))
	}
	require.NoError(t, table.Flush())

	expired := start.Add(time.Hour)
	fakeTime.Store(&expired)
	require.NoError(t, table.RunGC())

	seenMu.Lock()
	defer seenMu.Unlock()
	require.True(t, seen["primary"], "primary key should report isPrimaryKey=true")
	require.True(t, seen["standalone"], "standalone primary key should report isPrimaryKey=true")
	require.False(t, seen["secondary"], "secondary key should report isPrimaryKey=false")
}

// TestGCFilterNilDeletesOnTTL verifies that when no GCFilter is configured, TTL-expired segments are
// deleted exactly as before (the filter is purely additive).
func TestGCFilterNilDeletesOnTTL(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	start := time.Unix(1_700_000_000, 0)
	var fakeTime atomic.Pointer[time.Time]
	fakeTime.Store(&start)
	clock := func() time.Time { return *fakeTime.Load() }

	table := buildGCFilterTable(t, clock, "gcfilternil", directory, 2, nil)
	defer func() { require.NoError(t, table.Close()) }()

	require.NoError(t, table.SetTTL(10*time.Second))

	keyOrder := []string{"k0", "k1", "k2", "k3", "k4"}
	for _, k := range keyOrder {
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}
	require.NoError(t, table.Flush())

	expired := start.Add(time.Hour)
	fakeTime.Store(&expired)
	require.NoError(t, table.RunGC())

	// Sealed segments (k0..k3) are deleted; the mutable segment (k4) survives.
	requireKeyAbsent(t, table, "k0")
	requireKeyAbsent(t, table, "k1")
	requireKeyAbsent(t, table, "k2")
	requireKeyAbsent(t, table, "k3")
	requireKeyPresent(t, table, "k4")
}

// TestGCDeletesKeymapBeforeFiles verifies the GC ordering: GC first schedules a segment's keymap-entry deletion
// on the keymap manager, and deletes the segment's files only after the manager has durably applied that
// deletion. We drive the steps explicitly and observe the intermediate state: once the keymap deletion is
// scheduled and the manager synced, the keys are already gone from the keymap (Get misses), but the table still
// accounts for them because their files (and the key-count they contribute) are removed only afterward.
func TestGCDeletesKeymapBeforeFiles(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	start := time.Unix(1_700_000_000, 0)
	var fakeTime atomic.Pointer[time.Time]
	fakeTime.Store(&start)
	clock := func() time.Time { return *fakeTime.Load() }

	// Seal after every 2 keys: seg0[k0,k1], seg1[k2,k3], seg2(mutable)[k4].
	table := buildGCFilterTable(t, clock, "gcordering", directory, 2, nil)
	defer func() { require.NoError(t, table.Close()) }()
	d := table.(*DiskTable)

	require.NoError(t, table.SetTTL(10*time.Second))
	for _, k := range []string{"k0", "k1", "k2", "k3", "k4"} {
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}
	require.NoError(t, table.Flush())
	require.Equal(t, uint64(5), table.KeyCount())

	// Expire the sealed segments.
	expired := start.Add(time.Hour)
	fakeTime.Store(&expired)

	// Collect the expired sealed segments (seg0, seg1): schedule their keymap-entry deletion and advance the
	// durable gc-watermark past them, then sync the manager so the keymap entries are durably removed and the
	// deletion watermark advances. seg2 is mutable and survives.
	require.NoError(t, d.gcManager.runOnce())
	require.NoError(t, d.keymapManager.sync())

	// The keymap entries are now gone, so the keys are no longer readable...
	requireKeyAbsent(t, table, "k0")
	requireKeyAbsent(t, table, "k3")
	requireKeyPresent(t, table, "k4")

	// ...but the segment files have not been deleted yet, so the table still accounts for their keys.
	require.Equal(t, uint64(5), table.KeyCount())

	// Now delete the files of the segments whose keymap entries are durably gone.
	require.NoError(t, d.runGCPass())
	require.Equal(t, uint64(1), table.KeyCount())

	requireKeyAbsent(t, table, "k0")
	requireKeyPresent(t, table, "k4")
}

// TestGCWatermarkPreventsResurrectionAfterCrash is the regression test for the durable gc-watermark. It
// reproduces a crash at the worst moment during collection: a segment's keymap entries are durably deleted and
// the gc-watermark advanced past it, but its files have not yet been removed. On restart, repairKeymap finds
// those keys present in the segment files but missing from the keymap. Without the durable watermark it would
// mistake them for lost async writes and resurrect garbage-collected data; with it, repair refuses to touch
// segments below the watermark.
func TestGCWatermarkPreventsResurrectionAfterCrash(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	tableName := "gc-watermark-repair"
	logger := slog.Default()
	keymapPath := filepath.Join(directory, keymap.KeymapDirectoryName)
	tableRoot := filepath.Join(directory, tableName)

	start := time.Unix(1_700_000_000, 0)
	var fakeTime atomic.Pointer[time.Time]
	fakeTime.Store(&start)
	clock := func() time.Time { return *fakeTime.Load() }

	// build opens a single-shard PebbleDB-keymap table that seals after every 2 keys and never runs GC in the
	// background (GCPeriod is large), so the test can drive GC deterministically.
	build := func(km keymap.Keymap, reload bool) *DiskTable {
		keymapTypeFile, err := setupKeymapTypeFile(keymapPath, keymap.UnsafePebbleDBKeymapType)
		require.NoError(t, err)

		config, err := litt.DefaultConfig(directory)
		require.NoError(t, err)
		config.TargetSegmentFileSize = math.MaxUint32
		config.MaxSegmentKeyCount = 2
		config.GCPeriod = time.Hour
		config.Fsync = false

		tableConfig := litt.DefaultTableConfig(tableName)
		tableConfig.ShardingFactor = 1

		runtimeConfig := litt.DefaultRuntimeConfig()
		runtimeConfig.Clock = clock
		runtimeConfig.Logger = logger

		table, err := NewDiskTable(
			config, runtimeConfig, tableName, tableConfig, km, keymapPath, keymapTypeFile,
			[]string{directory}, reload, nil)
		require.NoError(t, err)
		return table.(*DiskTable)
	}

	// Session 1: write k0..k4 -> seg0[k0,k1], seg1[k2,k3], seg2(mutable)[k4]. Expire the sealed segments and run
	// a collection pass only (schedule keymap deletes + advance the durable gc-watermark), then sync the manager
	// so the keymap entries are durably gone. A clean Close does NOT delete the segment files, so this leaves
	// exactly the crashed-mid-collection state: seg0/seg1 files on disk, keymap entries gone, watermark = 2.
	km1, _, err := keymap.NewUnsafePebbleDBKeymap(logger, keymapPath, false)
	require.NoError(t, err)
	table := build(km1, true)
	require.NoError(t, table.SetTTL(10*time.Second))
	for _, k := range []string{"k0", "k1", "k2", "k3", "k4"} {
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}
	require.NoError(t, table.Flush())

	expired := start.Add(time.Hour)
	fakeTime.Store(&expired)

	require.NoError(t, table.gcManager.runOnce())
	require.NoError(t, table.keymapManager.sync())
	require.NoError(t, table.Close())

	// The gc-watermark file persisted at the table root and covers the collected segments (seg0, seg1).
	wmFile, err := LoadGCWatermarkFile(tableRoot)
	require.NoError(t, err)
	require.True(t, wmFile.IsDefined())
	require.Equal(t, uint32(2), wmFile.LowestReadableSegment())

	// Out-of-band, delete the newest key (k4, in the still-live seg2) from the keymap. This simulates a lost
	// async keymap write — a genuine orphan that repair SHOULD rescue. With no present key left in the keymap,
	// repair walks newest-first all the way down; without the watermark floor it would continue past seg2 and
	// resurrect the four garbage-collected keys in seg0/seg1.
	kmOOB, _, err := keymap.NewUnsafePebbleDBKeymap(logger, keymapPath, false)
	require.NoError(t, err)
	require.NoError(t, kmOOB.Delete([]*types.ScopedKey{{Key: []byte("k4")}}))
	require.NoError(t, kmOOB.Stop())

	// Session 2: reopen on the same directory (repair path). Wrap the keymap so we can assert repair rescues
	// only the real orphan (k4) and skips the garbage-collected segments below the watermark.
	km2, _, err := keymap.NewUnsafePebbleDBKeymap(logger, keymapPath, false)
	require.NoError(t, err)
	spy := &countingKeymap{Keymap: km2}
	reopened := build(spy, false)
	defer func() { require.NoError(t, reopened.Close()) }()

	// Repair must rescue exactly the one real orphan (k4), not the four garbage-collected keys below the
	// watermark. Without the fix it would rescue all five.
	require.Equal(t, 1, spy.putKeys, "repair must rescue only the orphan, not garbage-collected keys")

	for _, k := range []string{"k0", "k1", "k2", "k3"} {
		_, ok, err := reopened.Get([]byte(k))
		require.NoError(t, err)
		require.False(t, ok, "garbage-collected key %s must not be resurrected after restart", k)

		ok, err = reopened.Exists([]byte(k))
		require.NoError(t, err)
		require.False(t, ok, "Exists must not report resurrected key %s after restart", k)
	}

	// k4 (the rescued orphan, in the still-live seg2) must be readable again.
	v, ok, err := reopened.Get([]byte("k4"))
	require.NoError(t, err)
	require.True(t, ok, "rescued orphan key k4 should be readable")
	require.Equal(t, []byte("value-k4"), v)
}

// segmentIndexFromSnapshotFile extracts the segment index from a snapshot segment file name. Segment files are
// named "<index>.<ext>" (metadata/keys) or "<index>-<shard>.<ext>" (values), so the index is the leading run of
// digits up to the first '.' or '-'.
func segmentIndexFromSnapshotFile(t *testing.T, name string) uint32 {
	end := strings.IndexAny(name, ".-")
	require.GreaterOrEqual(t, end, 0, "unexpected snapshot file name %s", name)
	idx, err := strconv.ParseUint(name[:end], 10, 32)
	require.NoError(t, err)
	return uint32(idx)
}

// TestSnapshotSkipsCollectedSegments is the regression test for the snapshot resurrection bug: repairSnapshot
// must floor at the durable gc-watermark, not at the lowest segment physically present on disk. It reproduces
// the same crash-mid-collection state as TestGCWatermarkPreventsResurrectionAfterCrash (seg0/seg1 logically
// deleted, gc-watermark advanced to 2, their files still on disk) but with snapshotting enabled. On reopen the
// snapshot directory is rebuilt from scratch; without the fix repairSnapshot would symlink the garbage-collected
// seg0/seg1 into it, and an external restore of that snapshot — which lands in a fresh LittDB with no
// gc-watermark — would rescan those segments and resurrect the collected keys.
func TestSnapshotSkipsCollectedSegments(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	snapshotDirectory := t.TempDir()
	tableName := "snapshot-gc-watermark"
	logger := slog.Default()
	keymapPath := filepath.Join(directory, keymap.KeymapDirectoryName)
	tableRoot := filepath.Join(directory, tableName)

	start := time.Unix(1_700_000_000, 0)
	var fakeTime atomic.Pointer[time.Time]
	fakeTime.Store(&start)
	clock := func() time.Time { return *fakeTime.Load() }

	// build opens a single-shard PebbleDB-keymap table with snapshotting enabled, sealing after every 2 keys and
	// never running GC in the background (GCPeriod is large), so the test can drive GC deterministically.
	build := func(km keymap.Keymap, reload bool) *DiskTable {
		keymapTypeFile, err := setupKeymapTypeFile(keymapPath, keymap.UnsafePebbleDBKeymapType)
		require.NoError(t, err)

		config, err := litt.DefaultConfig(directory)
		require.NoError(t, err)
		config.TargetSegmentFileSize = math.MaxUint32
		config.MaxSegmentKeyCount = 2
		config.GCPeriod = time.Hour
		config.Fsync = false
		config.SnapshotDirectory = snapshotDirectory

		tableConfig := litt.DefaultTableConfig(tableName)
		tableConfig.ShardingFactor = 1

		runtimeConfig := litt.DefaultRuntimeConfig()
		runtimeConfig.Clock = clock
		runtimeConfig.Logger = logger

		table, err := NewDiskTable(
			config, runtimeConfig, tableName, tableConfig, km, keymapPath, keymapTypeFile,
			[]string{directory}, reload, nil)
		require.NoError(t, err)
		return table.(*DiskTable)
	}

	// Session 1: write k0..k4 -> seg0[k0,k1], seg1[k2,k3], seg2(mutable)[k4]. Expire the sealed segments and run a
	// collection pass only (schedule keymap deletes + advance the durable gc-watermark), then sync the manager so
	// the keymap entries are durably gone. A clean Close does NOT delete the segment files, so this leaves exactly
	// the crashed-mid-collection state: seg0/seg1 files on disk, keymap entries gone, watermark = 2.
	km1, _, err := keymap.NewUnsafePebbleDBKeymap(logger, keymapPath, false)
	require.NoError(t, err)
	table := build(km1, true)
	require.NoError(t, table.SetTTL(10*time.Second))
	for _, k := range []string{"k0", "k1", "k2", "k3", "k4"} {
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}
	require.NoError(t, table.Flush())

	expired := start.Add(time.Hour)
	fakeTime.Store(&expired)

	require.NoError(t, table.gcManager.runOnce())
	require.NoError(t, table.keymapManager.sync())
	require.NoError(t, table.Close())

	wmFile, err := LoadGCWatermarkFile(tableRoot)
	require.NoError(t, err)
	require.True(t, wmFile.IsDefined())
	require.Equal(t, uint32(2), wmFile.LowestReadableSegment())

	// Session 2: reopen on the same directory (repair path). NewDiskTable rebuilds the snapshot segments
	// directory from scratch via repairSnapshot.
	km2, _, err := keymap.NewUnsafePebbleDBKeymap(logger, keymapPath, false)
	require.NoError(t, err)
	reopened := build(km2, false)
	defer func() { require.NoError(t, reopened.Close()) }()

	// The rebuilt snapshot must not contain the garbage-collected segments (seg0, seg1). Only segments at or
	// above the gc-watermark (seg2 onward) may appear, otherwise an external restore would resurrect their keys.
	snapshotSegments := filepath.Join(snapshotDirectory, tableName, segment.SegmentDirectory)
	entries, err := os.ReadDir(snapshotSegments)
	require.NoError(t, err)
	for _, entry := range entries {
		idx := segmentIndexFromSnapshotFile(t, entry.Name())
		require.GreaterOrEqual(t, idx, uint32(2),
			"snapshot must not contain garbage-collected segment file %s (below gc-watermark)", entry.Name())
	}
}

// TestReadsSkipCollectedSegmentsBeforeReclaim is the regression test for Finding 1 on a live table. After a
// segment is logically deleted (its keymap entries durably removed and the gc-watermark advanced) but before
// the control loop reclaims its files, the segment still lives in the segments map. Iteration reads segment
// files directly (bypassing the keymap), and GetOldestKey walks the map, so without flooring at the deletion
// watermark they would surface the garbage-collected keys. This drives the runtime path: the deletion
// watermark is advanced at run time and refreshDeletionWatermark must pick it up at read time.
func TestReadsSkipCollectedSegmentsBeforeReclaim(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	start := time.Unix(1_700_000_000, 0)
	var fakeTime atomic.Pointer[time.Time]
	fakeTime.Store(&start)
	clock := func() time.Time { return *fakeTime.Load() }

	// Seal after every 2 keys: seg0[k0,k1], seg1[k2,k3], seg2(mutable)[k4].
	table := buildGCFilterTable(t, clock, "iter-live-gc", directory, 2, nil)
	defer func() { require.NoError(t, table.Close()) }()
	d := table.(*DiskTable)

	require.NoError(t, table.SetTTL(10*time.Second))
	for _, k := range []string{"k0", "k1", "k2", "k3", "k4"} {
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}
	require.NoError(t, table.Flush())

	// Expire and logically delete the sealed segments (advance the durable gc-watermark, apply the keymap
	// deletes) WITHOUT reclaiming their files: seg0/seg1 are now logically deleted but still in the map.
	expired := start.Add(time.Hour)
	fakeTime.Store(&expired)
	require.NoError(t, d.gcManager.runOnce())
	require.NoError(t, d.keymapManager.sync())

	// Iteration (forward and reverse) and GetOldestKey must skip the collected segments, even though their
	// files have not been reclaimed yet.
	fwd, err := table.Iterator(false)
	require.NoError(t, err)
	require.Equal(t, []string{"k4"}, entryKeys(drainIterator(t, fwd)))
	require.NoError(t, fwd.Close())

	rev, err := table.Iterator(true)
	require.NoError(t, err)
	require.Equal(t, []string{"k4"}, entryKeys(drainIterator(t, rev)))
	require.NoError(t, rev.Close())

	oldest, ok, err := table.GetOldestKey()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "k4", string(oldest), "GetOldestKey must not report a collected key")
}

// TestReadsSkipCollectedSegmentsAfterWatermarkOverflow is the regression test for the deletion-watermark
// channel overflow. A single RunGC collects more segments than the watermark channel can buffer, so the
// keymap manager publishes more monotonic watermarks (one per collected segment) than the channel holds.
// With a bare non-blocking send the newest (highest) watermarks would be dropped, leaving readableFloor stuck
// below the true durable-delete frontier — so GetOldestKey/GetNewestKey and iteration would surface keys from
// segments whose keymap entries were already deleted (keys that Get reports as absent). The drain-then-send
// (latest-value) publish keeps the freshest watermark, so the floor is correct.
func TestReadsSkipCollectedSegmentsAfterWatermarkOverflow(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	tableName := "iter-watermark-overflow"
	logger := slog.Default()
	keymapPath := filepath.Join(directory, keymap.KeymapDirectoryName)

	keymapTypeFile, err := setupKeymapTypeFile(keymapPath, keymap.MemKeymapType)
	require.NoError(t, err)
	keys, _, err := keymap.NewMemKeymap(logger, "", true)
	require.NoError(t, err)

	start := time.Unix(1_700_000_000, 0)
	var fakeTime atomic.Pointer[time.Time]
	fakeTime.Store(&start)
	clock := func() time.Time { return *fakeTime.Load() }

	config, err := litt.DefaultConfig(directory)
	require.NoError(t, err)
	config.TargetSegmentFileSize = math.MaxUint32
	config.MaxSegmentKeyCount = 1 // seal after every key, so each key gets its own segment
	config.GCPeriod = time.Hour   // disable background GC; the test drives it explicitly
	config.Fsync = false
	// Force overflow: collecting 6 segments in one RunGC publishes 6 monotonic watermarks into a 2-deep channel.
	config.KeymapManagerWatermarkChannelSize = 2

	tableConfig := litt.DefaultTableConfig(tableName)
	tableConfig.ShardingFactor = 1

	runtimeConfig := litt.DefaultRuntimeConfig()
	runtimeConfig.Clock = clock
	runtimeConfig.Logger = logger

	table, err := NewDiskTable(
		config, runtimeConfig, tableName, tableConfig, keys, keymapPath, keymapTypeFile,
		[]string{directory}, true, nil)
	require.NoError(t, err)
	defer func() { require.NoError(t, table.Close()) }()

	require.NoError(t, table.SetTTL(10*time.Second))

	// Write k0..k5, each in its own sealed segment (seg0..seg5).
	for i := 0; i < 6; i++ {
		k := fmt.Sprintf("k%d", i)
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}
	require.NoError(t, table.Flush())

	// Advance past the TTL so seg0..seg5 are all expired, then write one fresh key that stays live.
	expired := start.Add(time.Hour)
	fakeTime.Store(&expired)
	require.NoError(t, table.Put([]byte("k-live"), []byte("value-live")))
	require.NoError(t, table.Flush())

	// One RunGC collects all 6 expired segments. The keymap manager publishes watermarks 0..5 back-to-back
	// during the sync; the channel only holds 2. With the latest-value publish, the floor advances to cover
	// every collected segment.
	require.NoError(t, table.RunGC())

	// The collected keys are gone from point reads.
	for i := 0; i < 6; i++ {
		k := fmt.Sprintf("k%d", i)
		_, ok, err := table.Get([]byte(k))
		require.NoError(t, err)
		require.False(t, ok, "collected key %q must be absent from Get", k)
	}
	_, ok, err := table.Get([]byte("k-live"))
	require.NoError(t, err)
	require.True(t, ok, "live key must still be present")

	// Boundary-key reads must not report a key below the true deletion frontier: only k-live survives.
	oldest, ok, err := table.GetOldestKey()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "k-live", string(oldest), "GetOldestKey must not report a collected key after watermark overflow")

	newest, ok, err := table.GetNewestKey()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "k-live", string(newest), "GetNewestKey must not report a collected key after watermark overflow")

	// Iteration (forward and reverse) must not surface any collected key either.
	fwd, err := table.Iterator(false)
	require.NoError(t, err)
	require.Equal(t, []string{"k-live"}, entryKeys(drainIterator(t, fwd)))
	require.NoError(t, fwd.Close())

	rev, err := table.Iterator(true)
	require.NoError(t, err)
	require.Equal(t, []string{"k-live"}, entryKeys(drainIterator(t, rev)))
	require.NoError(t, rev.Close())
}

// TestReadsSkipCollectedSegmentsAfterCrash is the regression test for Finding 1 across a crash. It reproduces
// the crashed-mid-collection state (a segment's keymap entries durably deleted and the gc-watermark advanced
// past it, but its files not yet removed), then restarts. On restart the leftover segment files are present in
// the map and the control loop has not yet run a reclamation pass, so iteration / GetOldestKey would resurrect
// the garbage-collected keys unless they floor at the gc-watermark seeded at construction.
func TestReadsSkipCollectedSegmentsAfterCrash(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	tableName := "iter-crash-gc"
	logger := slog.Default()
	keymapPath := filepath.Join(directory, keymap.KeymapDirectoryName)

	start := time.Unix(1_700_000_000, 0)
	var fakeTime atomic.Pointer[time.Time]
	fakeTime.Store(&start)
	clock := func() time.Time { return *fakeTime.Load() }

	build := func(km keymap.Keymap, reload bool) *DiskTable {
		keymapTypeFile, err := setupKeymapTypeFile(keymapPath, keymap.UnsafePebbleDBKeymapType)
		require.NoError(t, err)

		config, err := litt.DefaultConfig(directory)
		require.NoError(t, err)
		config.TargetSegmentFileSize = math.MaxUint32
		config.MaxSegmentKeyCount = 2
		config.GCPeriod = time.Hour
		config.Fsync = false

		tableConfig := litt.DefaultTableConfig(tableName)
		tableConfig.ShardingFactor = 1

		runtimeConfig := litt.DefaultRuntimeConfig()
		runtimeConfig.Clock = clock
		runtimeConfig.Logger = logger

		table, err := NewDiskTable(
			config, runtimeConfig, tableName, tableConfig, km, keymapPath, keymapTypeFile,
			[]string{directory}, reload, nil)
		require.NoError(t, err)
		return table.(*DiskTable)
	}

	// Session 1: write k0..k4 -> seg0[k0,k1], seg1[k2,k3], seg2(mutable)[k4]. Expire and logically delete the
	// sealed segments, then Close WITHOUT reclaiming their files (a clean Close does not delete segment files),
	// leaving seg0/seg1 on disk with the gc-watermark advanced to 2.
	km1, _, err := keymap.NewUnsafePebbleDBKeymap(logger, keymapPath, false)
	require.NoError(t, err)
	table := build(km1, true)
	require.NoError(t, table.SetTTL(10*time.Second))
	for _, k := range []string{"k0", "k1", "k2", "k3", "k4"} {
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}
	require.NoError(t, table.Flush())

	expired := start.Add(time.Hour)
	fakeTime.Store(&expired)
	require.NoError(t, table.gcManager.runOnce())
	require.NoError(t, table.keymapManager.sync())
	require.NoError(t, table.Close())

	wmFile, err := LoadGCWatermarkFile(filepath.Join(directory, tableName))
	require.NoError(t, err)
	require.True(t, wmFile.IsDefined())
	require.Equal(t, uint32(2), wmFile.LowestReadableSegment())

	// Session 2: reopen on the same directory (repair path). seg0/seg1 files are still present but below the
	// durable gc-watermark; reads must not surface their keys.
	km2, _, err := keymap.NewUnsafePebbleDBKeymap(logger, keymapPath, false)
	require.NoError(t, err)
	reopened := build(km2, false)
	defer func() { require.NoError(t, reopened.Close()) }()

	fwd, err := reopened.Iterator(false)
	require.NoError(t, err)
	require.Equal(t, []string{"k4"}, entryKeys(drainIterator(t, fwd)))
	require.NoError(t, fwd.Close())

	rev, err := reopened.Iterator(true)
	require.NoError(t, err)
	require.Equal(t, []string{"k4"}, entryKeys(drainIterator(t, rev)))
	require.NoError(t, rev.Close())

	oldest, ok, err := reopened.GetOldestKey()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "k4", string(oldest), "GetOldestKey must not resurrect a collected key after restart")

	// Sanity: the collected keys are gone from point reads too.
	_, ok, err = reopened.Get([]byte("k0"))
	require.NoError(t, err)
	require.False(t, ok)
}

// TestCollectedKeymapEntriesPurgedAfterCrash is the regression test for Finding 2. It reproduces the state a
// crash leaves when GC advanced the durable gc-watermark past some segments but the asynchronous keymap deletes
// were lost (and the segment files were not yet reclaimed): the keymap still holds the collected keys. On
// restart the startup purge must delete those entries before the control loop can reclaim the files, so a
// collected key is never resurrected via Get or Exists.
//
// The lost-delete state is constructed directly rather than via a real GC pass: a clean Close drains the keymap
// manager and would apply the deletes, so instead we write cleanly and then advance the gc-watermark file
// out-of-band, mimicking "watermark durable, deletes lost".
func TestCollectedKeymapEntriesPurgedAfterCrash(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	tableName := "purge-crash-gc"
	logger := slog.Default()
	keymapPath := filepath.Join(directory, keymap.KeymapDirectoryName)
	tableRoot := filepath.Join(directory, tableName)

	start := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return start }

	build := func(km keymap.Keymap, reload bool) *DiskTable {
		keymapTypeFile, err := setupKeymapTypeFile(keymapPath, keymap.UnsafePebbleDBKeymapType)
		require.NoError(t, err)

		config, err := litt.DefaultConfig(directory)
		require.NoError(t, err)
		config.TargetSegmentFileSize = math.MaxUint32
		config.MaxSegmentKeyCount = 2
		config.GCPeriod = time.Hour
		config.Fsync = false

		tableConfig := litt.DefaultTableConfig(tableName)
		tableConfig.ShardingFactor = 1

		runtimeConfig := litt.DefaultRuntimeConfig()
		runtimeConfig.Clock = clock
		runtimeConfig.Logger = logger

		table, err := NewDiskTable(
			config, runtimeConfig, tableName, tableConfig, km, keymapPath, keymapTypeFile,
			[]string{directory}, reload, nil)
		require.NoError(t, err)
		return table.(*DiskTable)
	}

	// Session 1: write k0..k4 -> seg0[k0,k1], seg1[k2,k3], seg2(mutable)[k4]; clean Close. No GC ran, so the
	// keymap holds all of k0..k4 and every segment file is on disk.
	km1, _, err := keymap.NewUnsafePebbleDBKeymap(logger, keymapPath, false)
	require.NoError(t, err)
	table := build(km1, true)
	for _, k := range []string{"k0", "k1", "k2", "k3", "k4"} {
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}
	require.NoError(t, table.Flush())
	require.NoError(t, table.Close())

	// Out-of-band, advance the durable gc-watermark to 2: this is the state a crash leaves after GC advanced
	// the watermark past seg0/seg1 but their keymap deletes were lost and their files not yet reclaimed.
	wmFile, err := LoadGCWatermarkFile(tableRoot)
	require.NoError(t, err)
	require.NoError(t, wmFile.Update(2))

	// Session 2: reopen (repair path). The startup purge must delete the keymap entries for seg0/seg1 (k0..k3),
	// which repair never would, so the collected keys are gone from both Get and Exists.
	km2, _, err := keymap.NewUnsafePebbleDBKeymap(logger, keymapPath, false)
	require.NoError(t, err)
	reopened := build(km2, false)
	defer func() { require.NoError(t, reopened.Close()) }()

	for _, k := range []string{"k0", "k1", "k2", "k3"} {
		_, ok, err := reopened.Get([]byte(k))
		require.NoError(t, err)
		require.False(t, ok, "collected key %s must not be readable via Get after restart", k)

		ok, err = reopened.Exists([]byte(k))
		require.NoError(t, err)
		require.False(t, ok, "collected key %s must not be reported by Exists after restart", k)
	}

	// k4 is in seg2 (>= the gc-watermark), so it survives and stays readable.
	v, ok, err := reopened.Get([]byte("k4"))
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, []byte("value-k4"), v)
	ok, err = reopened.Exists([]byte("k4"))
	require.NoError(t, err)
	require.True(t, ok)
}

// TestExistsRequiresLiveSegment isolates the Exists hardening: a keymap entry whose backing segment is not in
// the live set (e.g. a stale entry a lost crash-time delete left behind, pointing at a since-reclaimed segment)
// must not be reported by Exists, matching Get. A bogus entry is inserted out-of-band to model that state.
func TestExistsRequiresLiveSegment(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	start := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return start }

	table := buildGCFilterTable(t, clock, "exists-live-seg", directory, 2, nil)
	defer func() { require.NoError(t, table.Close()) }()
	d := table.(*DiskTable)

	// A real, flushed key resolves through the keymap to a live segment: Exists must report it.
	require.NoError(t, table.Put([]byte("real"), []byte("value")))
	require.NoError(t, table.Flush())
	ok, err := table.Exists([]byte("real"))
	require.NoError(t, err)
	require.True(t, ok)

	// Insert a stale keymap entry pointing at a segment index that does not exist in the live set.
	require.NoError(t, d.keymap.Put([]*types.ScopedKey{
		{Key: []byte("ghost"), Address: types.NewAddress(9999, 0, 0, 0)},
	}))

	// Exists must not report the ghost (its segment is gone), and must agree with Get.
	ok, err = table.Exists([]byte("ghost"))
	require.NoError(t, err)
	require.False(t, ok, "Exists must not report a key whose backing segment is missing")

	_, ok, err = table.Get([]byte("ghost"))
	require.NoError(t, err)
	require.False(t, ok)
}

// TestGCWatermarkAboveHighestSegmentFailsToOpen verifies that a gc-watermark file pointing above the highest
// segment present on disk (the inconsistency a corrupted or externally-edited watermark file would produce)
// makes NewDiskTable return an error rather than nil-dereferencing a missing segment during startup
// purge/repair or boundary-key reads.
func TestGCWatermarkAboveHighestSegmentFailsToOpen(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	tableName := "watermark-too-high"
	logger := slog.Default()
	keymapPath := filepath.Join(directory, keymap.KeymapDirectoryName)
	tableRoot := filepath.Join(directory, tableName)

	start := time.Unix(1_700_000_000, 0)
	clock := func() time.Time { return start }

	build := func(km keymap.Keymap, reload bool) (litt.ManagedTable, error) {
		keymapTypeFile, err := setupKeymapTypeFile(keymapPath, keymap.UnsafePebbleDBKeymapType)
		require.NoError(t, err)

		config, err := litt.DefaultConfig(directory)
		require.NoError(t, err)
		config.TargetSegmentFileSize = math.MaxUint32
		config.MaxSegmentKeyCount = 2
		config.GCPeriod = time.Hour
		config.Fsync = false

		tableConfig := litt.DefaultTableConfig(tableName)
		tableConfig.ShardingFactor = 1

		runtimeConfig := litt.DefaultRuntimeConfig()
		runtimeConfig.Clock = clock
		runtimeConfig.Logger = logger

		return NewDiskTable(
			config, runtimeConfig, tableName, tableConfig, km, keymapPath, keymapTypeFile,
			[]string{directory}, reload, nil)
	}

	// Session 1: write k0..k4 across several segments (MaxSegmentKeyCount=2), then clean Close.
	km1, _, err := keymap.NewUnsafePebbleDBKeymap(logger, keymapPath, false)
	require.NoError(t, err)
	table, err := build(km1, true)
	require.NoError(t, err)
	for _, k := range []string{"k0", "k1", "k2", "k3", "k4"} {
		require.NoError(t, table.Put([]byte(k), []byte("value-"+k)))
	}
	require.NoError(t, table.Flush())
	require.NoError(t, table.Close())

	// Corrupt the durable gc-watermark to a value far above any segment on disk.
	wmFile, err := LoadGCWatermarkFile(tableRoot)
	require.NoError(t, err)
	require.NoError(t, wmFile.Update(1000))

	// Session 2: reopening must fail cleanly with a descriptive error, not panic.
	km2, _, err := keymap.NewUnsafePebbleDBKeymap(logger, keymapPath, false)
	require.NoError(t, err)
	reopened, err := build(km2, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "gc-watermark")
	require.Nil(t, reopened)
}
