package clickhouse

import (
	"context"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ClickHouse/ch-go"
	"github.com/ClickHouse/ch-go/proto"
	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/clickhouse/chtest"
)

func TestMain(m *testing.M) { chtest.Main(m) }

// testConfig returns a config for a throwaway database on the package's test server.
func testConfig(t *testing.T) Config {
	srv := chtest.Start(t)
	cfg := Config{
		Addr:     srv.Addr,
		Database: "sei_clickhouse_test",
		User:     srv.User,
		Password: srv.Password,
	}
	drop := []string{"DROP DATABASE IF EXISTS " + cfg.Database + " SYNC"}
	require.NoError(t, run(context.Background(), cfg.options("default", Options{}), drop))
	t.Cleanup(func() { require.NoError(t, run(context.Background(), cfg.options("default", Options{}), drop)) })
	return cfg
}

// count returns the single UInt64 column a that body selects.
func count(t *testing.T, client *Client, body string) uint64 {
	var a proto.ColUInt64
	var out uint64
	require.NoError(t, client.Do(t.Context(), ch.Query{
		Body:   body,
		Result: proto.Results{{Name: "a", Data: &a}},
		OnResult: func(context.Context, proto.Block) error {
			// A block can hold no rows.
			for _, v := range a {
				out = v
			}
			return nil
		},
	}))
	return out
}

func TestClientInsertStoresEveryInsert(t *testing.T) {
	cfg := testConfig(t)
	require.NoError(t, Migrate(t.Context(), cfg, fstest.MapFS{"migrations/0001_t.sql": {Data: []byte(
		"CREATE TABLE t (x UInt64) ENGINE = MergeTree ORDER BY x;")}}))
	client, err := Open(cfg, Options{})
	require.NoError(t, err)
	defer client.Close()

	for range 2 {
		var x proto.ColUInt64
		x.Append(1)
		require.NoError(t, client.Insert(t.Context(), "t", proto.Input{{Name: "x", Data: &x}}))
	}
	require.Equal(t, uint64(2), count(t, client, "SELECT count() AS a FROM t"), "a repeated insert is not dropped")
}

func TestClientKeepsTheConnectionAfterAServerException(t *testing.T) {
	cfg := testConfig(t)
	require.NoError(t, Migrate(t.Context(), cfg, fstest.MapFS{"migrations/0001_t.sql": {Data: []byte(
		"CREATE TABLE t (x UInt8) ENGINE = Memory;")}}))
	client, err := Open(cfg, Options{})
	require.NoError(t, err)
	defer client.Close()

	var exception *ch.Exception
	require.ErrorAs(t, client.Do(t.Context(), ch.Query{Body: "SELECT * FROM missing_table"}), &exception)
	require.Equal(t, int32(1), client.pool.Stat().TotalResources(), "the connection stays in the pool")
	require.Equal(t, uint64(1), count(t, client, "SELECT toUInt64(1) AS a"))

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	require.Error(t, client.Do(ctx, ch.Query{Body: "SELECT sleep(2)"}))
	// The pool destroys a discarded connection in the background.
	require.Eventually(t, func() bool { return client.pool.Stat().TotalResources() == 0 }, 5*time.Second, 10*time.Millisecond,
		"an interrupted connection is discarded")
}

func TestOpenWithoutClickHouse(t *testing.T) {
	client, err := Open(Config{Addr: "127.0.0.1:1", Database: "x"}, Options{})
	require.NoError(t, err)
	defer client.Close()
	require.Error(t, client.Do(t.Context(), ch.Query{Body: "SELECT 1"}))

	_, err = Open(Config{Database: "a; DROP TABLE x"}, Options{})
	require.ErrorContains(t, err, "not a plain identifier")
}
