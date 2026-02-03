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
