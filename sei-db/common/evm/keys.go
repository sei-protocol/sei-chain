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
	EVMKeyEmpty    EVMKeyKind = iota // Returned only for zero-length keys
	EVMKeyNonce                      // Stripped key: 20-byte address
	EVMKeyCodeHash                   // Stripped key: 20-byte address
	EVMKeyCode                       // Stripped key: 20-byte address
	EVMKeyStorage                    // Stripped key: addr||slot (20+32 bytes)
	EVMKeyLegacy                     // Full original key preserved (address mappings, codesize, etc.)
)

// ParseEVMKey parses an EVM key from the x/evm store keyspace.
//
// For optimized keys (nonce, code, codehash, storage), keyBytes is the stripped key.
// For legacy keys (all other EVM data), keyBytes is the full original key.
// Only returns EVMKeyEmpty for zero-length keys.
func ParseEVMKey(key []byte) (kind EVMKeyKind, keyBytes []byte) {
	if len(key) == 0 {
		return EVMKeyEmpty, nil
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

	case bytes.HasPrefix(key, evmtypes.StateKeyPrefix):
		if len(key) != len(evmtypes.StateKeyPrefix)+addressLen+slotLen {
			return EVMKeyLegacy, key
		}
		return EVMKeyStorage, key[len(evmtypes.StateKeyPrefix):]
	}

	// All other EVM keys go to legacy store (address mappings, codesize, etc.)
	return EVMKeyLegacy, key
}
