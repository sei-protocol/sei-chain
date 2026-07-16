package segment

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path"
	"sort"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/stretchr/testify/require"
)

// countFilesInDirectory returns the number of files in the given directory.
func countFilesInDirectory(t *testing.T, directory string) int {
	files, err := os.ReadDir(directory)
	require.NoError(t, err)
	return len(files)
}

func TestWriteAndReadSegmentSingleShard(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	rand := util.NewTestRandom()
	logger := slog.Default()
	directory := t.TempDir()

	index := rand.Uint32()
	valueCount := rand.Int32Range(1000, 2000)
	keys := make([][]byte, valueCount)
	values := make([][]byte, valueCount)
	for i := 0; i < int(valueCount); i++ {
		key := rand.PrintableVariableBytes(1, 100)
		keys[i] = key
		values[i] = rand.PrintableVariableBytes(1, 100)
	}

	// a map from keys to values
	expectedValues := make(map[string][]byte)

	// a map from keys to addresses
	addressMap := make(map[string]types.Address)

	expectedLargestShardSize := uint64(0)

	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	seg, err := CreateSegment(
		logger,
		util.NewErrorMonitor(ctx, logger, nil),
		index,
		[]*SegmentPath{segmentPath},
		false,
		1,
		false,
		32)

	require.NoError(t, err)

	// Write values to the segment.
	for i := 0; i < int(valueCount); i++ {
		key := keys[i]
		value := values[i]
		expectedValues[string(key)] = value

		expectedLargestShardSize += uint64(len(value))

		_, _, err := seg.Write(&types.PutRequest{Key: key, Value: value})
		largestShardSize := seg.GetMaxShardSize()
		require.NoError(t, err)
		require.Equal(t, expectedLargestShardSize, largestShardSize)

		// Occasionally flush the segment to disk.
		if rand.BoolWithProbability(0.25) {
			flushFunction, err := seg.Flush()
			require.NoError(t, err)
			flushedKeys, err := flushFunction()
			require.NoError(t, err)
			for _, flushedKey := range flushedKeys {
				addressMap[string(flushedKey.Key)] = flushedKey.Address
			}

			// after flushing, the address map should be the same size as the expected values map
			require.Equal(t, len(expectedValues), len(addressMap))
		}

		// Occasionally scan all addresses and values in the segment.
		if rand.BoolWithProbability(0.1) {
			flushFunction, err := seg.Flush()
			require.NoError(t, err)
			flushedKeys, err := flushFunction()
			require.NoError(t, err)
			for _, flushedKey := range flushedKeys {
				addressMap[string(flushedKey.Key)] = flushedKey.Address
			}

			// after flushing, the address map should be the same size as the expected values map
			require.Equal(t, len(expectedValues), len(addressMap))

			for k, addr := range addressMap {
				readValue, err := seg.Read([]byte(k), addr)
				require.NoError(t, err)
				require.Equal(t, expectedValues[k], readValue)
			}
		}
	}

	// Seal the segment and read all keys and values.
	require.False(t, seg.IsSealed())
	sealTime := rand.Time()
	flushedKeys, err := seg.Seal(sealTime)
	require.NoError(t, err)
	require.True(t, seg.IsSealed())

	for _, flushedKey := range flushedKeys {
		addressMap[string(flushedKey.Key)] = flushedKey.Address
	}

	// after flushing, the address map should be the same size as the expected values map
	require.Equal(t, len(expectedValues), len(addressMap))

	require.Equal(t, sealTime.UnixNano(), seg.GetSealTime().UnixNano())

	for k, addr := range addressMap {
		readValue, err := seg.Read([]byte(k), addr)
		require.NoError(t, err)
		require.Equal(t, expectedValues[k], readValue)
	}

	keysFromSegment, err := seg.GetKeys()
	require.NoError(t, err)
	for i, ka := range keysFromSegment {
		require.Equal(t, ka.Key, keys[i])
	}

	// Reopen the segment and read all keys and values.
	seg2, err := LoadSegment(
		logger,
		util.NewErrorMonitor(ctx, logger, nil),
		index,
		[]*SegmentPath{segmentPath},
		false,
		time.Now(),
		false)
	require.NoError(t, err)
	require.True(t, seg2.IsSealed())

	require.Equal(t, sealTime.UnixNano(), seg2.GetSealTime().UnixNano())

	for k, addr := range addressMap {
		readValue, err := seg2.Read([]byte(k), addr)
		require.NoError(t, err)
		require.Equal(t, expectedValues[k], readValue)
	}

	keysFromSegment2, err := seg2.GetKeys()
	require.NoError(t, err)
	require.Equal(t, keysFromSegment, keysFromSegment2)

	// delete the segment
	require.Equal(t, 3, countFilesInDirectory(t, segmentPath.SegmentDirectory()))

	err = seg.delete()
	require.NoError(t, err)

	require.Equal(t, 0, countFilesInDirectory(t, segmentPath.SegmentDirectory()))
}

func TestWriteAndReadSegmentMultiShard(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	rand := util.NewTestRandom()
	logger := slog.Default()
	directory := t.TempDir()

	index := rand.Uint32()
	valueCount := rand.Int32Range(1000, 2000)
	shardCount := uint8(rand.Uint32Range(2, 32))
	keys := make([][]byte, valueCount)
	values := make([][]byte, valueCount)
	for i := 0; i < int(valueCount); i++ {
		key := rand.PrintableVariableBytes(1, 100)
		keys[i] = key
		values[i] = rand.PrintableVariableBytes(1, 100)
	}

	// a map from keys to values
	expectedValues := make(map[string][]byte)

	// a map from keys to addresses
	addressMap := make(map[string]types.Address)

	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	seg, err := CreateSegment(
		logger,
		util.NewErrorMonitor(ctx, logger, nil),
		index,
		[]*SegmentPath{segmentPath},
		false,
		shardCount,
		false,
		32)

	require.NoError(t, err)

	// Write values to the segment.
	for i := 0; i < int(valueCount); i++ {
		key := keys[i]
		value := values[i]
		expectedValues[string(key)] = value

		_, _, err := seg.Write(&types.PutRequest{Key: key, Value: value})
		require.NoError(t, err)
		largestShardSize := seg.GetMaxShardSize()
		require.True(t, largestShardSize >= uint64(len(value)))

		// Occasionally flush the segment to disk.
		if rand.BoolWithProbability(0.25) {
			flushFunction, err := seg.Flush()
			require.NoError(t, err)
			flushedKeys, err := flushFunction()
			require.NoError(t, err)
			for _, flushedKey := range flushedKeys {
				addressMap[string(flushedKey.Key)] = flushedKey.Address
			}

			// after flushing, the address map should be the same size as the expected values map
			require.Equal(t, len(expectedValues), len(addressMap))
		}

		// Occasionally scan all addresses and values in the segment.
		if rand.BoolWithProbability(0.1) {
			flushFunction, err := seg.Flush()
			require.NoError(t, err)
			flushedKeys, err := flushFunction()
			require.NoError(t, err)
			for _, flushedKey := range flushedKeys {
				addressMap[string(flushedKey.Key)] = flushedKey.Address
			}

			// after flushing, the address map should be the same size as the expected values map
			require.Equal(t, len(expectedValues), len(addressMap))

			for k, addr := range addressMap {
				readValue, err := seg.Read([]byte(k), addr)
				require.NoError(t, err)
				require.Equal(t, expectedValues[k], readValue)
			}
		}
	}

	// Seal the segment and read all keys and values.
	require.False(t, seg.IsSealed())
	sealTime := rand.Time()
	flushedKeys, err := seg.Seal(sealTime)
	require.NoError(t, err)
	require.True(t, seg.IsSealed())

	for _, flushedKey := range flushedKeys {
		addressMap[string(flushedKey.Key)] = flushedKey.Address
	}

	// after flushing, the address map should be the same size as the expected values map
	require.Equal(t, len(expectedValues), len(addressMap))

	require.Equal(t, sealTime.UnixNano(), seg.GetSealTime().UnixNano())

	for k, addr := range addressMap {
		readValue, err := seg.Read([]byte(k), addr)
		require.NoError(t, err)
		require.Equal(t, expectedValues[k], readValue)
	}

	keysFromSegment, err := seg.GetKeys()
	require.NoError(t, err)
	// Sort keys. With more than one shard, keys may have random order.
	sort.Slice(keys, func(i, j int) bool {
		return bytes.Compare(keys[i], keys[j]) < 0
	})
	sort.Slice(keysFromSegment, func(i, j int) bool {
		return bytes.Compare(keysFromSegment[i].Key, keysFromSegment[j].Key) < 0
	})
	for i, ka := range keysFromSegment {
		require.Equal(t, ka.Key, keys[i])
	}

	// Reopen the segment and read all keys and values.
	seg2, err := LoadSegment(
		logger,
		util.NewErrorMonitor(ctx, logger, nil),
		index,
		[]*SegmentPath{segmentPath},
		false,
		time.Now(),
		false)
	require.NoError(t, err)
	require.True(t, seg2.IsSealed())

	require.Equal(t, sealTime.UnixNano(), seg2.GetSealTime().UnixNano())

	for k, addr := range addressMap {
		readValue, err := seg2.Read([]byte(k), addr)
		require.NoError(t, err)
		require.Equal(t, expectedValues[k], readValue)
	}

	keysFromSegment2, err := seg2.GetKeys()
	sort.Slice(keysFromSegment2, func(i, j int) bool {
		return bytes.Compare(keysFromSegment2[i].Key, keysFromSegment2[j].Key) < 0
	})
	require.NoError(t, err)
	require.Equal(t, keysFromSegment, keysFromSegment2)

	// delete the segment
	require.Equal(t, int(2+shardCount), countFilesInDirectory(t, segmentPath.SegmentDirectory()))

	err = seg.delete()
	require.NoError(t, err)

	require.Equal(t, 0, countFilesInDirectory(t, segmentPath.SegmentDirectory()))
}

// Tests writing and reading, but allocates more shards than values written to force some shards to be empty.
func TestWriteAndReadColdShard(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	rand := util.NewTestRandom()
	logger := slog.Default()
	directory := t.TempDir()

	index := rand.Uint32()
	shardCount := uint8(rand.Uint32Range(2, 32))
	valueCount := shardCount * 2
	keys := make([][]byte, valueCount)
	values := make([][]byte, valueCount)
	for i := 0; i < int(valueCount); i++ {
		key := rand.PrintableVariableBytes(1, 100)
		keys[i] = key
		values[i] = rand.PrintableVariableBytes(1, 100)
	}

	// a map from keys to values
	expectedValues := make(map[string][]byte)

	// a map from keys to addresses
	addressMap := make(map[string]types.Address)

	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)
	seg, err := CreateSegment(
		logger,
		util.NewErrorMonitor(ctx, logger, nil),
		index,
		[]*SegmentPath{segmentPath},
		false,
		shardCount,
		false,
		32)

	require.NoError(t, err)

	// Write values to the segment.
	for i := 0; i < int(valueCount); i++ {
		key := keys[i]
		value := values[i]
		expectedValues[string(key)] = value

		_, _, err := seg.Write(&types.PutRequest{Key: key, Value: value})
		require.NoError(t, err)
		largestShardSize := seg.GetMaxShardSize()
		require.True(t, largestShardSize >= uint64(len(value)))
	}

	// Seal the segment and read all keys and values.
	require.False(t, seg.IsSealed())
	sealTime := rand.Time()
	flushedKeys, err := seg.Seal(sealTime)
	require.NoError(t, err)
	require.True(t, seg.IsSealed())

	for _, flushedKey := range flushedKeys {
		addressMap[string(flushedKey.Key)] = flushedKey.Address
	}

	// after flushing, the address map should be the same size as the expected values map
	require.Equal(t, len(expectedValues), len(addressMap))

	require.Equal(t, sealTime.UnixNano(), seg.GetSealTime().UnixNano())

	for k, addr := range addressMap {
		readValue, err := seg.Read([]byte(k), addr)
		require.NoError(t, err)
		require.Equal(t, expectedValues[k], readValue)
	}

	keysFromSegment, err := seg.GetKeys()
	require.NoError(t, err)
	// Sort keys. With more than one shard, keys may have random order.
	sort.Slice(keys, func(i, j int) bool {
		return bytes.Compare(keys[i], keys[j]) < 0
	})
	sort.Slice(keysFromSegment, func(i, j int) bool {
		return bytes.Compare(keysFromSegment[i].Key, keysFromSegment[j].Key) < 0
	})
	for i, ka := range keysFromSegment {
		require.Equal(t, ka.Key, keys[i])
	}

	// Reopen the segment and read all keys and values.
	seg2, err := LoadSegment(
		logger,
		util.NewErrorMonitor(ctx, logger, nil),
		index,
		[]*SegmentPath{segmentPath},
		false,
		time.Now(),
		false)
	require.NoError(t, err)
	require.True(t, seg2.IsSealed())

	require.Equal(t, sealTime.UnixNano(), seg2.GetSealTime().UnixNano())

	for k, addr := range addressMap {
		readValue, err := seg2.Read([]byte(k), addr)
		require.NoError(t, err)
		require.Equal(t, expectedValues[k], readValue)
	}

	keysFromSegment2, err := seg2.GetKeys()
	sort.Slice(keysFromSegment2, func(i, j int) bool {
		return bytes.Compare(keysFromSegment2[i].Key, keysFromSegment2[j].Key) < 0
	})
	require.NoError(t, err)
	require.Equal(t, keysFromSegment, keysFromSegment2)

	// delete the segment
	require.Equal(t, int(2+shardCount), countFilesInDirectory(t, segmentPath.SegmentDirectory()))

	err = seg.delete()
	require.NoError(t, err)

	require.Equal(t, 0, countFilesInDirectory(t, segmentPath.SegmentDirectory()))
}

func TestGetFilePaths(t *testing.T) {
	ctx := t.Context()
	rand := util.NewTestRandom()
	logger := slog.Default()
	errorMonitor := util.NewErrorMonitor(ctx, logger, nil)

	index := rand.Uint32()
	shardingFactor := uint8(rand.Uint32Range(1, 10))

	segmentPath, err := NewSegmentPath(t.TempDir(), "", "table")
	require.NoError(t, err)

	err = os.MkdirAll(segmentPath.SegmentDirectory(), 0755)
	require.NoError(t, err)

	segment, err := CreateSegment(
		logger,
		errorMonitor,
		index,
		[]*SegmentPath{segmentPath},
		false,
		shardingFactor,
		false,
		32)
	require.NoError(t, err)

	files := segment.GetFilePaths()
	filesSet := make(map[string]struct{})
	for _, file := range files {
		filesSet[file] = struct{}{}
	}

	expectedCount := 0

	// metadata
	_, found := filesSet[segment.metadata.path()]
	require.True(t, found)
	expectedCount++

	// key file
	_, found = filesSet[segment.keys.path()]
	require.True(t, found)
	expectedCount++

	// value files
	for i := uint8(0); i < shardingFactor; i++ {
		_, found = filesSet[segment.shards[i].path()]
		require.True(t, found)
		expectedCount++
	}

	// make sure there aren't any additional files
	require.Equal(t, expectedCount, len(filesSet))

	// Compare values to functions that return specific file paths.
	require.Equal(t, segment.metadata.path(), segment.GetMetadataFilePath())
	require.Equal(t, segment.keys.path(), segment.GetKeyFilePath())
	valueFiles := segment.GetValueFilePaths()
	for i := uint8(0); i < shardingFactor; i++ {
		require.Equal(t, segment.shards[i].path(), valueFiles[i])
	}
}

// TestRoundRobinShardAssignment writes exactly `valuesPerShard * shardingFactor` keys to a segment and verifies
// that each shard received exactly `valuesPerShard` of them, in round-robin insertion order. This is the core
// guarantee of the round-robin shard assignment scheme: it does not rely on the contents of the keys at all.
func TestRoundRobinShardAssignment(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	rand := util.NewTestRandom()
	logger := slog.Default()
	directory := t.TempDir()

	const shardingFactor uint8 = 7
	const valuesPerShard = 13
	const valueCount = int(shardingFactor) * valuesPerShard

	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	err = segmentPath.MakeDirectories(false)
	require.NoError(t, err)

	seg, err := CreateSegment(
		logger,
		util.NewErrorMonitor(ctx, logger, nil),
		rand.Uint32(),
		[]*SegmentPath{segmentPath},
		false,
		shardingFactor,
		false,
		32)
	require.NoError(t, err)

	// Capture the address that the segment assigns to each write, in insertion order.
	insertionOrderShards := make([]uint8, 0, valueCount)

	for i := 0; i < valueCount; i++ {
		key := rand.PrintableVariableBytes(8, 32)
		value := rand.PrintableVariableBytes(8, 32)
		_, _, err := seg.Write(&types.PutRequest{Key: key, Value: value})
		require.NoError(t, err)

		flushFn, err := seg.Flush()
		require.NoError(t, err)
		flushed, err := flushFn()
		require.NoError(t, err)
		// Each iteration above should produce exactly one new flushed key (the one we just wrote).
		require.Len(t, flushed, 1)
		insertionOrderShards = append(insertionOrderShards, flushed[0].Address.ShardID())
	}

	// The i-th key written should land in shard (i % shardingFactor).
	for i, gotShard := range insertionOrderShards {
		expectedShard := uint8(i) % shardingFactor
		require.Equal(t, expectedShard, gotShard,
			"value at insertion index %d landed in shard %d, expected shard %d",
			i, gotShard, expectedShard)
	}

	// And each shard should have received exactly valuesPerShard values.
	perShardCounts := make(map[uint8]int)
	for _, s := range insertionOrderShards {
		perShardCounts[s]++
	}
	require.Len(t, perShardCounts, int(shardingFactor))
	for s := uint8(0); s < shardingFactor; s++ {
		require.Equal(t, valuesPerShard, perShardCounts[s],
			"shard %d received %d values, expected %d", s, perShardCounts[s], valuesPerShard)
	}
}

// writeNoErr is a tiny wrapper that asserts seg.Write succeeded. seg.Write returns three values, so
// we cannot pass its result directly to require.NoError.
func writeNoErr(t *testing.T, seg *Segment, req *types.PutRequest) {
	t.Helper()
	_, _, err := seg.Write(req)
	require.NoError(t, err)
}

// newSingleShardSegment is a small test helper that creates a fresh single-shard segment for tests
// that need to control on-disk layout exactly. It returns the segment and the segment path so the
// caller can locate the on-disk files after the segment is sealed.
func newSingleShardSegment(t *testing.T) (*Segment, *SegmentPath, uint32) {
	t.Helper()
	rand := util.NewTestRandom()
	logger := slog.Default()
	directory := t.TempDir()
	index := rand.Uint32()

	segmentPath, err := NewSegmentPath(directory, "", "table")
	require.NoError(t, err)
	require.NoError(t, segmentPath.MakeDirectories(false))

	seg, err := CreateSegment(
		logger,
		util.NewErrorMonitor(t.Context(), logger, nil),
		index,
		[]*SegmentPath{segmentPath},
		false,
		1,
		false,
		32,
	)
	require.NoError(t, err)
	return seg, segmentPath, index
}

// keysByKey indexes a slice of ScopedKey by key bytes for easier lookup.
func keysByKey(keys []*types.ScopedKey) map[string]*types.ScopedKey {
	out := make(map[string]*types.ScopedKey, len(keys))
	for _, k := range keys {
		out[string(k.Key)] = k
	}
	return out
}

// TestSegmentSecondaryKeyAddresses verifies that a Put with a primary plus several secondaries
// produces one ScopedKey per key, that each Address reads back the correct (sub-)range of the
// stored value, that the per-record Kind tags match the group structure, and that a Put with no
// secondaries emits a single Standalone record.
func TestSegmentSecondaryKeyAddresses(t *testing.T) {
	t.Parallel()

	value := []byte("the quick brown fox jumps over the lazy dog")
	primaryKey := []byte("primary")
	// Mix of strict sub-range secondaries and one alias-the-whole-value secondary.
	sk1 := &types.SecondaryKey{Key: []byte("quick"), Offset: 4, Length: 5}  // "quick"
	sk2 := &types.SecondaryKey{Key: []byte("brown"), Offset: 10, Length: 5} // "brown"
	sk3 := &types.SecondaryKey{Key: []byte("whole"), Offset: 0, Length: uint32(len(value))}
	standaloneKey := []byte("standalone")
	standaloneValue := []byte("no-secondaries-here")

	seg, _, _ := newSingleShardSegment(t)

	_, _, err := seg.Write(&types.PutRequest{
		Key:           primaryKey,
		Value:         value,
		SecondaryKeys: []*types.SecondaryKey{sk1, sk2, sk3},
	})
	require.NoError(t, err)

	_, _, err = seg.Write(&types.PutRequest{Key: standaloneKey, Value: standaloneValue})
	require.NoError(t, err)

	flushedKeys, err := seg.Seal(time.Now())
	require.NoError(t, err)
	require.Len(t, flushedKeys, 5)

	byKey := keysByKey(flushedKeys)

	// Primary readback.
	primary := byKey[string(primaryKey)]
	require.NotNil(t, primary)
	require.Equal(t, types.KeyKindPrimary, primary.Kind)
	got, err := seg.Read(primary.Key, primary.Address)
	require.NoError(t, err)
	require.Equal(t, value, got)

	// Secondary readback.
	for i, sk := range []*types.SecondaryKey{sk1, sk2, sk3} {
		entry := byKey[string(sk.Key)]
		require.NotNil(t, entry, "secondary %d missing from flushed keys", i)
		require.Equal(t, sk.Length, entry.Address.ValueSize())
		got, err := seg.Read(entry.Key, entry.Address)
		require.NoError(t, err)
		require.Equal(t, value[sk.Offset:sk.Offset+sk.Length], got)
	}

	// Kind tagging on the group: middle secondaries are KeyKindSecondary, last is FinalSecondary.
	require.Equal(t, types.KeyKindSecondary, byKey["quick"].Kind)
	require.Equal(t, types.KeyKindSecondary, byKey["brown"].Kind)
	require.Equal(t, types.KeyKindFinalSecondary, byKey["whole"].Kind)

	// Standalone Put: single record tagged KeyKindStandalone.
	standalone := byKey[string(standaloneKey)]
	require.NotNil(t, standalone)
	require.Equal(t, types.KeyKindStandalone, standalone.Kind)
	got, err = seg.Read(standalone.Key, standalone.Address)
	require.NoError(t, err)
	require.Equal(t, standaloneValue, got)
}

// TestKeyFileKindRoundTrip writes one of each KeyKind through Segment.Write, seals, reloads via
// LoadSegment, and verifies via GetKeys that the on-disk record kinds round-trip exactly. This
// locks in the on-disk byte ordering for the future "last-durable-primary" iteration PR.
func TestKeyFileKindRoundTrip(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	seg, segmentPath, index := newSingleShardSegment(t)

	writeNoErr(t, seg, &types.PutRequest{
		Key:   []byte("standalone"),
		Value: []byte("v0"),
	})

	writeNoErr(t, seg, &types.PutRequest{
		Key:   []byte("p1"),
		Value: []byte("hello world"),
		SecondaryKeys: []*types.SecondaryKey{
			{Key: []byte("hello"), Offset: 0, Length: 5},
		},
	})

	writeNoErr(t, seg, &types.PutRequest{
		Key:   []byte("p2"),
		Value: []byte("alphabet"),
		SecondaryKeys: []*types.SecondaryKey{
			{Key: []byte("alpha"), Offset: 0, Length: 5},
			{Key: []byte("bet"), Offset: 5, Length: 3},
		},
	})

	_, err := seg.Seal(time.Now())
	require.NoError(t, err)

	// Reload from disk and verify the on-disk record kinds.
	seg2, err := LoadSegment(
		logger,
		util.NewErrorMonitor(t.Context(), logger, nil),
		index,
		[]*SegmentPath{segmentPath},
		false,
		time.Now(),
		false,
	)
	require.NoError(t, err)

	keys, err := seg2.GetKeys()
	require.NoError(t, err)
	require.Len(t, keys, 6)

	// Record order is insertion order within the single key file goroutine.
	expected := []struct {
		key  string
		kind types.KeyKind
	}{
		{"standalone", types.KeyKindStandalone},
		{"p1", types.KeyKindPrimary},
		{"hello", types.KeyKindFinalSecondary},
		{"p2", types.KeyKindPrimary},
		{"alpha", types.KeyKindSecondary},
		{"bet", types.KeyKindFinalSecondary},
	}
	for i, exp := range expected {
		require.Equal(t, exp.key, string(keys[i].Key), "record %d key mismatch", i)
		require.Equal(t, exp.kind, keys[i].Kind, "record %d kind mismatch (key=%s)", i, exp.key)
	}
}

// markSegmentUnsealed flips the sealed byte on the segment's metadata file from 1 back to 0,
// simulating a segment that crashed before it could write the sealed metadata. We can't use a
// running segment for this because the Seal call is what shuts down the segment's goroutines; the
// pattern is to fully seal, then reach into the file system and corrupt the metadata.
func markSegmentUnsealed(t *testing.T, segmentPath *SegmentPath, index uint32) {
	t.Helper()
	metaPath := path.Join(segmentPath.SegmentDirectory(), fmt.Sprintf("%d%s", index, MetadataFileExtension))
	data, err := os.ReadFile(metaPath)
	require.NoError(t, err)
	require.Equal(t, V3MetadataSize, len(data))
	data[V3MetadataSize-1] = 0
	require.NoError(t, os.WriteFile(metaPath, data, 0600))
}

// truncateKeyFileBy truncates the segment's key file by `bytes` bytes from the end.
func truncateKeyFileBy(t *testing.T, segmentPath *SegmentPath, index uint32, bytes int) {
	t.Helper()
	keyPath := path.Join(segmentPath.SegmentDirectory(), fmt.Sprintf("%d%s", index, KeyFileExtension))
	data, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(data), bytes)
	require.NoError(t, os.WriteFile(keyPath, data[:len(data)-bytes], 0600))
}

// truncateValueFileBy truncates the segment's value file for the given shard by `bytes` bytes
// from the end.
func truncateValueFileBy(t *testing.T, segmentPath *SegmentPath, index uint32, shard uint8, bytes int) {
	t.Helper()
	valPath := path.Join(segmentPath.SegmentDirectory(), fmt.Sprintf("%d-%d%s", index, shard, ValuesFileExtension))
	data, err := os.ReadFile(valPath)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(data), bytes)
	require.NoError(t, os.WriteFile(valPath, data[:len(data)-bytes], 0600))
}

// reloadSegmentExpectingRecovery reloads a segment after corrupting it. Returns the post-recovery
// key list (sorted by insertion order from the key file).
func reloadSegmentExpectingRecovery(t *testing.T, segmentPath *SegmentPath, index uint32) ([]*types.ScopedKey, *Segment) {
	t.Helper()
	logger := slog.Default()
	seg, err := LoadSegment(
		logger,
		util.NewErrorMonitor(t.Context(), logger, nil),
		index,
		[]*SegmentPath{segmentPath},
		false,
		time.Now(),
		false,
	)
	require.NoError(t, err)
	keys, err := seg.GetKeys()
	require.NoError(t, err)
	return keys, seg
}

// TestSealLoadedSegmentSingleShardPrefix locks the single-shard durability invariant: with one shard
// all values append to a single value file and all keys to a single key file in write order, so after
// a crash the surviving Put groups form a contiguous PREFIX of the write order — never a gapped
// subset. Each sub-case seals a segment, truncates one file to simulate a torn tail, reloads, and
// asserts the survivors are exactly key000..key{j-1}.
func TestSealLoadedSegmentSingleShardPrefix(t *testing.T) {
	t.Parallel()

	const (
		n        = 8
		valueLen = 10 // each value below is exactly 10 bytes
	)
	keyFor := func(i int) []byte { return []byte(fmt.Sprintf("key%03d", i)) }
	valueFor := func(i int) []byte { return []byte(fmt.Sprintf("val%07d", i)) }

	// writeRun writes n standalone Puts to a fresh single-shard segment, seals it, and flips the
	// metadata back to unsealed to simulate a crash before the seal completed.
	writeRun := func(t *testing.T) (*SegmentPath, uint32) {
		seg, segmentPath, index := newSingleShardSegment(t)
		for i := 0; i < n; i++ {
			writeNoErr(t, seg, &types.PutRequest{Key: keyFor(i), Value: valueFor(i)})
		}
		_, err := seg.Seal(time.Now())
		require.NoError(t, err)
		markSegmentUnsealed(t, segmentPath, index)
		return segmentPath, index
	}

	// assertPrefix asserts the recovered keys are exactly key000..key{survivors-1} in write order with
	// no gaps, and that every survivor's value range fits within the (possibly truncated) value file.
	assertPrefix := func(t *testing.T, keys []*types.ScopedKey, survivors int, valueFileSize int) {
		require.Len(t, keys, survivors)
		for i := 0; i < survivors; i++ {
			require.Equal(t, string(keyFor(i)), string(keys[i].Key), "record %d", i)
			end := int(keys[i].Address.Offset()) + int(keys[i].Address.ValueSize())
			require.LessOrEqual(t, end, valueFileSize, "survivor %d value must fit in the value file", i)
		}
	}

	t.Run("value_file_torn_mid_value", func(t *testing.T) {
		t.Parallel()
		segmentPath, index := writeRun(t)
		// Values occupy [i*10,(i+1)*10); total 80 bytes. Truncate to 55, landing inside value 5
		// ([50,60)). Survivors are the values whose end <= 55, i.e. key000..key004.
		const truncatedSize = 55
		truncateValueFileBy(t, segmentPath, index, 0, n*valueLen-truncatedSize)
		keys, _ := reloadSegmentExpectingRecovery(t, segmentPath, index)
		assertPrefix(t, keys, 5, truncatedSize)
	})

	t.Run("key_file_torn_mid_record", func(t *testing.T) {
		t.Parallel()
		segmentPath, index := writeRun(t)
		// Keys are fixed length, so every key record is the same size. Cut 3*r-1 bytes from the tail:
		// records for key005..key007 are removed (key005's record is left 1 byte short, so it is torn
		// and discarded). The value file is intact, so the survivors are bounded by the key-file
		// prefix: key000..key004.
		r := int(keyRecordSize(keyFor(0)))
		truncateKeyFileBy(t, segmentPath, index, 3*r-1)
		keys, _ := reloadSegmentExpectingRecovery(t, segmentPath, index)
		assertPrefix(t, keys, 5, n*valueLen)
	})
}

// TestSealLoadedSegmentGroupAtomicity covers all of the torn-write scenarios that
// sealLoadedSegment must handle. Each subtest builds a sealed segment, manually corrupts it on
// disk to simulate a crash mid-write, flips the metadata's sealed bit back to false, then reloads
// and asserts which keys are kept and which are dropped. The "all-or-nothing per group" invariant
// is the property under test.
func TestSealLoadedSegmentGroupAtomicity(t *testing.T) {
	t.Parallel()

	// Each test case writes a sequence of PutRequests, then describes how to corrupt the on-disk
	// files before recovery. expectedKeys lists the keys (in key-file order) that should survive.
	t.Run("clean_standalone_survives", func(t *testing.T) {
		t.Parallel()
		seg, segmentPath, index := newSingleShardSegment(t)
		writeNoErr(t, seg, &types.PutRequest{Key: []byte("k1"), Value: []byte("v1")})
		_, err := seg.Seal(time.Now())
		require.NoError(t, err)
		markSegmentUnsealed(t, segmentPath, index)

		keys, _ := reloadSegmentExpectingRecovery(t, segmentPath, index)
		require.Len(t, keys, 1)
		require.Equal(t, "k1", string(keys[0].Key))
		require.Equal(t, types.KeyKindStandalone, keys[0].Kind)
	})

	t.Run("clean_group_survives", func(t *testing.T) {
		t.Parallel()
		seg, segmentPath, index := newSingleShardSegment(t)
		writeNoErr(t, seg, &types.PutRequest{
			Key:   []byte("p"),
			Value: []byte("hello"),
			SecondaryKeys: []*types.SecondaryKey{
				{Key: []byte("he"), Offset: 0, Length: 2},
				{Key: []byte("llo"), Offset: 2, Length: 3},
			},
		})
		_, err := seg.Seal(time.Now())
		require.NoError(t, err)
		markSegmentUnsealed(t, segmentPath, index)

		keys, _ := reloadSegmentExpectingRecovery(t, segmentPath, index)
		require.Len(t, keys, 3)
		require.Equal(t, types.KeyKindPrimary, keys[0].Kind)
		require.Equal(t, types.KeyKindSecondary, keys[1].Kind)
		require.Equal(t, types.KeyKindFinalSecondary, keys[2].Kind)
	})

	t.Run("primary_without_terminator_discarded", func(t *testing.T) {
		t.Parallel()
		// A Put of primary + 2 secondaries with the key file truncated such that only the primary
		// record remains. The primary has Kind=KeyKindPrimary but no FinalSecondary closes it, so
		// the whole group must be discarded.
		seg, segmentPath, index := newSingleShardSegment(t)
		writeNoErr(t, seg, &types.PutRequest{
			Key:   []byte("p"),
			Value: []byte("hello"),
			SecondaryKeys: []*types.SecondaryKey{
				{Key: []byte("he"), Offset: 0, Length: 2},
				{Key: []byte("llo"), Offset: 2, Length: 3},
			},
		})
		_, err := seg.Seal(time.Now())
		require.NoError(t, err)

		secondaryRecBytes := int(keyRecordSize([]byte("he")) + keyRecordSize([]byte("llo")))
		truncateKeyFileBy(t, segmentPath, index, secondaryRecBytes)
		markSegmentUnsealed(t, segmentPath, index)

		keys, _ := reloadSegmentExpectingRecovery(t, segmentPath, index)
		require.Empty(t, keys)
	})

	t.Run("primary_plus_partial_secondaries_discarded", func(t *testing.T) {
		t.Parallel()
		// Primary + 2 secondaries, key file truncated to drop the FinalSecondary record. Group is
		// torn (no closing terminator), discard.
		seg, segmentPath, index := newSingleShardSegment(t)
		writeNoErr(t, seg, &types.PutRequest{
			Key:   []byte("p"),
			Value: []byte("hello"),
			SecondaryKeys: []*types.SecondaryKey{
				{Key: []byte("he"), Offset: 0, Length: 2},
				{Key: []byte("llo"), Offset: 2, Length: 3},
			},
		})
		_, err := seg.Seal(time.Now())
		require.NoError(t, err)

		truncateKeyFileBy(t, segmentPath, index, int(keyRecordSize([]byte("llo"))))
		markSegmentUnsealed(t, segmentPath, index)

		keys, _ := reloadSegmentExpectingRecovery(t, segmentPath, index)
		require.Empty(t, keys)
	})

	t.Run("partial_key_record_discarded", func(t *testing.T) {
		t.Parallel()
		// Truncate the file mid-record (cut into the middle of a key's bytes). readKeys will stop
		// at that point and recovery should not commit the in-flight group.
		seg, segmentPath, index := newSingleShardSegment(t)
		writeNoErr(t, seg, &types.PutRequest{
			Key:   []byte("standalone-kept"),
			Value: []byte("v0"),
		})
		writeNoErr(t, seg, &types.PutRequest{
			Key:   []byte("torn-primary"),
			Value: []byte("hello"),
			SecondaryKeys: []*types.SecondaryKey{
				{Key: []byte("torn-secondary"), Offset: 0, Length: 5},
			},
		})
		_, err := seg.Seal(time.Now())
		require.NoError(t, err)

		truncateKeyFileBy(t, segmentPath, index, 5)
		markSegmentUnsealed(t, segmentPath, index)

		keys, _ := reloadSegmentExpectingRecovery(t, segmentPath, index)
		require.Len(t, keys, 1)
		require.Equal(t, "standalone-kept", string(keys[0].Key))
	})

	t.Run("group_discarded_when_value_file_torn", func(t *testing.T) {
		t.Parallel()
		// Primary + secondaries written; we truncate the value file so the primary's address (the
		// one with the largest [offset, offset+len) span) no longer fits. The whole group must drop —
		// even though a short secondary at the front of the value would individually fit.
		seg, segmentPath, index := newSingleShardSegment(t)
		writeNoErr(t, seg, &types.PutRequest{
			Key:   []byte("standalone-kept"),
			Value: []byte("survivor"),
		})
		writeNoErr(t, seg, &types.PutRequest{
			Key:   []byte("torn-primary"),
			Value: []byte("hellooooo"),
			SecondaryKeys: []*types.SecondaryKey{
				{Key: []byte("he"), Offset: 0, Length: 2},
				{Key: []byte("oo"), Offset: 7, Length: 2},
			},
		})
		_, err := seg.Seal(time.Now())
		require.NoError(t, err)

		truncateValueFileBy(t, segmentPath, index, 0, 3)
		markSegmentUnsealed(t, segmentPath, index)

		keys, _ := reloadSegmentExpectingRecovery(t, segmentPath, index)
		require.Len(t, keys, 1)
		require.Equal(t, "standalone-kept", string(keys[0].Key))
	})

	t.Run("group_survives_when_value_file_complete", func(t *testing.T) {
		t.Parallel()
		seg, segmentPath, index := newSingleShardSegment(t)
		writeNoErr(t, seg, &types.PutRequest{
			Key:   []byte("p"),
			Value: []byte("hellooooo"),
			SecondaryKeys: []*types.SecondaryKey{
				{Key: []byte("he"), Offset: 0, Length: 2},
				{Key: []byte("oo"), Offset: 7, Length: 2},
			},
		})
		_, err := seg.Seal(time.Now())
		require.NoError(t, err)
		markSegmentUnsealed(t, segmentPath, index)

		keys, _ := reloadSegmentExpectingRecovery(t, segmentPath, index)
		require.Len(t, keys, 3)
	})

	t.Run("two_clean_groups_plus_torn_third", func(t *testing.T) {
		t.Parallel()
		seg, segmentPath, index := newSingleShardSegment(t)
		writeNoErr(t, seg, &types.PutRequest{Key: []byte("first"), Value: []byte("v1")})
		writeNoErr(t, seg, &types.PutRequest{
			Key:   []byte("second-primary"),
			Value: []byte("hi"),
			SecondaryKeys: []*types.SecondaryKey{
				{Key: []byte("second-secondary"), Offset: 0, Length: 2},
			},
		})
		writeNoErr(t, seg, &types.PutRequest{
			Key:   []byte("third-primary"),
			Value: []byte("hi"),
			SecondaryKeys: []*types.SecondaryKey{
				{Key: []byte("third-secondary"), Offset: 0, Length: 2},
			},
		})
		_, err := seg.Seal(time.Now())
		require.NoError(t, err)

		truncateKeyFileBy(t, segmentPath, index, int(keyRecordSize([]byte("third-secondary"))))
		markSegmentUnsealed(t, segmentPath, index)

		keys, _ := reloadSegmentExpectingRecovery(t, segmentPath, index)
		require.Len(t, keys, 3)
		require.Equal(t, "first", string(keys[0].Key))
		require.Equal(t, "second-primary", string(keys[1].Key))
		require.Equal(t, "second-secondary", string(keys[2].Key))
	})
}
