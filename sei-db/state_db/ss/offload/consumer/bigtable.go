package consumer

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload/historical"
	"golang.org/x/sync/errgroup"
)

type BigtableConfig = historical.BigtableConfig

const (
	defaultBigtableMutationChunkRows        = 1024
	defaultBigtableMutationChunkConcurrency = 8
)

type bigtableSink struct {
	client           *historical.BigtableClient
	applyBulk        historical.BigtableApplyBulkFunc
	family           string
	shards           int
	bulkChunkRows    int
	bulkChunkWorkers int
}

var _ Sink = (*bigtableSink)(nil)

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
		client:           client,
		applyBulk:        client.ApplyBulk,
		family:           cfg.Family,
		shards:           cfg.Shards,
		bulkChunkRows:    defaultBigtableMutationChunkRows,
		bulkChunkWorkers: defaultBigtableMutationChunkConcurrency,
	}, nil
}

func (s *bigtableSink) Close() error {
	if s.client != nil {
		return s.client.Close()
	}
	return nil
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
		rows = s.appendRecordRowMutations(rows, rec.Entry)
	}
	if len(rows) == 0 {
		return nil
	}
	return s.applyRecordRowMutations(ctx, rows)
}

func (s *bigtableSink) applyRecordRowMutations(ctx context.Context, rows []historical.BigtableRowMutation) error {
	chunks := bigtableRowMutationChunks(rows, s.bulkChunkRows)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.bulkChunkWorkers)
	for _, chunk := range chunks {
		chunk := chunk
		g.Go(func() error {
			errs, err := s.applyBulk(gctx, chunk)
			return bigtableBulkError(chunk, errs, err)
		})
	}
	return g.Wait()
}

func (s *bigtableSink) appendRecordRowMutations(rows []historical.BigtableRowMutation, entry *proto.ChangelogEntry) []historical.BigtableRowMutation {
	for _, mutation := range compactMutations(entry) {
		rows = append(rows, s.mutationRow(entry.Version, mutation.storeName, mutation.pair))
	}
	for _, up := range entry.Upgrades {
		rows = append(rows, s.upgradeRow(entry.Version, up))
	}
	return rows
}

// mutationRow writes value+deleted cells for live pairs but only a deleted
// cell for tombstones, saving a cell per delete. Readers must therefore check
// the deleted column before trusting any value cell — a replayed live write
// followed by a tombstone leaves both cells on the row.
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
		total += entryMutationCapacity(rec.Entry) + len(rec.Entry.Upgrades)
	}
	return total
}

func bigtableRowMutationChunks(rows []historical.BigtableRowMutation, maxRows int) [][]historical.BigtableRowMutation {
	if len(rows) == 0 {
		return nil
	}
	if maxRows <= 0 {
		maxRows = len(rows)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].RowKey < rows[j].RowKey
	})

	chunks := make([][]historical.BigtableRowMutation, 0, (len(rows)+maxRows-1)/maxRows)
	start := 0
	startLocality := bigtableRowLocality(rows[0].RowKey)
	for i := 1; i < len(rows); i++ {
		locality := bigtableRowLocality(rows[i].RowKey)
		if i-start >= maxRows || locality != startLocality {
			chunks = append(chunks, rows[start:i])
			start = i
			startLocality = locality
		}
	}
	return append(chunks, rows[start:])
}

func bigtableRowLocality(rowKey string) string {
	// Mutation row keys are m|shard|store|key|version; keep chunks inside one
	// shard prefix so separate chunks can hit separate Bigtable tablets.
	if len(rowKey) >= 3 && rowKey[0] == 'm' {
		return rowKey[:3]
	}
	if len(rowKey) > 0 {
		return rowKey[:1]
	}
	return rowKey
}

func bigtableBulkError(rows []historical.BigtableRowMutation, errs []error, err error) error {
	if err != nil {
		return err
	}
	if len(errs) != len(rows) {
		return fmt.Errorf("bigtable returned %d mutation results for %d rows", len(errs), len(rows))
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
