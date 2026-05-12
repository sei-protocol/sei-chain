package consumer

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func TestScyllaConfigValidate(t *testing.T) {
	cfg := ScyllaConfig{
		Hosts:    []string{"127.0.0.1"},
		Keyspace: "sei_history",
	}
	require.NoError(t, cfg.Validate())

	cfg.TimeoutMS = -1
	require.ErrorContains(t, cfg.Validate(), "timeout")

	cfg.TimeoutMS = 0
	cfg.MutationWorkers = -1
	require.ErrorContains(t, cfg.Validate(), "mutation workers")
}

func TestScyllaConfigApplyDefaults(t *testing.T) {
	cfg := ScyllaConfig{
		Hosts:    []string{"127.0.0.1"},
		Keyspace: "sei_history",
	}
	cfg.ApplyDefaults()
	require.Equal(t, "local_quorum", cfg.Consistency)
	require.Equal(t, 2000, cfg.TimeoutMS)
	require.Equal(t, 2000, cfg.ConnectTimeoutMS)
	require.Equal(t, 4, cfg.NumConns)
	require.Equal(t, 16, cfg.MutationWorkers)
}

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

func TestScyllaCQLShape(t *testing.T) {
	for _, frag := range []string{
		"INSERT INTO state_mutations",
		"store_name",
		"state_key",
		"version",
		"value",
		"deleted",
	} {
		require.Contains(t, insertMutationCQL, frag)
	}
	for _, frag := range []string{
		"INSERT INTO state_versions",
		"bucket",
		"version",
		"kafka_topic",
		"kafka_partition",
		"kafka_offset",
		"ingested_at",
	} {
		require.Contains(t, insertVersionCQL, frag)
	}
	require.True(t, strings.Contains(selectLatestVersionCQL, "LIMIT 1"))
}

func TestScyllaSinkWritesRowsConcurrentlyBeforeVersionMarker(t *testing.T) {
	rowStarted := make(chan struct{}, 8)
	releaseRows := make(chan struct{})
	var activeRows atomic.Int32
	var sawConcurrentRows atomic.Bool
	var markerBeforeRowsDone atomic.Bool
	var versionMarkers atomic.Int32

	sink := &scyllaSink{
		mutationWorkers: 2,
		exec: func(ctx context.Context, stmt string, _ ...interface{}) error {
			if strings.Contains(stmt, "state_versions") {
				if activeRows.Load() != 0 {
					markerBeforeRowsDone.Store(true)
				}
				versionMarkers.Add(1)
				return nil
			}
			if activeRows.Add(1) > 1 {
				sawConcurrentRows.Store(true)
			}
			rowStarted <- struct{}{}
			select {
			case <-releaseRows:
			case <-ctx.Done():
				activeRows.Add(-1)
				return ctx.Err()
			}
			activeRows.Add(-1)
			return nil
		},
	}
	entry := &proto.ChangelogEntry{
		Version: 7,
		Changesets: []*proto.NamedChangeSet{{
			Name: "bank",
			Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{
				{Key: []byte("k1"), Value: []byte("v1")},
				{Key: []byte("k2"), Value: []byte("v2")},
				{Key: []byte("k3"), Value: []byte("v3")},
			}},
		}},
		Upgrades: []*proto.TreeNameUpgrade{{Name: "new-store"}},
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- sink.writeRecord(context.Background(), Record{Topic: "t", Partition: 1, Offset: 2, Entry: entry})
	}()

	releaseClosed := false
	closeRelease := func() {
		if !releaseClosed {
			close(releaseRows)
			releaseClosed = true
		}
	}
	defer closeRelease()

	for i := 0; i < 2; i++ {
		select {
		case <-rowStarted:
		case <-time.After(time.Second):
			closeRelease()
			t.Fatal("timed out waiting for concurrent row writes")
		}
	}
	require.True(t, sawConcurrentRows.Load())
	require.Equal(t, int32(0), versionMarkers.Load(), "version marker must wait for row writes")

	closeRelease()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for record write")
	}
	require.False(t, markerBeforeRowsDone.Load())
	require.Equal(t, int32(1), versionMarkers.Load())
}
