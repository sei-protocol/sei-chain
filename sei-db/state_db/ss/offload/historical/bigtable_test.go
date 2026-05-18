package historical

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
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
	require.ErrorContains(t, missingProject.Validate(), "project")
	require.ErrorContains(t, missingInstance.Validate(), "instance")
	require.ErrorContains(t, missingTable.Validate(), "table")
}

func TestBigtableMutationRowKeyOrdersLatestVersionFirst(t *testing.T) {
	key40 := BigtableMutationRowKey("bank", []byte("k1"), 40, 256)
	key60 := BigtableMutationRowKey("bank", []byte("k1"), 60, 256)
	key80 := BigtableMutationRowKey("bank", []byte("k1"), 80, 256)
	keys := []string{key40, key80, key60}
	sort.Strings(keys)
	require.Equal(t, []string{key80, key60, key40}, keys)

	version, ok := BigtableVersionFromRowKey(key60)
	require.True(t, ok)
	require.Equal(t, int64(60), version)
	require.NotEqual(t,
		BigtableMutationRowPrefix("bank", []byte("k"), 256),
		BigtableMutationRowPrefix("bank", []byte("k1"), 256),
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
	value, err := BigtableValueFromRow(row, DefaultBigtableFamily)
	require.NoError(t, err)
	require.Equal(t, []byte("value"), value.Bytes)
	require.Equal(t, int64(7), value.Version)
	value.Bytes[0] = 'V'
	require.Equal(t, []byte("value"), row.Cells[0].Value)

	row.Cells[1].Value = []byte{1}
	_, err = BigtableValueFromRow(row, DefaultBigtableFamily)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestBigtableReaderGetUsesMVCCRange(t *testing.T) {
	wantRow := BigtableMutationRowKey("bank", []byte("k"), 40, 256)
	reader := &bigtableReader{
		family: DefaultBigtableFamily,
		shards: 256,
		readRows: func(_ context.Context, startKey, endKey []byte, limit int64, family string, f func(BigtableRow) bool) error {
			require.Equal(t, []byte(BigtableMutationRowKey("bank", []byte("k"), 60, 256)), startKey)
			require.NotEmpty(t, endKey)
			require.Equal(t, int64(1), limit)
			require.Equal(t, DefaultBigtableFamily, family)
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

func TestBigtableLastVersionScansBuckets(t *testing.T) {
	versions := map[int]int64{
		3: 42,
		9: 70,
	}
	seen := make(map[int]struct{}, VersionBucketCount)
	var mu sync.Mutex

	got, err := BigtableLastVersion(context.Background(), func(_ context.Context, startKey, endKey []byte, limit int64, family string, f func(BigtableRow) bool) error {
		if len(startKey) != 3 || startKey[0] != bigtableVersionPrefix {
			return fmt.Errorf("unexpected start key %q", startKey)
		}
		if len(endKey) == 0 || limit != 1 || family != "" {
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
