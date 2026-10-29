package undo

import (
	"encoding/binary"
	"fmt"
	"math"
	"slices"

	"github.com/cockroachdb/pebble/v2"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
)

// The type of a record, which follows its bucket. An account's records hold its balance, nonce and
// code hash together.
const (
	storageRecord byte = 0x03
	codeRecord    byte = 0x07
	accountRecord byte = 0x0a
)

const (
	bucketLen  = 8
	heightLen  = 8
	addressLen = keys.AddressLen
	wordLen    = 32

	storageKeyLen = 53 // type[1] | address[20] | slot[32]

	// Prefixes exclude the trailing height[8].
	addressPrefixLen = 29 // bucket[8] | type[1] | address[20]
	storagePrefixLen = 61 // bucket[8] | type[1] | address[20] | slot[32]

	// An account record's value is empty for an account that did not exist, and otherwise
	// balance[32] | nonce[8], followed by codeHash[32] when the account held code.
	accountNoCodeLen = wordLen + 8
	accountLen       = accountNoCodeLen + wordLen

	// Metadata sorts above every bucket reachable by an int64 block height.
	metadataBucket = math.MaxUint64
)

// Comparer orders bucket | type | address | optional slot | height byte-wise.
var Comparer = func() *pebble.Comparer {
	c := *pebble.DefaultComparer
	c.Name = "ss_undolog_evm_comparator_v3"
	c.Split = splitKey
	// The store only uses point keys, not range keys or NextPrefix.
	c.ImmediateSuccessor = nil
	return &c
}()

// splitKey returns the prefix length excluding a record's height.
func splitKey(k []byte) int {
	// Bucket boundaries and metadata have no height suffix.
	if len(k) <= bucketLen || binary.BigEndian.Uint64(k) == metadataBucket {
		return len(k)
	}
	// Shortened index keys may end before the height's fixed offset.
	switch k[bucketLen] {
	case storageRecord:
		return min(len(k), storagePrefixLen)
	case codeRecord, accountRecord:
		return min(len(k), addressPrefixLen)
	default:
		return len(k)
	}
}

// recordTypeOf returns the type of the records holding the history of an EVM key of kind, and false
// for a kind the store keeps no history of. An account field's history is its account's.
func recordTypeOf(kind keys.EVMKeyKind) (byte, bool) {
	switch kind {
	case keys.EVMKeyBalance, keys.EVMKeyNonce, keys.EVMKeyCodeHash:
		return accountRecord, true
	case keys.EVMKeyCode:
		return codeRecord, true
	case keys.EVMKeyStorage:
		return storageRecord, true
	default:
		return 0, false
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
