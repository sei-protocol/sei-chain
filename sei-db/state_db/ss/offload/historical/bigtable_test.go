package historical

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

func TestBigtableConfigDefaultsAndValidate(t *testing.T) {
	cfg := BigtableConfig{
		ProjectID:  "project",
		InstanceID: "instance",
		Table:      "state",
	}
	cfg.ApplyDefaults()
	require.Equal(t, DefaultBigtableFamily, cfg.Family)
	require.Equal(t, DefaultBigtableShards, cfg.Shards)
	require.NoError(t, cfg.Validate())

	missingProject := BigtableConfig{InstanceID: "i", Table: "t", Family: "f"}
	missingInstance := BigtableConfig{ProjectID: "p", Table: "t", Family: "f"}
	missingTable := BigtableConfig{ProjectID: "p", InstanceID: "i", Family: "f"}
	missingShards := BigtableConfig{ProjectID: "p", InstanceID: "i", Table: "t", Family: "f"}
	require.ErrorContains(t, missingProject.Validate(), "project")
	require.ErrorContains(t, missingInstance.Validate(), "instance")
	require.ErrorContains(t, missingTable.Validate(), "table")
	require.ErrorContains(t, missingShards.Validate(), "shards")
}

func TestBigtableMutationRowKeyOrdersLatestVersionFirst(t *testing.T) {
	key40 := BigtableMutationRowKey("bank", []byte("k1"), 40, 256)
	key60 := BigtableMutationRowKey("bank", []byte("k1"), 60, 256)
	key80 := BigtableMutationRowKey("bank", []byte("k1"), 80, 256)
	keys := []string{key40, key80, key60}
	sort.Strings(keys)
	require.Equal(t, []string{key80, key60, key40}, keys)

	version, ok := bigtableVersionFromRowKey(key60)
	require.True(t, ok)
	require.Equal(t, int64(60), version)
	require.NotEqual(t,
		bigtableMutationRowPrefixBytes("bank", []byte("k"), 256),
		bigtableMutationRowPrefixBytes("bank", []byte("k1"), 256),
	)
}

func TestBigtableValueFromRow(t *testing.T) {
	rowKey := BigtableMutationRowKey("bank", []byte("k"), 7, 256)
	row := BigtableRow{
		Key: rowKey,
		Cells: []BigtableCell{
			{Family: DefaultBigtableFamily, Qualifier: BigtableValueColumn, Value: []byte("value")},
			{Family: DefaultBigtableFamily, Qualifier: BigtableDeletedColumn, Value: []byte{0}},
		},
	}
	value, err := bigtableValueFromRow(row, DefaultBigtableFamily)
	require.NoError(t, err)
	require.Equal(t, []byte("value"), value.Bytes)
	require.Equal(t, int64(7), value.Version)

	row.Cells[1].Value = []byte{1}
	_, err = bigtableValueFromRow(row, DefaultBigtableFamily)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestBigtableReaderGetUsesMVCCRange(t *testing.T) {
	wantRow := BigtableMutationRowKey("bank", []byte("k"), 40, 256)
	reader := &bigtableReader{
		family: DefaultBigtableFamily,
		shards: 256,
		readRows: func(_ context.Context, startKey, endKey []byte, limit int64, family string, f func(BigtableRow) bool, qualifiers ...string) error {
			require.Equal(t, []byte(BigtableMutationRowKey("bank", []byte("k"), 60, 256)), startKey)
			require.NotEmpty(t, endKey)
			require.Equal(t, int64(1), limit)
			require.Equal(t, DefaultBigtableFamily, family)
			require.Equal(t, []string{BigtableValueColumn, BigtableDeletedColumn}, qualifiers)
			f(BigtableRow{
				Key: wantRow,
				Cells: []BigtableCell{
					{Family: DefaultBigtableFamily, Qualifier: BigtableValueColumn, Value: []byte("v40")},
					{Family: DefaultBigtableFamily, Qualifier: BigtableDeletedColumn, Value: []byte{0}},
				},
			})
			return nil
		},
	}

	value, err := reader.Get(context.Background(), "bank", []byte("k"), 60)
	require.NoError(t, err)
	require.Equal(t, []byte("v40"), value.Bytes)
	require.Equal(t, int64(40), value.Version)
}

func TestBigtableReaderHasOnlyReadsDeletedColumn(t *testing.T) {
	wantRow := BigtableMutationRowKey("bank", []byte("k"), 40, 256)
	reader := &bigtableReader{
		family: DefaultBigtableFamily,
		shards: 256,
		readRows: func(_ context.Context, startKey, endKey []byte, limit int64, family string, f func(BigtableRow) bool, qualifiers ...string) error {
			require.Equal(t, []byte(BigtableMutationRowKey("bank", []byte("k"), 60, 256)), startKey)
			require.NotEmpty(t, endKey)
			require.Equal(t, int64(1), limit)
			require.Equal(t, DefaultBigtableFamily, family)
			require.Equal(t, []string{BigtableDeletedColumn}, qualifiers)
			f(BigtableRow{
				Key: wantRow,
				Cells: []BigtableCell{{
					Family:    DefaultBigtableFamily,
					Qualifier: BigtableDeletedColumn,
					Value:     []byte{0},
				}},
			})
			return nil
		},
	}

	ok, err := reader.Has(context.Background(), "bank", []byte("k"), 60)
	require.NoError(t, err)
	require.True(t, ok)
}

// A cell split across chunks carries the total size in every chunk except the
// last, whose ValueSize is zero; only then may the cell be committed.
func TestBigtableRowBuilderAssemblesSplitCells(t *testing.T) {
	var b bigtableRowBuilder

	row, committed, err := b.add(&bigtablepb.ReadRowsResponse_CellChunk{
		RowKey:     []byte("rk"),
		FamilyName: wrapperspb.String(DefaultBigtableFamily),
		Qualifier:  wrapperspb.Bytes([]byte(BigtableValueColumn)),
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
	require.Equal(t, BigtableValueColumn, row.Cells[0].Qualifier)
	require.Equal(t, []byte("hello bt"), row.Cells[0].Value)
}

func TestReadRowsWithRetryRetriesUnavailable(t *testing.T) {
	attempts := 0
	once := func(_ context.Context, _, _ []byte, _ int64, _ string, f func(BigtableRow) bool, _ ...string) error {
		attempts++
		if attempts < 3 {
			return status.Error(codes.Unavailable, "tablet moved")
		}
		f(BigtableRow{Key: "rk"})
		return nil
	}
	var got []string
	err := readRowsWithRetry(context.Background(), once, []byte("a"), []byte("b"), 1, "state", func(row BigtableRow) bool {
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
	once := func(_ context.Context, _, _ []byte, _ int64, _ string, f func(BigtableRow) bool, _ ...string) error {
		attempts++
		f(BigtableRow{Key: "rk"})
		return status.Error(codes.Unavailable, "mid-stream drop")
	}
	err := readRowsWithRetry(context.Background(), once, nil, nil, 0, "", func(BigtableRow) bool { return true })
	require.Error(t, err)
	require.Equal(t, 1, attempts)
}

func TestReadRowsWithRetryDoesNotRetryNonRetryable(t *testing.T) {
	attempts := 0
	once := func(_ context.Context, _, _ []byte, _ int64, _ string, _ func(BigtableRow) bool, _ ...string) error {
		attempts++
		return status.Error(codes.NotFound, "table missing")
	}
	err := readRowsWithRetry(context.Background(), once, nil, nil, 0, "", func(BigtableRow) bool { return true })
	require.Error(t, err)
	require.Equal(t, 1, attempts)
}

func TestBigtableLastVersionScansBuckets(t *testing.T) {
	versions := map[int]int64{
		3: 42,
		9: 70,
	}
	seen := make(map[int]struct{}, VersionBucketCount)
	var mu sync.Mutex

	got, err := BigtableLastVersion(context.Background(), func(_ context.Context, startKey, endKey []byte, limit int64, family string, f func(BigtableRow) bool, qualifiers ...string) error {
		if len(startKey) != 3 || startKey[0] != bigtableVersionPrefix {
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
			f(BigtableRow{Key: BigtableVersionRowKey(version)})
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, int64(70), got)
	require.Len(t, seen, VersionBucketCount)
}
