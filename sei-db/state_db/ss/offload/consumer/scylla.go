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

const (
	defaultScyllaMutationWorkers = 16

	// defaultScyllaRecordWorkers bounds how many records write their rows
	// concurrently, so peak in-flight CQL writes stay at
	// recordWorkers*mutationWorkers instead of len(batch)*mutationWorkers.
	defaultScyllaRecordWorkers = 4
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
	MutationWorkers  int
}

func (c ScyllaConfig) Configured() bool {
	return c.toHistorical().Configured()
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
	return historical.ScyllaLastVersion(ctx, s.session)
}

func (s *scyllaSink) Write(ctx context.Context, rec Record) error {
	return s.WriteBatch(ctx, []Record{rec})
}

func (s *scyllaSink) WriteBatch(ctx context.Context, records []Record) error {
	records = compactRecords(records)
	if len(records) == 0 {
		return nil
	}
	if len(records) == 1 {
		return s.writeRecord(ctx, records[0])
	}
	return s.writeRecordsPipelined(ctx, records)
}

func (s *scyllaSink) writeRecord(ctx context.Context, rec Record) error {
	if err := s.writeRecordRows(ctx, rec.Entry); err != nil {
		return err
	}
	return s.writeVersionMarker(ctx, rec)
}

func (s *scyllaSink) writeVersionMarker(ctx context.Context, rec Record) error {
	version := rec.Entry.Version
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

func (s *scyllaSink) writeRecordsPipelined(ctx context.Context, records []Record) error {
	rowCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var g errgroup.Group
	// A semaphore rather than errgroup.SetLimit keeps goroutine launching
	// non-blocking, so the ordered version-marker loop below overlaps with row
	// writes from the first record onward.
	sem := make(chan struct{}, defaultScyllaRecordWorkers)
	rowDone := make([]chan error, len(records))
	for i := range records {
		rowDone[i] = make(chan error, 1)
		i := i
		rec := records[i]
		g.Go(func() error {
			select {
			case sem <- struct{}{}:
			case <-rowCtx.Done():
				rowDone[i] <- rowCtx.Err()
				return rowCtx.Err()
			}
			defer func() { <-sem }()
			err := s.writeRecordRows(rowCtx, rec.Entry)
			if err != nil {
				err = fmt.Errorf("write scylla/cassandra rows version %d: %w", rec.Entry.Version, err)
			}
			rowDone[i] <- err
			return err
		})
	}
	for i, rec := range records {
		if err := <-rowDone[i]; err != nil {
			cancel()
			_ = g.Wait()
			return err
		}
		if err := s.writeVersionMarker(ctx, rec); err != nil {
			cancel()
			_ = g.Wait()
			return err
		}
	}
	return g.Wait()
}

func (s *scyllaSink) writeRecordRows(ctx context.Context, entry *proto.ChangelogEntry) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(s.mutationWorkers)
	for _, mutation := range compactMutations(entry) {
		mutation := mutation
		g.Go(func() error {
			return s.writeMutation(gctx, entry.Version, mutation.storeName, mutation.pair)
		})
	}
	for _, up := range entry.Upgrades {
		up := up
		g.Go(func() error {
			return s.writeUpgrade(gctx, entry.Version, up)
		})
	}
	return g.Wait()
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
