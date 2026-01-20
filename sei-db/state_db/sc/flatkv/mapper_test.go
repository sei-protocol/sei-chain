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
		wantAddr  Address
		wantSlot  Slot
		wantError bool
	}{
		{
			name:      "Nonce",
			key:       append(evmtypes.NonceKeyPrefix, addr[:]...),
			wantKind:  EVMKeyNonce,
			wantAddr:  addr,
			wantError: false,
		},
		{
			name:      "CodeHash",
			key:       append(evmtypes.CodeHashKeyPrefix, addr[:]...),
			wantKind:  EVMKeyCodeHash,
			wantAddr:  addr,
			wantError: false,
		},
		{
			name:      "Code",
			key:       append(evmtypes.CodeKeyPrefix, addr[:]...),
			wantKind:  EVMKeyCode,
			wantAddr:  addr,
			wantError: false,
		},
		{
			name:      "CodeSize",
			key:       append(evmtypes.CodeSizeKeyPrefix, addr[:]...),
			wantKind:  EVMKeyCodeSize,
			wantAddr:  addr,
			wantError: false,
		},
		{
			name:      "Storage",
			key:       append(append(evmtypes.StateKeyPrefix, addr[:]...), slot[:]...),
			wantKind:  EVMKeyStorage,
			wantAddr:  addr,
			wantSlot:  slot,
			wantError: false,
		},
		{
			name:      "UnknownPrefix",
			key:       []byte{0xFF, 0xAA},
			wantKind:  EVMKeyUnknown,
			wantError: false, // Unknown is not an error, just unknown kind
		},
		{
			name:      "Empty",
			key:       []byte{},
			wantKind:  EVMKeyUnknown,
			wantError: true,
		},
		{
			name:      "NonceTooShort",
			key:       evmtypes.NonceKeyPrefix,
			wantKind:  EVMKeyUnknown,
			wantError: true,
		},
		{
			name:      "NonceWrongLenShort",
			key:       append(evmtypes.NonceKeyPrefix, addr[:AddressLen-1]...),
			wantKind:  EVMKeyUnknown,
			wantError: true,
		},
		{
			name:      "NonceWrongLenLong",
			key:       append(evmtypes.NonceKeyPrefix, append(addr[:], 0x00)...),
			wantKind:  EVMKeyUnknown,
			wantError: true,
		},
		{
			name:      "StorageTooShort",
			key:       append(evmtypes.StateKeyPrefix, addr[:]...),
			wantKind:  EVMKeyUnknown,
			wantError: true,
		},
		{
			name:      "StorageWrongLenLong",
			key:       append(append(append(evmtypes.StateKeyPrefix, addr[:]...), slot[:]...), 0x00),
			wantKind:  EVMKeyUnknown,
			wantError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			kind, a, s, err := ParseEVMKey(tc.key)
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.wantKind, kind)
			if kind == EVMKeyStorage {
				require.Equal(t, tc.wantAddr, a)
				require.Equal(t, tc.wantSlot, s)
			} else if kind != EVMKeyUnknown {
				require.Equal(t, tc.wantAddr, a)
			}
		})
	}
}
