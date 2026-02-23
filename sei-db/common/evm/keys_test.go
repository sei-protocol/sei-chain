package evm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Test-local copies of x/evm/types key prefixes.
// Kept here (rather than importing evmtypes) to avoid a circular dependency:
//
//	common/evm (test) -> x/evm/types -> cosmos-sdk/server/config
//	-> sei-db/config -> sei-db/state_db/sc/flatkv -> common/evm
var (
	testEVMAddrToSeiPrefix = []byte{0x01}
	testSeiAddrToEVMPrefix = []byte{0x02}
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

	tests := []struct {
		name      string
		key       []byte
		wantKind  EVMKeyKind
		wantBytes []byte
	}{
		// Optimized keys - stripped
		{
			name:      "Nonce",
			key:       concat(nonceKeyPrefix, addr),
			wantKind:  EVMKeyNonce,
			wantBytes: addr,
		},
		{
			name:      "CodeHash",
			key:       concat(codeHashKeyPrefix, addr),
			wantKind:  EVMKeyCodeHash,
			wantBytes: addr,
		},
		{
			name:      "CodeSize goes to Legacy",
			key:       concat(codeSizeKeyPrefix, addr),
			wantKind:  EVMKeyLegacy,
			wantBytes: concat(codeSizeKeyPrefix, addr), // Full key preserved
		},
		{
			name:      "Code",
			key:       concat(codeKeyPrefix, addr),
			wantKind:  EVMKeyCode,
			wantBytes: addr,
		},
		{
			name:      "Storage",
			key:       concat(concat(stateKeyPrefix, addr), slot),
			wantKind:  EVMKeyStorage,
			wantBytes: concat(addr, slot),
		},
		// Legacy keys - keep full key (address mappings, unknown prefix, malformed, etc.)
		{
			name:      "EVMAddressToSeiAddress goes to Legacy",
			key:       concat(testEVMAddrToSeiPrefix, addr),
			wantKind:  EVMKeyLegacy,
			wantBytes: concat(testEVMAddrToSeiPrefix, addr), // Full key preserved
		},
		{
			name:      "SeiAddressToEVMAddress goes to Legacy",
			key:       concat(testSeiAddrToEVMPrefix, addr),
			wantKind:  EVMKeyLegacy,
			wantBytes: concat(testSeiAddrToEVMPrefix, addr), // Full key preserved
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
			key:       nonceKeyPrefix,
			wantKind:  EVMKeyLegacy,
			wantBytes: nonceKeyPrefix,
		},
		{
			name:      "NonceWrongLenShort goes to Legacy",
			key:       concat(nonceKeyPrefix, addr[:addressLen-1]),
			wantKind:  EVMKeyLegacy,
			wantBytes: concat(nonceKeyPrefix, addr[:addressLen-1]),
		},
		{
			name:      "NonceWrongLenLong goes to Legacy",
			key:       concat(nonceKeyPrefix, concat(addr, []byte{0x00})),
			wantKind:  EVMKeyLegacy,
			wantBytes: concat(nonceKeyPrefix, concat(addr, []byte{0x00})),
		},
		{
			name:      "StorageTooShort goes to Legacy",
			key:       concat(stateKeyPrefix, addr),
			wantKind:  EVMKeyLegacy,
			wantBytes: concat(stateKeyPrefix, addr),
		},
		{
			name:      "StorageWrongLenLong goes to Legacy",
			key:       concat(concat(concat(stateKeyPrefix, addr), slot), []byte{0x00}),
			wantKind:  EVMKeyLegacy,
			wantBytes: concat(concat(concat(stateKeyPrefix, addr), slot), []byte{0x00}),
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
	require.Equal(t, 0, InternalKeyLen(EVMKeyUnknown))
}

func TestEVMKeyUnknownAlias(t *testing.T) {
	// Verify EVMKeyUnknown == EVMKeyEmpty so FlatKV's "skip unknown" checks
	// still work correctly after introducing EVMKeyLegacy.
	require.Equal(t, EVMKeyEmpty, EVMKeyUnknown)
}
