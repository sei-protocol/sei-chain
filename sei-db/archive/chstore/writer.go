package chstore

import (
	"context"
	"fmt"
	"time"

	"github.com/ClickHouse/ch-go"
	"github.com/ClickHouse/ch-go/proto"
	"golang.org/x/sync/errgroup"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/clickhouse"
)

const (
	// writeTimeout bounds one attempt to write a batch.
	writeTimeout = time.Minute
	// writerConns is the number of data tables, which a write inserts at once.
	writerConns = 9
)

// writer writes batches to the archive. It is not safe for concurrent use.
type writer struct {
	client *clickhouse.Client
}

// newWriter returns a writer of the archive in cfg.
func newWriter(cfg clickhouse.Config) (*writer, error) {
	client, err := clickhouse.Open(cfg, clickhouse.Options{
		Name:     "sei-archive",
		MaxConns: writerConns,
		// ClickHouse answers an INSERT only once its parts are written, which can take longer than
		// ch-go's default 3s; each write attempt is bounded by its context instead.
		ReadTimeout: writeTimeout,
	})
	if err != nil {
		return nil, err
	}
	return &writer{client: client}, nil
}

// close closes the writer's connections.
func (w *writer) close() {
	w.client.Close()
}

// lastCommitted returns the highest block a committed batch holds, and false when no batch is
// committed.
func (w *writer) lastCommitted(ctx context.Context) (uint64, bool, error) {
	var last, batches proto.ColUInt64
	err := w.client.Do(ctx, ch.Query{
		Body: "SELECT max(end_block) AS last, count() AS batches FROM " + tableCommits,
		Result: proto.Results{
			{Name: "last", Data: &last},
			{Name: "batches", Data: &batches},
		},
	})
	if err != nil {
		return 0, false, fmt.Errorf("read %s: %w", tableCommits, err)
	}
	if batches.Rows() == 0 || batches[0] == 0 {
		return 0, false, nil
	}
	return last[0], true, nil
}

// write inserts b into every data table, then records the archive_commits row that marks its
// blocks complete. Writing the same batch again inserts nothing twice: every insert carries a
// deduplication token naming the table and the batch's block range.
func (w *writer) write(ctx context.Context, b *batch) error {
	// One table's failure does not cancel the others: ch-go does not interrupt a read blocked on
	// the server when its context is cancelled, and the tables' inserts are independent.
	var g errgroup.Group
	for _, t := range b.tables() {
		if t.input[0].Data.Rows() == 0 {
			continue
		}
		g.Go(func() error {
			if err := w.insert(ctx, t.name, t.input, b.first, b.last); err != nil {
				return fmt.Errorf("insert %s blocks %d-%d: %w", t.name, b.first, b.last, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	var start, end proto.ColUInt64
	at := new(proto.ColDateTime64).WithPrecision(proto.PrecisionMilli)
	start.Append(b.first)
	end.Append(b.last)
	at.Append(time.Now())
	commit := proto.Input{
		{Name: "start_block", Data: &start},
		{Name: "end_block", Data: &end},
		{Name: "committed_at", Data: at},
	}
	if err := w.insert(ctx, tableCommits, commit, b.first, b.last); err != nil {
		return fmt.Errorf("commit blocks %d-%d: %w", b.first, b.last, err)
	}
	return nil
}

// insert writes input to table, deduplicated by a token for the block range.
func (w *writer) insert(ctx context.Context, table string, input proto.Input, first, last uint64) error {
	return w.client.Insert(ctx, table, input, fmt.Sprintf("%s:%d-%d", table, first, last))
}
