package hashvault

import (
	"encoding/binary"
	"fmt"
)

// recordFormatVersion is the format version every value this package writes starts with.
const recordFormatVersion byte = 1

// hashSize is the length of a recorded block hash.
const hashSize = 32

// keySize is the length of a key: a block number, big-endian.
const keySize = 8

// valueSize is the length of a value: the format version, then the hash.
const valueSize = 1 + hashSize

// encodeKey returns the key a block's hash is recorded under.
func encodeKey(blockNumber uint64) []byte {
	key := make([]byte, keySize)
	binary.BigEndian.PutUint64(key, blockNumber)
	return key
}

// decodeKey returns the block number a key records.
func decodeKey(key []byte) (uint64, error) {
	if len(key) != keySize {
		return 0, fmt.Errorf("hash vault key is %d bytes, expected %d", len(key), keySize)
	}
	return binary.BigEndian.Uint64(key), nil
}

// encodeValue returns the value a hash is recorded as.
func encodeValue(hash [32]byte) []byte {
	value := make([]byte, valueSize)
	value[0] = recordFormatVersion
	copy(value[1:], hash[:])
	return value
}

// decodeValue returns the hash a value records.
func decodeValue(value []byte) ([32]byte, error) {
	var hash [32]byte
	if len(value) != valueSize {
		return hash, fmt.Errorf("hash vault value is %d bytes, expected %d", len(value), valueSize)
	}
	if value[0] != recordFormatVersion {
		return hash, fmt.Errorf("hash vault value has format version %d, expected %d",
			value[0], recordFormatVersion)
	}
	copy(hash[:], value[1:])
	return hash, nil
}
