package undo

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"

	"github.com/cockroachdb/pebble/v2"
)

const (
	bucketLen = 8
	keyLenLen = 2
	heightLen = 8

	// maxKeyLen is the length of the longest key a record can hold.
	maxKeyLen = math.MaxUint16

	// Metadata sorts above every bucket reachable by an int64 block height.
	metadataBucket = math.MaxUint64
)

// Comparer orders record keys, bucket | key length | key | height, byte-wise.
var Comparer = func() *pebble.Comparer {
	c := *pebble.DefaultComparer
	c.Name = "ss_undolog_comparator_v3"
	c.Split = splitKey
	// The store only uses point keys, not range keys or NextPrefix.
	c.ImmediateSuccessor = nil
	return &c
}()

// checkKey returns an error for a key no record can hold: an empty one, or one longer than maxKeyLen.
func checkKey(key []byte) error {
	if len(key) == 0 || len(key) > maxKeyLen {
		return fmt.Errorf("undo: key of %d bytes is outside 1..%d", len(key), maxKeyLen)
	}
	return nil
}

// splitKey returns the prefix length excluding a record's height.
func splitKey(k []byte) int {
	// Bucket boundaries and metadata have no height suffix.
	if len(k) <= bucketLen+keyLenLen || binary.BigEndian.Uint64(k) == metadataBucket {
		return len(k)
	}
	// Shortened index keys may end before the height.
	return min(len(k), bucketLen+keyLenLen+int(binary.BigEndian.Uint16(k[bucketLen:])))
}

// recordKeyLen returns the encoded length of a record for key.
func recordKeyLen(key []byte) int {
	return bucketLen + keyLenLen + len(key) + heightLen
}

// encodeRecordKey writes bucket | key length | key | height into dst.
func encodeRecordKey(dst []byte, bucket uint64, key []byte, height uint64) {
	binary.BigEndian.PutUint64(dst, bucket)
	binary.BigEndian.PutUint16(dst[bucketLen:], uint16(len(key))) //nolint:gosec // checkKey bounds the length
	n := bucketLen + keyLenLen + copy(dst[bucketLen+keyLenLen:], key)
	binary.BigEndian.PutUint64(dst[n:], height)
}

// appendRecordKey appends the encoded record key to dst.
func appendRecordKey(dst []byte, bucket uint64, key []byte, height uint64) []byte {
	n, size := len(dst), recordKeyLen(key)
	dst = slices.Grow(dst, size)[:n+size]
	encodeRecordKey(dst[n:], bucket, key, height)
	return dst
}

// decodeRecordHeight returns the block height of a record key.
func decodeRecordHeight(k []byte) (uint64, error) {
	prefixLen := splitKey(k)
	if len(k)-prefixLen != heightLen {
		return 0, fmt.Errorf("undo: key %x is not an undo record", k)
	}
	return binary.BigEndian.Uint64(k[prefixLen:]), nil
}

// bucketBoundary returns the exclusive upper bound of all preceding buckets.
func bucketBoundary(bucket uint64) []byte {
	return binary.BigEndian.AppendUint64(nil, bucket)
}

// metadataKey returns the key of a metadata value.
func metadataKey(name string) []byte {
	return append(bucketBoundary(metadataBucket), name...)
}
