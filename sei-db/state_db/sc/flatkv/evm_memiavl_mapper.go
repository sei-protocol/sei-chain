package flatkv

import (
	"bytes"
	"errors"
	"fmt"

	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
)

var (
	// ErrMalformedMemIAVLKey indicates invalid EVM key encoding.
	ErrMalformedMemIAVLKey = errors.New("flatkv: malformed memiavl evm key")
)

// EVMKeyKind identifies a memiavl EVM key family.
type EVMKeyKind uint8

const (
	EVMKeyUnknown EVMKeyKind = iota
	EVMKeyNonce
	EVMKeyCodeHash
	EVMKeyCode
	EVMKeyCodeSize
	EVMKeyStorage
)

// ParseEVMKey parses a memiavl EVM key (x/evm store keyspace) into a typed selector.
//
// For non-storage keys, slot is returned as zero value.
// For unknown prefixes, returns (EVMKeyUnknown, zero, zero, nil).
func ParseEVMKey(key []byte) (kind EVMKeyKind, addr Address, slot Slot, err error) {
	if len(key) == 0 {
		return EVMKeyUnknown, Address{}, Slot{}, fmt.Errorf("%w: empty key", ErrMalformedMemIAVLKey)
	}

	switch {
	case bytes.HasPrefix(key, evmtypes.NonceKeyPrefix):
		if len(key) != len(evmtypes.NonceKeyPrefix)+AddressLen {
			return EVMKeyUnknown, Address{}, Slot{}, fmt.Errorf("%w: nonce key len=%d", ErrMalformedMemIAVLKey, len(key))
		}
		a, ok := AddressFromBytes(key[len(evmtypes.NonceKeyPrefix):])
		if !ok {
			return EVMKeyUnknown, Address{}, Slot{}, fmt.Errorf("%w: nonce addr len=%d", ErrMalformedMemIAVLKey, AddressLen)
		}
		return EVMKeyNonce, a, Slot{}, nil

	case bytes.HasPrefix(key, evmtypes.CodeHashKeyPrefix):
		if len(key) != len(evmtypes.CodeHashKeyPrefix)+AddressLen {
			return EVMKeyUnknown, Address{}, Slot{}, fmt.Errorf("%w: codehash key len=%d", ErrMalformedMemIAVLKey, len(key))
		}
		a, ok := AddressFromBytes(key[len(evmtypes.CodeHashKeyPrefix):])
		if !ok {
			return EVMKeyUnknown, Address{}, Slot{}, fmt.Errorf("%w: codehash addr len=%d", ErrMalformedMemIAVLKey, AddressLen)
		}
		return EVMKeyCodeHash, a, Slot{}, nil

	case bytes.HasPrefix(key, evmtypes.CodeKeyPrefix):
		if len(key) != len(evmtypes.CodeKeyPrefix)+AddressLen {
			return EVMKeyUnknown, Address{}, Slot{}, fmt.Errorf("%w: code key len=%d", ErrMalformedMemIAVLKey, len(key))
		}
		a, ok := AddressFromBytes(key[len(evmtypes.CodeKeyPrefix):])
		if !ok {
			return EVMKeyUnknown, Address{}, Slot{}, fmt.Errorf("%w: code addr len=%d", ErrMalformedMemIAVLKey, AddressLen)
		}
		return EVMKeyCode, a, Slot{}, nil

	case bytes.HasPrefix(key, evmtypes.CodeSizeKeyPrefix):
		if len(key) != len(evmtypes.CodeSizeKeyPrefix)+AddressLen {
			return EVMKeyUnknown, Address{}, Slot{}, fmt.Errorf("%w: codesize key len=%d", ErrMalformedMemIAVLKey, len(key))
		}
		a, ok := AddressFromBytes(key[len(evmtypes.CodeSizeKeyPrefix):])
		if !ok {
			return EVMKeyUnknown, Address{}, Slot{}, fmt.Errorf("%w: codesize addr len=%d", ErrMalformedMemIAVLKey, AddressLen)
		}
		return EVMKeyCodeSize, a, Slot{}, nil

	case bytes.HasPrefix(key, evmtypes.StateKeyPrefix):
		if len(key) != len(evmtypes.StateKeyPrefix)+AddressLen+SlotLen {
			return EVMKeyUnknown, Address{}, Slot{}, fmt.Errorf("%w: storage key len=%d", ErrMalformedMemIAVLKey, len(key))
		}
		off := len(evmtypes.StateKeyPrefix)
		a, ok := AddressFromBytes(key[off : off+AddressLen])
		if !ok {
			return EVMKeyUnknown, Address{}, Slot{}, fmt.Errorf("%w: storage addr len=%d", ErrMalformedMemIAVLKey, AddressLen)
		}
		s, ok := SlotFromBytes(key[off+AddressLen:])
		if !ok {
			return EVMKeyUnknown, Address{}, Slot{}, fmt.Errorf("%w: storage slot len=%d", ErrMalformedMemIAVLKey, SlotLen)
		}
		return EVMKeyStorage, a, s, nil
	}

	return EVMKeyUnknown, Address{}, Slot{}, nil
}
