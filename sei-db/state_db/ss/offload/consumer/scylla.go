package consumer

import (
	"context"
	"fmt"
	"time"

	"github.com/gocql/gocql"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload/historical"
)

type ScyllaConfig struct {
	Hosts            []string
	Keyspace         string
	Username         string
	Password         string
	Datacenter       string
	Consistency      string
	TimeoutMS        int
	ConnectTimeoutMS int
	NumConns         int
}

func (c *ScyllaConfig) ApplyDefaults() {
	cfg := c.toHistorical()
	cfg.ApplyDefaults()
	c.Consistency = cfg.Consistency
	c.TimeoutMS = int(cfg.Timeout / time.Millisecond)
	c.ConnectTimeoutMS = int(cfg.ConnectTimeout / time.Millisecond)
	c.NumConns = cfg.NumConns
}

func (c *ScyllaConfig) Validate() error {
	cfg := c.toHistorical()
	cfg.ApplyDefaults()
	return cfg.Validate()
}

func (c ScyllaConfig) toHistorical() historical.ScyllaConfig {
	return historical.ScyllaConfig{
		Hosts:          c.Hosts,
		Keyspace:       c.Keyspace,
		Username:       c.Username,
		Password:       c.Password,
		Datacenter:     c.Datacenter,
		Consistency:    c.Consistency,
		Timeout:        time.Duration(c.TimeoutMS) * time.Millisecond,
		ConnectTimeout: time.Duration(c.ConnectTimeoutMS) * time.Millisecond,
		NumConns:       c.NumConns,
	}
}

type scyllaSink struct {
	session *gocql.Session
}

var _ Sink = (*scyllaSink)(nil)
var _ BatchSink = (*scyllaSink)(nil)

func NewScyllaSink(cfg ScyllaConfig) (Sink, error) {
	session, err := historical.OpenScyllaSession(cfg.toHistorical())
	if err != nil {
		return nil, err
	}
	return &scyllaSink{session: session}, nil
}

func (s *scyllaSink) Close() error {
	s.session.Close()
	return nil
}

func (s *scyllaSink) LastVersion(ctx context.Context) (int64, error) {
	var maxVersion int64
	for bucket := 0; bucket < historical.VersionBucketCount; bucket++ {
		var version int64
		err := s.session.Query(selectLatestVersionCQL, bucket).WithContext(ctx).Scan(&version)
		if err != nil {
			if err == gocql.ErrNotFound {
				continue
			}
			return 0, fmt.Errorf("read latest scylla/cassandra version bucket %d: %w", bucket, err)
		}
		if version > maxVersion {
			maxVersion = version
		}
	}
	return maxVersion, nil
}

func (s *scyllaSink) Write(ctx context.Context, rec Record) error {
	return s.WriteBatch(ctx, []Record{rec})
}

func (s *scyllaSink) WriteBatch(ctx context.Context, records []Record) error {
	for _, rec := range compactRecords(records) {
		if err := s.writeRecord(ctx, rec); err != nil {
			return err
		}
	}
	return nil
}

func compactRecords(records []Record) []Record {
	for _, rec := range records {
		if rec.Entry == nil {
			out := make([]Record, 0, len(records))
			for _, rec := range records {
				if rec.Entry != nil {
					out = append(out, rec)
				}
			}
			return out
		}
	}
	return records
}

func (s *scyllaSink) writeRecord(ctx context.Context, rec Record) error {
	entry := rec.Entry
	if entry == nil {
		return nil
	}
	version := entry.Version
	for _, ncs := range entry.Changesets {
		for _, pair := range ncs.Changeset.Pairs {
			if err := s.writeMutation(ctx, version, ncs.Name, pair); err != nil {
				return err
			}
		}
	}
	for _, up := range entry.Upgrades {
		if err := s.writeUpgrade(ctx, version, up); err != nil {
			return err
		}
	}
	if err := s.session.Query(insertVersionCQL,
		historical.VersionBucket(version),
		version,
		rec.Topic,
		rec.Partition,
		rec.Offset,
		time.Now(),
	).WithContext(ctx).Exec(); err != nil {
		return fmt.Errorf("insert scylla/cassandra version %d: %w", version, err)
	}
	return nil
}

func (s *scyllaSink) writeMutation(ctx context.Context, version int64, storeName string, pair *proto.KVPair) error {
	deleted := pair.Delete || pair.Value == nil
	value := pair.Value
	if deleted {
		value = nil
	}
	if err := s.session.Query(insertMutationCQL,
		storeName,
		pair.Key,
		version,
		value,
		deleted,
	).WithContext(ctx).Exec(); err != nil {
		return fmt.Errorf("insert scylla/cassandra mutation store=%s version=%d: %w", storeName, version, err)
	}
	return nil
}

func (s *scyllaSink) writeUpgrade(ctx context.Context, version int64, up *proto.TreeNameUpgrade) error {
	if err := s.session.Query(insertUpgradeCQL,
		version,
		up.Name,
		up.RenameFrom,
		up.Delete,
	).WithContext(ctx).Exec(); err != nil {
		return fmt.Errorf("insert scylla/cassandra tree upgrade version=%d name=%s: %w", version, up.Name, err)
	}
	return nil
}

const selectLatestVersionCQL = `
SELECT version
FROM state_versions
WHERE bucket = ?
LIMIT 1`

const insertVersionCQL = `
INSERT INTO state_versions (
    bucket, version, kafka_topic, kafka_partition, kafka_offset, ingested_at
) VALUES (?, ?, ?, ?, ?, ?)`

const insertMutationCQL = `
INSERT INTO state_mutations (
    store_name, state_key, version, value, deleted
) VALUES (?, ?, ?, ?, ?)`

const insertUpgradeCQL = `
INSERT INTO state_tree_upgrades (
    version, name, rename_from, deleted
) VALUES (?, ?, ?, ?)`
