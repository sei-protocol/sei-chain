package vtype

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// SplitValue() must work on every VType: each one below is serialized at a known height, and splitting it must
// recover its version, that height, and its data.
func TestSplitValueWorksOnEveryVType(t *testing.T) {
	const blockHeight = 0x0102030405060708
	balance := Balance(*toArray32(leftPad32([]byte{0xba})))
	codeHash := CodeHash(*toArray32(leftPad32([]byte{0xc0})))
	storageValue := toArray32(leftPad32([]byte{0x5e}))

	tests := []struct {
		name            string
		value           VType
		expectedVersion byte
		expectedData    func(serialized []byte) []byte
	}{
		{
			name:            "compact account",
			value:           NewAccountData().SetBlockHeight(blockHeight).SetBalance(&balance).SetNonce(7),
			expectedVersion: byte(AccountDataVersion0),
			expectedData:    func(serialized []byte) []byte { return serialized[accountBalanceStart:] },
		},
		{
			name: "full account",
			value: NewAccountData().SetBlockHeight(blockHeight).SetBalance(&balance).SetNonce(7).
				SetCodeHash(&codeHash),
			expectedVersion: byte(AccountDataVersion0),
			expectedData:    func(serialized []byte) []byte { return serialized[accountBalanceStart:] },
		},
		{
			name:            "storage",
			value:           NewStorageData().SetBlockHeight(blockHeight).SetValue(storageValue),
			expectedVersion: byte(StorageDataVersion0),
			expectedData:    func(serialized []byte) []byte { return serialized[storageValueStart:] },
		},
		{
			name:            "code",
			value:           NewCodeData().SetBlockHeight(blockHeight).SetBytecode([]byte{0x60, 0x80}),
			expectedVersion: byte(CodeDataVersion0),
			expectedData:    func(serialized []byte) []byte { return serialized[codeBytecodeStart:] },
		},
		{
			name:            "misc",
			value:           NewMiscData().SetBlockHeight(blockHeight).SetValue([]byte("value")),
			expectedVersion: byte(MiscDataVersion0),
			expectedData:    func(serialized []byte) []byte { return serialized[miscValueStart:] },
		},
		{
			name:            "empty misc",
			value:           NewMiscData().SetBlockHeight(blockHeight).SetValue([]byte{}),
			expectedVersion: byte(MiscDataVersion0),
			expectedData:    func(serialized []byte) []byte { return serialized[miscValueStart:] },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serialized := tt.value.Serialize()
			version, height, data, err := SplitValue(serialized)
			require.NoError(t, err)
			require.Equal(t, tt.expectedVersion, version)
			require.Equal(t, uint64(blockHeight), height)
			require.Equal(t, tt.expectedData(serialized), data)
		})
	}
}

func TestSplitValueRejectsValueShorterThanHeader(t *testing.T) {
	value := SerializeMisc(9, nil)
	for length := 0; length < valueHeaderLength; length++ {
		_, _, _, err := SplitValue(value[:length])
		require.Error(t, err, "value truncated to %d bytes", length)
	}
}
