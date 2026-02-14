package evm

import (
	"bytes"
	"errors"
)

const (
	addressLen = 20
	slotLen    = 32
)

// EVM key prefixes — mirrored from x/evm/types/keys.go.
// These are immutable on-disk format markers; changing them would break
// all existing state, so duplicating here is safe and avoids pulling in the
// heavy x/evm/types dependency (which transitively imports cosmos-sdk).
var (
	stateKeyPrefix    = []byte{0x03}
	codeKeyPrefix     = []byte{0x07}
	codeHashKeyPrefix = []byte{0x08}
	codeSizeKeyPrefix = []byte{0x09}
	nonceKeyPrefix    = []byte{0x0a}
)

// StateKeyPrefix returns the storage state key prefix (0x03).
// Exported for callers that need the raw prefix (e.g. iterator bounds).
func StateKeyPrefix() []byte { return stateKeyPrefix }

var (
	// ErrMalformedEVMKey indicates invalid EVM key encoding.
	ErrMalformedEVMKey = errors.New("sei-db: malformed evm key")
)

// EVMKeyKind identifies an EVM key family.
type EVMKeyKind uint8

const (
	EVMKeyEmpty    EVMKeyKind = iota // Returned only for zero-length keys
	EVMKeyNonce                      // Stripped key: 20-byte address
	EVMKeyCodeHash                   // Stripped key: 20-byte address
	EVMKeyCode                       // Stripped key: 20-byte address
	EVMKeyStorage                    // Stripped key: addr||slot (20+32 bytes)
	EVMKeyLegacy                     // Full original key preserved (address mappings, codesize, etc.)
)

// EVMKeyUnknown is an alias for EVMKeyEmpty, used by FlatKV to test for
// unrecognised/empty keys.
const EVMKeyUnknown = EVMKeyEmpty

// ParseEVMKey parses an EVM key from the x/evm store keyspace.
//
// For optimized keys (nonce, code, codehash, storage), keyBytes is the stripped key.
// For legacy keys (all other EVM data including codesize), keyBytes is the full original key.
// Only returns EVMKeyEmpty for zero-length keys.
func ParseEVMKey(key []byte) (kind EVMKeyKind, keyBytes []byte) {
	if len(key) == 0 {
		return EVMKeyEmpty, nil
	}

	switch {
	case bytes.HasPrefix(key, nonceKeyPrefix):
		if len(key) != len(nonceKeyPrefix)+addressLen {
			return EVMKeyLegacy, key // Malformed but still EVM data
		}
		return EVMKeyNonce, key[len(nonceKeyPrefix):]

	case bytes.HasPrefix(key, codeHashKeyPrefix):
		if len(key) != len(codeHashKeyPrefix)+addressLen {
			return EVMKeyLegacy, key
		}
		return EVMKeyCodeHash, key[len(codeHashKeyPrefix):]

	case bytes.HasPrefix(key, codeKeyPrefix):
		if len(key) != len(codeKeyPrefix)+addressLen {
			return EVMKeyLegacy, key
		}
		return EVMKeyCode, key[len(codeKeyPrefix):]

	case bytes.HasPrefix(key, stateKeyPrefix):
		if len(key) != len(stateKeyPrefix)+addressLen+slotLen {
			return EVMKeyLegacy, key
		}
		return EVMKeyStorage, key[len(stateKeyPrefix):]
	}

	// All other EVM keys go to legacy store (address mappings, codesize, etc.)
	return EVMKeyLegacy, key
}

// BuildMemIAVLEVMKey builds a memiavl key from internal bytes.
// This is the reverse of ParseEVMKey for optimized key types.
//
// NOTE: This is primarily used for tests and temporary compatibility.
// FlatKV stores data in internal format; this function converts back to
// memiavl format for Iterator/Exporter output.
func BuildMemIAVLEVMKey(kind EVMKeyKind, keyBytes []byte) []byte {
	var prefix []byte
	switch kind {
	case EVMKeyStorage:
		prefix = stateKeyPrefix
	case EVMKeyNonce:
		prefix = nonceKeyPrefix
	case EVMKeyCodeHash:
		prefix = codeHashKeyPrefix
	case EVMKeyCode:
		prefix = codeKeyPrefix
	default:
		return nil
	}

	result := make([]byte, 0, len(prefix)+len(keyBytes))
	result = append(result, prefix...)
	result = append(result, keyBytes...)
	return result
}

// InternalKeyLen returns the expected internal key length for a given kind.
// Used for validation in Iterator and tests.
func InternalKeyLen(kind EVMKeyKind) int {
	switch kind {
	case EVMKeyStorage:
		return addressLen + slotLen // 52 bytes
	case EVMKeyNonce, EVMKeyCodeHash, EVMKeyCode:
		return addressLen // 20 bytes
	default:
		return 0
	}
}
