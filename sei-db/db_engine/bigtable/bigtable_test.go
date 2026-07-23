package bigtable

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"

	"cloud.google.com/go/bigtable/apiv2/bigtablepb"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestConfigDefaultsAndValidate(t *testing.T) {
	cfg := Config{
		ProjectID:  "project",
		InstanceID: "instance",
		Table:      "state",
	}
	cfg.ApplyDefaults()
	require.Equal(t, DefaultFamily, cfg.Family)
	require.Equal(t, DefaultShards, cfg.Shards)
	require.NoError(t, cfg.Validate())

	missingProject := Config{InstanceID: "i", Table: "t", Family: "f"}
	missingInstance := Config{ProjectID: "p", Table: "t", Family: "f"}
	missingTable := Config{ProjectID: "p", InstanceID: "i", Family: "f"}
	missingShards := Config{ProjectID: "p", InstanceID: "i", Table: "t", Family: "f"}
	require.ErrorContains(t, missingProject.Validate(), "project")
	require.ErrorContains(t, missingInstance.Validate(), "instance")
	require.ErrorContains(t, missingTable.Validate(), "table")
	require.ErrorContains(t, missingShards.Validate(), "shards")
}

func TestConfigConfigured(t *testing.T) {
	full := Config{ProjectID: "p", InstanceID: "i", Table: "t"}
	require.True(t, full.Configured())

	onlyProject := Config{ProjectID: "p"}
	require.False(t, onlyProject.Configured())

	require.False(t, Config{}.Configured())
}

func TestMutationRowKeyOrdersLatestVersionFirst(t *testing.T) {
	key40 := MutationRowKey("bank", []byte("k1"), 40, 256)
	key60 := MutationRowKey("bank", []byte("k1"), 60, 256)
	key80 := MutationRowKey("bank", []byte("k1"), 80, 256)
	keys := []string{key40, key80, key60}
	sort.Strings(keys)
	require.Equal(t, []string{key80, key60, key40}, keys)

	version, ok := VersionFromRowKey(key60)
	require.True(t, ok)
	require.Equal(t, int64(60), version)
	require.NotEqual(t,
		mutationRowPrefixBytes("bank", []byte("k"), 256),
		mutationRowPrefixBytes("bank", []byte("k1"), 256),
	)
}

// A cell split across chunks carries the total size in every chunk except the
// last, whose ValueSize is zero; only then may the cell be committed.
func TestRowBuilderAssemblesSplitCells(t *testing.T) {
	var b rowBuilder

	row, committed, err := b.add(&bigtablepb.ReadRowsResponse_CellChunk{
		RowKey:     []byte("rk"),
		FamilyName: wrapperspb.String(DefaultFamily),
		Qualifier:  wrapperspb.Bytes([]byte(ValueColumn)),
		Value:      []byte("hel"),
		ValueSize:  8,
	})
	require.NoError(t, err)
	require.False(t, committed)
	require.Empty(t, row.Key)

	_, committed, err = b.add(&bigtablepb.ReadRowsResponse_CellChunk{Value: []byte("lo "), ValueSize: 8})
	require.NoError(t, err)
	require.False(t, committed)

	row, committed, err = b.add(&bigtablepb.ReadRowsResponse_CellChunk{
		Value:     []byte("bt"),
		RowStatus: &bigtablepb.ReadRowsResponse_CellChunk_CommitRow{CommitRow: true},
	})
	require.NoError(t, err)
	require.True(t, committed)
	require.Equal(t, "rk", row.Key)
	require.Len(t, row.Cells, 1)
	require.Equal(t, ValueColumn, row.Cells[0].Qualifier)
	require.Equal(t, []byte("hello bt"), row.Cells[0].Value)
}

func TestReadRowsWithRetryRetriesUnavailable(t *testing.T) {
	attempts := 0
	once := func(_ context.Context, _, _ []byte, _ int64, _ string, f func(Row) bool, _ ...string) error {
		attempts++
		if attempts < 3 {
			return status.Error(codes.Unavailable, "tablet moved")
		}
		f(Row{Key: "rk"})
		return nil
	}
	var got []string
	err := readRowsWithRetry(context.Background(), once, []byte("a"), []byte("b"), 1, "state", func(row Row) bool {
		got = append(got, row.Key)
		return false
	})
	require.NoError(t, err)
	require.Equal(t, 3, attempts)
	require.Equal(t, []string{"rk"}, got)
}

func TestReadRowsWithRetryStopsAfterDelivery(t *testing.T) {
	// Once a row reached the callback, a retry would re-deliver it; the error
	// must surface instead.
	attempts := 0
	once := func(_ context.Context, _, _ []byte, _ int64, _ string, f func(Row) bool, _ ...string) error {
		attempts++
		f(Row{Key: "rk"})
		return status.Error(codes.Unavailable, "mid-stream drop")
	}
	err := readRowsWithRetry(context.Background(), once, nil, nil, 0, "", func(Row) bool { return true })
	require.Error(t, err)
	require.Equal(t, 1, attempts)
}

func TestReadRowsWithRetryDoesNotRetryNonRetryable(t *testing.T) {
	attempts := 0
	once := func(_ context.Context, _, _ []byte, _ int64, _ string, _ func(Row) bool, _ ...string) error {
		attempts++
		return status.Error(codes.NotFound, "table missing")
	}
	err := readRowsWithRetry(context.Background(), once, nil, nil, 0, "", func(Row) bool { return true })
	require.Error(t, err)
	require.Equal(t, 1, attempts)
}

func TestVersionBucket(t *testing.T) {
	require.Equal(t, 0, VersionBucket(0))
	require.Equal(t, 1, VersionBucket(1))
	require.Equal(t, 0, VersionBucket(VersionBucketCount))
	require.Equal(t, 1, VersionBucket(-1))
}

func TestLastVersionScansBuckets(t *testing.T) {
	versions := map[int]int64{
		3: 42,
		9: 70,
	}
	seen := make(map[int]struct{}, VersionBucketCount)
	var mu sync.Mutex

	got, err := LastVersion(context.Background(), func(_ context.Context, startKey, endKey []byte, limit int64, family string, f func(Row) bool, qualifiers ...string) error {
		if len(startKey) != 3 || startKey[0] != versionPrefix {
			return fmt.Errorf("unexpected start key %q", startKey)
		}
		if len(endKey) == 0 || limit != 1 || family != "" || len(qualifiers) != 0 {
			return fmt.Errorf("unexpected scan params")
		}
		bucket := int(startKey[1])<<8 | int(startKey[2])
		mu.Lock()
		seen[bucket] = struct{}{}
		version := versions[bucket]
		mu.Unlock()
		if version > 0 {
			f(Row{Key: VersionRowKey(version)})
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, int64(70), got)
	require.Len(t, seen, VersionBucketCount)
}
