package consumer

import (
	"context"
	"errors"
	"strings"
	"sync"
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

func TestScyllaSinkCompactsDuplicateMutations(t *testing.T) {
	type write struct {
		value   []byte
		deleted bool
	}
	var mu sync.Mutex
	writes := make(map[string]write)
	sink := &scyllaSink{
		mutationWorkers: 1,
		exec: func(_ context.Context, stmt string, values ...interface{}) error {
			if !strings.Contains(stmt, "state_mutations") {
				return nil
			}
			storeName := values[0].(string)
			key := string(values[1].([]byte))
			value := values[3].([]byte)
			deleted := values[4].(bool)
			mu.Lock()
			writes[storeName+"/"+key] = write{value: value, deleted: deleted}
			mu.Unlock()
			return nil
		},
	}
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

	require.NoError(t, sink.writeRecordRows(context.Background(), entry))
	require.Len(t, writes, 3)
	require.Equal(t, write{value: []byte("new")}, writes["bank/k"])
	require.Equal(t, write{deleted: true}, writes["bank/drop"])
	require.Equal(t, write{value: []byte("separate-store")}, writes["evm/k"])
}

func TestScyllaSinkWriteBatchPipelinesRowsAndOrdersMarkers(t *testing.T) {
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

	sink := &scyllaSink{
		mutationWorkers: 1,
		exec: func(ctx context.Context, stmt string, values ...interface{}) error {
			switch {
			case strings.Contains(stmt, "state_mutations"):
				version := values[2].(int64)
				if activeRows.Add(1) > 1 {
					sawConcurrentRows.Store(true)
				}
				rowStarted <- version
				select {
				case <-releaseRows[version]:
				case <-ctx.Done():
					activeRows.Add(-1)
					return ctx.Err()
				}
				activeRows.Add(-1)
				mu.Lock()
				rowsDone[version] = true
				mu.Unlock()
				return nil
			case strings.Contains(stmt, "state_versions"):
				version := values[1].(int64)
				mu.Lock()
				if !rowsDone[version] {
					markerBeforeRowsDone = true
				}
				markers = append(markers, version)
				mu.Unlock()
				markerWritten <- version
				return nil
			default:
				return nil
			}
		},
	}
	records := []Record{
		{
			Topic:     "t",
			Partition: 0,
			Offset:    10,
			Entry: &proto.ChangelogEntry{
				Version: 1,
				Changesets: []*proto.NamedChangeSet{{
					Name:      "bank",
					Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("k1"), Value: []byte("v1")}}},
				}},
			},
		},
		{
			Topic:     "t",
			Partition: 0,
			Offset:    11,
			Entry: &proto.ChangelogEntry{
				Version: 2,
				Changesets: []*proto.NamedChangeSet{{
					Name:      "bank",
					Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("k2"), Value: []byte("v2")}}},
				}},
			},
		},
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

func TestScyllaSinkWriteBatchReturnsRowErrorAfterLaterRowFailure(t *testing.T) {
	rowErr := errors.New("row write failed")
	rowStarted := make(chan int64, 2)
	releaseFirst := make(chan struct{})

	sink := &scyllaSink{
		mutationWorkers: 1,
		exec: func(ctx context.Context, stmt string, values ...interface{}) error {
			if !strings.Contains(stmt, "state_mutations") {
				return nil
			}
			version := values[2].(int64)
			rowStarted <- version
			if version == 2 {
				return rowErr
			}
			select {
			case <-releaseFirst:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		},
	}
	records := []Record{
		{
			Entry: &proto.ChangelogEntry{
				Version: 1,
				Changesets: []*proto.NamedChangeSet{{
					Name:      "bank",
					Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("k1"), Value: []byte("v1")}}},
				}},
			},
		},
		{
			Entry: &proto.ChangelogEntry{
				Version: 2,
				Changesets: []*proto.NamedChangeSet{{
					Name:      "bank",
					Changeset: proto.ChangeSet{Pairs: []*proto.KVPair{{Key: []byte("k2"), Value: []byte("v2")}}},
				}},
			},
		},
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
			close(releaseFirst)
			t.Fatal("timed out waiting for row writes")
		}
	}
	close(releaseFirst)

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, rowErr)
		require.NotErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for batch write")
	}
}
