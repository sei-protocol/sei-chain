package undo

import (
	"bytes"
	"encoding/binary"
	"math"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
)

// testKeys are keys of several lengths, among them keys that are a byte prefix of another and a key of the longest
// length a record holds.
func testKeys() [][]byte {
	return [][]byte{
		{0x00},
		{0xff},
		{0x00, 0x00},
		[]byte("bank/a"),
		[]byte("bank/ab"),
		append([]byte("evm/\x03"), bytes.Repeat([]byte{0xff}, 52)...),
		bytes.Repeat([]byte{0x01}, maxKeyLen),
	}
}

// randomKey returns a short key over a small alphabet, so keys often share bytes and extend one another.
func randomKey(rng *rand.Rand) []byte {
	key := make([]byte, 1+rng.IntN(6))
	for i := range key {
		key[i] = byte(rng.IntN(3))
	}
	return key
}

func randomRecord(rng *rand.Rand) []byte {
	return appendRecordKey(nil, uint64(rng.IntN(3)), randomKey(rng), uint64(rng.IntN(1000)))
}

// recordPrefix returns the prefix of key's records in bucket: everything but the height.
func recordPrefix(bucket uint64, key []byte) []byte {
	record := appendRecordKey(nil, bucket, key, 0)
	return record[:len(record)-heightLen]
}

func requirePrefixOrder(t *testing.T, a, b []byte) {
	t.Helper()
	an, bn := splitKey(a), splitKey(b)
	want := bytes.Compare(a[:an], b[:bn])
	if want == 0 {
		want = bytes.Compare(a[an:], b[bn:])
	}
	require.Equal(t, want, Comparer.Compare(a, b), "a=%x b=%x", a, b)
	require.Equal(t, want == 0, Comparer.Equal(a, b))
}

func TestComparerPrefixAndSuffixOrder(t *testing.T) {
	// CheckComparer also removes arbitrary leading bytes to test synthetic prefixes.
	// Our bucket/length offsets require the pinned format without that feature.
	opts := newPebbleOptions(nil)
	require.Less(t, opts.FormatMajorVersion, pebble.FormatSyntheticPrefixSuffix)

	var keys [][]byte
	for _, bucket := range []uint64{0, 1, math.MaxInt64} {
		keys = append(keys, bucketBoundary(bucket))
		for _, key := range testKeys() {
			keys = append(keys, recordPrefix(bucket, key))
			for _, height := range []uint64{0, 1, 255, 256, 1 << 40, math.MaxInt64} {
				keys = append(keys, appendRecordKey(nil, bucket, key, height))
			}
		}
	}
	keys = append(keys, nil, metadataKey("latest"), metadataKey("bucket_size"))
	for _, a := range keys {
		for _, b := range keys {
			requirePrefixOrder(t, a, b)
		}
	}

	rng := rand.New(rand.NewPCG(1, 2))
	for range 20_000 {
		requirePrefixOrder(t, randomRecord(rng), randomRecord(rng))
	}
}

func TestRecordEncoding(t *testing.T) {
	for _, key := range testKeys() {
		want := binary.BigEndian.AppendUint64(nil, 2)
		want = binary.BigEndian.AppendUint16(want, uint16(len(key))) //nolint:gosec // test keys fit
		want = append(want, key...)
		want = binary.BigEndian.AppendUint64(want, 77)
		record := appendRecordKey(nil, 2, key, 77)
		require.Equal(t, want, record)
		require.Len(t, record, bucketLen+keyLenLen+len(key)+heightLen)
		require.Equal(t, len(record)-heightLen, splitKey(record))
		height, err := decodeRecordHeight(record)
		require.NoError(t, err)
		require.Equal(t, uint64(77), height)
		for _, invalid := range [][]byte{record[:len(record)-1], append(bytes.Clone(record), 0)} {
			_, err := decodeRecordHeight(invalid)
			require.Error(t, err)
		}
	}
	for _, prefix := range [][]byte{nil, bucketBoundary(2), metadataKey("latest"), metadataKey("bucket_size")} {
		require.Equal(t, len(prefix), splitKey(prefix))
		_, err := decodeRecordHeight(prefix)
		require.Error(t, err)
	}
	// A record whose length claims more key bytes than it holds is not a record.
	record := appendRecordKey(nil, 0, []byte{1, 2, 3}, 1)
	binary.BigEndian.PutUint16(record[bucketLen:], 200)
	_, err := decodeRecordHeight(record)
	require.Error(t, err)
}

func TestCheckKeyBoundsTheKeyLength(t *testing.T) {
	require.Error(t, checkKey(nil))
	require.Error(t, checkKey(make([]byte, maxKeyLen+1)))
	require.NoError(t, checkKey([]byte{0}))
	require.NoError(t, checkKey(make([]byte, maxKeyLen)))
}

func TestRecordsOfABucketSitBetweenItsBoundaries(t *testing.T) {
	for _, bucket := range []uint64{0, 1, math.MaxInt64} {
		for _, key := range testKeys() {
			for _, height := range []uint64{0, 1, math.MaxInt64} {
				record := appendRecordKey(nil, bucket, key, height)
				require.Negative(t, Comparer.Compare(bucketBoundary(bucket), record))
				require.Negative(t, Comparer.Compare(record, bucketBoundary(bucket+1)))
			}
		}
	}
	require.Negative(t, Comparer.Compare(bucketBoundary(math.MaxInt64+1), metadataKey("bucket_size")))
}

func TestRecordsOfAKeyAscendByHeight(t *testing.T) {
	for _, key := range testKeys() {
		var records [][]byte
		for _, height := range []uint64{12, 3, 1 << 33, 0, 255, 256} {
			records = append(records, appendRecordKey(nil, 4, key, height))
		}
		slices.SortFunc(records, Comparer.Compare)
		var heights []uint64
		for _, r := range records {
			h, err := decodeRecordHeight(r)
			require.NoError(t, err)
			heights = append(heights, h)
		}
		require.Equal(t, []uint64{0, 3, 12, 255, 256, 1 << 33}, heights)
	}
}

// A key's records stay together even when another key extends it, so a prefix seek finds them all.
func TestRecordsOfAKeyStayTogetherWhenAnotherKeyExtendsIt(t *testing.T) {
	short, long := []byte("bank/a"), []byte("bank/a\x00")
	var records [][]byte
	for _, height := range []uint64{0, 1, 0xff, 1 << 40} {
		records = append(records, appendRecordKey(nil, 0, short, height), appendRecordKey(nil, 0, long, height))
	}
	slices.SortFunc(records, Comparer.Compare)
	var lengths []int
	for _, r := range records {
		lengths = append(lengths, int(binary.BigEndian.Uint16(r[bucketLen:])))
	}
	require.Equal(t, []int{6, 6, 6, 6, 7, 7, 7, 7}, lengths)
}

func TestSeparatorAndSuccessorStayInOrder(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	for range 5_000 {
		a, b := randomRecord(rng), randomRecord(rng)
		if rng.IntN(2) == 0 {
			// Also exercise separators shortened within the height suffix.
			b = bytes.Clone(a)
			binary.BigEndian.PutUint64(b[len(b)-heightLen:], uint64(rng.IntN(1000)))
		}
		if Comparer.Compare(a, b) > 0 {
			a, b = b, a
		}
		if Comparer.Compare(a, b) < 0 {
			sep := Comparer.Separator(nil, a, b)
			require.LessOrEqual(t, Comparer.Compare(a, sep), 0, "a=%x sep=%x", a, sep)
			require.Negative(t, Comparer.Compare(sep, b), "sep=%x b=%x", sep, b)
			requirePrefixOrder(t, a, sep)
			requirePrefixOrder(t, sep, b)
		}
		succ := Comparer.Successor(nil, a)
		require.LessOrEqual(t, Comparer.Compare(a, succ), 0, "a=%x succ=%x", a, succ)
		requirePrefixOrder(t, a, succ)
	}
}
