package ss

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/pruning"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/types"
	iavl "github.com/sei-protocol/sei-chain/sei-iavl"
	"github.com/stretchr/testify/require"
)

func TestNewStateStore(t *testing.T) {
	tempDir := os.TempDir()
	homeDir := filepath.Join(tempDir, "pebbledb")
	ssConfig := config.StateStoreConfig{
		Backend:          string(PebbleDBBackend),
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
	// Closing the state store without waiting for data to be fully flushed
	err = stateStore.Close()
	require.NoError(t, err)

	// Reopen a new state store
	stateStore, err = NewStateStore(logger.NewNopLogger(), homeDir, ssConfig)
	require.NoError(t, err)

	// Make sure key and values can be found
	for i := 1; i < 50; i++ {
		value, err := stateStore.Get("storeA", int64(i), []byte(fmt.Sprintf("key%d", i)))
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("value%d", i), string(value))
	}

}

// mockStateStore is a minimal StateStore implementation for testing Close idempotency
type mockStateStore struct {
	closeCount atomic.Int32
}

func (m *mockStateStore) Get(storeKey string, version int64, key []byte) ([]byte, error) {
	return nil, nil
}
func (m *mockStateStore) Has(storeKey string, version int64, key []byte) (bool, error) {
	return false, nil
}
func (m *mockStateStore) GetLatestVersion() int64 {
	return 0
}
func (m *mockStateStore) SetLatestVersion(version int64) error {
	return nil
}
func (m *mockStateStore) GetEarliestVersion() int64 {
	return 0
}
func (m *mockStateStore) SetEarliestVersion(version int64, ignoreVersion bool) error {
	return nil
}
func (m *mockStateStore) GetLatestMigratedKey() ([]byte, error) {
	return nil, nil
}
func (m *mockStateStore) GetLatestMigratedModule() (string, error) {
	return "", nil
}
func (m *mockStateStore) WriteBlockRangeHash(storeKey string, beginBlockRange, endBlockRange int64, hash []byte) error {
	return nil
}
func (m *mockStateStore) Iterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	return nil, nil
}
func (m *mockStateStore) ReverseIterator(storeKey string, version int64, start, end []byte) (types.DBIterator, error) {
	return nil, nil
}
func (m *mockStateStore) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error {
	return nil
}
func (m *mockStateStore) ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error {
	return nil
}
func (m *mockStateStore) Prune(version int64) error {
	return nil
}
func (m *mockStateStore) Close() error {
	m.closeCount.Add(1)
	return nil
}
func (m *mockStateStore) RawIterate(storeKey string, fn func([]byte, []byte, int64) bool) (bool, error) {
	return false, nil
}
func (m *mockStateStore) Import(version int64, ch <-chan types.SnapshotNode) error {
	return nil
}
func (m *mockStateStore) RawImport(ch <-chan types.RawSnapshotNode) error {
	return nil
}

func TestPrunableStateStoreCloseIdempotent(t *testing.T) {
	mock := &mockStateStore{}
	manager := pruning.NewPruningManager(logger.NewNopLogger(), mock, 0, 0) // disabled pruning

	pss := &PrunableStateStore{
		StateStore:     mock,
		pruningManager: manager,
	}

	// Close multiple times should only close underlying store once
	err := pss.Close()
	require.NoError(t, err)

	err = pss.Close()
	require.NoError(t, err)

	err = pss.Close()
	require.NoError(t, err)

	// Verify underlying Close was called exactly once
	require.Equal(t, int32(1), mock.closeCount.Load())
}

func TestPrunableStateStoreCloseConcurrent(t *testing.T) {
	mock := &mockStateStore{}
	manager := pruning.NewPruningManager(logger.NewNopLogger(), mock, 0, 0)

	pss := &PrunableStateStore{
		StateStore:     mock,
		pruningManager: manager,
	}

	// Concurrent Close calls should not panic and only close once
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = pss.Close()
		}()
	}
	wg.Wait()

	// Verify underlying Close was called exactly once
	require.Equal(t, int32(1), mock.closeCount.Load())
}
