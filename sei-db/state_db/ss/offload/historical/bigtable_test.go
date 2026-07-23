package historical

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/bigtable"
)

func TestBigtableValueFromRow(t *testing.T) {
	rowKey := bigtable.MutationRowKey("bank", []byte("k"), 7, 256)
	row := bigtable.Row{
		Key: rowKey,
		Cells: []bigtable.Cell{
			{Family: bigtable.DefaultFamily, Qualifier: bigtable.ValueColumn, Value: []byte("value")},
			{Family: bigtable.DefaultFamily, Qualifier: bigtable.DeletedColumn, Value: []byte{0}},
		},
	}
	value, err := bigtableValueFromRow(row, bigtable.DefaultFamily)
	require.NoError(t, err)
	require.Equal(t, []byte("value"), value.Bytes)
	require.Equal(t, int64(7), value.Version)

	row.Cells[1].Value = []byte{1}
	_, err = bigtableValueFromRow(row, bigtable.DefaultFamily)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestBigtableReaderGetUsesMVCCRange(t *testing.T) {
	wantRow := bigtable.MutationRowKey("bank", []byte("k"), 40, 256)
	reader := &bigtableReader{
		family: bigtable.DefaultFamily,
		shards: 256,
		readRows: func(_ context.Context, startKey, endKey []byte, limit int64, family string, f func(bigtable.Row) bool, qualifiers ...string) error {
			require.Equal(t, []byte(bigtable.MutationRowKey("bank", []byte("k"), 60, 256)), startKey)
			require.NotEmpty(t, endKey)
			require.Equal(t, int64(1), limit)
			require.Equal(t, bigtable.DefaultFamily, family)
			require.Equal(t, []string{bigtable.ValueColumn, bigtable.DeletedColumn}, qualifiers)
			f(bigtable.Row{
				Key: wantRow,
				Cells: []bigtable.Cell{
					{Family: bigtable.DefaultFamily, Qualifier: bigtable.ValueColumn, Value: []byte("v40")},
					{Family: bigtable.DefaultFamily, Qualifier: bigtable.DeletedColumn, Value: []byte{0}},
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
	wantRow := bigtable.MutationRowKey("bank", []byte("k"), 40, 256)
	reader := &bigtableReader{
		family: bigtable.DefaultFamily,
		shards: 256,
		readRows: func(_ context.Context, startKey, endKey []byte, limit int64, family string, f func(bigtable.Row) bool, qualifiers ...string) error {
			require.Equal(t, []byte(bigtable.MutationRowKey("bank", []byte("k"), 60, 256)), startKey)
			require.NotEmpty(t, endKey)
			require.Equal(t, int64(1), limit)
			require.Equal(t, bigtable.DefaultFamily, family)
			require.Equal(t, []string{bigtable.DeletedColumn}, qualifiers)
			f(bigtable.Row{
				Key: wantRow,
				Cells: []bigtable.Cell{{
					Family:    bigtable.DefaultFamily,
					Qualifier: bigtable.DeletedColumn,
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
