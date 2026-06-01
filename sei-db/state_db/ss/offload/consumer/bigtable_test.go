package consumer

import (
	"context"
	"errors"
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
	var calls [][]string

	sink := &bigtableSink{
		family: historical.DefaultBigtableFamily,
		shards: historical.DefaultBigtableShards,
		applyBulk: func(_ context.Context, mutations []historical.BigtableRowMutation) ([]error, error) {
			call := make([]string, 0, len(mutations))
			for _, mutation := range mutations {
				call = append(call, mutation.RowKey)
			}
			calls = append(calls, call)
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
	require.Equal(t, [][]string{
		{
			historical.BigtableMutationRowKey("bank", []byte("k1"), 1, historical.DefaultBigtableShards),
			historical.BigtableMutationRowKey("bank", []byte("k2"), 2, historical.DefaultBigtableShards),
		},
		{
			historical.BigtableVersionRowKey(1),
			historical.BigtableVersionRowKey(2),
		},
	}, calls)
}

func TestBigtableBulkErrorValidatesMutationResultCount(t *testing.T) {
	rows := []historical.BigtableRowMutation{{RowKey: "row-1"}, {RowKey: "row-2"}}

	err := bigtableBulkError(rows, []error{nil}, nil)

	require.ErrorContains(t, err, "mutation results")
}

func TestBigtableBulkErrorWrapsRowError(t *testing.T) {
	rowErr := errors.New("failed")
	rows := []historical.BigtableRowMutation{{RowKey: "row-1"}}

	err := bigtableBulkError(rows, []error{rowErr}, nil)

	require.ErrorIs(t, err, rowErr)
	require.ErrorContains(t, err, "row-1")
}
