package statewal

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/giga/types/mutation"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/vtype"
)

// makeMutation returns a mutation that writes value to key in a non-EVM module.
func makeMutation(moduleName string, key []byte, value []byte) mutation.Mutation {
	return mutation.Mutation{
		Key:      ktype.BuildPhysicalKey(moduleName, key),
		NewValue: vtype.SerializeMisc(1, value),
	}
}

func TestSerializeBlockRoundTrip(t *testing.T) {
	mutations := []mutation.Mutation{
		makeMutation("bank", []byte("a"), []byte("1")),
		makeMutation("staking", []byte("b"), []byte("2")),
	}
	serialized := serializeBlock(mutations)
	require.Equal(t, mutationFormatVersion, serialized[0], "serialization must begin with the format version")

	read, err := deserializeBlock(serialized)
	require.NoError(t, err)
	require.Equal(t, mutations, read)
}

func TestDeserializeBlockRejectsInvalidInput(t *testing.T) {
	_, err := deserializeBlock(nil)
	require.Error(t, err)

	_, err = deserializeBlock([]byte{mutationFormatVersion + 1})
	require.Error(t, err)

	// A block in the changeset format, which is never written.
	_, err = deserializeBlock([]byte{mutationFormatVersion - 1})
	require.Error(t, err)

	// A version 2 block whose mutations are truncated.
	serialized := serializeBlock([]mutation.Mutation{makeMutation("bank", []byte("a"), nil)})
	_, err = deserializeBlock(serialized[:len(serialized)-1])
	require.Error(t, err)
}

func TestComputePlaceholderHashRejectsInvalidInput(t *testing.T) {
	_, err := computePlaceholderHash(nil)
	require.Error(t, err)

	// A block in the changeset format, which is never written.
	_, err = computePlaceholderHash([]byte{mutationFormatVersion - 1})
	require.Error(t, err)
}
