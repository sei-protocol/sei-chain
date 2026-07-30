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
		require.Equal(t, changesetFormatVersion, data[0], "payload must begin with the format version")

		got, err := deserializeChangesets(data)
		require.NoError(t, err)
		require.Equal(t, cs, got)
	})

	t.Run("empty changeset list", func(t *testing.T) {
		data, err := serializeChangesets([]*proto.NamedChangeSet{})
		require.NoError(t, err)
		require.Equal(t, []byte{changesetFormatVersion}, data) // just the version byte

		got, err := deserializeChangesets(data)
		require.NoError(t, err)
		require.Empty(t, got)
	})
}

func TestDeserializeUnknownVersion(t *testing.T) {
	// A payload whose leading version byte is not recognized must be rejected before any decoding.
	_, err := deserializeChangesets([]byte{changesetFormatVersion + 1})
	require.Error(t, err)

	// An empty payload is missing the version byte entirely.
	_, err = deserializeChangesets(nil)
	require.Error(t, err)
}

func TestDeserializeChangesetsTruncated(t *testing.T) {
	cs := []*proto.NamedChangeSet{
		makeChangeSet("bank", []byte("hello"), []byte("world")),
	}
	data, err := serializeChangesets(cs)
	require.NoError(t, err)

	// Every strict prefix that reaches past the version byte is truncated. Because the enclosing record is
	// length-delimited by the underlying WAL, a truncated payload here is corruption and must surface an
	// error, never a silent partial decode. (Length 1 is the bare version byte, a valid empty payload.)
	for length := 2; length < len(data); length++ {
		_, err := deserializeChangesets(data[:length])
		require.Error(t, err)
	}
}

func TestDeserializeCorruptChangeset(t *testing.T) {
	// A length prefix pointing at bytes that are not a valid NamedChangeSet protobuf must surface an error.
	// Layout: [version][len=2][0x08 0xFF] where 0x08 is a varint field tag (field 1, wire type 0) followed by
	// a truncated varint, which the protobuf decoder rejects.
	payload := []byte{changesetFormatVersion, 0x02, 0x08, 0xFF}
	_, err := deserializeChangesets(payload)
	require.Error(t, err)
}
