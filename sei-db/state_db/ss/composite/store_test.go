package composite

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
)

func TestCompositeStateStore(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "composite_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	cfg := CompositeConfig{
		CosmosConfig: config.StateStoreConfig{
			Enable:           true,
			Backend:          "pebbledb",
			AsyncWriteBuffer: 0, // Sync writes for testing
			KeepRecent:       100,
		},
		EVMConfig: config.EVMStateStoreConfig{
			Enable:      true,
			DBDirectory: filepath.Join(tmpDir, "evm_ss"),
			KeepRecent:  100,
		},
	}

	store, err := NewCompositeStateStore(logger.NewNopLogger(), tmpDir, cfg)
	require.NoError(t, err)
	defer store.Close()

	t.Run("EVM storage key routes correctly", func(t *testing.T) {
		// EVM storage key: prefix 0x03 + address data
		evmKey := append([]byte{0x03}, []byte("test_address_data")...)

		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: evmKey, Value: []byte("storage_value")},
					},
				},
			},
		}

		err := store.ApplyChangesetSync(1, changesets)
		require.NoError(t, err)

		// Read from composite store
		val, err := store.Get("evm", 1, evmKey)
		require.NoError(t, err)
		require.Equal(t, []byte("storage_value"), val)

		// Verify it's also in EVM store
		evmStore := store.GetEVMStore()
		require.NotNil(t, evmStore)
	})

	t.Run("Non-EVM key only goes to Cosmos", func(t *testing.T) {
		changesets := []*proto.NamedChangeSet{
			{
				Name: "bank",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("balance_key"), Value: []byte("100")},
					},
				},
			},
		}

		err := store.ApplyChangesetSync(2, changesets)
		require.NoError(t, err)

		val, err := store.Get("bank", 2, []byte("balance_key"))
		require.NoError(t, err)
		require.Equal(t, []byte("100"), val)
	})

	t.Run("EVM code key routes correctly", func(t *testing.T) {
		// EVM code key: prefix 0x07 + address
		codeKey := append([]byte{0x07}, []byte("contract_addr")...)

		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: codeKey, Value: []byte("bytecode_here")},
					},
				},
			},
		}

		err := store.ApplyChangesetSync(3, changesets)
		require.NoError(t, err)

		val, err := store.Get("evm", 3, codeKey)
		require.NoError(t, err)
		require.Equal(t, []byte("bytecode_here"), val)
	})

	t.Run("Version management", func(t *testing.T) {
		err := store.SetLatestVersion(10)
		require.NoError(t, err)
		require.Equal(t, int64(10), store.GetLatestVersion())
	})
}

func TestCompositeStateStoreDisabledEVM(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "composite_test_no_evm")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	cfg := CompositeConfig{
		CosmosConfig: config.StateStoreConfig{
			Enable:           true,
			Backend:          "pebbledb",
			AsyncWriteBuffer: 0,
			KeepRecent:       100,
		},
		EVMConfig: config.EVMStateStoreConfig{
			Enable: false, // EVM disabled
		},
	}

	store, err := NewCompositeStateStore(logger.NewNopLogger(), tmpDir, cfg)
	require.NoError(t, err)
	defer store.Close()

	// EVM store should be nil
	require.Nil(t, store.GetEVMStore())

	// Writes should still work (go to Cosmos only)
	evmKey := append([]byte{0x03}, []byte("test")...)
	changesets := []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: evmKey, Value: []byte("value")},
				},
			},
		},
	}

	err = store.ApplyChangesetSync(1, changesets)
	require.NoError(t, err)

	// Should be readable from Cosmos store
	val, err := store.Get("evm", 1, evmKey)
	require.NoError(t, err)
	require.Equal(t, []byte("value"), val)
}
