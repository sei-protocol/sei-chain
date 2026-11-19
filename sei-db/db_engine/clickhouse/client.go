// Package clickhouse runs statements against a ClickHouse database over a pool of native
// connections, and applies versioned schema migrations to it.
package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/ClickHouse/ch-go"
	"github.com/ClickHouse/ch-go/chpool"
	"github.com/ClickHouse/ch-go/proto"
)

// Config locates a ClickHouse database and the credentials to use it.
type Config struct {
	// Addr is the ClickHouse native-protocol address, host:port.
	Addr string
	// Database is a plain identifier: letters, digits and underscores.
	Database string
	User     string
	Password string
}

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Validate reports an error when c's database is not a plain identifier.
func (c Config) Validate() error {
	if !identifier.MatchString(c.Database) {
		return fmt.Errorf("clickhouse database %q is not a plain identifier", c.Database)
	}
	return nil
}

// options returns the connection options for database.
func (c Config) options(database string, opts Options) ch.Options {
	return ch.Options{
		Address:     c.Addr,
		Database:    database,
		User:        c.User,
		Password:    c.Password,
		Compression: ch.CompressionLZ4,
		ClientName:  opts.Name,
		ReadTimeout: opts.ReadTimeout,
	}
}

// Options tunes a Client.
type Options struct {
	// Name identifies the client in the server's query log.
	Name string
	// MaxConns is how many statements the client runs at once; 0 uses chpool's default.
	MaxConns int32
	// ReadTimeout bounds each wait for the server; 0 uses ch-go's default.
	ReadTimeout time.Duration
}

// Client runs statements against one ClickHouse database. It is safe for concurrent use.
type Client struct {
	addr string
	pool *chpool.Pool
}

// Open returns a client of the database in cfg. It connects only when a statement needs a
// connection, so ClickHouse need not be reachable.
func Open(cfg Config, opts Options) (*Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	pool, err := chpool.New(context.Background(), chpool.Options{
		ClientOptions: cfg.options(cfg.Database, opts),
		MaxConns:      opts.MaxConns,
	})
	if err != nil {
		return nil, fmt.Errorf("open clickhouse connection pool: %w", err)
	}
	return &Client{addr: cfg.Addr, pool: pool}, nil
}

// Close closes the client's connections.
func (c *Client) Close() {
	c.pool.Close()
}

// Do runs q on a pooled connection.
func (c *Client) Do(ctx context.Context, q ch.Query) error {
	conn, err := c.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("connect to clickhouse %s: %w", c.addr, err)
	}
	err = conn.Do(ctx, q)
	var exception *ch.Exception
	if err != nil && !errors.As(err, &exception) {
		// A server exception leaves the connection in step with the server; any other error may
		// leave it in the middle of a packet.
		_ = conn.Close()
		return err
	}
	conn.Release()
	return err
}

// Insert writes input into table and returns once the server has stored it. An insert carrying a
// token the table has already seen, within its deduplication window, writes nothing.
func (c *Client) Insert(ctx context.Context, table string, input proto.Input, token string) error {
	return c.Do(ctx, ch.Query{
		Body:  input.Into(table),
		Input: input,
		Settings: []ch.Setting{
			{Key: "insert_deduplication_token", Value: token},
			// The server-side async insert buffer does not deduplicate by token.
			{Key: "async_insert", Value: "0"},
		},
	})
}
