package pruning

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/logger"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// mockStateStore is a minimal StateStore implementation for testing
type mockStateStore struct {
	latestVersion int64
	pruneCount    atomic.Int32
	closeCount    atomic.Int32
}

func (m *mockStateStore) Get(storeKey string, version int64, key []byte) ([]byte, error) {
	return nil, nil
}
func (m *mockStateStore) Has(storeKey string, version int64, key []byte) (bool, error) {
	return false, nil
}
func (m *mockStateStore) GetLatestVersion() int64 {
	return m.latestVersion
}
func (m *mockStateStore) SetLatestVersion(version int64) error {
	m.latestVersion = version
	return nil
}
func (m *mockStateStore) GetEarliestVersion() int64 {
	return 1
}
func (m *mockStateStore) SetEarliestVersion(version int64, ignoreVersion bool) error {
	return nil
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
	m.pruneCount.Add(1)
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

func TestManagerStartStop(t *testing.T) {
	store := &mockStateStore{latestVersion: 100}
	manager := NewPruningManager(logger.NewNopLogger(), store, 10, 1)

	// Start should launch the goroutine
	manager.Start()

	// Give it time to run at least one prune cycle
	time.Sleep(100 * time.Millisecond)

	// Stop should gracefully terminate
	manager.Stop()

	// Verify prune was called at least once
	require.GreaterOrEqual(t, store.pruneCount.Load(), int32(1))
}

func TestManagerStopIdempotent(t *testing.T) {
	store := &mockStateStore{latestVersion: 100}
	manager := NewPruningManager(logger.NewNopLogger(), store, 10, 1)

	manager.Start()
	time.Sleep(50 * time.Millisecond)

	// Stop multiple times should not panic
	manager.Stop()
	manager.Stop()
	manager.Stop()
}

func TestManagerStartIdempotent(t *testing.T) {
	store := &mockStateStore{latestVersion: 100}
	manager := NewPruningManager(logger.NewNopLogger(), store, 10, 1)

	// Start multiple times should only launch one goroutine
	manager.Start()
	manager.Start()
	manager.Start()

	time.Sleep(50 * time.Millisecond)
	manager.Stop()
}

func TestManagerStopConcurrent(t *testing.T) {
	store := &mockStateStore{latestVersion: 100}
	manager := NewPruningManager(logger.NewNopLogger(), store, 10, 1)

	manager.Start()
	time.Sleep(50 * time.Millisecond)

	// Concurrent Stop calls should not panic
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			manager.Stop()
		}()
	}
	wg.Wait()
}

func TestManagerDisabledPruning(t *testing.T) {
	store := &mockStateStore{latestVersion: 100}

	// keepRecent <= 0 should disable pruning
	manager := NewPruningManager(logger.NewNopLogger(), store, 0, 1)
	manager.Start()
	time.Sleep(50 * time.Millisecond)
	manager.Stop()

	require.Equal(t, int32(0), store.pruneCount.Load())

	// pruneInterval <= 0 should also disable pruning
	manager2 := NewPruningManager(logger.NewNopLogger(), store, 10, 0)
	manager2.Start()
	time.Sleep(50 * time.Millisecond)
	manager2.Stop()

	require.Equal(t, int32(0), store.pruneCount.Load())
}
