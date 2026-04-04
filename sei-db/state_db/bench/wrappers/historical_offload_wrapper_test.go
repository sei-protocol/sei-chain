package wrappers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	dbTypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload"
)

type mockHistoricalOffloadStateStore struct {
	latestVersion int64
	asyncVersion  int64
	asyncChanges  []*proto.NamedChangeSet
	asyncCalls    int
	syncCalls     int
	closeCalls    int
}

func (m *mockHistoricalOffloadStateStore) Get(_ string, _ int64, _ []byte) ([]byte, error) {
	return nil, nil
}

func (m *mockHistoricalOffloadStateStore) Has(_ string, _ int64, _ []byte) (bool, error) {
	return false, nil
}

func (m *mockHistoricalOffloadStateStore) Iterator(_ string, _ int64, _, _ []byte) (dbTypes.DBIterator, error) {
	return nil, nil
}

func (m *mockHistoricalOffloadStateStore) ReverseIterator(_ string, _ int64, _, _ []byte) (dbTypes.DBIterator, error) {
	return nil, nil
}

func (m *mockHistoricalOffloadStateStore) RawIterate(_ string, _ func([]byte, []byte, int64) bool) (bool, error) {
	return false, nil
}

func (m *mockHistoricalOffloadStateStore) GetLatestVersion() int64 {
	return m.latestVersion
}

func (m *mockHistoricalOffloadStateStore) SetLatestVersion(version int64) error {
	m.latestVersion = version
	return nil
}

func (m *mockHistoricalOffloadStateStore) GetEarliestVersion() int64 {
	return 0
}

func (m *mockHistoricalOffloadStateStore) SetEarliestVersion(_ int64, _ bool) error {
	return nil
}

func (m *mockHistoricalOffloadStateStore) ApplyChangesetSync(_ int64, _ []*proto.NamedChangeSet) error {
	m.syncCalls++
	return nil
}

func (m *mockHistoricalOffloadStateStore) ApplyChangesetAsync(version int64, changesets []*proto.NamedChangeSet) error {
	m.asyncCalls++
	m.asyncVersion = version
	m.asyncChanges = changesets
	m.latestVersion = version
	return nil
}

func (m *mockHistoricalOffloadStateStore) Prune(_ int64) error {
	return nil
}

func (m *mockHistoricalOffloadStateStore) Import(_ int64, _ <-chan dbTypes.SnapshotNode) error {
	return nil
}

func (m *mockHistoricalOffloadStateStore) Close() error {
	m.closeCalls++
	return nil
}

type mockOffloadStream struct {
	closeCalls int
}

func (m *mockOffloadStream) Publish(_ context.Context, _ *proto.ChangelogEntry) (offload.Ack, error) {
	return offload.Ack{Accepted: true}, nil
}

func (m *mockOffloadStream) Replay(_ context.Context, _ offload.ReplayRequest, _ func(*proto.ChangelogEntry) error) error {
	return nil
}

func (m *mockOffloadStream) Close() error {
	m.closeCalls++
	return nil
}

func TestHistoricalOffloadWrapperUsesAsyncSSWritesAndDummyReads(t *testing.T) {
	store := &mockHistoricalOffloadStateStore{latestVersion: 4}
	stream := &mockOffloadStream{}
	wrapper := NewHistoricalOffloadWrapper(store, stream)

	changesets := []*proto.NamedChangeSet{{Name: EVMStoreName}}

	err := wrapper.ApplyChangeSets(changesets)
	require.NoError(t, err)
	require.Equal(t, 1, store.asyncCalls)
	require.Equal(t, int64(5), store.asyncVersion)
	require.Equal(t, changesets, store.asyncChanges)
	require.Zero(t, store.syncCalls)
	require.Equal(t, int64(5), wrapper.Version())

	data, found, err := wrapper.Read([]byte("ignored"))
	require.NoError(t, err)
	require.Nil(t, data)
	require.False(t, found)

	version, err := wrapper.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(5), version)

	require.NoError(t, wrapper.Close())
	require.Equal(t, 1, store.closeCalls)
	require.Equal(t, 1, stream.closeCalls)
}

func TestHistoricalOffloadWrapperImporterUnsupported(t *testing.T) {
	wrapper := NewHistoricalOffloadWrapper(&mockHistoricalOffloadStateStore{}, &mockOffloadStream{})

	importer, err := wrapper.Importer(1)
	require.Nil(t, importer)
	require.Error(t, err)
}

func TestHistoricalOffloadWrapperGetPhaseTimerIsNil(t *testing.T) {
	wrapper := NewHistoricalOffloadWrapper(&mockHistoricalOffloadStateStore{}, &mockOffloadStream{})
	require.Nil(t, wrapper.GetPhaseTimer())
}

func TestSetHistoricalOffloadStreamFactoryAllowsOverrideAndReset(t *testing.T) {
	original := currentHistoricalOffloadStreamFactory()
	t.Cleanup(func() {
		SetHistoricalOffloadStreamFactory(original)
	})

	custom := func(_ context.Context, _ string, _ config.StateStoreConfig) (offload.Stream, error) {
		return &mockOffloadStream{}, nil
	}

	SetHistoricalOffloadStreamFactory(custom)
	require.NotNil(t, currentHistoricalOffloadStreamFactory())

	stream, err := currentHistoricalOffloadStreamFactory()(context.Background(), "", config.DefaultStateStoreConfig())
	require.NoError(t, err)
	require.IsType(t, &mockOffloadStream{}, stream)

	SetHistoricalOffloadStreamFactory(nil)
	stream, err = currentHistoricalOffloadStreamFactory()(context.Background(), "", config.DefaultStateStoreConfig())
	require.NoError(t, err)
	require.Implements(t, (*offload.Stream)(nil), stream)
}

func TestNewBufferedHistoricalOffloadStreamAcceptsPublish(t *testing.T) {
	stream := NewBufferedHistoricalOffloadStream(1)
	closer, ok := stream.(interface{ Close() error })
	require.True(t, ok)
	t.Cleanup(func() {
		require.NoError(t, closer.Close())
	})

	ack, err := stream.Publish(context.Background(), &proto.ChangelogEntry{Version: 1})
	require.NoError(t, err)
	require.True(t, ack.Accepted)
}

var _ DBWrapper = NewHistoricalOffloadWrapper(&mockHistoricalOffloadStateStore{}, &mockOffloadStream{})
var _ dbTypes.StateStore = (*mockHistoricalOffloadStateStore)(nil)
var _ offload.Stream = (*mockOffloadStream)(nil)
