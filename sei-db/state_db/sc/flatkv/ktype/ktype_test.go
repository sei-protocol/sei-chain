package ktype

import (
	"bytes"
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

func TestWritePhysicalKeysToBufferMatchBuild(t *testing.T) {
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
		require.Equal(t, BuildPhysicalKey(tc.moduleName, tc.key),
			WritePhysicalKeyToBuffer(nil, tc.moduleName, tc.key))
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
		require.Equal(t, BuildPhysicalEVMKey(kind, key), WritePhysicalEVMKeyToBuffer(nil, kind, key))
	}
}

func TestWritePhysicalKeysToBufferOverwriteAndReuseBuffer(t *testing.T) {
	key := []byte{0x04, 0x05}
	buf := make([]byte, 0, 64)
	buf = append(buf, "stale contents"...)

	written := WritePhysicalKeyToBuffer(buf, "module", key)
	require.Equal(t, BuildPhysicalKey("module", key), written)
	require.Same(t, &buf[0], &written[0], "a buffer with enough capacity must be reused")

	written = WritePhysicalEVMKeyToBuffer(buf, keys.EVMKeyCode, key)
	require.Equal(t, BuildPhysicalEVMKey(keys.EVMKeyCode, key), written)
	require.Same(t, &buf[0], &written[0], "a buffer with enough capacity must be reused")
}

func TestParseLogicalKey(t *testing.T) {
	address := bytes.Repeat([]byte{0x11}, AddressLen)
	slotKey := append(bytes.Clone(address), bytes.Repeat([]byte{0x22}, SlotLen)...)
	tests := []struct {
		name        string
		moduleName  string
		logicalKey  []byte
		kind        keys.EVMKeyKind
		strippedKey []byte
	}{
		{
			name:        "non-EVM module",
			moduleName:  "bank",
			logicalKey:  []byte("supply"),
			kind:        keys.EVMKeyMisc,
			strippedKey: []byte("supply"),
		},
		{
			name:        "EVM storage",
			moduleName:  keys.EVMStoreKey,
			logicalKey:  keys.BuildEVMKey(keys.EVMKeyStorage, slotKey),
			kind:        keys.EVMKeyStorage,
			strippedKey: slotKey,
		},
		{
			name:        "EVM balance",
			moduleName:  keys.EVMStoreKey,
			logicalKey:  keys.BuildEVMKey(keys.EVMKeyBalance, address),
			kind:        keys.EVMKeyBalance,
			strippedKey: address,
		},
		{
			name:        "EVM misc is returned unchanged",
			moduleName:  keys.EVMStoreKey,
			logicalKey:  []byte{0x01, 0x02},
			kind:        keys.EVMKeyMisc,
			strippedKey: []byte{0x01, 0x02},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, strippedKey, err := ParseLogicalKey(tc.moduleName, tc.logicalKey)
			require.NoError(t, err)
			require.Equal(t, tc.kind, kind)
			require.Equal(t, tc.strippedKey, strippedKey)
		})
	}
}

func TestParseLogicalKeyRejectsEmptyKeys(t *testing.T) {
	_, _, err := ParseLogicalKey("", []byte("key"))
	require.ErrorContains(t, err, "empty module name")

	_, _, err = ParseLogicalKey(keys.EVMStoreKey, nil)
	require.ErrorContains(t, err, "empty EVM key")
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
