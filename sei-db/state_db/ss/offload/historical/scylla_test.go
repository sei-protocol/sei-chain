package historical

import (
	"testing"
	"time"

	"github.com/gocql/gocql"
	"github.com/stretchr/testify/require"
)

func TestScyllaConfigApplyDefaults(t *testing.T) {
	cfg := ScyllaConfig{
		Hosts:    []string{"127.0.0.1"},
		Keyspace: "sei_history",
	}
	cfg.ApplyDefaults()
	require.Equal(t, defaultScyllaConsistency, cfg.Consistency)
	require.Equal(t, defaultScyllaTimeout, cfg.Timeout)
	require.Equal(t, defaultScyllaTimeout, cfg.ConnectTimeout)
	require.Equal(t, defaultScyllaNumConns, cfg.NumConns)
}

func TestScyllaConfigValidate(t *testing.T) {
	tests := []struct {
		name string
		cfg  ScyllaConfig
		err  string
	}{
		{"missing hosts", ScyllaConfig{Keyspace: "ks"}, "hosts"},
		{"blank host", ScyllaConfig{Hosts: []string{" "}, Keyspace: "ks"}, "blanks"},
		{"missing keyspace", ScyllaConfig{Hosts: []string{"127.0.0.1"}}, "keyspace"},
		{"password without username", ScyllaConfig{Hosts: []string{"127.0.0.1"}, Keyspace: "ks", Password: "secret"}, "username"},
		{"bad consistency", ScyllaConfig{Hosts: []string{"127.0.0.1"}, Keyspace: "ks", Consistency: "bad"}, "consistency"},
		{"negative timeout", ScyllaConfig{Hosts: []string{"127.0.0.1"}, Keyspace: "ks", Timeout: -time.Second}, "timeout"},
		{"negative connect timeout", ScyllaConfig{Hosts: []string{"127.0.0.1"}, Keyspace: "ks", ConnectTimeout: -time.Second}, "connect"},
		{"negative conns", ScyllaConfig{Hosts: []string{"127.0.0.1"}, Keyspace: "ks", NumConns: -1}, "conns"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.err)
		})
	}
}

func TestParseConsistency(t *testing.T) {
	tests := []struct {
		in  string
		out gocql.Consistency
	}{
		{"", gocql.LocalQuorum},
		{"local_quorum", gocql.LocalQuorum},
		{"LOCAL_ONE", gocql.LocalOne},
		{"one", gocql.One},
		{"quorum", gocql.Quorum},
		{"all", gocql.All},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, err := parseConsistency(tc.in)
			require.NoError(t, err)
			require.Equal(t, tc.out, got)
		})
	}
}

func TestVersionBucket(t *testing.T) {
	require.Equal(t, 0, VersionBucket(0))
	require.Equal(t, 1, VersionBucket(1))
	require.Equal(t, 0, VersionBucket(VersionBucketCount))
	require.Equal(t, 1, VersionBucket(-1))
}

func TestPointLookupCQLShape(t *testing.T) {
	for _, q := range []string{getLookupCQL, hasLookupCQL} {
		for _, frag := range []string{
			"FROM state_mutations",
			"store_name = ?",
			"state_key = ?",
			"version <= ?",
			"ORDER BY version DESC",
			"LIMIT 1",
		} {
			require.Contains(t, q, frag)
		}
	}
}

func TestLatestVersionCQLShape(t *testing.T) {
	require.Contains(t, selectLatestVersionCQL, "FROM state_versions")
	require.Contains(t, selectLatestVersionCQL, "bucket = ?")
	require.Contains(t, selectLatestVersionCQL, "LIMIT 1")
}
