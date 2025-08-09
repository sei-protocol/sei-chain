package types

import (
	"encoding/hex"
	"fmt"
	"strings"
)

// HexBytes is a wrapper around []byte that encodes data as hexadecimal strings
// for use in JSON.
type HexBytes []byte

// Marshal needed for protobuf compatibility
func (bz HexBytes) Marshal() ([]byte, error) {
	return bz, nil
}

// Unmarshal needed for protobuf compatibility
func (bz *HexBytes) Unmarshal(data []byte) error {
	*bz = data
	return nil
}

// Bytes fulfills various interfaces in light-client, etc...
func (bz HexBytes) Bytes() []byte {
	return bz
}
func (bz HexBytes) String() string {
	return strings.ToUpper(hex.EncodeToString(bz))
}

// Format writes either address of 0th element in a slice in base 16 notation,
// with leading 0x (%p), or casts HexBytes to bytes and writes as hexadecimal
// string to s.
func (bz HexBytes) Format(s fmt.State, verb rune) {
	switch verb {
	case 'p':
		s.Write([]byte(fmt.Sprintf("%p", bz)))
	default:
		s.Write([]byte(fmt.Sprintf("%X", []byte(bz))))
	}
}
