package ktype

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStorageKey(t *testing.T) {
	var addr Address
	addr[0] = 0x01

	slot := Slot{0x02}
	sk := StorageKey(addr, slot)
	require.Len(t, sk, AddressLen+SlotLen)
	require.Equal(t, byte(0x01), sk[0])
	require.Equal(t, byte(0x02), sk[AddressLen])
}

func TestPrefixEnd(t *testing.T) {
	tests := []struct {
		name   string
		prefix []byte
		expect []byte
	}{
		{"nil", nil, nil},
		{"empty", []byte{}, nil},
		{"simple", []byte{0x01}, []byte{0x02}},
		{"carry", []byte{0x01, 0xFF}, []byte{0x02}},
		{"multi-carry", []byte{0x01, 0xFF, 0xFF}, []byte{0x02}},
		{"all-ff", []byte{0xFF, 0xFF}, nil},
		{"mixed", []byte{0xAA, 0xFF, 0x05}, []byte{0xAA, 0xFF, 0x06}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := PrefixEnd(tc.prefix)
			require.Equal(t, tc.expect, got)
		})
	}
}
