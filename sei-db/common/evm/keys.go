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
	EVMKeyCodeSize
	EVMKeyStorage
	EVMKeyLegacy // Catch-all for other EVM keys (address mappings)
)

// ParseEVMKey parses an EVM key from the x/evm store keyspace.
//
// For optimized keys (nonce, code, codehash, storage), keyBytes is the stripped key.
// For legacy keys (all other EVM data), keyBytes is the full original key.
// Only returns EVMKeyUnknown for empty keys.
func ParseEVMKey(key []byte) (kind EVMKeyKind, keyBytes []byte) {
	if len(key) == 0 {
		return EVMKeyUnknown, nil
	}

	switch {
	case bytes.HasPrefix(key, evmtypes.NonceKeyPrefix):
		if len(key) != len(evmtypes.NonceKeyPrefix)+addressLen {
			return EVMKeyLegacy, key // Malformed but still EVM data
		}
		return EVMKeyNonce, key[len(evmtypes.NonceKeyPrefix):]

	case bytes.HasPrefix(key, evmtypes.CodeHashKeyPrefix):
		if len(key) != len(evmtypes.CodeHashKeyPrefix)+addressLen {
			return EVMKeyLegacy, key
		}
		return EVMKeyCodeHash, key[len(evmtypes.CodeHashKeyPrefix):]

	case bytes.HasPrefix(key, evmtypes.CodeKeyPrefix):
		if len(key) != len(evmtypes.CodeKeyPrefix)+addressLen {
			return EVMKeyLegacy, key
		}
		return EVMKeyCode, key[len(evmtypes.CodeKeyPrefix):]

	case bytes.HasPrefix(key, evmtypes.CodeSizeKeyPrefix):
		if len(key) != len(evmtypes.CodeSizeKeyPrefix)+addressLen {
			return EVMKeyLegacy, key
		}
		return EVMKeyCodeSize, key[len(evmtypes.CodeSizeKeyPrefix):]

	case bytes.HasPrefix(key, evmtypes.StateKeyPrefix):
		if len(key) != len(evmtypes.StateKeyPrefix)+addressLen+slotLen {
			return EVMKeyLegacy, key
		}
		return EVMKeyStorage, key[len(evmtypes.StateKeyPrefix):]
	}

	// All other EVM keys go to legacy store (address mappings: 0x01, 0x02)
	return EVMKeyLegacy, key
}
