package chstore

import (
	"context"
	"errors"
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

// errInterruptedBatch reports that the newest batch was interrupted after its pending row and before
// its committed row, so its tables may hold some of its rows.
var errInterruptedBatch = errors.New("archive batch was interrupted before its commit")

// nextBlock returns the block after the newest committed row, or 0 when archive_commits is empty. It
// fails with errInterruptedBatch when the newest batch has a pending row and no committed row.
func (w *writer) nextBlock(ctx context.Context) (uint64, error) {
	var (
		starts, ends proto.ColUInt64
		committed    proto.ColBool
		found        bool
		start, end   uint64
		isCommitted  bool
	)
	err := w.client.Do(ctx, ch.Query{
		// The newest batch's committed row sorts before its pending row, which has the same end_block.
		Body: "SELECT start_block, end_block, committed FROM " + tableCommits +
			" ORDER BY end_block DESC, committed DESC LIMIT 1",
		Result: proto.Results{
			{Name: "start_block", Data: &starts},
			{Name: "end_block", Data: &ends},
			{Name: "committed", Data: &committed},
		},
		OnResult: func(context.Context, proto.Block) error {
			if !found && ends.Rows() > 0 {
				found, start, end, isCommitted = true, starts[0], ends[0], committed[0]
			}
			return nil
		},
	})
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", tableCommits, err)
	}
	if !found {
		return 0, nil
	}
	if !isCommitted {
		return 0, fmt.Errorf("%w: blocks %d-%d must be removed from every table before archiving resumes",
			errInterruptedBatch, start, end)
	}
	return end + 1, nil
}

// write inserts a pending row for b, then b's rows into every data table, then the committed row that
// marks its blocks complete. A write that fails leaves b pending, and writing b again inserts the rows
// it already stored a second time.
func (w *writer) write(ctx context.Context, b *batch) error {
	if err := w.insertCommit(ctx, b.first, b.last, false); err != nil {
		return fmt.Errorf("mark blocks %d-%d pending: %w", b.first, b.last, err)
	}
	// One table's failure does not cancel the others: ch-go does not interrupt a read blocked on
	// the server when its context is cancelled, and the tables' inserts are independent.
	var g errgroup.Group
	for _, t := range b.tables() {
		if t.input[0].Data.Rows() == 0 {
			continue
		}
		g.Go(func() error {
			if err := w.client.Insert(ctx, t.name, t.input); err != nil {
				return fmt.Errorf("insert %s blocks %d-%d: %w", t.name, b.first, b.last, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	if err := w.insertCommit(ctx, b.first, b.last, true); err != nil {
		return fmt.Errorf("commit blocks %d-%d: %w", b.first, b.last, err)
	}
	return nil
}

// insertCommit records blocks first through last in archive_commits, as committed or as pending.
func (w *writer) insertCommit(ctx context.Context, first, last uint64, committed bool) error {
	var start, end proto.ColUInt64
	var flag proto.ColBool
	at := new(proto.ColDateTime64).WithPrecision(proto.PrecisionMilli)
	start.Append(first)
	end.Append(last)
	flag.Append(committed)
	at.Append(time.Now())
	row := proto.Input{
		{Name: "start_block", Data: &start},
		{Name: "end_block", Data: &end},
		{Name: "committed", Data: &flag},
		{Name: "written_at", Data: at},
	}
	return w.client.Insert(ctx, tableCommits, row)
}
