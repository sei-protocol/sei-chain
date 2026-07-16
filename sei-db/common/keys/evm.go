package keys

import (
	"bytes"
)

// AddressLen is the length in bytes of an EVM address (20 bytes, eth-style).
// Exported so that other packages (e.g. common/rand, benchmarks) can share a
// single canonical definition instead of maintaining their own copies.
const AddressLen = 20

const slotLen = 32

// FlatKVStoreKey is the module name used when exporting/importing data from
// the FlatKV backend. Treated as a separate module in state-sync snapshots
// so that import routes data exclusively to FlatKV.
const FlatKVStoreKey = "flatkv"

// EVM key prefixes — mirrored from x/evm/types/keys.go.
// These are immutable on-disk format markers; changing them would break
// all existing state, so duplicating here is safe and avoids pulling in the
// heavy x/evm/types dependency (which transitively imports cosmos-sdk).
var (
	stateKeyPrefix    = []byte{0x03}
	codeKeyPrefix     = []byte{0x07}
	codeHashKeyPrefix = []byte{0x08}
	nonceKeyPrefix    = []byte{0x0a}
)

// StateKeyPrefix returns the storage state key prefix (0x03).
// Exported for callers that need the raw prefix (e.g. iterator bounds).
func StateKeyPrefix() []byte { return stateKeyPrefix }

// EVMKeyKind identifies an EVM key family.
type EVMKeyKind uint8

const (
	EVMKeyEmpty    EVMKeyKind = iota // Returned only for zero-length keys
	EVMKeyNonce                      // Stripped key: 20-byte address
	EVMKeyCodeHash                   // Stripped key: 20-byte address
	EVMKeyCode                       // Stripped key: 20-byte address
	EVMKeyStorage                    // Stripped key: addr||slot (20+32 bytes)
	EVMKeyMisc                       // Full original key preserved (address mappings, codesize, etc.)
)

// ParseEVMKey parses an EVM key from the x/evm store keyspace.
//
// For optimized keys (nonce, code, codehash, storage), keyBytes is the stripped key.
// For misc keys (all other EVM data including codesize), keyBytes is the full original key.
// Only returns EVMKeyEmpty for zero-length keys.
func ParseEVMKey(key []byte) (kind EVMKeyKind, keyBytes []byte) {
	if len(key) == 0 {
		return EVMKeyEmpty, nil
	}

	switch {
	case bytes.HasPrefix(key, nonceKeyPrefix):
		if len(key) != len(nonceKeyPrefix)+AddressLen {
			return EVMKeyMisc, key // Malformed but still EVM data
		}
		return EVMKeyNonce, key[len(nonceKeyPrefix):]

	case bytes.HasPrefix(key, codeHashKeyPrefix):
		if len(key) != len(codeHashKeyPrefix)+AddressLen {
			return EVMKeyMisc, key
		}
		return EVMKeyCodeHash, key[len(codeHashKeyPrefix):]

	case bytes.HasPrefix(key, codeKeyPrefix):
		if len(key) != len(codeKeyPrefix)+AddressLen {
			return EVMKeyMisc, key
		}
		return EVMKeyCode, key[len(codeKeyPrefix):]

	case bytes.HasPrefix(key, stateKeyPrefix):
		if len(key) != len(stateKeyPrefix)+AddressLen+slotLen {
			return EVMKeyMisc, key
		}
		return EVMKeyStorage, key[len(stateKeyPrefix):]
	}

	// All other EVM keys go to the misc store (address mappings, codesize, etc.)
	return EVMKeyMisc, key
}

// EVMKeyPrefixByte returns the single-byte on-disk prefix for a given key kind.
// Returns (0, false) for kinds that have no fixed prefix (e.g. EVMKeyMisc).
func EVMKeyPrefixByte(kind EVMKeyKind) (byte, bool) {
	switch kind {
	case EVMKeyStorage:
		return stateKeyPrefix[0], true
	case EVMKeyNonce:
		return nonceKeyPrefix[0], true
	case EVMKeyCodeHash:
		return codeHashKeyPrefix[0], true
	case EVMKeyCode:
		return codeKeyPrefix[0], true
	default:
		return 0, false
	}
}

// BuildEVMKey builds a memiavl key from internal bytes.
// This is the reverse of ParseEVMKey for optimized key types.
//
// NOTE: This is primarily used for tests and temporary compatibility.
// FlatKV stores data in internal format; this function converts back to
// memiavl format for Iterator/Exporter output.
func BuildEVMKey(kind EVMKeyKind, keyBytes []byte) []byte {
	prefix, ok := EVMKeyPrefixByte(kind)
	if !ok {
		return nil
	}
	result := make([]byte, 1+len(keyBytes))
	result[0] = prefix
	copy(result[1:], keyBytes)
	return result
}

// InternalKeyLen returns the expected internal key length for a given kind.
// Used for validation in Iterator and tests.
func InternalKeyLen(kind EVMKeyKind) int {
	switch kind {
	case EVMKeyStorage:
		return AddressLen + slotLen // 52 bytes
	case EVMKeyNonce, EVMKeyCodeHash, EVMKeyCode:
		return AddressLen // 20 bytes
	default:
		return 0
	}
}
