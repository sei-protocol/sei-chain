package evm

import (
	"testing"

	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestParseEVMKey(t *testing.T) {
	addr := make([]byte, addressLen)
	for i := range addr {
		addr[i] = 0xAA
	}
	slot := make([]byte, slotLen)
	for i := range slot {
		slot[i] = 0xBB
	}

	concat := func(a, b []byte) []byte {
		out := make([]byte, 0, len(a)+len(b))
		out = append(out, a...)
		out = append(out, b...)
		return out
	}

	// Sanity-check: inlined prefixes match the canonical evmtypes values.
	require.Equal(t, stateKeyPrefix, evmtypes.StateKeyPrefix)
	require.Equal(t, codeKeyPrefix, evmtypes.CodeKeyPrefix)
	require.Equal(t, codeHashKeyPrefix, evmtypes.CodeHashKeyPrefix)
	require.Equal(t, codeSizeKeyPrefix, evmtypes.CodeSizeKeyPrefix)
	require.Equal(t, nonceKeyPrefix, evmtypes.NonceKeyPrefix)

	tests := []struct {
		name      string
		key       []byte
		wantKind  EVMKeyKind
		wantBytes []byte
	}{
		// Optimized keys - stripped
		{
			name:      "Nonce",
			key:       concat(evmtypes.NonceKeyPrefix, addr),
			wantKind:  EVMKeyNonce,
			wantBytes: addr,
		},
		{
			name:      "CodeHash",
			key:       concat(evmtypes.CodeHashKeyPrefix, addr),
			wantKind:  EVMKeyCodeHash,
			wantBytes: addr,
		},
		{
			name:      "CodeSize",
			key:       concat(evmtypes.CodeSizeKeyPrefix, addr),
			wantKind:  EVMKeyCodeSize,
			wantBytes: addr,
		},
		{
			name:      "Code",
			key:       concat(evmtypes.CodeKeyPrefix, addr),
			wantKind:  EVMKeyCode,
			wantBytes: addr,
		},
		{
			name:      "Storage",
			key:       concat(concat(evmtypes.StateKeyPrefix, addr), slot),
			wantKind:  EVMKeyStorage,
			wantBytes: concat(addr, slot),
		},
		// Legacy keys - keep full key (address mappings, unknown prefix, malformed, etc.)
		{
			name:      "EVMAddressToSeiAddress goes to Legacy",
			key:       concat(evmtypes.EVMAddressToSeiAddressKeyPrefix, addr),
			wantKind:  EVMKeyLegacy,
			wantBytes: concat(evmtypes.EVMAddressToSeiAddressKeyPrefix, addr), // Full key preserved
		},
		{
			name:      "SeiAddressToEVMAddress goes to Legacy",
			key:       concat(evmtypes.SeiAddressToEVMAddressKeyPrefix, addr),
			wantKind:  EVMKeyLegacy,
			wantBytes: concat(evmtypes.SeiAddressToEVMAddressKeyPrefix, addr), // Full key preserved
		},
		{
			name:      "UnknownPrefix goes to Legacy",
			key:       []byte{0xFF, 0xAA},
			wantKind:  EVMKeyLegacy,
			wantBytes: []byte{0xFF, 0xAA}, // Full key preserved
		},
		{
			name:      "Empty returns EVMKeyEmpty",
			key:       []byte{},
			wantKind:  EVMKeyEmpty,
			wantBytes: nil,
		},
		{
			name:      "NonceTooShort goes to Legacy",
			key:       evmtypes.NonceKeyPrefix,
			wantKind:  EVMKeyLegacy,
			wantBytes: evmtypes.NonceKeyPrefix,
		},
		{
			name:      "NonceWrongLenShort goes to Legacy",
			key:       concat(evmtypes.NonceKeyPrefix, addr[:addressLen-1]),
			wantKind:  EVMKeyLegacy,
			wantBytes: concat(evmtypes.NonceKeyPrefix, addr[:addressLen-1]),
		},
		{
			name:      "NonceWrongLenLong goes to Legacy",
			key:       concat(evmtypes.NonceKeyPrefix, concat(addr, []byte{0x00})),
			wantKind:  EVMKeyLegacy,
			wantBytes: concat(evmtypes.NonceKeyPrefix, concat(addr, []byte{0x00})),
		},
		{
			name:      "StorageTooShort goes to Legacy",
			key:       concat(evmtypes.StateKeyPrefix, addr),
			wantKind:  EVMKeyLegacy,
			wantBytes: concat(evmtypes.StateKeyPrefix, addr),
		},
		{
			name:      "StorageWrongLenLong goes to Legacy",
			key:       concat(concat(concat(evmtypes.StateKeyPrefix, addr), slot), []byte{0x00}),
			wantKind:  EVMKeyLegacy,
			wantBytes: concat(concat(concat(evmtypes.StateKeyPrefix, addr), slot), []byte{0x00}),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, keyBytes := ParseEVMKey(tc.key)
			require.Equal(t, tc.wantKind, kind)
			require.Equal(t, tc.wantBytes, keyBytes)
		})
	}
}

func TestBuildMemIAVLEVMKey(t *testing.T) {
	addr := make([]byte, addressLen)
	for i := range addr {
		addr[i] = 0xAA
	}
	slot := make([]byte, slotLen)
	for i := range slot {
		slot[i] = 0xBB
	}

	concat := func(a, b []byte) []byte {
		out := make([]byte, 0, len(a)+len(b))
		out = append(out, a...)
		out = append(out, b...)
		return out
	}

	tests := []struct {
		name     string
		kind     EVMKeyKind
		keyBytes []byte
		want     []byte
	}{
		{
			name:     "Nonce",
			kind:     EVMKeyNonce,
			keyBytes: addr,
			want:     concat(nonceKeyPrefix, addr),
		},
		{
			name:     "CodeHash",
			kind:     EVMKeyCodeHash,
			keyBytes: addr,
			want:     concat(codeHashKeyPrefix, addr),
		},
		{
			name:     "Code",
			kind:     EVMKeyCode,
			keyBytes: addr,
			want:     concat(codeKeyPrefix, addr),
		},
		{
			name:     "CodeSize",
			kind:     EVMKeyCodeSize,
			keyBytes: addr,
			want:     concat(codeSizeKeyPrefix, addr),
		},
		{
			name:     "Storage",
			kind:     EVMKeyStorage,
			keyBytes: concat(addr, slot),
			want:     concat(stateKeyPrefix, concat(addr, slot)),
		},
		{
			name:     "Unknown",
			kind:     EVMKeyUnknown,
			keyBytes: addr,
			want:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildMemIAVLEVMKey(tc.kind, tc.keyBytes)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestInternalKeyLen(t *testing.T) {
	require.Equal(t, addressLen+slotLen, InternalKeyLen(EVMKeyStorage))
	require.Equal(t, addressLen, InternalKeyLen(EVMKeyNonce))
	require.Equal(t, addressLen, InternalKeyLen(EVMKeyCodeHash))
	require.Equal(t, addressLen, InternalKeyLen(EVMKeyCode))
	require.Equal(t, addressLen, InternalKeyLen(EVMKeyCodeSize))
	require.Equal(t, 0, InternalKeyLen(EVMKeyUnknown))
}

func TestEVMKeyUnknownAlias(t *testing.T) {
	// Verify EVMKeyUnknown == EVMKeyEmpty so FlatKV's "skip unknown" checks
	// still work correctly after introducing EVMKeyLegacy.
	require.Equal(t, EVMKeyEmpty, EVMKeyUnknown)
}
