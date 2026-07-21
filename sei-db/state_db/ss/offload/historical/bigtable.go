package historical

import (
	"context"
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/bigtable"
)

// NewBigtableReader opens a Bigtable-backed historical Reader.
func NewBigtableReader(cfg bigtable.Config) (Reader, error) {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	client, err := bigtable.NewClient(context.Background(), cfg)
	if err != nil {
		return nil, err
	}
	return &bigtableReader{
		client:   client,
		readRows: client.ReadRows,
		family:   cfg.Family,
		shards:   cfg.Shards,
	}, nil
}

type bigtableReader struct {
	client   *bigtable.Client
	readRows bigtable.ReadRowsFunc
	family   string
	shards   int
}

var _ Reader = (*bigtableReader)(nil)

func (r *bigtableReader) Close() error {
	if r.client != nil {
		return r.client.Close()
	}
	return nil
}

func (r *bigtableReader) LastVersion(ctx context.Context) (int64, error) {
	return bigtable.LastVersion(ctx, r.readRows)
}

func (r *bigtableReader) Has(ctx context.Context, storeName string, key []byte, targetVersion int64) (bool, error) {
	start, end := bigtable.MutationRowRange(storeName, key, targetVersion, r.shards)
	var row bigtable.Row
	err := r.readRows(ctx, start, end, 1, r.family, func(r bigtable.Row) bool {
		row = r
		return false
	}, bigtable.DeletedColumn)
	if err != nil {
		return false, fmt.Errorf("bigtable has lookup: %w", err)
	}
	if row.Key == "" {
		return false, nil
	}
	deleted, err := bigtableDeletedFromRow(row, r.family)
	if err != nil {
		return false, err
	}
	return !deleted, nil
}

func (r *bigtableReader) Get(ctx context.Context, storeName string, key []byte, targetVersion int64) (Value, error) {
	start, end := bigtable.MutationRowRange(storeName, key, targetVersion, r.shards)
	var row bigtable.Row
	err := r.readRows(ctx, start, end, 1, r.family, func(r bigtable.Row) bool {
		row = r
		return false
	}, bigtable.ValueColumn, bigtable.DeletedColumn)
	if err != nil {
		return Value{}, fmt.Errorf("bigtable get lookup: %w", err)
	}
	if row.Key == "" {
		return Value{}, ErrNotFound
	}
	return bigtableValueFromRow(row, r.family)
}

// bigtableValueFromRow interprets a mutation row. The returned Value aliases
// the row's cell buffer, which the row builder allocates per cell.
func bigtableValueFromRow(row bigtable.Row, family string) (Value, error) {
	version, ok := bigtable.VersionFromRowKey(row.Key)
	if !ok {
		return Value{}, fmt.Errorf("invalid bigtable mutation row key")
	}
	var value []byte
	deleted := false
	for _, cell := range row.Cells {
		if cell.Family != family {
			continue
		}
		switch cell.Qualifier {
		case bigtable.ValueColumn:
			value = cell.Value
		case bigtable.DeletedColumn:
			deleted = len(cell.Value) > 0 && cell.Value[0] == 1
		}
	}
	if deleted || value == nil {
		return Value{}, ErrNotFound
	}
	return Value{Bytes: value, Version: version}, nil
}

func bigtableDeletedFromRow(row bigtable.Row, family string) (bool, error) {
	if _, ok := bigtable.VersionFromRowKey(row.Key); !ok {
		return false, fmt.Errorf("invalid bigtable mutation row key")
	}
	for _, cell := range row.Cells {
		if cell.Family == family && cell.Qualifier == bigtable.DeletedColumn {
			return len(cell.Value) > 0 && cell.Value[0] == 1, nil
		}
	}
	return false, nil
}
