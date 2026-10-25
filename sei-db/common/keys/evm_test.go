package keys

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
	addr := make([]byte, AddressLen)
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
		{
			name:      "Balance",
			key:       concat(balanceKeyPrefix, addr),
			wantKind:  EVMKeyBalance,
			wantBytes: addr,
		},
		// Legacy keys - keep full key (address mappings, unknown prefix, malformed, etc.)
		{
			name:      "EVMAddressToSeiAddress goes to Legacy",
			key:       concat(testEVMAddrToSeiPrefix, addr),
			wantKind:  EVMKeyMisc,
			wantBytes: concat(testEVMAddrToSeiPrefix, addr), // Full key preserved
		},
		{
			name:      "SeiAddressToEVMAddress goes to Legacy",
			key:       concat(testSeiAddrToEVMPrefix, addr),
			wantKind:  EVMKeyMisc,
			wantBytes: concat(testSeiAddrToEVMPrefix, addr), // Full key preserved
		},
		{
			name:      "UnknownPrefix goes to Legacy",
			key:       []byte{0xFF, 0xAA},
			wantKind:  EVMKeyMisc,
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
			wantKind:  EVMKeyMisc,
			wantBytes: nonceKeyPrefix,
		},
		{
			name:      "NonceWrongLenShort goes to Legacy",
			key:       concat(nonceKeyPrefix, addr[:AddressLen-1]),
			wantKind:  EVMKeyMisc,
			wantBytes: concat(nonceKeyPrefix, addr[:AddressLen-1]),
		},
		{
			name:      "NonceWrongLenLong goes to Legacy",
			key:       concat(nonceKeyPrefix, concat(addr, []byte{0x00})),
			wantKind:  EVMKeyMisc,
			wantBytes: concat(nonceKeyPrefix, concat(addr, []byte{0x00})),
		},
		{
			name:      "StorageTooShort goes to Legacy",
			key:       concat(stateKeyPrefix, addr),
			wantKind:  EVMKeyMisc,
			wantBytes: concat(stateKeyPrefix, addr),
		},
		{
			name:      "StorageWrongLenLong goes to Legacy",
			key:       concat(concat(concat(stateKeyPrefix, addr), slot), []byte{0x00}),
			wantKind:  EVMKeyMisc,
			wantBytes: concat(concat(concat(stateKeyPrefix, addr), slot), []byte{0x00}),
		},
		{
			name:      "BalanceTooShort goes to Legacy",
			key:       balanceKeyPrefix,
			wantKind:  EVMKeyMisc,
			wantBytes: balanceKeyPrefix,
		},
		{
			name:      "BalanceWrongLenLong goes to Legacy",
			key:       concat(balanceKeyPrefix, concat(addr, []byte{0x00})),
			wantKind:  EVMKeyMisc,
			wantBytes: concat(balanceKeyPrefix, concat(addr, []byte{0x00})),
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
	addr := make([]byte, AddressLen)
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
			name:     "Balance",
			kind:     EVMKeyBalance,
			keyBytes: addr,
			want:     concat(balanceKeyPrefix, addr),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildEVMKey(tc.kind, tc.keyBytes)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestPutEVMKey(t *testing.T) {
	addr := make([]byte, AddressLen)
	for i := range addr {
		addr[i] = 0xAA
	}
	slot := make([]byte, slotLen)
	for i := range slot {
		slot[i] = 0xBB
	}
	concat := func(parts ...[]byte) []byte {
		var out []byte
		for _, part := range parts {
			out = append(out, part...)
		}
		return out
	}

	for _, tc := range []struct {
		name  string
		kind  EVMKeyKind
		parts [][]byte
		want  []byte
	}{
		{
			name:  "Balance",
			kind:  EVMKeyBalance,
			parts: [][]byte{addr},
			want:  concat(balanceKeyPrefix, addr),
		},
		{
			name:  "Storage",
			kind:  EVMKeyStorage,
			parts: [][]byte{addr, slot},
			want:  concat(stateKeyPrefix, addr, slot),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dst := make([]byte, len(tc.want))
			require.True(t, PutEVMKey(dst, tc.kind, tc.parts...))
			require.Equal(t, tc.want, dst)
		})
	}

	for _, tc := range []struct {
		name string
		kind EVMKeyKind
		dst  []byte
	}{
		{
			name: "kind without prefix",
			kind: EVMKeyKind(255),
			dst:  make([]byte, 1+AddressLen),
		},
		{
			name: "destination too long",
			kind: EVMKeyBalance,
			dst:  make([]byte, 2+AddressLen),
		},
		{
			name: "destination too short",
			kind: EVMKeyBalance,
			dst:  make([]byte, AddressLen),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i := range tc.dst {
				tc.dst[i] = 0xCC
			}
			before := append([]byte(nil), tc.dst...)
			require.False(t, PutEVMKey(tc.dst, tc.kind, addr))
			require.Equal(t, before, tc.dst)
		})
	}
}

func TestInternalKeyLen(t *testing.T) {
	require.Equal(t, AddressLen+slotLen, InternalKeyLen(EVMKeyStorage))
	require.Equal(t, AddressLen, InternalKeyLen(EVMKeyNonce))
	require.Equal(t, AddressLen, InternalKeyLen(EVMKeyCodeHash))
	require.Equal(t, AddressLen, InternalKeyLen(EVMKeyCode))
	require.Equal(t, AddressLen, InternalKeyLen(EVMKeyBalance))
}

// The prefix bytes this package mirrors from x/evm/types must stay distinct: ParseEVMKey classifies on
// the first byte alone, so two families sharing one would silently reparse each other's rows.
func TestEVMKeyPrefixesAreDistinct(t *testing.T) {
	seen := map[byte]EVMKeyKind{}
	for _, kind := range []EVMKeyKind{EVMKeyNonce, EVMKeyCodeHash, EVMKeyCode, EVMKeyStorage, EVMKeyBalance} {
		prefix, ok := EVMKeyPrefixByte(kind)
		require.True(t, ok, "kind %v has no prefix byte", kind)
		previous, duplicate := seen[prefix]
		require.False(t, duplicate, "kinds %v and %v both use prefix 0x%02x", previous, kind, prefix)
		seen[prefix] = kind
	}
}
