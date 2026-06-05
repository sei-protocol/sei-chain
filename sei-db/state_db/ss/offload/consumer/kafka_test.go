package consumer

import (
	"testing"

	gogoproto "github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"

	dbproto "github.com/sei-protocol/sei-chain/sei-db/proto"
)

func TestDecodeEntryRoundtrip(t *testing.T) {
	entry := &dbproto.ChangelogEntry{
		Version: 42,
		Changesets: []*dbproto.NamedChangeSet{{
			Name: "evm",
			Changeset: dbproto.ChangeSet{
				Pairs: []*dbproto.KVPair{
					{Key: []byte("k1"), Value: []byte("v1")},
					{Key: []byte("k2"), Delete: true},
				},
			},
		}},
	}

	payload, err := gogoproto.Marshal(entry)
	require.NoError(t, err)

	got, err := DecodeEntry(payload)
	require.NoError(t, err)
	require.Equal(t, entry.Version, got.Version)
	require.Len(t, got.Changesets, 1)
	require.Equal(t, "evm", got.Changesets[0].Name)
	require.Len(t, got.Changesets[0].Changeset.Pairs, 2)
	require.Equal(t, []byte("v1"), got.Changesets[0].Changeset.Pairs[0].Value)
	require.True(t, got.Changesets[0].Changeset.Pairs[1].Delete)
}

func TestDecodeEntryRejectsGarbage(t *testing.T) {
	_, err := DecodeEntry([]byte{0xff, 0xff, 0xff})
	require.Error(t, err)
}
