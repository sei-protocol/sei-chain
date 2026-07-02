package wal

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

func TestEntrySerializeRoundTrip(t *testing.T) {
	t.Run("changeset with multiple named change sets", func(t *testing.T) {
		entry := NewFlatKVWalEntry(42, []*proto.NamedChangeSet{
			makeChangeSet("bank", []byte("a"), []byte("1")),
			makeChangeSet("evm", []byte("b"), []byte("2")),
		})

		data, err := entry.Serialize()
		require.NoError(t, err)

		got, ok, err := DeserializeFlatKVWalEntry(data)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, entry, got)
	})

	t.Run("empty (non-nil) changeset", func(t *testing.T) {
		entry := NewFlatKVWalEntry(7, []*proto.NamedChangeSet{})
		data, err := entry.Serialize()
		require.NoError(t, err)

		got, ok, err := DeserializeFlatKVWalEntry(data)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, uint64(7), got.BlockNumber)
		require.False(t, got.EndOfBlock)
		require.Empty(t, got.Changeset)
	})

	t.Run("end of block marker", func(t *testing.T) {
		entry := NewFlatKVEndOfBlockEntry(99)
		data, err := entry.Serialize()
		require.NoError(t, err)

		got, ok, err := DeserializeFlatKVWalEntry(data)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, uint64(99), got.BlockNumber)
		require.True(t, got.EndOfBlock)
		require.Nil(t, got.Changeset)
	})
}

func TestDeserializeTruncated(t *testing.T) {
	entry := NewFlatKVWalEntry(42, []*proto.NamedChangeSet{
		makeChangeSet("bank", []byte("hello"), []byte("world")),
	})
	data, err := entry.Serialize()
	require.NoError(t, err)

	// Every strict prefix is either incomplete (ok=false) or, by chance, a shorter valid record; it must
	// never yield the original entry and never panic.
	for length := 0; length < len(data); length++ {
		got, ok, err := DeserializeFlatKVWalEntry(data[:length])
		if ok {
			require.NotEqual(t, entry, got)
		}
		_ = err
	}

	// Empty input is cleanly reported as incomplete.
	got, ok, err := DeserializeFlatKVWalEntry(nil)
	require.NoError(t, err)
	require.False(t, ok)
	require.Nil(t, got)
}

func TestDeserializeCorruptChangeset(t *testing.T) {
	// A changeset record whose declared change set length points at bytes that are not a valid
	// NamedChangeSet protobuf must surface an error, not a false "ok".
	// Layout: [kind=changeset][blockNumber=1][count=1][len=2][0x08 0xFF] where 0x08 is a varint field tag
	// (field 1, wire type 0) followed by a truncated varint, which the protobuf decoder rejects.
	payload := []byte{byte(kindChangeset), 0x01, 0x01, 0x02, 0x08, 0xFF}
	_, ok, err := DeserializeFlatKVWalEntry(payload)
	require.Error(t, err)
	require.False(t, ok)
}

func TestDeserializeUnknownKind(t *testing.T) {
	_, ok, err := DeserializeFlatKVWalEntry([]byte{0xFF, 0x01})
	require.Error(t, err)
	require.False(t, ok)
}
