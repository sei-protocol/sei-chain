//go:build foundationdb

package consumer

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload/historical"
)

type FoundationDBConfig = historical.FoundationDBConfig

type foundationDBSink struct {
	client *historical.FoundationDBClient
	prefix string
	shards int
}

var _ Sink = (*foundationDBSink)(nil)
var _ BatchSink = (*foundationDBSink)(nil)

func NewFoundationDBSink(cfg FoundationDBConfig) (Sink, error) {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	client, err := historical.OpenFoundationDBClient(cfg)
	if err != nil {
		return nil, err
	}
	return &foundationDBSink{client: client, prefix: cfg.Prefix, shards: cfg.Shards}, nil
}

func (s *foundationDBSink) Close() error {
	return s.client.Close()
}

func (s *foundationDBSink) LastVersion(ctx context.Context) (int64, error) {
	return s.client.LastVersion(ctx)
}

func (s *foundationDBSink) Write(ctx context.Context, rec Record) error {
	return s.WriteBatch(ctx, []Record{rec})
}

func (s *foundationDBSink) WriteBatch(ctx context.Context, records []Record) error {
	records = compactRecords(records)
	if len(records) == 0 {
		return nil
	}
	if len(records) == 1 {
		return s.writeRecord(ctx, records[0])
	}
	return s.writeRecordsPipelined(ctx, records)
}

func (s *foundationDBSink) writeRecord(ctx context.Context, rec Record) error {
	writes := make([]historical.FoundationDBWrite, 0, 1+entryWriteCapacity(rec.Entry))
	writes = s.appendRecordWrites(writes, rec.Entry.Version, rec.Entry)
	writes = append(writes, s.versionWrite(rec))
	if err := s.client.WriteBatch(ctx, writes); err != nil {
		return fmt.Errorf("write foundationdb record version=%d: %w", rec.Entry.Version, err)
	}
	return nil
}

func (s *foundationDBSink) writeRecordsPipelined(ctx context.Context, records []Record) error {
	g, gctx := errgroup.WithContext(ctx)
	for _, rec := range records {
		rec := rec
		g.Go(func() error {
			if err := s.writeRecordRows(gctx, rec.Entry.Version, rec.Entry); err != nil {
				return fmt.Errorf("write foundationdb rows version=%d: %w", rec.Entry.Version, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	versionWrites := make([]historical.FoundationDBWrite, 0, len(records))
	for _, rec := range records {
		versionWrites = append(versionWrites, s.versionWrite(rec))
	}
	if err := s.client.WriteBatch(ctx, versionWrites); err != nil {
		return fmt.Errorf("write foundationdb version markers: %w", err)
	}
	return nil
}

func (s *foundationDBSink) writeRecordRows(ctx context.Context, version int64, entry *proto.ChangelogEntry) error {
	writes := make([]historical.FoundationDBWrite, 0, entryWriteCapacity(entry))
	writes = s.appendRecordWrites(writes, version, entry)
	return s.client.WriteBatch(ctx, writes)
}

func (s *foundationDBSink) appendRecordWrites(writes []historical.FoundationDBWrite, version int64, entry *proto.ChangelogEntry) []historical.FoundationDBWrite {
	for _, mutation := range compactMutations(entry) {
		writes = append(writes, s.mutationWrite(version, mutation.storeName, mutation.pair))
	}
	for _, up := range entry.Upgrades {
		writes = append(writes, s.upgradeWrite(version, up))
	}
	return writes
}

func (s *foundationDBSink) mutationWrite(version int64, storeName string, pair *proto.KVPair) historical.FoundationDBWrite {
	deleted := pair.Delete || pair.Value == nil
	value := pair.Value
	if deleted {
		value = nil
	}
	return historical.FoundationDBWrite{
		Key:   historical.FoundationDBMutationKey(s.prefix, storeName, pair.Key, version, s.shards),
		Value: historical.FoundationDBMutationValue(value, deleted),
	}
}

func (s *foundationDBSink) upgradeWrite(version int64, up *proto.TreeNameUpgrade) historical.FoundationDBWrite {
	return historical.FoundationDBWrite{
		Key:   historical.FoundationDBUpgradeKey(s.prefix, version, up.Name),
		Value: historical.FoundationDBUpgradeValue(up.RenameFrom, up.Delete),
	}
}

func (s *foundationDBSink) versionWrite(rec Record) historical.FoundationDBWrite {
	version := rec.Entry.Version
	value := []byte(rec.Topic + "\x00" +
		strconv.Itoa(rec.Partition) + "\x00" +
		strconv.FormatInt(rec.Offset, 10) + "\x00" +
		strconv.FormatInt(time.Now().UnixNano(), 10))
	return historical.FoundationDBWrite{
		Key:   historical.FoundationDBVersionKey(s.prefix, version),
		Value: value,
	}
}
