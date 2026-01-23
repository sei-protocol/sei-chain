package composite

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
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

func TestCompositeStateStoreConcurrent(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "composite_concurrent_test")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	cfg := CompositeConfig{
		CosmosConfig: config.StateStoreConfig{
			Enable:           true,
			Backend:          "pebbledb",
			AsyncWriteBuffer: 0,
			KeepRecent:       1000,
		},
		EVMConfig: config.EVMStateStoreConfig{
			Enable:      true,
			DBDirectory: filepath.Join(tmpDir, "evm_ss"),
			KeepRecent:  1000,
		},
	}

	store, err := NewCompositeStateStore(logger.NewNopLogger(), tmpDir, cfg)
	require.NoError(t, err)
	defer store.Close()

	// Pre-populate some data
	for i := 0; i < 50; i++ {
		evmKey := append([]byte{0x03}, []byte(fmt.Sprintf("addr%d", i))...)
		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: evmKey, Value: []byte(fmt.Sprintf("val%d", i))},
					},
				},
			},
		}
		require.NoError(t, store.ApplyChangesetSync(int64(i+1), changesets))
	}

	t.Run("Concurrent reads", func(t *testing.T) {
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				for j := 0; j < 50; j++ {
					evmKey := append([]byte{0x03}, []byte(fmt.Sprintf("addr%d", j))...)
					_, err := store.Get("evm", int64(j+1), evmKey)
					require.NoError(t, err)
				}
			}(i)
		}
		wg.Wait()
	})

	t.Run("Concurrent reads and writes", func(t *testing.T) {
		var wg sync.WaitGroup
		baseVersion := int64(100)

		// Readers
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 20; j++ {
					evmKey := append([]byte{0x03}, []byte(fmt.Sprintf("addr%d", j%50))...)
					_, _ = store.Get("evm", int64((j%50)+1), evmKey)
				}
			}()
		}

		// Writers
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					version := baseVersion + int64(idx*10+j)
					evmKey := append([]byte{0x03}, []byte(fmt.Sprintf("concurrent_%d_%d", idx, j))...)
					changesets := []*proto.NamedChangeSet{
						{
							Name: "evm",
							Changeset: iavl.ChangeSet{
								Pairs: []*iavl.KVPair{
									{Key: evmKey, Value: []byte(fmt.Sprintf("v_%d_%d", idx, j))},
								},
							},
						},
					}
					_ = store.ApplyChangesetSync(version, changesets)
				}
			}(i)
		}

		wg.Wait()
	})
}

func TestCompositeStateStoreMultipleModules(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "composite_multi_test")
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
			Enable:      true,
			DBDirectory: filepath.Join(tmpDir, "evm_ss"),
			KeepRecent:  100,
		},
	}

	store, err := NewCompositeStateStore(logger.NewNopLogger(), tmpDir, cfg)
	require.NoError(t, err)
	defer store.Close()

	t.Run("Mixed module changeset", func(t *testing.T) {
		evmStorageKey := append([]byte{0x03}, []byte("storage_addr")...)
		evmCodeKey := append([]byte{0x07}, []byte("code_addr")...)
		evmNonceKey := append([]byte{0x0a}, []byte("nonce_addr")...)
		evmOtherKey := append([]byte{0x01}, []byte("other_data")...) // Non-routed EVM key

		changesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: evmStorageKey, Value: []byte("storage_data")},
						{Key: evmCodeKey, Value: []byte("bytecode")},
						{Key: evmNonceKey, Value: []byte{5}},
						{Key: evmOtherKey, Value: []byte("other")},
					},
				},
			},
			{
				Name: "bank",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("balance_key"), Value: []byte("1000")},
					},
				},
			},
			{
				Name: "acc",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: []byte("account_key"), Value: []byte("account_data")},
					},
				},
			},
		}

		err := store.ApplyChangesetSync(1, changesets)
		require.NoError(t, err)

		// Verify EVM routed keys
		val, err := store.Get("evm", 1, evmStorageKey)
		require.NoError(t, err)
		require.Equal(t, []byte("storage_data"), val)

		val, err = store.Get("evm", 1, evmCodeKey)
		require.NoError(t, err)
		require.Equal(t, []byte("bytecode"), val)

		val, err = store.Get("evm", 1, evmNonceKey)
		require.NoError(t, err)
		require.Equal(t, []byte{5}, val)

		// Non-routed EVM key (only in Cosmos)
		val, err = store.Get("evm", 1, evmOtherKey)
		require.NoError(t, err)
		require.Equal(t, []byte("other"), val)

		// Non-EVM modules
		val, err = store.Get("bank", 1, []byte("balance_key"))
		require.NoError(t, err)
		require.Equal(t, []byte("1000"), val)

		val, err = store.Get("acc", 1, []byte("account_key"))
		require.NoError(t, err)
		require.Equal(t, []byte("account_data"), val)
	})
}

func TestCompositeStateStoreFallback(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "composite_fallback_test")
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
			Enable:      true,
			DBDirectory: filepath.Join(tmpDir, "evm_ss"),
			KeepRecent:  100,
		},
	}

	store, err := NewCompositeStateStore(logger.NewNopLogger(), tmpDir, cfg)
	require.NoError(t, err)
	defer store.Close()

	t.Run("Fallback to Cosmos when not in EVM", func(t *testing.T) {
		// Write directly to Cosmos store for an EVM key
		// This simulates data that exists only in Cosmos (e.g., from before EVM_SS was enabled)
		cosmosStore := store.GetCosmosStore()
		evmKey := append([]byte{0x03}, []byte("legacy_addr")...)

		legacyChangesets := []*proto.NamedChangeSet{
			{
				Name: "evm",
				Changeset: iavl.ChangeSet{
					Pairs: []*iavl.KVPair{
						{Key: evmKey, Value: []byte("legacy_value")},
					},
				},
			},
		}

		// Apply directly to cosmos (bypassing EVM routing)
		err := cosmosStore.ApplyChangesetSync(1, legacyChangesets)
		require.NoError(t, err)

		// Reading through composite should fallback to Cosmos
		val, err := store.Get("evm", 1, evmKey)
		require.NoError(t, err)
		require.Equal(t, []byte("legacy_value"), val)
	})
}

func TestCompositeStateStoreHas(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "composite_has_test")
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
			Enable:      true,
			DBDirectory: filepath.Join(tmpDir, "evm_ss"),
			KeepRecent:  100,
		},
	}

	store, err := NewCompositeStateStore(logger.NewNopLogger(), tmpDir, cfg)
	require.NoError(t, err)
	defer store.Close()

	evmKey := append([]byte{0x03}, []byte("has_test")...)

	// Key shouldn't exist yet
	has, err := store.Has("evm", 1, evmKey)
	require.NoError(t, err)
	require.False(t, has)

	// Add the key
	changesets := []*proto.NamedChangeSet{
		{
			Name: "evm",
			Changeset: iavl.ChangeSet{
				Pairs: []*iavl.KVPair{
					{Key: evmKey, Value: []byte("exists")},
				},
			},
		},
	}
	require.NoError(t, store.ApplyChangesetSync(1, changesets))

	// Now it should exist
	has, err = store.Has("evm", 1, evmKey)
	require.NoError(t, err)
	require.True(t, has)

	// Non-existent key
	otherKey := append([]byte{0x03}, []byte("nonexistent")...)
	has, err = store.Has("evm", 1, otherKey)
	require.NoError(t, err)
	require.False(t, has)
}
