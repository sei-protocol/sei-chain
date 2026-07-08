package statewal

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func makeChangeSet(name string, key []byte, value []byte) *proto.NamedChangeSet {
	return &proto.NamedChangeSet{
		Name: name,
		Changeset: proto.ChangeSet{
			Pairs: []*proto.KVPair{{Key: key, Value: value}},
		},
	}
}

func TestChangesetsRoundTrip(t *testing.T) {
	t.Run("multiple named change sets", func(t *testing.T) {
		cs := []*proto.NamedChangeSet{
			makeChangeSet("bank", []byte("a"), []byte("1")),
			makeChangeSet("evm", []byte("b"), []byte("2")),
		}

		data, err := serializeChangesets(cs)
		require.NoError(t, err)

		got, err := deserializeChangesets(data)
		require.NoError(t, err)
		require.Equal(t, cs, got)
	})

	t.Run("empty changeset list", func(t *testing.T) {
		data, err := serializeChangesets([]*proto.NamedChangeSet{})
		require.NoError(t, err)
		require.Empty(t, data)

		got, err := deserializeChangesets(data)
		require.NoError(t, err)
		require.Empty(t, got)
	})
}

func TestDeserializeChangesetsTruncated(t *testing.T) {
	cs := []*proto.NamedChangeSet{
		makeChangeSet("bank", []byte("hello"), []byte("world")),
	}
	data, err := serializeChangesets(cs)
	require.NoError(t, err)

	// Every strict prefix is truncated. Because the enclosing record is length-delimited by the underlying
	// WAL, a truncated payload here is corruption and must surface an error, never a silent partial decode.
	for length := 1; length < len(data); length++ {
		_, err := deserializeChangesets(data[:length])
		require.Error(t, err)
	}
}

func TestDeserializeCorruptChangeset(t *testing.T) {
	// A length prefix pointing at bytes that are not a valid NamedChangeSet protobuf must surface an error.
	// Layout: [len=2][0x08 0xFF] where 0x08 is a varint field tag (field 1, wire type 0) followed by a
	// truncated varint, which the protobuf decoder rejects.
	payload := []byte{0x02, 0x08, 0xFF}
	_, err := deserializeChangesets(payload)
	require.Error(t, err)
}
