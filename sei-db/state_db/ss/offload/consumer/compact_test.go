package consumer

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func TestCompactRecordsDropsNilEntries(t *testing.T) {
	records := compactRecords([]Record{
		{Entry: &proto.ChangelogEntry{Version: 1}},
		{},
		{Entry: &proto.ChangelogEntry{Version: 2}},
	})
	require.Len(t, records, 2)
	require.Equal(t, int64(1), records[0].Entry.Version)
	require.Equal(t, int64(2), records[1].Entry.Version)
}

func TestCompactRecordsCollapsesRedeliveredVersions(t *testing.T) {
	records := compactRecords([]Record{
		{Offset: 10, Entry: &proto.ChangelogEntry{Version: 1}},
		{Offset: 11, Entry: &proto.ChangelogEntry{Version: 2}},
		{Offset: 12, Entry: &proto.ChangelogEntry{Version: 1}},
		{Offset: 13, Entry: &proto.ChangelogEntry{Version: 3}},
	})
	require.Len(t, records, 3)
	// Version order is preserved; the redelivered version keeps its slot but
	// carries the newest offset for the version marker.
	require.Equal(t, int64(1), records[0].Entry.Version)
	require.Equal(t, int64(12), records[0].Offset)
	require.Equal(t, int64(2), records[1].Entry.Version)
	require.Equal(t, int64(3), records[2].Entry.Version)
}

func TestCompactMutationsKeepsLastWritePerKey(t *testing.T) {
	entry := &proto.ChangelogEntry{
		Version: 9,
		Changesets: []*proto.NamedChangeSet{{
			Name: "bank",
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("k"), Value: []byte("old")},
				{Key: []byte("drop"), Value: []byte("present")},
				{Key: []byte("k"), Value: []byte("new")},
				{Key: []byte("drop"), Delete: true},
			}},
		}, {
			Name: "evm",
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("k"), Value: []byte("separate-store")},
			}},
		}},
	}

	mutations := compactMutations(entry)
	require.Len(t, mutations, 3)
	byKey := map[string]*proto.KVPair{}
	for _, m := range mutations {
		byKey[m.storeName+"/"+string(m.pair.Key)] = m.pair
	}
	require.Equal(t, []byte("new"), byKey["bank/k"].Value)
	require.True(t, byKey["bank/drop"].Delete)
	require.Equal(t, []byte("separate-store"), byKey["evm/k"].Value)
}
