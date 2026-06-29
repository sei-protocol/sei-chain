package disktable

import (
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
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
