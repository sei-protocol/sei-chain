package rlog

import (
	"fmt"
	"sync"
	"testing"

	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func TestReplay(t *testing.T) {
	rlogManger := prepareTestData(t)
	var total = 0
	rlogManger.reader.Replay(1, 2, func(index uint64, entry proto.ReplayLogEntry) error {
		total++
		switch index {
		case 1:
			require.Equal(t, "test", entry.Changesets[0].Name)
			require.Equal(t, []byte("hello"), entry.Changesets[0].Changeset.Pairs[0].Key)
			require.Equal(t, []byte("world"), entry.Changesets[0].Changeset.Pairs[0].Value)
		case 2:
			require.Equal(t, []byte("hello1"), entry.Changesets[0].Changeset.Pairs[0].Key)
			require.Equal(t, []byte("world1"), entry.Changesets[0].Changeset.Pairs[0].Value)
			require.Equal(t, []byte("hello2"), entry.Changesets[0].Changeset.Pairs[1].Key)
			require.Equal(t, []byte("world2"), entry.Changesets[0].Changeset.Pairs[1].Value)
		default:
			require.Fail(t, fmt.Sprintf("unexpected index %d", index))
		}
		return nil
	})
	require.Equal(t, 2, total)
	err := rlogManger.Close()
	require.NoError(t, err)
}

func TestSubscribe(t *testing.T) {
	rlogManger := prepareTestData(t)
	var total = 0
	wg := sync.WaitGroup{}
	wg.Add(1)
	rlogManger.Reader().StartSubscriber(1, func(index uint64, entry proto.ReplayLogEntry) error {
		total++
		require.Equal(t, "test", entry.Changesets[0].Name)
		require.LessOrEqual(t, index, uint64(3))
		// stop at index 3
		if index >= 3 {
			wg.Done()
		}
		return nil
	})
	// wait until it caught up
	wg.Wait()
	err := rlogManger.Reader().CheckSubscriber()
	require.NoError(t, err)
	err = rlogManger.Close()
	require.NoError(t, err)
	require.Equal(t, 3, total)
}

func TestRandomRead(t *testing.T) {
	rlogManger := prepareTestData(t)
	entry, err := rlogManger.reader.ReadAt(2)
	require.NoError(t, err)
	require.Equal(t, []byte("hello1"), entry.Changesets[0].Changeset.Pairs[0].Key)
	require.Equal(t, []byte("world1"), entry.Changesets[0].Changeset.Pairs[0].Value)
	require.Equal(t, []byte("hello2"), entry.Changesets[0].Changeset.Pairs[1].Key)
	require.Equal(t, []byte("world2"), entry.Changesets[0].Changeset.Pairs[1].Value)
	entry, err = rlogManger.reader.ReadAt(1)
	require.NoError(t, err)
	require.Equal(t, []byte("hello"), entry.Changesets[0].Changeset.Pairs[0].Key)
	require.Equal(t, []byte("world"), entry.Changesets[0].Changeset.Pairs[0].Value)
	entry, err = rlogManger.reader.ReadAt(3)
	require.NoError(t, err)
	require.Equal(t, []byte("hello3"), entry.Changesets[0].Changeset.Pairs[0].Key)
	require.Equal(t, []byte("world3"), entry.Changesets[0].Changeset.Pairs[0].Value)
}

func prepareTestData(t *testing.T) *Manager {
	dir := t.TempDir()
	manager, err := NewManager(logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	writeTestData(manager)
	return manager
}

func writeTestData(manager *Manager) {
	for i, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{
				Name:      "test",
				Changeset: changes,
			},
		}
		entry := &proto.ReplayLogEntry{}
		entry.Changesets = cs
		_ = manager.writer.Write(LogEntry{Index: uint64(i + 1), Data: *entry})
	}
}
