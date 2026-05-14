package consumer

import (
	"context"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload/historical"
	"github.com/stretchr/testify/require"
)

func TestBigtableSinkWritesMutationRowsAndVersionMarker(t *testing.T) {
	var rows []string
	sink := &bigtableSink{
		family: historical.DefaultBigtableFamily,
		shards: historical.DefaultBigtableShards,
		applyBulk: func(_ context.Context, mutations []historical.BigtableRowMutation) ([]error, error) {
			for _, mutation := range mutations {
				rows = append(rows, mutation.RowKey)
			}
			return make([]error, len(mutations)), nil
		},
	}
	entry := &proto.ChangelogEntry{
		Version: 7,
		Changesets: []*proto.NamedChangeSet{{
			Name: "bank",
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("k1"), Value: []byte("old")},
				{Key: []byte("k1"), Value: []byte("new")},
				{Key: []byte("drop"), Delete: true},
			}},
		}},
		Upgrades: []*proto.TreeNameUpgrade{{Name: "new-store"}},
	}

	require.NoError(t, sink.Write(context.Background(), Record{Topic: "t", Partition: 1, Offset: 2, Entry: entry}))
	require.Len(t, rows, 4)
	require.Equal(t, historical.BigtableMutationRowKey("bank", []byte("k1"), 7, historical.DefaultBigtableShards), rows[0])
	require.Equal(t, historical.BigtableMutationRowKey("bank", []byte("drop"), 7, historical.DefaultBigtableShards), rows[1])
	require.Equal(t, historical.BigtableUpgradeRowKey(7, "new-store"), rows[2])
	require.Equal(t, historical.BigtableVersionRowKey(7), rows[3])
}

func TestBigtableSinkWriteBatchWritesRowsBeforeMarkers(t *testing.T) {
	var rowVersions []int64
	var markerVersions []int64
	var markerBeforeRowsDone bool

	sink := &bigtableSink{
		family: historical.DefaultBigtableFamily,
		shards: historical.DefaultBigtableShards,
		applyBulk: func(_ context.Context, mutations []historical.BigtableRowMutation) ([]error, error) {
			isMarkerBatch := len(mutations) == 1 && mutations[0].RowKey == historical.BigtableVersionRowKey(mustBigtableVersion(t, mutations[0].RowKey))
			if isMarkerBatch && len(rowVersions) != 2 {
				markerBeforeRowsDone = true
			}
			for _, mutation := range mutations {
				version := mustBigtableVersion(t, mutation.RowKey)
				if mutation.RowKey == historical.BigtableVersionRowKey(version) {
					markerVersions = append(markerVersions, version)
				} else {
					rowVersions = append(rowVersions, version)
				}
			}
			return make([]error, len(mutations)), nil
		},
	}
	records := []Record{
		{Entry: &proto.ChangelogEntry{
			Version: 1,
			Changesets: []*proto.NamedChangeSet{{
				Name:      "bank",
				Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("k1"), Value: []byte("v1")}}},
			}},
		}},
		{Entry: &proto.ChangelogEntry{
			Version: 2,
			Changesets: []*proto.NamedChangeSet{{
				Name:      "bank",
				Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("k2"), Value: []byte("v2")}}},
			}},
		}},
	}

	require.NoError(t, sink.WriteBatch(context.Background(), records))
	require.False(t, markerBeforeRowsDone)
	require.Equal(t, []int64{1, 2}, rowVersions)
	require.Equal(t, []int64{1, 2}, markerVersions)
}

func mustBigtableVersion(t *testing.T, rowKey string) int64 {
	t.Helper()
	version, ok := historical.BigtableVersionFromRowKey(rowKey)
	require.True(t, ok)
	return version
}
