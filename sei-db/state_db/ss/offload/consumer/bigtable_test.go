package consumer

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload/historical"
	"github.com/stretchr/testify/require"
)

func TestBigtableConfigApplyDefaults(t *testing.T) {
	cfg := BigtableConfig{
		ProjectID:  "project",
		InstanceID: "instance",
		Table:      "state",
	}
	cfg.ApplyDefaults()
	require.Equal(t, historical.DefaultBigtableFamily, cfg.Family)
	require.Equal(t, historical.DefaultBigtableShards, cfg.Shards)
	require.Equal(t, defaultBigtableMutationWorkers, cfg.MutationWorkers)
	require.NoError(t, cfg.Validate())
}

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

func TestBigtableSinkWriteBatchPipelinesRowsAndOrdersMarkers(t *testing.T) {
	rowStarted := make(chan int64, 2)
	markerWritten := make(chan int64, 2)
	releaseRows := map[int64]chan struct{}{
		1: make(chan struct{}),
		2: make(chan struct{}),
	}
	var activeRows atomic.Int32
	var sawConcurrentRows atomic.Bool
	var mu sync.Mutex
	rowsDone := make(map[int64]bool)
	var markers []int64
	var markerBeforeRowsDone bool

	sink := &bigtableSink{
		family:          historical.DefaultBigtableFamily,
		shards:          historical.DefaultBigtableShards,
		mutationWorkers: 2,
		applyBulk: func(ctx context.Context, mutations []historical.BigtableRowMutation) ([]error, error) {
			version, ok := historical.BigtableVersionFromRowKey(mutations[0].RowKey)
			require.True(t, ok)
			if mutations[0].RowKey == historical.BigtableVersionRowKey(version) {
				mu.Lock()
				if !rowsDone[version] {
					markerBeforeRowsDone = true
				}
				markers = append(markers, version)
				mu.Unlock()
				markerWritten <- version
				return make([]error, len(mutations)), nil
			}
			if activeRows.Add(1) > 1 {
				sawConcurrentRows.Store(true)
			}
			rowStarted <- version
			select {
			case <-releaseRows[version]:
			case <-ctx.Done():
				activeRows.Add(-1)
				return nil, ctx.Err()
			}
			activeRows.Add(-1)
			mu.Lock()
			rowsDone[version] = true
			mu.Unlock()
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

	errCh := make(chan error, 1)
	go func() {
		errCh <- sink.WriteBatch(context.Background(), records)
	}()

	started := map[int64]bool{}
	for len(started) < 2 {
		select {
		case version := <-rowStarted:
			started[version] = true
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for pipelined row writes")
		}
	}
	require.True(t, sawConcurrentRows.Load())

	close(releaseRows[2])
	select {
	case version := <-markerWritten:
		t.Fatalf("marker %d written before earlier record rows completed", version)
	case <-time.After(100 * time.Millisecond):
	}

	close(releaseRows[1])
	for _, want := range []int64{1, 2} {
		select {
		case got := <-markerWritten:
			require.Equal(t, want, got)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for marker %d", want)
		}
	}
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for batch write")
	}
	mu.Lock()
	defer mu.Unlock()
	require.False(t, markerBeforeRowsDone)
	require.Equal(t, []int64{1, 2}, markers)
}
