package flatkv

import (
	"testing"

	evmtypes "github.com/sei-protocol/sei-chain/x/evm/types"
	"github.com/stretchr/testify/require"
)

func TestFlatKVParseEVMKey(t *testing.T) {
	addr := Address{}
	for i := range addr {
		addr[i] = 0xAA
	}
	slot := Slot{}
	for i := range slot {
		slot[i] = 0xBB
	}

	tests := []struct {
		name      string
		key       []byte
		wantKind  EVMKeyKind
		wantBytes []byte
	}{
		{
			name:      "Nonce",
			key:       append(evmtypes.NonceKeyPrefix, addr[:]...),
			wantKind:  EVMKeyNonce,
			wantBytes: addr[:],
		},
		{
			name:      "CodeHash",
			key:       append(evmtypes.CodeHashKeyPrefix, addr[:]...),
			wantKind:  EVMKeyCodeHash,
			wantBytes: addr[:],
		},
		{
			name:      "Code",
			key:       append(evmtypes.CodeKeyPrefix, addr[:]...),
			wantKind:  EVMKeyCode,
			wantBytes: addr[:],
		},
		{
			name:      "CodeSize",
			key:       append(evmtypes.CodeSizeKeyPrefix, addr[:]...),
			wantKind:  EVMKeyCodeSize,
			wantBytes: addr[:],
		},
		{
			name:      "Storage",
			key:       append(append(evmtypes.StateKeyPrefix, addr[:]...), slot[:]...),
			wantKind:  EVMKeyStorage,
			wantBytes: append(addr[:], slot[:]...),
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
			key:      append(evmtypes.NonceKeyPrefix, addr[:AddressLen-1]...),
			wantKind: EVMKeyUnknown,
		},
		{
			name:     "NonceWrongLenLong",
			key:      append(evmtypes.NonceKeyPrefix, append(addr[:], 0x00)...),
			wantKind: EVMKeyUnknown,
		},
		{
			name:     "StorageTooShort",
			key:      append(evmtypes.StateKeyPrefix, addr[:]...),
			wantKind: EVMKeyUnknown,
		},
		{
			name:     "StorageWrongLenLong",
			key:      append(append(append(evmtypes.StateKeyPrefix, addr[:]...), slot[:]...), 0x00),
			wantKind: EVMKeyUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, keyBytes := ParseEVMKey(tc.key)
			require.Equal(t, tc.wantKind, kind)
			if kind != EVMKeyUnknown {
				require.Equal(t, tc.wantBytes, keyBytes)
			} else {
				require.Nil(t, keyBytes)
			}
		})
	}
}
