package keymap

import (
	"os"
	"path"
	"testing"

	"log/slog"

	"github.com/cockroachdb/pebble/v2"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/types"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util/test"
	"github.com/stretchr/testify/require"
	"github.com/syndtr/goleveldb/leveldb"
)

var builders = []keymapBuilder{
	buildMemKeymap,
	buildLevelDBKeymap,
	buildPebbleKeymap,
}

type keymapBuilder func(logger *slog.Logger, path string) (Keymap, error)

func buildMemKeymap(logger *slog.Logger, path string) (Keymap, error) {
	kmap, _, err := NewMemKeymap(logger, path, true, nil)
	if err != nil {
		return nil, err
	}

	return kmap, nil
}

func buildLevelDBKeymap(logger *slog.Logger, path string) (Keymap, error) {
	kmap, _, err := NewUnsafeLevelDBKeymap(logger, path, true, nil)
	if err != nil {
		return nil, err
	}

	return kmap, nil
}

func buildPebbleKeymap(logger *slog.Logger, path string) (Keymap, error) {
	kmap, _, err := NewUnsafePebbleKeymap(logger, path, true, nil)
	if err != nil {
		return nil, err
	}

	return kmap, nil
}

func testBasicBehavior(t *testing.T, keymap Keymap) {
	rand := test.NewTestRandom()

	expected := make(map[string]types.Address)

	operations := 1000
	for i := 0; i < operations; i++ {
		choice := rand.Float64()
		if choice < 0.5 {
			// Write a random value
			key := []byte(rand.String(32))
			address := types.NewAddress(rand.Uint32(), rand.Uint32(), uint8(rand.Uint32()), rand.Uint32())

			err := keymap.Put([]types.ScopedKey{{Key: key, Address: address}})
			require.NoError(t, err)
			expected[string(key)] = address
		} else if choice < 0.75 {
			// Delete a few random values
			numberToDelete := rand.Int32Range(1, 10)
			numberToDelete = min(numberToDelete, int32(len(expected)))
			keysToDelete := make([]types.ScopedKey, 0, numberToDelete)
			for key := range expected {
				if numberToDelete == int32(len(keysToDelete)) {
					break
				}
				keysToDelete = append(keysToDelete, types.ScopedKey{Key: []byte(key)})
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
			pairs := make([]types.ScopedKey, numberToWrite)
			for i := 0; i < int(numberToWrite); i++ {
				key := []byte(rand.String(32))
				address := types.NewAddress(rand.Uint32(), rand.Uint32(), uint8(rand.Uint32()), rand.Uint32())
				pairs[i] = types.ScopedKey{Key: key, Address: address}
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

func TestLinkedValueEncoding(t *testing.T) {
	t.Parallel()

	addr := types.NewAddress(42, 1000, 3, 5000)

	// Round-trip with prevKey
	prevKey := []byte("previous-key-data")
	encoded := encodeLinkedValue(addr, prevKey)
	decodedAddr, decodedPrevKey, err := decodeLinkedValue(encoded)
	require.NoError(t, err)
	require.Equal(t, addr, decodedAddr)
	require.Equal(t, prevKey, decodedPrevKey)

	// Round-trip with nil prevKey (chain head)
	encoded = encodeLinkedValue(addr, nil)
	decodedAddr, decodedPrevKey, err = decodeLinkedValue(encoded)
	require.NoError(t, err)
	require.Equal(t, addr, decodedAddr)
	require.Nil(t, decodedPrevKey)

	// Round-trip with empty prevKey (treated same as nil)
	encoded = encodeLinkedValue(addr, []byte{})
	decodedAddr, decodedPrevKey, err = decodeLinkedValue(encoded)
	require.NoError(t, err)
	require.Equal(t, addr, decodedAddr)
	require.Nil(t, decodedPrevKey)

	// Legacy 13-byte value (backward compat)
	legacy := addr.Serialize()
	require.Equal(t, types.AddressLength, len(legacy))
	decodedAddr, decodedPrevKey, err = decodeLinkedValue(legacy)
	require.NoError(t, err)
	require.Equal(t, addr, decodedAddr)
	require.Nil(t, decodedPrevKey)

	// Too short
	_, _, err = decodeLinkedValue([]byte{1, 2, 3})
	require.Error(t, err)

	// Truncated linked value header (between 13 and 17 bytes)
	_, _, err = decodeLinkedValue(make([]byte, types.AddressLength+2))
	require.Error(t, err)

	// Length field doesn't match actual data
	badLinked := make([]byte, types.AddressLength+prevKeyLenSize+5)
	addr.SerializeInto(badLinked[:types.AddressLength])
	badLinked[types.AddressLength] = 0
	badLinked[types.AddressLength+1] = 0
	badLinked[types.AddressLength+2] = 0
	badLinked[types.AddressLength+3] = 99 // prevKeyLen=99 but only 5 bytes follow
	_, _, err = decodeLinkedValue(badLinked)
	require.Error(t, err)
}

func TestReverseIterator(t *testing.T) {
	t.Parallel()
	logger := test.GetLogger()
	for _, builder := range builders {
		dbDir := path.Join(t.TempDir(), "keymap")
		kmap, err := builder(logger, dbDir)
		require.NoError(t, err)

		batch1 := []types.ScopedKey{
			{Key: []byte("key1"), Address: types.NewAddress(1, 0, 0, 100)},
			{Key: []byte("key2"), Address: types.NewAddress(1, 100, 0, 200)},
		}
		batch2 := []types.ScopedKey{
			{Key: []byte("key3"), Address: types.NewAddress(2, 0, 0, 300)},
		}

		err = kmap.Put(batch1)
		require.NoError(t, err)
		err = kmap.Put(batch2)
		require.NoError(t, err)

		iter, err := kmap.ReverseIterator()
		require.NoError(t, err)

		key, addr, exists, err := iter.Next()
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, []byte("key3"), key)
		require.Equal(t, types.NewAddress(2, 0, 0, 300), addr)

		key, addr, exists, err = iter.Next()
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, []byte("key2"), key)
		require.Equal(t, types.NewAddress(1, 100, 0, 200), addr)

		key, addr, exists, err = iter.Next()
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, []byte("key1"), key)
		require.Equal(t, types.NewAddress(1, 0, 0, 100), addr)

		_, _, exists, err = iter.Next()
		require.NoError(t, err)
		require.False(t, exists)

		require.NoError(t, iter.Close())
		require.NoError(t, kmap.Destroy())
	}
}

func TestReverseIteratorDelete(t *testing.T) {
	t.Parallel()
	logger := test.GetLogger()
	for _, builder := range builders {
		dbDir := path.Join(t.TempDir(), "keymap")
		kmap, err := builder(logger, dbDir)
		require.NoError(t, err)

		keys := []types.ScopedKey{
			{Key: []byte("a"), Address: types.NewAddress(1, 0, 0, 10)},
			{Key: []byte("b"), Address: types.NewAddress(1, 10, 0, 20)},
			{Key: []byte("c"), Address: types.NewAddress(1, 30, 0, 30)},
		}
		require.NoError(t, kmap.Put(keys))

		iter, err := kmap.ReverseIterator()
		require.NoError(t, err)

		// Delete "c" via iterator
		key, _, exists, err := iter.Next()
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, []byte("c"), key)
		require.NoError(t, iter.Delete())

		// Delete "b" via iterator
		key, _, exists, err = iter.Next()
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, []byte("b"), key)
		require.NoError(t, iter.Delete())

		// "a" is still reachable
		key, _, exists, err = iter.Next()
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, []byte("a"), key)

		_, _, exists, err = iter.Next()
		require.NoError(t, err)
		require.False(t, exists)

		require.NoError(t, iter.Close())

		// Verify via Get: "a" exists, "b" and "c" are gone
		_, exists, err = kmap.Get([]byte("a"))
		require.NoError(t, err)
		require.True(t, exists)

		_, exists, err = kmap.Get([]byte("b"))
		require.NoError(t, err)
		require.False(t, exists)

		_, exists, err = kmap.Get([]byte("c"))
		require.NoError(t, err)
		require.False(t, exists)

		require.NoError(t, kmap.Destroy())
	}
}

func TestReverseIteratorMissingLink(t *testing.T) {
	t.Parallel()
	logger := test.GetLogger()
	for _, builder := range builders {
		dbDir := path.Join(t.TempDir(), "keymap")
		kmap, err := builder(logger, dbDir)
		require.NoError(t, err)

		batch1 := []types.ScopedKey{
			{Key: []byte("x"), Address: types.NewAddress(1, 0, 0, 10)},
			{Key: []byte("y"), Address: types.NewAddress(1, 10, 0, 20)},
		}
		batch2 := []types.ScopedKey{
			{Key: []byte("z"), Address: types.NewAddress(2, 0, 0, 30)},
		}
		require.NoError(t, kmap.Put(batch1))
		require.NoError(t, kmap.Put(batch2))

		// Delete "y" directly (simulates GC removing a middle segment)
		require.NoError(t, kmap.Delete([]types.ScopedKey{{Key: []byte("y")}}))

		// Chain is z -> y -> x, but y is missing, so iteration stops after z
		iter, err := kmap.ReverseIterator()
		require.NoError(t, err)

		key, _, exists, err := iter.Next()
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, []byte("z"), key)

		_, _, exists, err = iter.Next()
		require.NoError(t, err)
		require.False(t, exists)

		require.NoError(t, iter.Close())
		require.NoError(t, kmap.Destroy())
	}
}

func TestReverseIteratorEmpty(t *testing.T) {
	t.Parallel()
	logger := test.GetLogger()
	for _, builder := range builders {
		dbDir := path.Join(t.TempDir(), "keymap")
		kmap, err := builder(logger, dbDir)
		require.NoError(t, err)

		iter, err := kmap.ReverseIterator()
		require.NoError(t, err)

		_, _, exists, err := iter.Next()
		require.NoError(t, err)
		require.False(t, exists)

		require.NoError(t, iter.Close())
		require.NoError(t, kmap.Destroy())
	}
}

func TestReverseIteratorAcrossRestart(t *testing.T) {
	t.Parallel()
	logger := test.GetLogger()

	// Test with LevelDB
	t.Run("LevelDB", func(t *testing.T) {
		dbDir := path.Join(t.TempDir(), "keymap")

		kmap, _, err := NewUnsafeLevelDBKeymap(logger, dbDir, false, nil)
		require.NoError(t, err)

		batch1 := []types.ScopedKey{
			{Key: []byte("k1"), Address: types.NewAddress(1, 0, 0, 10)},
			{Key: []byte("k2"), Address: types.NewAddress(1, 10, 0, 20)},
		}
		require.NoError(t, kmap.Put(batch1))
		require.NoError(t, kmap.Stop())

		// Reopen and write more
		kmap, _, err = NewUnsafeLevelDBKeymap(logger, dbDir, false, nil)
		require.NoError(t, err)

		batch2 := []types.ScopedKey{
			{Key: []byte("k3"), Address: types.NewAddress(2, 0, 0, 30)},
		}
		require.NoError(t, kmap.Put(batch2))

		// Reverse iterate: should see k3, k2, k1 (chain spans restart)
		iter, err := kmap.ReverseIterator()
		require.NoError(t, err)

		key, _, exists, err := iter.Next()
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, []byte("k3"), key)

		key, _, exists, err = iter.Next()
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, []byte("k2"), key)

		key, _, exists, err = iter.Next()
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, []byte("k1"), key)

		_, _, exists, err = iter.Next()
		require.NoError(t, err)
		require.False(t, exists)

		require.NoError(t, iter.Close())
		require.NoError(t, kmap.Destroy())
	})

	// Test with Pebble (must use safe variant since WAL is needed for data to survive restart)
	t.Run("Pebble", func(t *testing.T) {
		dbDir := path.Join(t.TempDir(), "keymap")

		kmap, _, err := NewPebbleKeymap(logger, dbDir, false, nil)
		require.NoError(t, err)

		batch1 := []types.ScopedKey{
			{Key: []byte("k1"), Address: types.NewAddress(1, 0, 0, 10)},
			{Key: []byte("k2"), Address: types.NewAddress(1, 10, 0, 20)},
		}
		require.NoError(t, kmap.Put(batch1))
		require.NoError(t, kmap.Stop())

		// Reopen and write more
		kmap, _, err = NewPebbleKeymap(logger, dbDir, false, nil)
		require.NoError(t, err)

		batch2 := []types.ScopedKey{
			{Key: []byte("k3"), Address: types.NewAddress(2, 0, 0, 30)},
		}
		require.NoError(t, kmap.Put(batch2))

		// Reverse iterate: should see k3, k2, k1 (chain spans restart)
		iter, err := kmap.ReverseIterator()
		require.NoError(t, err)

		key, _, exists, err := iter.Next()
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, []byte("k3"), key)

		key, _, exists, err = iter.Next()
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, []byte("k2"), key)

		key, _, exists, err = iter.Next()
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, []byte("k1"), key)

		_, _, exists, err = iter.Next()
		require.NoError(t, err)
		require.False(t, exists)

		require.NoError(t, iter.Close())
		require.NoError(t, kmap.Destroy())
	})
}

func TestBackwardCompatLegacyValues(t *testing.T) {
	t.Parallel()
	logger := test.GetLogger()

	addr := types.NewAddress(10, 200, 3, 4000)
	legacyValue := addr.Serialize()

	// LevelDB: write a legacy 13-byte value directly, verify Get can read it
	t.Run("LevelDB", func(t *testing.T) {
		dbDir := path.Join(t.TempDir(), "keymap")

		db, err := leveldb.OpenFile(dbDir, nil)
		require.NoError(t, err)
		require.NoError(t, db.Put([]byte("legacy-key"), legacyValue, nil))
		require.NoError(t, db.Close())

		kmap, _, err := NewUnsafeLevelDBKeymap(logger, dbDir, false, nil)
		require.NoError(t, err)

		gotAddr, exists, err := kmap.Get([]byte("legacy-key"))
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, addr, gotAddr)

		// ReverseIterator returns empty since there's no latestKeyMetaKey
		iter, err := kmap.ReverseIterator()
		require.NoError(t, err)
		_, _, exists, err = iter.Next()
		require.NoError(t, err)
		require.False(t, exists)
		require.NoError(t, iter.Close())

		require.NoError(t, kmap.Destroy())
	})

	// Pebble: write a legacy 13-byte value directly, verify Get can read it
	t.Run("Pebble", func(t *testing.T) {
		dbDir := path.Join(t.TempDir(), "keymap")

		db, err := pebble.Open(dbDir, &pebble.Options{
			FormatMajorVersion: pebble.FormatVirtualSSTables,
		})
		require.NoError(t, err)
		require.NoError(t, db.Set([]byte("legacy-key"), legacyValue, pebble.Sync))
		require.NoError(t, db.Close())

		kmap, _, err := NewUnsafePebbleKeymap(logger, dbDir, false, nil)
		require.NoError(t, err)

		gotAddr, exists, err := kmap.Get([]byte("legacy-key"))
		require.NoError(t, err)
		require.True(t, exists)
		require.Equal(t, addr, gotAddr)

		// ReverseIterator returns empty since there's no latestKeyMetaKey
		iter, err := kmap.ReverseIterator()
		require.NoError(t, err)
		_, _, exists, err = iter.Next()
		require.NoError(t, err)
		require.False(t, exists)
		require.NoError(t, iter.Close())

		require.NoError(t, kmap.Destroy())
	})
}

func TestRestart(t *testing.T) {
	t.Parallel()
	rand := test.NewTestRandom()
	logger := test.GetLogger()
	testDir := t.TempDir()
	dbDir := path.Join(testDir, "keymap")

	keymap, _, err := NewUnsafeLevelDBKeymap(logger, dbDir, true, nil)
	require.NoError(t, err)

	expected := make(map[string]types.Address)

	operations := 1000
	for i := 0; i < operations; i++ {
		choice := rand.Float64()
		if choice < 0.5 {
			// Write a random value
			key := []byte(rand.String(32))
			address := types.NewAddress(rand.Uint32(), rand.Uint32(), uint8(rand.Uint32()), rand.Uint32())

			err := keymap.Put([]types.ScopedKey{{Key: key, Address: address}})
			require.NoError(t, err)
			expected[string(key)] = address
		} else if choice < 0.75 {
			// Delete a few random values
			numberToDelete := rand.Int32Range(1, 10)
			numberToDelete = min(numberToDelete, int32(len(expected)))
			keysToDelete := make([]types.ScopedKey, 0, numberToDelete)
			for key := range expected {
				if numberToDelete == int32(len(keysToDelete)) {
					break
				}
				keysToDelete = append(keysToDelete, types.ScopedKey{Key: []byte(key)})
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
			pairs := make([]types.ScopedKey, numberToWrite)
			for i := 0; i < int(numberToWrite); i++ {
				key := []byte(rand.String(32))
				address := types.NewAddress(rand.Uint32(), rand.Uint32(), uint8(rand.Uint32()), rand.Uint32())
				pairs[i] = types.ScopedKey{Key: key, Address: address}
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

	keymap, _, err = NewUnsafeLevelDBKeymap(logger, dbDir, true, nil)
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
			address := types.NewAddress(rand.Uint32(), rand.Uint32(), uint8(rand.Uint32()), rand.Uint32())

			err := keymap.Put([]types.ScopedKey{{Key: key, Address: address}})
			require.NoError(t, err)
			expected[string(key)] = address
		} else if choice < 0.75 {
			// Delete a few random values
			numberToDelete := rand.Int32Range(1, 10)
			numberToDelete = min(numberToDelete, int32(len(expected)))
			keysToDelete := make([]types.ScopedKey, 0, numberToDelete)
			for key := range expected {
				if numberToDelete == int32(len(keysToDelete)) {
					break
				}
				keysToDelete = append(keysToDelete, types.ScopedKey{Key: []byte(key)})
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
			pairs := make([]types.ScopedKey, numberToWrite)
			for i := 0; i < int(numberToWrite); i++ {
				key := []byte(rand.String(32))
				address := types.NewAddress(rand.Uint32(), rand.Uint32(), uint8(rand.Uint32()), rand.Uint32())
				pairs[i] = types.ScopedKey{Key: key, Address: address}
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
