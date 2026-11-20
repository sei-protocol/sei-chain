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
	keyLenLen = 4
	heightLen = 8

	// Metadata sorts above every bucket reachable by an int64 block height.
	metadataBucket = math.MaxUint64
)

// Comparer orders bucket | key length | key | height byte-wise. The key length makes a record's prefix
// self-delimiting, so byte order agrees with ordering by prefix and then by height.
var Comparer = func() *pebble.Comparer {
	c := *pebble.DefaultComparer
	c.Name = "ss_undolog_comparator_v3"
	c.Split = splitKey
	// The store only uses point keys, not range keys or NextPrefix.
	c.ImmediateSuccessor = nil
	return &c
}()

// splitKey returns the prefix length excluding a record's height.
func splitKey(k []byte) int {
	// Bucket boundaries and metadata have no height suffix.
	if len(k) < bucketLen+keyLenLen || binary.BigEndian.Uint64(k) == metadataBucket {
		return len(k)
	}
	// Shortened index keys may end before the height's offset.
	prefixLen := uint64(bucketLen+keyLenLen) + uint64(binary.BigEndian.Uint32(k[bucketLen:]))
	return int(min(uint64(len(k)), prefixLen)) //nolint:gosec // bounded by len(k)
}

// recordKeyLen returns the encoded length of a record for key.
func recordKeyLen(key []byte) int {
	return bucketLen + keyLenLen + len(key) + heightLen
}

// encodeRecordKey writes bucket | len(key) | key | height into dst.
func encodeRecordKey(dst []byte, bucket uint64, key []byte, height uint64) {
	binary.BigEndian.PutUint64(dst, bucket)
	binary.BigEndian.PutUint32(dst[bucketLen:], uint32(len(key))) //nolint:gosec // state keys are far below 4 GiB
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
