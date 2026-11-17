package archive

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/ClickHouse/ch-go"
	"github.com/ClickHouse/ch-go/chpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// ErrNotFound reports that the archive holds no committed object for a key.
var ErrNotFound = errors.New("not found in the archive")

// errCoverageUnknown reports that the reader has not yet loaded which blocks are committed.
var errCoverageUnknown = errors.New("archive coverage not loaded yet")

const (
	defaultReaderMaxConns     = 16
	defaultReaderQueryTimeout = 2 * time.Second
	// lookupMaxRows fails a lookup that scans instead of reading one granule per part.
	lookupMaxRows = 5_000_000
)

var readDuration, _ = otel.Meter("sei_archive").Float64Histogram(
	"archive_read_duration_seconds",
	metric.WithDescription("Duration of archive read queries, by query and outcome"),
	metric.WithUnit("s"),
)

// ReaderOptions bounds the reader's use of ClickHouse.
type ReaderOptions struct {
	// MaxConns is the number of concurrent queries; 0 uses 16.
	MaxConns int32
	// QueryTimeout bounds each lookup; 0 uses 2s.
	QueryTimeout time.Duration
}

// Reader answers queries over committed archive blocks. It is safe for concurrent use.
type Reader struct {
	pool     *chpool.Pool
	lookup   queryLimits
	coverage coverage
	// cancel stops the coverage refresh, which closes done when it returns.
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
}

// queryLimits bounds one query: how long it may run and, when non-zero, how many rows it may read.
type queryLimits struct {
	timeout time.Duration
	maxRows uint64
}

// OpenReader returns a reader of the archive in cfg. It does not require ClickHouse to be reachable.
func OpenReader(cfg Config, opts ReaderOptions) (*Reader, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if opts.MaxConns <= 0 {
		opts.MaxConns = defaultReaderMaxConns
	}
	if opts.QueryTimeout <= 0 {
		opts.QueryTimeout = defaultReaderQueryTimeout
	}
	client := cfg.options(cfg.Database)
	client.ClientName = "sei-archive-reader"
	// The query timeout and max_execution_time bound a query; this catches a connection that
	// stops answering.
	client.ReadTimeout = opts.QueryTimeout + time.Second
	// New, unlike Dial, connects only when a query needs a connection.
	pool, err := chpool.New(context.Background(), chpool.Options{
		ClientOptions: client,
		MaxConns:      opts.MaxConns,
		MinConns:      0,
	})
	if err != nil {
		return nil, fmt.Errorf("open archive connection pool: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &Reader{
		pool:   pool,
		lookup: queryLimits{timeout: opts.QueryTimeout, maxRows: lookupMaxRows},
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go r.refreshCoverageLoop(ctx)
	return r, nil
}

// Close stops the reader and closes its connections.
func (r *Reader) Close() {
	r.closeOnce.Do(func() {
		r.cancel()
		<-r.done
		r.pool.Close()
	})
}

// run executes q, named name in metrics, errors and the server's query log, within limits.
func (r *Reader) run(ctx context.Context, name string, limits queryLimits, q ch.Query) error {
	ctx, cancel := context.WithTimeout(ctx, limits.timeout)
	defer cancel()
	// The server enforces the timeout as well: ch-go does not interrupt a read blocked on the
	// server when its context ends.
	seconds := max(1, int(math.Ceil(limits.timeout.Seconds())))
	q.Settings = append(q.Settings,
		ch.Setting{Key: "log_comment", Value: "sei-archive:" + name},
		ch.Setting{Key: "max_execution_time", Value: strconv.Itoa(seconds)},
	)
	if limits.maxRows > 0 {
		q.Settings = append(q.Settings, ch.Setting{Key: "max_rows_to_read", Value: strconv.FormatUint(limits.maxRows, 10)})
	}
	start := time.Now()
	err := r.pool.Do(ctx, q)
	outcome := "ok"
	if err != nil {
		outcome = "error"
	}
	readDuration.Record(ctx, time.Since(start).Seconds(),
		metric.WithAttributes(attribute.String("query", name), attribute.String("outcome", outcome)))
	if err != nil {
		return fmt.Errorf("archive %s: %w", name, err)
	}
	return nil
}
