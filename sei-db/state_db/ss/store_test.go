package ss

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/cosmos/iavl"
	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func TestNewStateStore(t *testing.T) {
	tempDir := os.TempDir()
	homeDir := filepath.Join(tempDir, "pebbledb")
	ssConfig := config.StateStoreConfig{
		Backend:          config.PebbleDBBackend,
		AsyncWriteBuffer: 100,
		KeepRecent:       500,
	}
	stateStore, err := NewStateStore(logger.NewNopLogger(), homeDir, ssConfig)
	require.NoError(t, err)
	for i := 1; i < 50; i++ {
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
	err = stateStore.Close()
	require.NoError(t, err)

	stateStore, err = NewStateStore(logger.NewNopLogger(), homeDir, ssConfig)
	require.NoError(t, err)

	for i := 1; i < 50; i++ {
		value, err := stateStore.Get("storeA", int64(i), []byte(fmt.Sprintf("key%d", i)))
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("value%d", i), string(value))
	}
}
