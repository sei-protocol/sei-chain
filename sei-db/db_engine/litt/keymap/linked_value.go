package keymap

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
)

// latestKeyMetaKey is a reserved metadata key that stores the most recently written user key.
// The \x00 prefix ensures it cannot collide with normal user keys (which are store-prefixed).
var latestKeyMetaKey = []byte("\x00__litt_latest")

const prevKeyLenSize = 4

// encodeLinkedValue encodes an address and prevKey into the linked-list value format.
//
//	[Address (13 bytes)][prevKeyLen (4 bytes)][prevKey (N bytes)]
//
// When prevKey is nil, prevKeyLen is 0 and no prevKey bytes are written.
func encodeLinkedValue(address types.Address, prevKey []byte) []byte {
	if len(prevKey) > math.MaxUint32 {
		panic(fmt.Sprintf("prevKey length %d exceeds uint32 max", len(prevKey)))
	}
	size := types.AddressLength + prevKeyLenSize + len(prevKey)
	buf := make([]byte, size)
	address.SerializeInto(buf[:types.AddressLength])
	binary.BigEndian.PutUint32(buf[types.AddressLength:types.AddressLength+prevKeyLenSize], uint32(len(prevKey))) //nolint:gosec
	if len(prevKey) > 0 {
		copy(buf[types.AddressLength+prevKeyLenSize:], prevKey)
	}
	return buf
}

// decodeLinkedValue decodes the address and prevKey from a keymap value.
// Handles both legacy 13-byte values (Address only, no linked-list info) and
// new linked-list values (Address + prevKeyLen + prevKey).
func decodeLinkedValue(data []byte) (address types.Address, prevKey []byte, err error) {
	if len(data) < types.AddressLength {
		return types.Address{}, nil, fmt.Errorf("invalid data length: %d", len(data))
	}

	address, err = types.DeserializeAddress(data[:types.AddressLength])
	if err != nil {
		return types.Address{}, nil, fmt.Errorf("failed to deserialize address: %w", err)
	}

	if len(data) == types.AddressLength {
		return address, nil, nil
	}

	if len(data) < types.AddressLength+prevKeyLenSize {
		return types.Address{}, nil, fmt.Errorf("invalid linked value length: %d", len(data))
	}

	prevKeyLen := binary.BigEndian.Uint32(data[types.AddressLength : types.AddressLength+prevKeyLenSize])
	expectedLen := types.AddressLength + prevKeyLenSize + int(prevKeyLen)
	if len(data) != expectedLen {
		return types.Address{}, nil,
			fmt.Errorf("linked value length mismatch: got %d, expected %d", len(data), expectedLen)
	}

	if prevKeyLen > 0 {
		prevKey = make([]byte, prevKeyLen)
		copy(prevKey, data[types.AddressLength+prevKeyLenSize:])
	}

	return address, prevKey, nil
}
