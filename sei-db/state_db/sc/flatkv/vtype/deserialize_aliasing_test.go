package vtype

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Every Deserialize* must copy the bytes it is handed, never alias them. Callers routinely pass a
// buffer borrowed from the storage engine or from a live iterator, and the getters hand out
// subslices of whatever the value holds — so aliasing would let an unrelated write change a value
// that has already been read. The existing suite covers the Set*/Parse* paths only, which is why
// these test the Deserialize* path specifically.
func TestDeserializeCopiesInsteadOfAliasing(t *testing.T) {
	scribble := func(b []byte) {
		for i := range b {
			b[i] = 0xFF
		}
	}

	t.Run("AccountData full form", func(t *testing.T) {
		codeHash := CodeHash{0x11, 0x22}
		source := NewAccountData().SetBlockHeight(5).SetNonce(7).SetCodeHash(&codeHash).Serialize()
		buffer := append([]byte(nil), source...)
		require.Len(t, buffer, accountDataLength)

		account, err := DeserializeAccountData(buffer)
		require.NoError(t, err)
		scribble(buffer)

		require.Equal(t, int64(5), account.GetBlockHeight())
		require.Equal(t, uint64(7), account.GetNonce())
		require.Equal(t, codeHash, *account.GetCodeHash())
		require.Equal(t, source, account.Serialize())
	})

	t.Run("AccountData compact form", func(t *testing.T) {
		source := NewAccountData().SetBlockHeight(6).SetNonce(8).Serialize()
		buffer := append([]byte(nil), source...)
		require.Len(t, buffer, accountCompactLength)

		account, err := DeserializeAccountData(buffer)
		require.NoError(t, err)
		scribble(buffer)

		require.Equal(t, int64(6), account.GetBlockHeight())
		require.Equal(t, uint64(8), account.GetNonce())
		require.Equal(t, source, account.Serialize())
	})

	t.Run("StorageData", func(t *testing.T) {
		value := [32]byte{0x01, 0x02, 0x03}
		source := NewStorageData().SetBlockHeight(9).SetValue(&value).Serialize()
		buffer := append([]byte(nil), source...)

		storage, err := DeserializeStorageData(buffer)
		require.NoError(t, err)
		scribble(buffer)

		require.Equal(t, int64(9), storage.GetBlockHeight())
		require.Equal(t, value, *storage.GetValue())
		require.Equal(t, source, storage.Serialize())
	})

	t.Run("CodeData", func(t *testing.T) {
		bytecode := []byte{0xAA, 0xBB, 0xCC}
		source := NewCodeDataFrom(10, bytecode).Serialize()
		buffer := append([]byte(nil), source...)

		code, err := DeserializeCodeData(buffer)
		require.NoError(t, err)
		scribble(buffer)

		require.Equal(t, int64(10), code.GetBlockHeight())
		require.Equal(t, bytecode, code.GetBytecode())
		require.Equal(t, source, code.Serialize())
	})

	t.Run("MiscData", func(t *testing.T) {
		value := []byte{0xDD, 0xEE}
		source := NewMiscDataFrom(11, value).Serialize()
		buffer := append([]byte(nil), source...)

		misc, err := DeserializeMiscData(buffer)
		require.NoError(t, err)
		scribble(buffer)

		require.Equal(t, int64(11), misc.GetBlockHeight())
		require.Equal(t, value, misc.GetValue())
		require.Equal(t, source, misc.Serialize())
	})
}

// The write-path constructors must copy their input too: the raw bytes they are given belong to the
// changeset, which the caller is free to reuse once the value has been built.
func TestConstructorsCopyTheirInput(t *testing.T) {
	t.Run("NewStorageDataFrom", func(t *testing.T) {
		raw := make([]byte, StorageValueLength)
		raw[0] = 0x01
		storage, err := NewStorageDataFrom(1, raw)
		require.NoError(t, err)
		raw[0] = 0xFF
		require.Equal(t, byte(0x01), storage.GetValue()[0])
	})

	t.Run("NewCodeDataFrom", func(t *testing.T) {
		raw := []byte{0x01, 0x02}
		code := NewCodeDataFrom(1, raw)
		raw[0] = 0xFF
		require.Equal(t, []byte{0x01, 0x02}, code.GetBytecode())
	})

	t.Run("NewMiscDataFrom", func(t *testing.T) {
		raw := []byte{0x03, 0x04}
		misc := NewMiscDataFrom(1, raw)
		raw[0] = 0xFF
		require.Equal(t, []byte{0x03, 0x04}, misc.GetValue())
	})

	t.Run("SetCodeHashBytes", func(t *testing.T) {
		raw := make([]byte, CodeHashLen)
		raw[0] = 0x07
		account, err := NewAccountData().SetCodeHashBytes(raw)
		require.NoError(t, err)
		raw[0] = 0xFF
		require.Equal(t, byte(0x07), account.GetCodeHash()[0])
	})
}

// A deleted misc entry keeps the delete flag distinct from an empty value, since an empty value is
// a legitimate write for a Cosmos module.
func TestDeletedMiscDataIsDistinctFromEmptyValue(t *testing.T) {
	deleted := NewDeletedMiscData(12)
	require.True(t, deleted.IsDelete())
	require.Equal(t, int64(12), deleted.GetBlockHeight())

	empty := NewMiscDataFrom(12, []byte{})
	require.False(t, empty.IsDelete())
	require.Equal(t, int64(12), empty.GetBlockHeight())
	require.Empty(t, empty.GetValue())
}
