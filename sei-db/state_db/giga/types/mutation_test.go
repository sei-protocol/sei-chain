package types

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

func TestMutationRowAccessors(t *testing.T) {
	previous := vtype.SerializeMisc(7, []byte("old"))
	value := vtype.SerializeMisc(9, []byte("new"))
	m := NewMutation("k", value, previous)

	height, present, err := m.BlockHeight()
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, uint64(9), height)

	height, present, err = m.PreviousBlockHeight()
	require.NoError(t, err)
	require.True(t, present)
	require.Equal(t, uint64(7), height)

	payload, err := m.Payload()
	require.NoError(t, err)
	require.Equal(t, []byte("new"), payload)

	payload, err = m.PreviousPayload()
	require.NoError(t, err)
	require.Equal(t, []byte("old"), payload)
}

// A deletion has no current row and a new key no previous one. Neither is an error.
func TestMutationRowAccessorsWithoutARow(t *testing.T) {
	m := NewMutation("k", nil, nil)

	_, present, err := m.BlockHeight()
	require.NoError(t, err)
	require.False(t, present)

	_, present, err = m.PreviousBlockHeight()
	require.NoError(t, err)
	require.False(t, present)

	payload, err := m.Payload()
	require.NoError(t, err)
	require.Nil(t, payload)

	payload, err = m.PreviousPayload()
	require.NoError(t, err)
	require.Nil(t, payload)
}

// Every flatKV row type starts with the header Mutation reads, so its accessors agree with each type's
// own serialization.
func TestMutationRowAccessorsAgreeWithEveryRowType(t *testing.T) {
	const height = 123456
	storageValue := make([]byte, vtype.StorageValueLength)
	storageValue[0] = 7
	storage, err := vtype.SerializeStorage(height, storageValue)
	require.NoError(t, err)
	balance := vtype.Balance{}
	balance[31] = 9
	account := vtype.NewAccountData().SetBlockHeight(height).SetBalance(&balance).SetNonce(3).Serialize()

	rows := map[string][2][]byte{
		"account": {account, account[rowHeaderLength:]},
		"storage": {storage, storageValue},
		"code":    {vtype.SerializeCode(height, []byte{0x60, 0x80}), {0x60, 0x80}},
		"misc":    {vtype.SerializeMisc(height, []byte("value")), []byte("value")},
	}
	for name, row := range rows {
		m := NewMutation("k", row[0], nil)
		got, present, err := m.BlockHeight()
		require.NoError(t, err, name)
		require.True(t, present, name)
		require.Equal(t, uint64(height), got, name)
		payload, err := m.Payload()
		require.NoError(t, err, name)
		require.Equal(t, row[1], payload, name)
	}
}

func TestMutationRowAccessorsRejectAMalformedRow(t *testing.T) {
	m := NewMutation("k", []byte{0, 1, 2}, nil)
	_, _, err := m.BlockHeight()
	require.Error(t, err)
	_, err = m.Payload()
	require.Error(t, err)
}

func TestMutationRowAccessorsRejectAnUnknownVersion(t *testing.T) {
	row := vtype.SerializeMisc(1, []byte("v"))
	row[rowVersionStart] = 1
	m := NewMutation("k", row, nil)
	_, _, err := m.BlockHeight()
	require.Error(t, err)
	_, err = m.Payload()
	require.Error(t, err)
}
