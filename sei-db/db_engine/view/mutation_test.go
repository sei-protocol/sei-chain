package view

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

func TestMutationRowAccessorsRejectAMalformedRow(t *testing.T) {
	m := NewMutation("k", []byte{0, 1, 2}, nil)
	_, _, err := m.BlockHeight()
	require.Error(t, err)
	_, err = m.Payload()
	require.Error(t, err)
}
