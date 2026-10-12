package undo

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"

	"github.com/cockroachdb/pebble/v2"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
)

var (
	StateKeyPrefix    = []byte{0x03}
	CodeKeyPrefix     = []byte{0x07}
	CodeHashKeyPrefix = []byte{0x08}
	NonceKeyPrefix    = []byte{0x0a}
	BalanceKeyPrefix  = []byte{0x21}
)

const (
	bucketLen  = 8
	heightLen  = 8
	addressLen = keys.AddressLen
	wordLen    = 32

	addressKeyLen = 21 // type[1] | address[20]
	storageKeyLen = 53 // type[1] | address[20] | slot[32]

	// Prefixes exclude the trailing height[8].
	addressPrefixLen = 29 // bucket[8] | type[1] | address[20]
	storagePrefixLen = 61 // bucket[8] | type[1] | address[20] | slot[32]

	// Metadata sorts above every bucket reachable by an int64 block height.
	metadataBucket = math.MaxUint64
)

// Comparer orders bucket | type | address | optional slot | height byte-wise.
var Comparer = func() *pebble.Comparer {
	c := *pebble.DefaultComparer
	c.Name = "ss_undolog_evm_comparator_v2"
	c.Split = splitKey
	// The store only uses point keys, not range keys or NextPrefix.
	c.ImmediateSuccessor = nil
	return &c
}()

// keyLayout returns the logical key and value lengths for a supported EVM key.
// Unknown types return zero lengths; code has a variable value length of -1.
func keyLayout(key []byte) (keyLen, valueLen int) {
	if len(key) == 0 {
		return 0, 0
	}
	switch key[0] {
	case StateKeyPrefix[0]:
		return storageKeyLen, wordLen
	case CodeKeyPrefix[0]:
		return addressKeyLen, -1
	case CodeHashKeyPrefix[0], BalanceKeyPrefix[0]:
		return addressKeyLen, wordLen
	case NonceKeyPrefix[0]:
		return addressKeyLen, 8
	default:
		return 0, 0
	}
}

// splitKey returns the prefix length excluding a record's height.
func splitKey(k []byte) int {
	// Bucket boundaries and metadata have no height suffix.
	if len(k) <= bucketLen || binary.BigEndian.Uint64(k) == metadataBucket {
		return len(k)
	}
	// Shortened index keys may end before the height's fixed offset.
	switch k[bucketLen] {
	case StateKeyPrefix[0]:
		return min(len(k), storagePrefixLen)
	case CodeKeyPrefix[0], CodeHashKeyPrefix[0], NonceKeyPrefix[0], BalanceKeyPrefix[0]:
		return min(len(k), addressPrefixLen)
	default:
		return len(k)
	}
}

// recordKeyLen returns the encoded length of a record for key.
func recordKeyLen(key []byte) int {
	return bucketLen + len(key) + heightLen
}

// encodeRecordKey writes bucket | key | height into dst.
func encodeRecordKey(dst []byte, bucket uint64, key []byte, height uint64) {
	binary.BigEndian.PutUint64(dst, bucket)
	n := bucketLen + copy(dst[bucketLen:], key)
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
