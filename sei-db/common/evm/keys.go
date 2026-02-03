package evm

import (
	"bytes"
	"errors"

	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

const (
	addressLen = 20
	slotLen    = 32
)

var (
	// ErrMalformedEVMKey indicates invalid EVM key encoding.
	ErrMalformedEVMKey = errors.New("sei-db: malformed evm key")
)

// EVMKeyKind identifies an EVM key family.
type EVMKeyKind uint8

const (
	EVMKeyUnknown EVMKeyKind = iota
	EVMKeyNonce
	EVMKeyCodeHash
	EVMKeyCode
	EVMKeyStorage
	EVMKeyCodeSize // Parsed but not stored by FlatKV (computed from len(Code))
)

// ParseEVMKey parses an EVM key from the x/evm store keyspace.
//
// For non-storage keys, keyBytes is the 20-byte address.
// For storage keys, keyBytes is addr||slot (20+32 bytes).
// For unknown or malformed keys, returns (EVMKeyUnknown, nil).
func ParseEVMKey(key []byte) (kind EVMKeyKind, keyBytes []byte) {
	if len(key) == 0 {
		return EVMKeyUnknown, nil
	}

	switch {
	case bytes.HasPrefix(key, evmtypes.NonceKeyPrefix):
		if len(key) != len(evmtypes.NonceKeyPrefix)+addressLen {
			return EVMKeyUnknown, nil
		}
		return EVMKeyNonce, key[len(evmtypes.NonceKeyPrefix):]

	case bytes.HasPrefix(key, evmtypes.CodeHashKeyPrefix):
		if len(key) != len(evmtypes.CodeHashKeyPrefix)+addressLen {
			return EVMKeyUnknown, nil
		}
		return EVMKeyCodeHash, key[len(evmtypes.CodeHashKeyPrefix):]

	case bytes.HasPrefix(key, evmtypes.CodeSizeKeyPrefix):
		if len(key) != len(evmtypes.CodeSizeKeyPrefix)+addressLen {
			return EVMKeyUnknown, nil
		}
		return EVMKeyCodeSize, key[len(evmtypes.CodeSizeKeyPrefix):]

	case bytes.HasPrefix(key, evmtypes.CodeKeyPrefix):
		if len(key) != len(evmtypes.CodeKeyPrefix)+addressLen {
			return EVMKeyUnknown, nil
		}
		return EVMKeyCode, key[len(evmtypes.CodeKeyPrefix):]

	case bytes.HasPrefix(key, evmtypes.StateKeyPrefix):
		if len(key) != len(evmtypes.StateKeyPrefix)+addressLen+slotLen {
			return EVMKeyUnknown, nil
		}
		return EVMKeyStorage, key[len(evmtypes.StateKeyPrefix):]
	}

	return EVMKeyUnknown, nil
}

// BuildMemIAVLEVMKey builds a memiavl key from internal bytes.
// This is the reverse of ParseEVMKey.
//
// NOTE: This is primarily used for tests and temporary compatibility.
// FlatKV stores data in internal format; this function converts back to
// memiavl format for Iterator/Exporter output. In a future refactor,
// FlatKV may use its own export format and this function could be removed.
func BuildMemIAVLEVMKey(kind EVMKeyKind, keyBytes []byte) []byte {
	var prefix []byte
	switch kind {
	case EVMKeyStorage:
		prefix = evmtypes.StateKeyPrefix
	case EVMKeyNonce:
		prefix = evmtypes.NonceKeyPrefix
	case EVMKeyCodeHash:
		prefix = evmtypes.CodeHashKeyPrefix
	case EVMKeyCode:
		prefix = evmtypes.CodeKeyPrefix
	case EVMKeyCodeSize:
		prefix = evmtypes.CodeSizeKeyPrefix
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
	case EVMKeyNonce, EVMKeyCodeHash, EVMKeyCodeSize, EVMKeyCode:
		return addressLen // 20 bytes
	default:
		return 0
	}
}
