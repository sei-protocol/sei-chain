package consumer

import (
	"context"
	"fmt"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload/historical"
)

type BigtableConfig = historical.BigtableConfig

type bigtableSink struct {
	client    *historical.BigtableClient
	applyBulk historical.BigtableApplyBulkFunc
	readRows  historical.BigtableReadRowsFunc
	family    string
	shards    int
}

var _ Sink = (*bigtableSink)(nil)
var _ BatchSink = (*bigtableSink)(nil)

func NewBigtableSink(cfg BigtableConfig) (Sink, error) {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	ctx := context.Background()
	client, err := historical.OpenBigtableClient(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &bigtableSink{
		client:    client,
		applyBulk: client.ApplyBulk,
		readRows:  client.ReadRows,
		family:    cfg.Family,
		shards:    cfg.Shards,
	}, nil
}

func (s *bigtableSink) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
}

func (s *bigtableSink) LastVersion(ctx context.Context) (int64, error) {
	return historical.BigtableLastVersion(ctx, s.readRows)
}

func (s *bigtableSink) Write(ctx context.Context, rec Record) error {
	return s.WriteBatch(ctx, []Record{rec})
}

func (s *bigtableSink) WriteBatch(ctx context.Context, records []Record) error {
	records = compactRecords(records)
	if len(records) == 0 {
		return nil
	}
	if err := s.writeRecordRows(ctx, records); err != nil {
		return err
	}
	for _, rec := range records {
		if err := s.writeVersionMarker(ctx, rec); err != nil {
			return err
		}
	}
	return nil
}

func (s *bigtableSink) writeRecordRows(ctx context.Context, records []Record) error {
	var rows []historical.BigtableRowMutation
	for _, rec := range records {
		rows = append(rows, s.recordRowMutations(rec.Entry.Version, rec.Entry)...)
	}
	if len(rows) == 0 {
		return nil
	}
	errs, err := s.applyBulk(ctx, rows)
	return bigtableBulkError(rows, errs, err)
}

func (s *bigtableSink) recordRowMutations(version int64, entry *proto.ChangelogEntry) []historical.BigtableRowMutation {
	rows := make([]historical.BigtableRowMutation, 0)
	for _, mutation := range compactMutations(entry) {
		rows = append(rows, s.mutationRow(version, mutation.storeName, mutation.pair))
	}
	for _, up := range entry.Upgrades {
		rows = append(rows, s.upgradeRow(version, up))
	}
	return rows
}

func (s *bigtableSink) mutationRow(version int64, storeName string, pair *proto.KVPair) historical.BigtableRowMutation {
	ts := historical.BigtableTimestamp(version)
	deleted := pair.Delete || pair.Value == nil
	row := historical.BigtableRowMutation{
		RowKey: historical.BigtableMutationRowKey(storeName, pair.Key, version, s.shards),
	}
	if !deleted {
		row.SetCells = append(row.SetCells, historical.BigtableSetCell{
			Family:          s.family,
			Qualifier:       historical.BigtableValueColumn,
			TimestampMicros: ts,
			Value:           pair.Value,
		})
	}
	row.SetCells = append(row.SetCells, historical.BigtableSetCell{
		Family:          s.family,
		Qualifier:       historical.BigtableDeletedColumn,
		TimestampMicros: ts,
		Value:           boolByte(deleted),
	})
	return row
}

func (s *bigtableSink) upgradeRow(version int64, up *proto.TreeNameUpgrade) historical.BigtableRowMutation {
	ts := historical.BigtableTimestamp(version)
	return historical.BigtableRowMutation{
		RowKey: historical.BigtableUpgradeRowKey(version, up.Name),
		SetCells: []historical.BigtableSetCell{
			{Family: s.family, Qualifier: "rename_from", TimestampMicros: ts, Value: []byte(up.RenameFrom)},
			{Family: s.family, Qualifier: historical.BigtableDeletedColumn, TimestampMicros: ts, Value: boolByte(up.Delete)},
		},
	}
}

func (s *bigtableSink) writeVersionMarker(ctx context.Context, rec Record) error {
	version := rec.Entry.Version
	ts := historical.BigtableTimestamp(version)
	row := historical.BigtableRowMutation{
		RowKey: historical.BigtableVersionRowKey(version),
		SetCells: []historical.BigtableSetCell{
			{Family: s.family, Qualifier: "topic", TimestampMicros: ts, Value: []byte(rec.Topic)},
			{Family: s.family, Qualifier: "partition", TimestampMicros: ts, Value: []byte(fmt.Sprintf("%d", rec.Partition))},
			{Family: s.family, Qualifier: "offset", TimestampMicros: ts, Value: []byte(fmt.Sprintf("%d", rec.Offset))},
			{Family: s.family, Qualifier: "ingested_at_unix_nano", TimestampMicros: ts, Value: []byte(fmt.Sprintf("%d", time.Now().UnixNano()))},
		},
	}
	errs, err := s.applyBulk(ctx, []historical.BigtableRowMutation{row})
	if err := bigtableBulkError([]historical.BigtableRowMutation{row}, errs, err); err != nil {
		return fmt.Errorf("insert bigtable version %d: %w", version, err)
	}
	return nil
}

func bigtableBulkError(rows []historical.BigtableRowMutation, errs []error, err error) error {
	if err != nil {
		return err
	}
	for i, rowErr := range errs {
		if rowErr != nil {
			return fmt.Errorf("row %q: %w", rows[i].RowKey, rowErr)
		}
	}
	return nil
}

func boolByte(v bool) []byte {
	if v {
		return []byte{1}
	}
	return []byte{0}
}
