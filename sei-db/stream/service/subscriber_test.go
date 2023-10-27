package service

import (
	"sync"
	"testing"

	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/sei-protocol/sei-db/stream/changelog"
	"github.com/stretchr/testify/require"
)

var (
	ChangeSets = []iavl.ChangeSet{
		{Pairs: changelog.MockKVPairs("hello", "world")},
		{Pairs: changelog.MockKVPairs("hello1", "world1", "hello2", "world2")},
		{Pairs: changelog.MockKVPairs("hello3", "world3")},
	}
)

func TestSubscribe(t *testing.T) {
	dir := t.TempDir()
	logStream, err := changelog.NewStream(logger.NewNopLogger(), dir, changelog.Config{})
	require.NoError(t, err)
	for i, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{
				Name:      "test",
				Changeset: changes,
			},
		}
		entry := &proto.ChangelogEntry{}
		entry.Changesets = cs
		err := logStream.Write(uint64(i+1), *entry)
		require.NoError(t, err)
	}
	var total = 0
	wg := sync.WaitGroup{}
	wg.Add(3)
	service := NewSubscriber(logger.NewNopLogger(), dir, func(index uint64, entry proto.ChangelogEntry) error {
		total += 1
		wg.Done()
		return nil
	})
	service.Start(uint64(1))
	// wait until it caught up
	wg.Wait()
	err = service.CheckError()
	require.NoError(t, err)
	err = service.Stop()
	require.NoError(t, err)
	require.Equal(t, 3, total)
}
