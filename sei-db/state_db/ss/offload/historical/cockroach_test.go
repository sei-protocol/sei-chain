package historical

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSplitLookupsParallelArrays(t *testing.T) {
	stores, keys := splitLookups([]Lookup{
		{StoreName: "evm", Key: "k1"},
		{StoreName: "bank", Key: "k2"},
		{StoreName: "evm", Key: ""},
	})
	require.Equal(t, []string{"evm", "bank", "evm"}, stores)
	require.Equal(t, [][]byte{[]byte("k1"), []byte("k2"), {}}, keys)
}

func TestSplitLookupsEmpty(t *testing.T) {
	stores, keys := splitLookups(nil)
	require.Empty(t, stores)
	require.Empty(t, keys)
}

func TestAostStmtFormatsDuration(t *testing.T) {
	require.Equal(t,
		"SET TRANSACTION AS OF SYSTEM TIME with_max_staleness('10s')",
		aostStmt(10*time.Second))
	require.Equal(t,
		"SET TRANSACTION AS OF SYSTEM TIME with_max_staleness('1m30s')",
		aostStmt(90*time.Second))
}

// TestBatchLookupSQLShape pins the salient pieces of the batch query so an
// accidental edit that loses LATERAL, the descending order, or the LIMIT 1
// (each of which is needed for the per-pair PK-seek plan) breaks loudly
// instead of silently regressing into a full version-history scan.
func TestBatchLookupSQLShape(t *testing.T) {
	for _, frag := range []string{
		"unnest($1::STRING[], $2::BYTES[])",
		"LATERAL",
		"FROM state_mutations",
		"version <= $3",
		"ORDER BY version DESC",
		"LIMIT 1",
	} {
		require.Containsf(t, batchLookupSQL, frag,
			"batchLookupSQL missing required fragment %q", frag)
	}
}

func TestCockroachConfigValidate(t *testing.T) {
	cases := []struct {
		name string
		cfg  CockroachConfig
		err  string
	}{
		{"missing dsn", CockroachConfig{}, "dsn"},
		{"blank dsn", CockroachConfig{DSN: "   "}, "dsn"},
		{"negative open conns", CockroachConfig{DSN: "x", MaxOpenConns: -1}, "open"},
		{"negative idle conns", CockroachConfig{DSN: "x", MaxIdleConns: -1}, "idle"},
		{"negative staleness", CockroachConfig{DSN: "x", FollowerReadStaleness: -1}, "staleness"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			require.Error(t, err)
			require.True(t, strings.Contains(err.Error(), tc.err),
				"err %q should contain %q", err, tc.err)
		})
	}
}

func TestCockroachConfigApplyDefaults(t *testing.T) {
	c := CockroachConfig{DSN: "x"}
	c.ApplyDefaults()
	require.Equal(t, 16, c.MaxOpenConns)
	require.Equal(t, 16, c.MaxIdleConns)
	require.Equal(t, 30*time.Minute, c.ConnMaxLifetime)
	require.Equal(t, time.Duration(0), c.FollowerReadStaleness,
		"staleness defaults to strongly-consistent reads; operators opt in")
}
