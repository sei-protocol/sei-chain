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

var evmTypes = []byte{StateKeyPrefix[0], CodeKeyPrefix[0], CodeHashKeyPrefix[0], NonceKeyPrefix[0], BalanceKeyPrefix[0]}

func evmKey(kind, fill byte) []byte {
	size, _ := keyLayout([]byte{kind})
	key := bytes.Repeat([]byte{fill}, size)
	key[0] = kind
	return key
}

func randomRecord(rng *rand.Rand) []byte {
	key := evmKey(evmTypes[rng.IntN(len(evmTypes))], 0)
	for i := 1; i < len(key); i++ {
		key[i] = byte(rng.IntN(3))
	}
	return appendRecordKey(nil, uint64(rng.IntN(3)), key, uint64(rng.IntN(1000)))
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
	// Our bucket/type offsets require the pinned format without that feature.
	opts := newPebbleOptions(nil)
	require.Less(t, opts.FormatMajorVersion, pebble.FormatSyntheticPrefixSuffix)

	var keys [][]byte
	for _, bucket := range []uint64{0, 1, math.MaxInt64} {
		keys = append(keys, bucketBoundary(bucket))
		for _, kind := range evmTypes {
			for _, fill := range []byte{0, 0xff} {
				key := evmKey(kind, fill)
				prefix := append(bucketBoundary(bucket), key...)
				keys = append(keys, prefix)
				for _, height := range []uint64{0, 1, 255, 256, 1 << 40, math.MaxInt64} {
					keys = append(keys, appendRecordKey(nil, bucket, key, height))
				}
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
	for _, kind := range evmTypes {
		key := evmKey(kind, 0xff)
		want := binary.BigEndian.AppendUint64(nil, 2)
		want = append(want, key...)
		want = binary.BigEndian.AppendUint64(want, 77)
		record := appendRecordKey(nil, 2, key, 77)
		require.Equal(t, want, record)
		size := 37
		if kind == StateKeyPrefix[0] {
			size = 69
		}
		require.Len(t, record, size)
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
	_, err := decodeRecordHeight(appendRecordKey(nil, 0, []byte{0xff}, 1))
	require.Error(t, err)
}

func TestRecordsOfABucketSitBetweenItsBoundaries(t *testing.T) {
	for _, bucket := range []uint64{0, 1, math.MaxInt64} {
		for _, kind := range evmTypes {
			for _, fill := range []byte{0, 0xff} {
				for _, height := range []uint64{0, 1, math.MaxInt64} {
					record := appendRecordKey(nil, bucket, evmKey(kind, fill), height)
					require.Negative(t, Comparer.Compare(bucketBoundary(bucket), record))
					require.Negative(t, Comparer.Compare(record, bucketBoundary(bucket+1)))
				}
			}
		}
	}
	require.Negative(t, Comparer.Compare(bucketBoundary(math.MaxInt64+1), metadataKey("bucket_size")))
}

func TestRecordsOfAKeyAscendByHeight(t *testing.T) {
	for _, kind := range evmTypes {
		var records [][]byte
		for _, height := range []uint64{12, 3, 1 << 33, 0, 255, 256} {
			records = append(records, appendRecordKey(nil, 4, evmKey(kind, 1), height))
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
