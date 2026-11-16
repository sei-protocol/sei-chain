package archive

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ClickHouse/ch-go"
	"github.com/ClickHouse/ch-go/proto"
	"golang.org/x/sync/errgroup"
)

// writer writes batches to the archive over one native connection per table, redialing a
// connection that failed. It is not safe for concurrent use.
type writer struct {
	cfg Config
	// mu guards conns, which a write uses from one goroutine per table.
	mu    sync.Mutex
	conns map[string]*ch.Client
}

func newWriter(cfg Config) *writer {
	return &writer{cfg: cfg, conns: map[string]*ch.Client{}}
}

// close closes the writer's connections.
func (w *writer) close() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, c := range w.conns {
		_ = c.Close()
	}
	clear(w.conns)
}

// lastCommitted returns the highest block a committed batch holds, and false when no batch is
// committed.
func (w *writer) lastCommitted(ctx context.Context) (uint64, bool, error) {
	var last, batches proto.ColUInt64
	err := w.do(ctx, tableCommits, ch.Query{
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

// insert writes input to table synchronously, deduplicated by a token for the block range.
func (w *writer) insert(ctx context.Context, table string, input proto.Input, first, last uint64) error {
	return w.do(ctx, table, ch.Query{
		Body:  input.Into(table),
		Input: input,
		Settings: []ch.Setting{
			{Key: "insert_deduplication_token", Value: fmt.Sprintf("%s:%d-%d", table, first, last)},
			// The server-side async insert buffer does not deduplicate by token.
			{Key: "async_insert", Value: "0"},
		},
	})
}

// do runs q on table's connection, dropping the connection when q fails.
func (w *writer) do(ctx context.Context, table string, q ch.Query) error {
	c, err := w.conn(ctx, table)
	if err != nil {
		return err
	}
	if err := c.Do(ctx, q); err != nil {
		w.mu.Lock()
		_ = c.Close()
		delete(w.conns, table)
		w.mu.Unlock()
		return err
	}
	return nil
}

// conn returns table's connection, dialing one when there is none.
func (w *writer) conn(ctx context.Context, table string) (*ch.Client, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if c := w.conns[table]; c != nil && !c.IsClosed() {
		return c, nil
	}
	c, err := ch.Dial(ctx, w.cfg.options(w.cfg.Database))
	if err != nil {
		return nil, fmt.Errorf("dial clickhouse %s: %w", w.cfg.Addr, err)
	}
	w.conns[table] = c
	return c, nil
}
