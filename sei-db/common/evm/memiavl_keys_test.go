package evm

import (
	"testing"

	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestParseMemIAVLEVMKey(t *testing.T) {
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
			name:      "Code",
			key:       concat(evmtypes.CodeKeyPrefix, addr),
			wantKind:  EVMKeyCode,
			wantBytes: addr,
		},
		{
			name:      "CodeSize",
			key:       concat(evmtypes.CodeSizeKeyPrefix, addr),
			wantKind:  EVMKeyCodeSize,
			wantBytes: addr,
		},
		{
			name:      "Storage",
			key:       concat(concat(evmtypes.StateKeyPrefix, addr), slot),
			wantKind:  EVMKeyStorage,
			wantBytes: concat(addr, slot),
		},
		{
			name:     "UnknownPrefix",
			key:      []byte{0xFF, 0xAA},
			wantKind: EVMKeyUnknown,
		},
		{
			name:     "Empty",
			key:      []byte{},
			wantKind: EVMKeyUnknown,
		},
		{
			name:     "NonceTooShort",
			key:      evmtypes.NonceKeyPrefix,
			wantKind: EVMKeyUnknown,
		},
		{
			name:     "NonceWrongLenShort",
			key:      concat(evmtypes.NonceKeyPrefix, addr[:addressLen-1]),
			wantKind: EVMKeyUnknown,
		},
		{
			name:     "NonceWrongLenLong",
			key:      concat(evmtypes.NonceKeyPrefix, concat(addr, []byte{0x00})),
			wantKind: EVMKeyUnknown,
		},
		{
			name:     "StorageTooShort",
			key:      concat(evmtypes.StateKeyPrefix, addr),
			wantKind: EVMKeyUnknown,
		},
		{
			name:     "StorageWrongLenLong",
			key:      concat(concat(concat(evmtypes.StateKeyPrefix, addr), slot), []byte{0x00}),
			wantKind: EVMKeyUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, keyBytes := ParseMemIAVLEVMKey(tc.key)
			require.Equal(t, tc.wantKind, kind)
			if kind != EVMKeyUnknown {
				require.Equal(t, tc.wantBytes, keyBytes)
			} else {
				require.Nil(t, keyBytes)
			}
		})
	}
}
