package wrappers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

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

func TestHistoricalOffloadConfigValidateRequiresKafkaConfig(t *testing.T) {
	cfg := &HistoricalOffloadConfig{Provider: "kafka"}
	err := cfg.Validate()
	require.ErrorContains(t, err, "historical offload kafka config is required")
}

var _ DBWrapper = NewHistoricalOffloadWrapper(&mockOffloadStream{})
var _ offload.Stream = (*mockOffloadStream)(nil)
