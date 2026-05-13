package historical

import (
	"context"
	"reflect"
	"strings"
	"sync/atomic"
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

func TestScyllaHostSelectionPolicyIsTokenAware(t *testing.T) {
	for _, tc := range []struct {
		name       string
		datacenter string
	}{
		{"no datacenter", ""},
		{"with datacenter", "dc1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := scyllaHostSelectionPolicy(tc.datacenter)
			require.NotNil(t, policy)
			require.Contains(t, reflect.TypeOf(policy).String(), "tokenAware")
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
	require.True(t, strings.Contains(selectLatestVersionCQL, "LIMIT 1"))
}

func TestScyllaReaderBatchGetParallelizesLookups(t *testing.T) {
	started := make(chan string, 4)
	release := make(chan struct{})
	var active atomic.Int32
	var sawConcurrent atomic.Bool

	reader := &scyllaReader{
		get: func(ctx context.Context, _ string, key []byte, targetVersion int64) (Value, error) {
			if active.Add(1) > 1 {
				sawConcurrent.Store(true)
			}
			defer active.Add(-1)
			keyString := string(key)
			started <- keyString
			select {
			case <-release:
			case <-ctx.Done():
				return Value{}, ctx.Err()
			}
			if keyString == "missing" {
				return Value{}, ErrNotFound
			}
			return Value{Bytes: []byte("value-" + keyString), Version: targetVersion - 1}, nil
		},
	}

	errCh := make(chan error, 1)
	var got map[Lookup]Value
	lookups := []Lookup{
		{StoreName: "bank", Key: "k1"},
		{StoreName: "bank", Key: "missing"},
		{StoreName: "evm", Key: "k2"},
	}
	go func() {
		var err error
		got, err = reader.BatchGet(context.Background(), 10, lookups)
		errCh <- err
	}()

	releaseClosed := false
	closeRelease := func() {
		if !releaseClosed {
			close(release)
			releaseClosed = true
		}
	}
	defer closeRelease()

	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			closeRelease()
			t.Fatal("timed out waiting for concurrent lookups")
		}
	}
	require.True(t, sawConcurrent.Load())

	closeRelease()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for batch get")
	}
	require.Len(t, got, 2)
	require.Equal(t, []byte("value-k1"), got[lookups[0]].Bytes)
	require.Equal(t, []byte("value-k2"), got[lookups[2]].Bytes)
	require.NotContains(t, got, lookups[1])
}
