package wrappers

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	dbTypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	scTypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
)

type mockDBWrapper struct {
	appliedEntries []*proto.ChangelogEntry
	commitVersion  int64
	applyErr       error
	commitErr      error
}

func (m *mockDBWrapper) ApplyChangeSets(entry *proto.ChangelogEntry) error {
	m.appliedEntries = append(m.appliedEntries, entry)
	return m.applyErr
}

func (m *mockDBWrapper) Read(_ []byte) ([]byte, bool, error) {
	return nil, false, nil
}

func (m *mockDBWrapper) Commit() (int64, error) {
	return m.commitVersion, m.commitErr
}

func (m *mockDBWrapper) Close() error {
	return nil
}

func (m *mockDBWrapper) Version() int64 {
	return m.commitVersion
}

func (m *mockDBWrapper) LoadVersion(_ int64) error {
	return nil
}

func (m *mockDBWrapper) Importer(_ int64) (scTypes.Importer, error) {
	return nil, nil
}

func (m *mockDBWrapper) GetPhaseTimer() *metrics.PhaseTimer {
	return nil
}

type mockStateStore struct {
	latestVersion int64
	asyncVersion  int64
	asyncChanges  []*proto.NamedChangeSet
	asyncCalls    int
	syncVersion   int64
	syncChanges   []*proto.NamedChangeSet
	syncCalls     int
}

func (m *mockStateStore) Get(_ string, _ int64, _ []byte) ([]byte, error) {
	return nil, nil
}

func (m *mockStateStore) Has(_ string, _ int64, _ []byte) (bool, error) {
	return false, nil
}

func (m *mockStateStore) Iterator(_ string, _ int64, _, _ []byte) (dbTypes.DBIterator, error) {
	return nil, nil
}

func (m *mockStateStore) ReverseIterator(_ string, _ int64, _, _ []byte) (dbTypes.DBIterator, error) {
	return nil, nil
}

func (m *mockStateStore) RawIterate(_ string, _ func([]byte, []byte, int64) bool) (bool, error) {
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
	return 0
}

func (m *mockStateStore) SetEarliestVersion(_ int64, _ bool) error {
	return nil
}

func (m *mockStateStore) ApplyChangesetSync(version int64, changesets []*proto.NamedChangeSet) error {
	m.syncCalls++
	m.syncVersion = version
	m.syncChanges = changesets
	m.latestVersion = version
	return nil
}

func (m *mockStateStore) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error {
	m.asyncCalls++
	m.asyncVersion = version
	m.asyncChanges = changesets
	m.latestVersion = version
	return nil
}

func (m *mockStateStore) Prune(_ int64) error {
	return nil
}

func (m *mockStateStore) Import(_ int64, _ <-chan dbTypes.SnapshotNode) error {
	return nil
}

func (m *mockStateStore) Close() error {
	return nil
}

func TestCombinedWrapperApplyChangeSetsUsesAsyncSS(t *testing.T) {
	sc := &mockDBWrapper{commitVersion: 7}
	ss := &mockStateStore{latestVersion: 7}
	wrapper := NewCombinedWrapper(sc, ss)

	changesets := []*proto.NamedChangeSet{{Name: EVMStoreName}}
	entry := &proto.ChangelogEntry{
		Version:    8,
		Changesets: changesets,
	}

	err := wrapper.ApplyChangeSets(entry)
	require.NoError(t, err)
	require.Len(t, sc.appliedEntries, 1)
	require.Same(t, entry, sc.appliedEntries[0])
	require.Equal(t, 1, ss.asyncCalls)
	require.Equal(t, entry.Version, ss.asyncVersion)
	require.Equal(t, changesets, ss.asyncChanges)
	require.Zero(t, ss.syncCalls)
	require.Equal(t, entry.Version, wrapper.Version())
}

func TestStateStoreWrapperApplyChangeSetsUsesEntryVersion(t *testing.T) {
	store := &mockStateStore{latestVersion: 11}
	wrapper := NewStateStoreWrapper(store)

	changesets := []*proto.NamedChangeSet{{Name: EVMStoreName}}
	entry := &proto.ChangelogEntry{
		Version:    15,
		Changesets: changesets,
	}

	err := wrapper.ApplyChangeSets(entry)
	require.NoError(t, err)
	require.Equal(t, 1, store.syncCalls)
	require.Equal(t, entry.Version, store.syncVersion)
	require.Equal(t, changesets, store.syncChanges)
	require.Zero(t, store.asyncCalls)
	require.Equal(t, entry.Version, wrapper.Version())
	version, err := wrapper.Commit()
	require.NoError(t, err)
	require.Equal(t, entry.Version, version)
}
