package vtype

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// rowsOfEveryType returns one serialized row of each type, all stamped with height, along with the payload
// each one carries after its header.
func rowsOfEveryType(t *testing.T, height int64) map[string][2][]byte {
	t.Helper()
	storageValue := make([]byte, StorageValueLength)
	storageValue[0] = 7
	storage, err := SerializeStorage(height, storageValue)
	require.NoError(t, err)

	balance := Balance{}
	balance[31] = 9
	account := NewAccountData().SetBlockHeight(height).SetBalance(&balance).SetNonce(3).Serialize()

	return map[string][2][]byte{
		"account": {account, account[rowHeaderLength:]},
		"storage": {storage, storageValue},
		"code":    {SerializeCode(height, []byte{0x60, 0x80}), {0x60, 0x80}},
		"misc":    {SerializeMisc(height, []byte("value")), []byte("value")},
	}
}

// Every row type shares one header, so the type-agnostic readers agree with each type's deserializer.
func TestRowHeaderAgreesWithEveryRowType(t *testing.T) {
	const height = 123456
	for name, row := range rowsOfEveryType(t, height) {
		got, err := RowBlockHeight(row[0])
		require.NoError(t, err, name)
		require.Equal(t, uint64(height), got, name)

		payload, err := RowPayload(row[0])
		require.NoError(t, err, name)
		require.Equal(t, row[1], payload, name)
	}

	account, err := DeserializeAccountData(rowsOfEveryType(t, height)["account"][0])
	require.NoError(t, err)
	require.Equal(t, int64(height), account.GetBlockHeight())
}

func TestRowHeaderRejectsMalformedRows(t *testing.T) {
	_, err := RowBlockHeight(make([]byte, rowHeaderLength-1))
	require.Error(t, err, "a row shorter than its header")
	_, err = RowPayload(nil)
	require.Error(t, err, "an empty row")

	unknownVersion := SerializeMisc(1, []byte("v"))
	unknownVersion[rowVersionStart] = 1
	_, err = RowBlockHeight(unknownVersion)
	require.Error(t, err, "an unknown serialization version")
	_, err = RowPayload(unknownVersion)
	require.Error(t, err, "an unknown serialization version")
}
