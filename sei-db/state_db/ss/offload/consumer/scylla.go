package consumer

import (
	"context"
	"fmt"
	"time"

	"github.com/gocql/gocql"
	"golang.org/x/sync/errgroup"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload/historical"
)

const defaultScyllaMutationWorkers = 16

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
	MutationWorkers  int
}

func (c *ScyllaConfig) ApplyDefaults() {
	cfg := c.toHistorical()
	cfg.ApplyDefaults()
	c.Consistency = cfg.Consistency
	c.TimeoutMS = int(cfg.Timeout / time.Millisecond)
	c.ConnectTimeoutMS = int(cfg.ConnectTimeout / time.Millisecond)
	c.NumConns = cfg.NumConns
	if c.MutationWorkers == 0 {
		c.MutationWorkers = defaultScyllaMutationWorkers
	}
}

func (c *ScyllaConfig) Validate() error {
	cfg := c.toHistorical()
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return err
	}
	if c.MutationWorkers < 0 {
		return fmt.Errorf("scylla/cassandra mutation workers must be non-negative")
	}
	return nil
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
	session         *gocql.Session
	exec            scyllaExecFunc
	mutationWorkers int
}

var _ Sink = (*scyllaSink)(nil)
var _ BatchSink = (*scyllaSink)(nil)

func NewScyllaSink(cfg ScyllaConfig) (Sink, error) {
	cfg.ApplyDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	session, err := historical.OpenScyllaSession(cfg.toHistorical())
	if err != nil {
		return nil, err
	}
	return &scyllaSink{
		session:         session,
		exec:            sessionExec(session),
		mutationWorkers: cfg.MutationWorkers,
	}, nil
}

func (s *scyllaSink) Close() error {
	if s.session != nil {
		s.session.Close()
	}
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
	if err := s.writeRecordRows(ctx, version, entry); err != nil {
		return err
	}
	if err := s.exec(ctx, insertVersionCQL,
		historical.VersionBucket(version),
		version,
		rec.Topic,
		rec.Partition,
		rec.Offset,
		time.Now(),
	); err != nil {
		return fmt.Errorf("insert scylla/cassandra version %d: %w", version, err)
	}
	return nil
}

func (s *scyllaSink) writeRecordRows(ctx context.Context, version int64, entry *proto.ChangelogEntry) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.effectiveMutationWorkers())
	for _, mutation := range compactMutations(entry) {
		mutation := mutation
		g.Go(func() error {
			return s.writeMutation(gctx, version, mutation.storeName, mutation.pair)
		})
	}
	for _, up := range entry.Upgrades {
		up := up
		g.Go(func() error {
			return s.writeUpgrade(gctx, version, up)
		})
	}
	return g.Wait()
}

type scyllaMutation struct {
	storeName string
	pair      *proto.KVPair
}

type scyllaMutationKey struct {
	storeName string
	key       string
}

func compactMutations(entry *proto.ChangelogEntry) []scyllaMutation {
	mutations := make([]scyllaMutation, 0)
	indexByKey := make(map[scyllaMutationKey]int)
	for _, ncs := range entry.Changesets {
		storeName := ncs.Name
		for _, pair := range ncs.Changeset.Pairs {
			key := scyllaMutationKey{storeName: storeName, key: string(pair.Key)}
			if idx, ok := indexByKey[key]; ok {
				mutations[idx].pair = pair
				continue
			}
			indexByKey[key] = len(mutations)
			mutations = append(mutations, scyllaMutation{storeName: storeName, pair: pair})
		}
	}
	return mutations
}

func (s *scyllaSink) effectiveMutationWorkers() int {
	if s.mutationWorkers <= 0 {
		return defaultScyllaMutationWorkers
	}
	return s.mutationWorkers
}

func (s *scyllaSink) writeMutation(ctx context.Context, version int64, storeName string, pair *proto.KVPair) error {
	deleted := pair.Delete || pair.Value == nil
	value := pair.Value
	if deleted {
		value = nil
	}
	if err := s.exec(ctx, insertMutationCQL,
		storeName,
		pair.Key,
		version,
		value,
		deleted,
	); err != nil {
		return fmt.Errorf("insert scylla/cassandra mutation store=%s version=%d: %w", storeName, version, err)
	}
	return nil
}

func (s *scyllaSink) writeUpgrade(ctx context.Context, version int64, up *proto.TreeNameUpgrade) error {
	if err := s.exec(ctx, insertUpgradeCQL,
		version,
		up.Name,
		up.RenameFrom,
		up.Delete,
	); err != nil {
		return fmt.Errorf("insert scylla/cassandra tree upgrade version=%d name=%s: %w", version, up.Name, err)
	}
	return nil
}

type scyllaExecFunc func(ctx context.Context, stmt string, values ...interface{}) error

func sessionExec(session *gocql.Session) scyllaExecFunc {
	return func(ctx context.Context, stmt string, values ...interface{}) error {
		return session.Query(stmt, values...).WithContext(ctx).Exec()
	}
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
