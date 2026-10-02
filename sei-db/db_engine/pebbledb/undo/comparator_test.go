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

func heightSuffix(height uint64) []byte {
	s := make([]byte, heightLen+1)
	binary.BigEndian.PutUint64(s, height)
	s[heightLen] = suffixLen
	return s
}

func recordPrefix(bucket uint64, key []byte) []byte {
	full := appendRecordKey(nil, bucket, key, 0)
	return full[:splitKey(full)]
}

func TestComparerPassesPebbleCheck(t *testing.T) {
	prefixes := [][]byte{
		recordPrefix(0, []byte{0x03, 0xaa}),
		recordPrefix(0, []byte{0x03, 0xaa, 0x00}),
		recordPrefix(1, []byte{0x0a}),
		recordPrefix(7, []byte{}),
		bucketBoundary(0),
		bucketBoundary(1),
		metadataKey("latest"),
	}
	suffixes := [][]byte{heightSuffix(1), heightSuffix(2), heightSuffix(1 << 40)}
	require.NoError(t, pebble.CheckComparer(Comparer, prefixes, suffixes))
}

func TestCompareMatchesPrefixThenSuffixOrder(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	randomKey := func() []byte {
		key := make([]byte, rng.IntN(4))
		for i := range key {
			// A small alphabet, zero included, so that keys often share prefixes.
			key[i] = byte(rng.IntN(3))
		}
		bucket := uint64(rng.IntN(3))
		if rng.IntN(4) == 0 {
			return bucketBoundary(bucket)
		}
		return appendRecordKey(nil, bucket, key, uint64(rng.IntN(4)))
	}
	for range 20_000 {
		a, b := randomKey(), randomKey()
		ap, bp := a[:splitKey(a)], b[:splitKey(b)]
		want := bytes.Compare(ap, bp)
		if want == 0 {
			want = bytes.Compare(a[splitKey(a):], b[splitKey(b):])
		}
		require.Equal(t, want, compareKeys(a, b), "a=%x b=%x", a, b)
		require.Equal(t, want == 0, Comparer.Equal(a, b))
	}
}

func TestRecordsOfABucketSitBetweenItsBoundaries(t *testing.T) {
	keys := [][]byte{{}, {0x00}, {0xff, 0xff}, bytes.Repeat([]byte{0xff}, 60)}
	for bucket := uint64(0); bucket < 3; bucket++ {
		lower, upper := bucketBoundary(bucket), bucketBoundary(bucket+1)
		for _, key := range keys {
			for _, height := range []uint64{0, 1, bucket*10 + 9, 1<<63 - 1} {
				record := appendRecordKey(nil, bucket, key, height)
				require.Negative(t, compareKeys(lower, record))
				require.Negative(t, compareKeys(record, upper))
			}
		}
	}
	// Metadata sorts above every bucket a height reaches, so no excise range covers it.
	lastBucket := uint64(math.MaxInt64)
	require.Negative(t, compareKeys(appendRecordKey(nil, lastBucket, bytes.Repeat([]byte{0xff}, 60), math.MaxInt64),
		bucketBoundary(lastBucket+1)))
	require.Negative(t, compareKeys(bucketBoundary(lastBucket+1), metadataKey("bucket_size")))
}

func TestRecordsOfAKeyAscendByHeight(t *testing.T) {
	var records [][]byte
	for _, height := range []uint64{12, 3, 1 << 33, 0, 255, 256} {
		records = append(records, appendRecordKey(nil, 4, []byte{0x03, 0x01}, height))
	}
	slices.SortFunc(records, compareKeys)
	var heights []uint64
	for _, r := range records {
		h, err := decodeRecordHeight(r)
		require.NoError(t, err)
		heights = append(heights, h)
	}
	require.Equal(t, []uint64{0, 3, 12, 255, 256, 1 << 33}, heights)
}

func TestSeparatorAndSuccessorStayInOrder(t *testing.T) {
	rng := rand.New(rand.NewPCG(3, 4))
	randomRecord := func() []byte {
		key := make([]byte, 1+rng.IntN(6))
		for i := range key {
			key[i] = byte(rng.IntN(256))
		}
		return appendRecordKey(nil, uint64(rng.IntN(3)), key, uint64(rng.IntN(100)))
	}
	for range 5_000 {
		a, b := randomRecord(), randomRecord()
		if compareKeys(a, b) > 0 {
			a, b = b, a
		}
		if compareKeys(a, b) < 0 {
			sep := Comparer.Separator(nil, a, b)
			require.LessOrEqual(t, compareKeys(a, sep), 0, "a=%x sep=%x", a, sep)
			require.Negative(t, compareKeys(sep, b), "sep=%x b=%x", sep, b)
		}
		succ := Comparer.Successor(nil, a)
		require.LessOrEqual(t, compareKeys(a, succ), 0, "a=%x succ=%x", a, succ)
	}
}

func TestSplitFindsThePrefixOfEveryKeyShape(t *testing.T) {
	record := appendRecordKey(nil, 2, []byte{0x09}, 77)
	require.Equal(t, len(record)-heightLen-1, splitKey(record))
	boundary := bucketBoundary(2)
	require.Equal(t, len(boundary), splitKey(boundary))
	meta := metadataKey("latest")
	require.Equal(t, len(meta), splitKey(meta))
}
