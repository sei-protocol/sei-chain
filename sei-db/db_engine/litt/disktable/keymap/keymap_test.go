package keymap

import (
	"os"
	"path"
	"testing"

	"github.com/Layr-Labs/eigenda/litt/types"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigenda/test"
	"github.com/Layr-Labs/eigenda/test/random"
	"github.com/Layr-Labs/eigensdk-go/logging"
	"github.com/stretchr/testify/require"
)

var builders = []keymapBuilder{
	buildMemKeymap,
	buildLevelDBKeymap,
}

type keymapBuilder func(logger logging.Logger, path string) (Keymap, error)

func buildMemKeymap(logger logging.Logger, path string) (Keymap, error) {
	kmap, _, err := NewMemKeymap(logger, path, true)
	if err != nil {
		return nil, err
	}

	return kmap, nil
}

func buildLevelDBKeymap(logger logging.Logger, path string) (Keymap, error) {
	kmap, _, err := NewUnsafeLevelDBKeymap(logger, path, true)
	if err != nil {
		return nil, err
	}

	return kmap, nil
}

func testBasicBehavior(t *testing.T, keymap Keymap) {
	rand := random.NewTestRandom()

	expected := make(map[string]types.Address)

	operations := 1000
	for i := 0; i < operations; i++ {
		choice := rand.Float64()
		if choice < 0.5 {
			// Write a random value
			key := []byte(rand.String(32))
			address := types.Address(rand.Uint64())

			err := keymap.Put([]*types.ScopedKey{{Key: key, Address: address}})
			require.NoError(t, err)
			expected[string(key)] = address
		} else if choice < 0.75 {
			// Delete a few random values
			numberToDelete := rand.Int32Range(1, 10)
			numberToDelete = min(numberToDelete, int32(len(expected)))
			keysToDelete := make([]*types.ScopedKey, 0, numberToDelete)
			for key := range expected {
				if numberToDelete == int32(len(keysToDelete)) {
					break
				}
				keysToDelete = append(keysToDelete, &types.ScopedKey{Key: []byte(key)})
				numberToDelete--
			}

			err := keymap.Delete(keysToDelete)
			require.NoError(t, err)
			for _, key := range keysToDelete {
				delete(expected, string(key.Key))
			}
		} else {
			// Write a batch of random values
			numberToWrite := rand.Int32Range(1, 10)
			pairs := make([]*types.ScopedKey, numberToWrite)
			for i := 0; i < int(numberToWrite); i++ {
				key := []byte(rand.String(32))
				address := types.Address(rand.Uint64())
				pairs[i] = &types.ScopedKey{Key: key, Address: address}
				expected[string(key)] = address
			}
			err := keymap.Put(pairs)
			require.NoError(t, err)
		}

		// Every once in a while, verify that the keymap is correct
		if rand.BoolWithProbability(0.1) {
			for key, expectedAddress := range expected {
				address, ok, err := keymap.Get([]byte(key))
				require.NoError(t, err)
				require.True(t, ok)
				require.Equal(t, expectedAddress, address)
			}
		}
	}

	for key, expectedAddress := range expected {
		address, ok, err := keymap.Get([]byte(key))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, expectedAddress, address)
	}

	err := keymap.Destroy()
	require.NoError(t, err)
}

func TestBasicBehavior(t *testing.T) {
	t.Parallel()
	testDir := t.TempDir()
	dbDir := path.Join(testDir, "keymap")

	logger := test.GetLogger()
	for _, builder := range builders {
		keymap, err := builder(logger, dbDir)
		require.NoError(t, err)
		testBasicBehavior(t, keymap)

		// verify that test dir is empty (destroy should have deleted everything)
		exists, err := util.Exists(dbDir)
		require.NoError(t, err)

		if exists {
			// Directory exists. Make sure it's empty.
			entries, err := os.ReadDir(dbDir)
			require.NoError(t, err)
			require.Empty(t, entries)
		}
	}
}

func TestRestart(t *testing.T) {
	t.Parallel()
	rand := random.NewTestRandom()
	logger := test.GetLogger()
	testDir := t.TempDir()
	dbDir := path.Join(testDir, "keymap")

	keymap, _, err := NewUnsafeLevelDBKeymap(logger, dbDir, true)
	require.NoError(t, err)

	expected := make(map[string]types.Address)

	operations := 1000
	for i := 0; i < operations; i++ {
		choice := rand.Float64()
		if choice < 0.5 {
			// Write a random value
			key := []byte(rand.String(32))
			address := types.Address(rand.Uint64())

			err := keymap.Put([]*types.ScopedKey{{Key: key, Address: address}})
			require.NoError(t, err)
			expected[string(key)] = address
		} else if choice < 0.75 {
			// Delete a few random values
			numberToDelete := rand.Int32Range(1, 10)
			numberToDelete = min(numberToDelete, int32(len(expected)))
			keysToDelete := make([]*types.ScopedKey, 0, numberToDelete)
			for key := range expected {
				if numberToDelete == int32(len(keysToDelete)) {
					break
				}
				keysToDelete = append(keysToDelete, &types.ScopedKey{Key: []byte(key)})
				numberToDelete--
			}

			err := keymap.Delete(keysToDelete)
			require.NoError(t, err)
			for _, key := range keysToDelete {
				delete(expected, string(key.Key))
			}
		} else {
			// Write a batch of random values
			numberToWrite := rand.Int32Range(1, 10)
			pairs := make([]*types.ScopedKey, numberToWrite)
			for i := 0; i < int(numberToWrite); i++ {
				key := []byte(rand.String(32))
				address := types.Address(rand.Uint64())
				pairs[i] = &types.ScopedKey{Key: key, Address: address}
				expected[string(key)] = address
			}
			err := keymap.Put(pairs)
			require.NoError(t, err)
		}

		// Every once in a while, verify that the keymap is correct
		if rand.BoolWithProbability(0.1) {
			for key, expectedAddress := range expected {
				address, ok, err := keymap.Get([]byte(key))
				require.NoError(t, err)
				require.True(t, ok)
				require.Equal(t, expectedAddress, address)
			}
		}
	}

	for key, expectedAddress := range expected {
		address, ok, err := keymap.Get([]byte(key))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, expectedAddress, address)
	}

	// Shut down the keymap and reload it
	err = keymap.Stop()
	require.NoError(t, err)

	keymap, _, err = NewUnsafeLevelDBKeymap(logger, dbDir, true)
	require.NoError(t, err)

	// Expected data should be present
	for key, expectedAddress := range expected {
		address, ok, err := keymap.Get([]byte(key))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, expectedAddress, address)
	}

	for i := 0; i < operations; i++ {
		choice := rand.Float64()
		if choice < 0.5 {
			// Write a random value
			key := []byte(rand.String(32))
			address := types.Address(rand.Uint64())

			err := keymap.Put([]*types.ScopedKey{{Key: key, Address: address}})
			require.NoError(t, err)
			expected[string(key)] = address
		} else if choice < 0.75 {
			// Delete a few random values
			numberToDelete := rand.Int32Range(1, 10)
			numberToDelete = min(numberToDelete, int32(len(expected)))
			keysToDelete := make([]*types.ScopedKey, 0, numberToDelete)
			for key := range expected {
				if numberToDelete == int32(len(keysToDelete)) {
					break
				}
				keysToDelete = append(keysToDelete, &types.ScopedKey{Key: []byte(key)})
				numberToDelete--
			}

			err := keymap.Delete(keysToDelete)
			require.NoError(t, err)
			for _, key := range keysToDelete {
				delete(expected, string(key.Key))
			}
		} else {
			// Write a batch of random values
			numberToWrite := rand.Int32Range(1, 10)
			pairs := make([]*types.ScopedKey, numberToWrite)
			for i := 0; i < int(numberToWrite); i++ {
				key := []byte(rand.String(32))
				address := types.Address(rand.Uint64())
				pairs[i] = &types.ScopedKey{Key: key, Address: address}
				expected[string(key)] = address
			}
			err := keymap.Put(pairs)
			require.NoError(t, err)
		}

		// Every once in a while, verify that the keymap is correct
		if rand.BoolWithProbability(0.1) {
			for key, expectedAddress := range expected {
				address, ok, err := keymap.Get([]byte(key))
				require.NoError(t, err)
				require.True(t, ok)
				require.Equal(t, expectedAddress, address)
			}
		}
	}

	for key, expectedAddress := range expected {
		address, ok, err := keymap.Get([]byte(key))
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, expectedAddress, address)
	}

	err = keymap.Destroy()
	require.NoError(t, err)
}
