package consumer

import (
	"context"
	"fmt"
	"strconv"
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
	return s.writeVersionMarkers(ctx, records)
}

func (s *bigtableSink) writeRecordRows(ctx context.Context, records []Record) error {
	rows := make([]historical.BigtableRowMutation, 0, bigtableRowMutationCount(records))
	for _, rec := range records {
		rows = s.appendRecordRowMutations(rows, rec.Entry.Version, rec.Entry)
	}
	if len(rows) == 0 {
		return nil
	}
	errs, err := s.applyBulk(ctx, rows)
	return bigtableBulkError(rows, errs, err)
}

func (s *bigtableSink) appendRecordRowMutations(rows []historical.BigtableRowMutation, version int64, entry *proto.ChangelogEntry) []historical.BigtableRowMutation {
	mutations := compactMutations(entry)
	for _, mutation := range mutations {
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
	rowKey := historical.BigtableMutationRowKey(storeName, pair.Key, version, s.shards)
	if deleted {
		return historical.BigtableRowMutation{
			RowKey: rowKey,
			SetCells: []historical.BigtableSetCell{{
				Family:          s.family,
				Qualifier:       historical.BigtableDeletedColumn,
				TimestampMicros: ts,
				Value:           boolByte(true),
			}},
		}
	}
	return historical.BigtableRowMutation{
		RowKey: rowKey,
		SetCells: []historical.BigtableSetCell{
			{Family: s.family, Qualifier: historical.BigtableValueColumn, TimestampMicros: ts, Value: pair.Value},
			{Family: s.family, Qualifier: historical.BigtableDeletedColumn, TimestampMicros: ts, Value: boolByte(false)},
		},
	}
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

func (s *bigtableSink) writeVersionMarkers(ctx context.Context, records []Record) error {
	rows := make([]historical.BigtableRowMutation, 0, len(records))
	ingestedAt := []byte(strconv.FormatInt(time.Now().UnixNano(), 10))
	for _, rec := range records {
		version := rec.Entry.Version
		ts := historical.BigtableTimestamp(version)
		rows = append(rows, historical.BigtableRowMutation{
			RowKey: historical.BigtableVersionRowKey(version),
			SetCells: []historical.BigtableSetCell{
				{Family: s.family, Qualifier: "topic", TimestampMicros: ts, Value: []byte(rec.Topic)},
				{Family: s.family, Qualifier: "partition", TimestampMicros: ts, Value: []byte(strconv.Itoa(rec.Partition))},
				{Family: s.family, Qualifier: "offset", TimestampMicros: ts, Value: []byte(strconv.FormatInt(rec.Offset, 10))},
				{Family: s.family, Qualifier: "ingested_at_unix_nano", TimestampMicros: ts, Value: ingestedAt},
			},
		})
	}
	errs, err := s.applyBulk(ctx, rows)
	if err := bigtableBulkError(rows, errs, err); err != nil {
		return fmt.Errorf("insert bigtable version markers: %w", err)
	}
	return nil
}

func bigtableRowMutationCount(records []Record) int {
	total := 0
	for _, rec := range records {
		for _, changeset := range rec.Entry.Changesets {
			total += len(changeset.Changeset.Pairs)
		}
		total += len(rec.Entry.Upgrades)
	}
	return total
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
