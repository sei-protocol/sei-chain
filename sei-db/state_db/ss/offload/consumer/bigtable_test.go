package consumer

import (
	"context"
	"errors"
	"testing"
	"time"

	kafkago "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload/historical"
)

// entryMessage marshals entry into a Kafka message the sink can decode.
func entryMessage(t *testing.T, entry *proto.ChangelogEntry) kafkago.Message {
	t.Helper()
	payload, err := entry.Marshal()
	require.NoError(t, err)
	return kafkago.Message{Value: payload}
}

func TestBigtableSinkWritesMutationRowsAndVersionMarker(t *testing.T) {
	var rows []string
	sink := &bigtableSink{
		family:           historical.DefaultBigtableFamily,
		shards:           historical.DefaultBigtableShards,
		bulkChunkWorkers: 1,
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

	msg := entryMessage(t, entry)
	msg.Topic, msg.Partition, msg.Offset = "t", 1, 2
	require.NoError(t, sink.WriteBatch(context.Background(), []kafkago.Message{msg}))
	require.Len(t, rows, 4)
	require.ElementsMatch(t, []string{
		historical.BigtableMutationRowKey("bank", []byte("k1"), 7, historical.DefaultBigtableShards),
		historical.BigtableMutationRowKey("bank", []byte("drop"), 7, historical.DefaultBigtableShards),
		historical.BigtableUpgradeRowKey(7, "new-store"),
	}, rows[:3])
	require.Equal(t, historical.BigtableVersionRowKey(7), rows[3])
}

func TestBigtableSinkWriteBatchRejectsUndecodableMessage(t *testing.T) {
	applyBulkCalls := 0
	sink := &bigtableSink{
		family:           historical.DefaultBigtableFamily,
		shards:           historical.DefaultBigtableShards,
		bulkChunkWorkers: 1,
		applyBulk: func(_ context.Context, mutations []historical.BigtableRowMutation) ([]error, error) {
			applyBulkCalls++
			return make([]error, len(mutations)), nil
		},
	}

	err := sink.WriteBatch(context.Background(), []kafkago.Message{{Offset: 42, Value: []byte{0xff, 0xff}}})
	require.ErrorContains(t, err, "decode message at offset 42")
	require.Zero(t, applyBulkCalls)
}

func TestBigtableSinkWriteBatchWritesRowsBeforeMarkers(t *testing.T) {
	var calls [][]string

	sink := &bigtableSink{
		family:           historical.DefaultBigtableFamily,
		shards:           historical.DefaultBigtableShards,
		bulkChunkWorkers: 1,
		applyBulk: func(_ context.Context, mutations []historical.BigtableRowMutation) ([]error, error) {
			call := make([]string, 0, len(mutations))
			for _, mutation := range mutations {
				call = append(call, mutation.RowKey)
			}
			calls = append(calls, call)
			return make([]error, len(mutations)), nil
		},
	}
	msgs := []kafkago.Message{
		entryMessage(t, &proto.ChangelogEntry{
			Version: 1,
			Changesets: []*proto.NamedChangeSet{{
				Name:      "bank",
				Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("k1"), Value: []byte("v1")}}},
			}},
		}),
		entryMessage(t, &proto.ChangelogEntry{
			Version: 2,
			Changesets: []*proto.NamedChangeSet{{
				Name:      "bank",
				Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("k2"), Value: []byte("v2")}}},
			}},
		}),
	}

	require.NoError(t, sink.WriteBatch(context.Background(), msgs))
	require.GreaterOrEqual(t, len(calls), 2)
	require.ElementsMatch(t, []string{
		historical.BigtableMutationRowKey("bank", []byte("k1"), 1, historical.DefaultBigtableShards),
		historical.BigtableMutationRowKey("bank", []byte("k2"), 2, historical.DefaultBigtableShards),
	}, flattenCalls(calls[:len(calls)-1]))
	require.Equal(t, []string{
		historical.BigtableVersionRowKey(1),
		historical.BigtableVersionRowKey(2),
	}, calls[len(calls)-1])
}

func TestBigtableRowMutationChunksSortsAndGroupsByLocality(t *testing.T) {
	rows := []historical.BigtableRowMutation{
		{RowKey: string([]byte{'u', 'z'})},
		{RowKey: string([]byte{'m', 0, 2, 'b'})},
		{RowKey: string([]byte{'m', 0, 1, 'c'})},
		{RowKey: string([]byte{'m', 0, 1, 'a'})},
		{RowKey: string([]byte{'m', 0, 1, 'b'})},
		{RowKey: string([]byte{'m', 0, 2, 'a'})},
	}

	chunks := bigtableRowMutationChunks(rows, 2)

	require.Equal(t, [][]string{
		{
			string([]byte{'m', 0, 1, 'a'}),
			string([]byte{'m', 0, 1, 'b'}),
		},
		{
			string([]byte{'m', 0, 1, 'c'}),
		},
		{
			string([]byte{'m', 0, 2, 'a'}),
			string([]byte{'m', 0, 2, 'b'}),
		},
		{
			string([]byte{'u', 'z'}),
		},
	}, chunkRowKeys(chunks))
}

func TestBigtableSinkWriteBatchChunksRecordRowsBeforeMarkers(t *testing.T) {
	var calls [][]string
	sink := &bigtableSink{
		family:           historical.DefaultBigtableFamily,
		shards:           2,
		bulkChunkRows:    1,
		bulkChunkWorkers: 1,
		applyBulk: func(_ context.Context, mutations []historical.BigtableRowMutation) ([]error, error) {
			call := make([]string, 0, len(mutations))
			for _, mutation := range mutations {
				call = append(call, mutation.RowKey)
			}
			calls = append(calls, call)
			return make([]error, len(mutations)), nil
		},
	}
	msgs := []kafkago.Message{
		entryMessage(t, &proto.ChangelogEntry{
			Version: 1,
			Changesets: []*proto.NamedChangeSet{{
				Name: "bank",
				Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
					{Key: []byte("k1"), Value: []byte("v1")},
					{Key: []byte("k2"), Value: []byte("v2")},
				}},
			}},
			Upgrades: []*proto.TreeNameUpgrade{{Name: "new-store"}},
		}),
		entryMessage(t, &proto.ChangelogEntry{
			Version: 2,
			Changesets: []*proto.NamedChangeSet{{
				Name: "bank",
				Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
					{Key: []byte("k3"), Value: []byte("v3")},
				}},
			}},
		}),
	}

	require.NoError(t, sink.WriteBatch(context.Background(), msgs))

	require.Greater(t, len(calls), 2)
	markerCall := calls[len(calls)-1]
	require.Equal(t, []string{
		historical.BigtableVersionRowKey(1),
		historical.BigtableVersionRowKey(2),
	}, markerCall)
	for _, call := range calls[:len(calls)-1] {
		require.Len(t, call, 1)
		require.NotEqual(t, markerCall, call)
	}
	require.ElementsMatch(t, []string{
		historical.BigtableMutationRowKey("bank", []byte("k1"), 1, 2),
		historical.BigtableMutationRowKey("bank", []byte("k2"), 1, 2),
		historical.BigtableUpgradeRowKey(1, "new-store"),
		historical.BigtableMutationRowKey("bank", []byte("k3"), 2, 2),
	}, flattenCalls(calls[:len(calls)-1]))
}

func TestBigtableSinkAppliesRecordChunksConcurrently(t *testing.T) {
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	sink := &bigtableSink{
		bulkChunkRows:    1,
		bulkChunkWorkers: 2,
		applyBulk: func(ctx context.Context, rows []historical.BigtableRowMutation) ([]error, error) {
			select {
			case started <- struct{}{}:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			select {
			case <-release:
				return make([]error, len(rows)), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	rows := []historical.BigtableRowMutation{
		{RowKey: string([]byte{'m', 0, 1, 'a'})},
		{RowKey: string([]byte{'m', 0, 2, 'a'})},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- sink.applyRecordRowMutations(context.Background(), rows)
	}()

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			close(release)
			require.FailNow(t, "timed out waiting for concurrent bigtable chunks")
		}
	}
	close(release)
	require.NoError(t, <-errCh)
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

func chunkRowKeys(chunks [][]historical.BigtableRowMutation) [][]string {
	out := make([][]string, 0, len(chunks))
	for _, chunk := range chunks {
		out = append(out, flattenRowMutations(chunk))
	}
	return out
}

func flattenCalls(calls [][]string) []string {
	var out []string
	for _, call := range calls {
		out = append(out, call...)
	}
	return out
}

func flattenRowMutations(rows []historical.BigtableRowMutation) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.RowKey)
	}
	return out
}
