package ktype

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
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

func TestAppendPhysicalKeysMatchAllocating(t *testing.T) {
	moduleCases := []struct {
		moduleName string
		key        []byte
	}{
		{moduleName: "evm", key: []byte{0x01, 0x02}},
		{moduleName: "", key: []byte{0x03}},
		{moduleName: "bank", key: []byte{}},
		{moduleName: "", key: nil},
	}
	for _, tc := range moduleCases {
		require.Equal(t, ModulePhysicalKey(tc.moduleName, tc.key),
			AppendModulePhysicalKey(nil, tc.moduleName, tc.key))
	}

	kinds := []keys.EVMKeyKind{
		keys.EVMKeyStorage,
		keys.EVMKeyCode,
		keys.EVMKeyNonce,
		keys.EVMKeyCodeHash,
		keys.EVMKeyBalance,
	}
	key := []byte{0x04, 0x05}
	for _, kind := range kinds {
		require.Equal(t, EVMPhysicalKey(kind, key), AppendEVMPhysicalKey(nil, kind, key))
	}

	prefix := []byte("prefix")
	require.Equal(t, append(append([]byte{}, prefix...), ModulePhysicalKey("module", key)...),
		AppendModulePhysicalKey(prefix, "module", key))
	require.Equal(t, append(append([]byte{}, prefix...), EVMPhysicalKey(keys.EVMKeyCode, key)...),
		AppendEVMPhysicalKey(prefix, keys.EVMKeyCode, key))
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
