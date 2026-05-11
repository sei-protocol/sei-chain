package consumer

import (
	"strings"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/stretchr/testify/require"
)

func TestScyllaConfigValidate(t *testing.T) {
	cfg := ScyllaConfig{
		Hosts:    []string{"127.0.0.1"},
		Keyspace: "sei_history",
	}
	require.NoError(t, cfg.Validate())

	cfg.TimeoutMS = -1
	require.ErrorContains(t, cfg.Validate(), "timeout")
}

func TestScyllaConfigApplyDefaults(t *testing.T) {
	cfg := ScyllaConfig{
		Hosts:    []string{"127.0.0.1"},
		Keyspace: "sei_history",
	}
	cfg.ApplyDefaults()
	require.Equal(t, "local_quorum", cfg.Consistency)
	require.Equal(t, 2000, cfg.TimeoutMS)
	require.Equal(t, 2000, cfg.ConnectTimeoutMS)
	require.Equal(t, 4, cfg.NumConns)
}

func TestCompactRecordsDropsNilEntries(t *testing.T) {
	records := compactRecords([]Record{
		{Entry: &proto.ChangelogEntry{Version: 1}},
		{},
		{Entry: &proto.ChangelogEntry{Version: 2}},
	})
	require.Len(t, records, 2)
	require.Equal(t, int64(1), records[0].Entry.Version)
	require.Equal(t, int64(2), records[1].Entry.Version)
}

func TestScyllaCQLShape(t *testing.T) {
	for _, frag := range []string{
		"INSERT INTO state_mutations",
		"store_name",
		"state_key",
		"version",
		"value",
		"deleted",
	} {
		require.Contains(t, insertMutationCQL, frag)
	}
	for _, frag := range []string{
		"INSERT INTO state_versions",
		"bucket",
		"version",
		"kafka_topic",
		"kafka_partition",
		"kafka_offset",
		"ingested_at",
	} {
		require.Contains(t, insertVersionCQL, frag)
	}
	require.True(t, strings.Contains(selectLatestVersionCQL, "LIMIT 1"))
}
