package wrappers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload"
)

type mockOffloadStream struct {
	closeCalls int
	calls      int
	entries    []*proto.ChangelogEntry
}

func (m *mockOffloadStream) Publish(_ context.Context, entry *proto.ChangelogEntry) (offload.Ack, error) {
	m.calls++
	m.entries = append(m.entries, entry)
	return offload.Ack{Accepted: true}, nil
}

func (m *mockOffloadStream) Close() error {
	m.closeCalls++
	return nil
}

func TestHistoricalOffloadWrapperPublishesWithoutLocalWrites(t *testing.T) {
	stream := &mockOffloadStream{}
	wrapper := NewHistoricalOffloadWrapper(stream)

	entry := &proto.ChangelogEntry{
		Version:    5,
		Changesets: []*proto.NamedChangeSet{{Name: EVMStoreName}},
	}

	err := wrapper.ApplyChangeSets(entry)
	require.NoError(t, err)
	require.Equal(t, 1, stream.calls)
	require.Len(t, stream.entries, 1)
	require.Equal(t, entry.Version, stream.entries[0].Version)
	require.Equal(t, entry.Changesets, stream.entries[0].Changesets)
	require.Equal(t, int64(5), wrapper.Version())

	data, found, err := wrapper.Read([]byte("ignored"))
	require.NoError(t, err)
	require.Nil(t, data)
	require.False(t, found)

	version, err := wrapper.Commit()
	require.NoError(t, err)
	require.Equal(t, int64(5), version)

	require.NoError(t, wrapper.Close())
	require.Equal(t, 1, stream.closeCalls)
}

func TestHistoricalOffloadWrapperImporterUnsupported(t *testing.T) {
	wrapper := NewHistoricalOffloadWrapper(&mockOffloadStream{})

	importer, err := wrapper.Importer(1)
	require.Nil(t, importer)
	require.Error(t, err)
}

func TestHistoricalOffloadWrapperGetPhaseTimerIsNil(t *testing.T) {
	wrapper := NewHistoricalOffloadWrapper(&mockOffloadStream{})
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

func TestHistoricalOffloadBackendUsesProvidedStateStoreConfig(t *testing.T) {
	original := currentHistoricalOffloadStreamFactory()
	t.Cleanup(func() {
		SetHistoricalOffloadStreamFactory(original)
	})

	var captured config.StateStoreConfig
	SetHistoricalOffloadStreamFactory(func(_ context.Context, _ string, cfg config.StateStoreConfig) (offload.Stream, error) {
		captured = cfg
		return &mockOffloadStream{}, nil
	})

	cfg := DefaultBenchStateStoreConfig()
	cfg.AsyncWriteBuffer = 321
	cfg.Backend = config.RocksDBBackend

	wrapper, err := NewDBImpl(context.Background(), SSHistoricalOffload, t.TempDir(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, wrapper.Close())
	})

	require.Equal(t, 321, captured.AsyncWriteBuffer)
	require.Equal(t, config.RocksDBBackend, captured.Backend)
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

var _ DBWrapper = NewHistoricalOffloadWrapper(&mockOffloadStream{})
var _ offload.Stream = (*mockOffloadStream)(nil)
