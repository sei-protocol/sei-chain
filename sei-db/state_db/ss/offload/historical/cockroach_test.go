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

func TestAostClauseFormatsDuration(t *testing.T) {
	require.Equal(t,
		"AS OF SYSTEM TIME with_max_staleness('10s')",
		aostClause(10*time.Second))
	require.Equal(t,
		"AS OF SYSTEM TIME with_max_staleness('1m30s')",
		aostClause(90*time.Second))
}

func TestInlineAOSTPointLookupOff(t *testing.T) {
	require.Equal(t, getLookupSQL, inlineAOSTPointLookup(getLookupSQL, 0))
	require.Equal(t, hasLookupSQL, inlineAOSTPointLookup(hasLookupSQL, 0))
}

// AOST is attached to the state_mutations table reference so the WHERE/ORDER/
// LIMIT clauses still drive the same descending-PK index seek.
func TestInlineAOSTPointLookupOn(t *testing.T) {
	for _, q := range []string{getLookupSQL, hasLookupSQL} {
		got := inlineAOSTPointLookup(q, 5*time.Second)
		require.Contains(t, got,
			"FROM state_mutations AS OF SYSTEM TIME with_max_staleness('5s')")
		idxAost := strings.Index(got, "AS OF SYSTEM TIME")
		idxWhere := strings.Index(got, "WHERE")
		require.Greater(t, idxWhere, idxAost,
			"WHERE must follow AOST so the index seek path is unchanged")
		idxLimit := strings.Index(got, "LIMIT 1")
		require.Greater(t, idxLimit, idxWhere)
	}
}

func TestInlineAOSTBatchLookupOff(t *testing.T) {
	require.Equal(t, batchLookupSQL, inlineAOSTBatchLookup(batchLookupSQL, 0))
}

// CRDB rejects AOST inside a subquery unless the outer SELECT also AOSTs,
// so for the LATERAL form the clause has to sit at the top level.
func TestInlineAOSTBatchLookupOn(t *testing.T) {
	got := inlineAOSTBatchLookup(batchLookupSQL, 90*time.Second)
	require.True(t, strings.HasSuffix(strings.TrimSpace(got),
		"AS OF SYSTEM TIME with_max_staleness('1m30s')"),
		"AOST must terminate the outer SELECT, got: %q", got)
	require.Equal(t, 1, strings.Count(got, "AS OF SYSTEM TIME"),
		"AOST must not also appear in the LATERAL subquery")
}

// Pins the LATERAL/DESC/LIMIT 1 trio that drives the per-pair PK seek;
// losing any of them silently regresses to a full version-history scan.
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

func TestPointLookupSQLShape(t *testing.T) {
	for _, q := range []string{getLookupSQL, hasLookupSQL} {
		for _, frag := range []string{
			"FROM state_mutations",
			"store_name = $1",
			"key = $2",
			"version <= $3",
			"ORDER BY version DESC",
			"LIMIT 1",
		} {
			require.Contains(t, q, frag)
		}
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
