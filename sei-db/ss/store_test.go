package ss

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-db/common/logger"
	"github.com/sei-protocol/sei-db/config"
	"github.com/sei-protocol/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func TestNewStateStore(t *testing.T) {
	tempDir := os.TempDir()
	homeDir := filepath.Join(tempDir, "seidb")
	ssConfig := config.StateStoreConfig{
		DedicatedChangelog: true,
		Backend:            string(PebbleDBBackend),
		AsyncWriteBuffer:   50,
		KeepRecent:         500,
	}
	stateStore, err := NewStateStore(logger.NewNopLogger(), homeDir, ssConfig)
	require.NoError(t, err)
	for i := 1; i < 20; i++ {
		var changesets []*proto.NamedChangeSet
		kvPair := &iavl.KVPair{
			Delete: false,
			Key:    []byte(fmt.Sprintf("key%d", i)),
			Value:  []byte(fmt.Sprintf("value%d", i)),
		}
		var pairs []*iavl.KVPair
		pairs = append(pairs, kvPair)
		cs := iavl.ChangeSet{Pairs: pairs}
		ncs := &proto.NamedChangeSet{
			Name:      "storeA",
			Changeset: cs,
		}
		changesets = append(changesets, ncs)
		err := stateStore.ApplyChangesetAsync(int64(i), changesets)
		require.NoError(t, err)
	}
	// Closing the state store without waiting for data to be fully flushed
	err = stateStore.Close()
	require.NoError(t, err)

	// Reopen a new state store
	stateStore, err = NewStateStore(logger.NewNopLogger(), homeDir, ssConfig)
	require.NoError(t, err)

	// Make sure key and values can be found
	for i := 1; i < 20; i++ {
		value, err := stateStore.Get("storeA", int64(i), []byte(fmt.Sprintf("key%d", i)))
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("value%d", i), string(value))
	}

}
