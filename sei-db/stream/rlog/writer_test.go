package rlog

import (
	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/stretchr/testify/require"
	"testing"
)

var (
	ChangeSets = []iavl.ChangeSet{
		{Pairs: mockKVPairs("hello", "world")},
		{Pairs: mockKVPairs("hello", "world1", "hello1", "world1")},
		{Pairs: mockKVPairs("hello2", "world1", "hello3", "world1")},
	}
)

func TestSynchronousWrite(t *testing.T) {
	dir := t.TempDir()
	manager, err := NewManager(logger.NewNopLogger(), dir, Config{})
	require.NoError(t, err)
	for i, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{
				Name:      "test",
				Changeset: changes,
			},
		}
		entry := &proto.ReplayLogEntry{}
		entry.Changesets = cs
		err := manager.writer.Write(LogEntry{Index: uint64(i + 1), Data: *entry})
		require.NoError(t, err)
	}
	lastIndex, err := manager.LastIndex()
	require.NoError(t, err)
	require.Equal(t, uint64(3), lastIndex)

}

func TestAsyncWrite(t *testing.T) {
	dir := t.TempDir()
	manager, err := NewManager(logger.NewNopLogger(), dir, Config{WriteBufferSize: 10})
	require.NoError(t, err)
	for i, changes := range ChangeSets {
		cs := []*proto.NamedChangeSet{
			{
				Name:      "test",
				Changeset: changes,
			},
		}
		entry := &proto.ReplayLogEntry{}
		entry.Changesets = cs
		err := manager.writer.Write(LogEntry{Index: uint64(i + 1), Data: *entry})
		require.NoError(t, err)
		lastIndex, err := manager.LastIndex()
		require.NoError(t, err)
		// Writes happen async, so lastIndex should not move yet
		require.Greater(t, uint64(3), lastIndex)
	}
	// Wait for all writes to be flushed
	err = manager.writer.CheckAsyncCommit()
	require.NoError(t, err)
	err = manager.writer.WaitAsyncCommit()
	require.NoError(t, err)
	lastIndex, err := manager.LastIndex()
	require.NoError(t, err)
	require.Equal(t, uint64(3), lastIndex)
}

func mockKVPairs(kvPairs ...string) []*iavl.KVPair {
	result := make([]*iavl.KVPair, len(kvPairs)/2)
	for i := 0; i < len(kvPairs); i += 2 {
		result[i/2] = &iavl.KVPair{
			Key:   []byte(kvPairs[i]),
			Value: []byte(kvPairs[i+1]),
		}
	}
	return result
}
