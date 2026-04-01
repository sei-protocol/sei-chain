package flatkv

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFlatKVPrefixEnd(t *testing.T) {
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

func TestFlatKVTypeConversions(t *testing.T) {
	t.Run("AddressFromBytes", func(t *testing.T) {
		valid := make([]byte, AddressLen)
		_, ok := AddressFromBytes(valid)
		require.True(t, ok)

		invalid := make([]byte, AddressLen-1)
		_, ok = AddressFromBytes(invalid)
		require.False(t, ok)
	})

	t.Run("SlotFromBytes", func(t *testing.T) {
		valid := make([]byte, SlotLen)
		_, ok := SlotFromBytes(valid)
		require.True(t, ok)

		invalid := make([]byte, SlotLen+1)
		_, ok = SlotFromBytes(invalid)
		require.False(t, ok)
	})
}

func TestIsMetaKey(t *testing.T) {
	require.True(t, isMetaKey(metaVersionKey))
	require.True(t, isMetaKey(metaLtHashKey))
	require.True(t, isMetaKey([]byte("_meta/future")))
	require.False(t, isMetaKey([]byte{0x00}))
	require.False(t, isMetaKey(AccountKey(Address{0x01})))
	require.False(t, isMetaKey(StorageKey(Address{0x01}, Slot{0x02})))
}
