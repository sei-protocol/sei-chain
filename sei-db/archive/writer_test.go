package archive

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ClickHouse/ch-go"
	"github.com/ClickHouse/ch-go/proto"
	"github.com/stretchr/testify/require"
)

// testConfig returns a config for a throwaway database on the ClickHouse server named by
// SEI_ARCHIVE_TEST_CLICKHOUSE, skipping the test when it is unset.
func testConfig(t *testing.T) Config {
	addr := os.Getenv("SEI_ARCHIVE_TEST_CLICKHOUSE")
	if addr == "" {
		t.Skip("SEI_ARCHIVE_TEST_CLICKHOUSE is not set")
	}
	cfg := Config{
		Addr:     addr,
		Database: "sei_archive_test",
		User:     "default",
		Password: os.Getenv("SEI_ARCHIVE_TEST_PASSWORD"),
	}
	dropDatabase(t, cfg)
	t.Cleanup(func() { dropDatabase(t, cfg) })
	return cfg
}

func dropDatabase(t *testing.T, cfg Config) {
	require.NoError(t, run(context.Background(), cfg.options("default"),
		[]string{"DROP DATABASE IF EXISTS " + cfg.Database + " SYNC"}))
}

// query returns the single UInt64 column a of the rows that body selects.
func query(t *testing.T, cfg Config, body string) []uint64 {
	client, err := ch.Dial(t.Context(), cfg.options(cfg.Database))
	require.NoError(t, err)
	defer func() { _ = client.Close() }()
	var (
		a   proto.ColUInt64
		out []uint64
	)
	require.NoError(t, client.Do(t.Context(), ch.Query{
		Body:   body,
		Result: proto.Results{{Name: "a", Data: &a}},
		OnResult: func(context.Context, proto.Block) error {
			out = append(out, a...)
			return nil
		},
	}))
	return out
}

// rowCounts returns the row count of every archive table.
func rowCounts(t *testing.T, cfg Config) map[string]uint64 {
	out := map[string]uint64{}
	for _, table := range []string{tableBlocks, tableTxs, tableLocator, tableLogs, tableAccountChanges, tableAccountHistory,
		tableStorageChanges, tableStorageHistory, tableCode, tableCommits} {
		out[table] = query(t, cfg, "SELECT count() AS a FROM "+table)[0]
	}
	return out
}

func testBatch(t *testing.T, from, to uint64) *batch {
	b := newBatch()
	for n := from; n <= to; n++ {
		block, result := testBlock(n)
		require.NoError(t, b.add(&block, result))
	}
	return b
}

func TestWrite(t *testing.T) {
	cfg := testConfig(t)
	require.NoError(t, migrate(t.Context(), cfg))
	w := newWriter(cfg)
	defer w.close()

	_, ok, err := w.lastCommitted(t.Context())
	require.NoError(t, err)
	require.False(t, ok)

	first := testBatch(t, 1, 3)
	require.NoError(t, w.write(t.Context(), first))
	// A retried batch is deduplicated by its insert tokens.
	require.NoError(t, w.write(t.Context(), first))
	require.NoError(t, w.write(t.Context(), testBatch(t, 4, 5)))

	last, ok, err := w.lastCommitted(t.Context())
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(5), last)
	require.Equal(t, map[string]uint64{
		tableBlocks:         5,
		tableTxs:            10,
		tableLocator:        10, // each block and its executed transaction
		tableLogs:           5,
		tableAccountChanges: 10,
		tableAccountHistory: 10,
		tableStorageChanges: 10,
		tableStorageHistory: 10,
		tableCode:           5,
		tableCommits:        2,
	}, rowCounts(t, cfg))

	require.Equal(t, []uint64{1, 0}, query(t, cfg,
		"SELECT toUInt64(executed) AS a FROM transactions_receipts WHERE block_number = 2 ORDER BY transaction_index"))
	// A transaction is found by its hash prefix.
	_, result := testBlock(2)
	executed := result.Txs[0].Hash
	require.Equal(t, []uint64{2 << txIndexBits}, query(t, cfg, fmt.Sprintf(
		"SELECT location AS a FROM hash_locator WHERE hash_prefix = %d AND object_kind = %d",
		binary.BigEndian.Uint64(executed[:8]), locatorTx)))
	require.Equal(t, []uint64{900}, query(t, cfg,
		"SELECT toUInt64(balance) AS a FROM account_changes WHERE block_number = 2 AND nonce = 5"))
	require.Equal(t, []uint64{0}, query(t, cfg,
		"SELECT toUInt64(count()) AS a FROM storage_changes WHERE block_number = 2 AND value != repeat('\\0', 32)"+
			" AND slot = unhex('52'||repeat('00', 31))"))
}

// TestStateReads runs the two reads the account and storage tables are laid out for: a slot or an
// account at a past height, and everything one block changed.
func TestStateReads(t *testing.T) {
	cfg := testConfig(t)
	require.NoError(t, migrate(t.Context(), cfg))
	w := newWriter(cfg)
	defer w.close()
	require.NoError(t, w.write(t.Context(), testBatch(t, 1, 5)))

	// Block 1's contract wrote slot 0x51 in block 1 only, so at height 4 it still holds that value.
	const (
		contract = "toFixedString(unhex('000000000000000000000000000000000000c001'), 20)"
		slot     = "toFixedString(unhex('51' || repeat('00', 31)), 32)"
		sender   = "toFixedString(unhex('000000000000000000000000000000000000005e'), 20)"
	)
	require.Equal(t, []uint64{1}, query(t, cfg, "SELECT block_number AS a FROM storage_history"+
		" WHERE sipHash64(address, slot) = sipHash64("+contract+", "+slot+") AND address = "+contract+
		" AND slot = "+slot+" AND block_number <= 4 ORDER BY block_number DESC LIMIT 1"))
	require.Equal(t, []uint64{1}, query(t, cfg, "SELECT toUInt64(count()) AS a FROM storage_changes"+
		" WHERE block_number = 1 AND address = "+contract+" AND slot = "+slot+
		" AND value = toFixedString(unhex('7a01' || repeat('00', 30)), 32)"))

	// The sender changes in every block, so its newest change at height 3 is block 3's.
	require.Equal(t, []uint64{3}, query(t, cfg, "SELECT block_number AS a FROM account_history"+
		" WHERE sipHash64(address) = sipHash64("+sender+") AND address = "+sender+
		" AND block_number <= 3 ORDER BY block_number DESC LIMIT 1"))
	require.Equal(t, []uint64{7}, query(t, cfg,
		"SELECT nonce AS a FROM account_changes WHERE block_number = 3 AND address = "+sender))

	// Everything block 2 changed.
	require.Equal(t, []uint64{2}, query(t, cfg, "SELECT toUInt64(count()) AS a FROM account_changes WHERE block_number = 2"))
	require.Equal(t, []uint64{2}, query(t, cfg, "SELECT toUInt64(count()) AS a FROM storage_changes WHERE block_number = 2"))
}

func TestWriteRetriesAPartialInsert(t *testing.T) {
	cfg := testConfig(t)
	require.NoError(t, migrate(t.Context(), cfg))
	w := newWriter(cfg)
	defer w.close()
	b := testBatch(t, 1, 3)

	require.NoError(t, run(t.Context(), cfg.options(cfg.Database), []string{"DETACH TABLE blocks"}))
	start := time.Now()
	require.Error(t, w.write(t.Context(), b))
	require.Less(t, time.Since(start), 10*time.Second, "a failed table does not stall the others")
	_, ok, err := w.lastCommitted(t.Context())
	require.NoError(t, err)
	require.False(t, ok, "a partly written batch is not committed")

	require.NoError(t, run(t.Context(), cfg.options(cfg.Database), []string{"ATTACH TABLE blocks"}))
	require.NoError(t, w.write(t.Context(), b))
	require.Equal(t, map[string]uint64{
		tableBlocks:         3,
		tableTxs:            6,
		tableLocator:        6,
		tableLogs:           3,
		tableAccountChanges: 6,
		tableAccountHistory: 6,
		tableStorageChanges: 6,
		tableStorageHistory: 6,
		tableCode:           3,
		tableCommits:        1,
	}, rowCounts(t, cfg))
}

func TestMigrateRejectsUnsafeDatabase(t *testing.T) {
	require.ErrorContains(t, migrate(t.Context(), Config{Database: "a; DROP TABLE x"}), "not a plain identifier")
}

func TestExporterWritesExecutedBlocks(t *testing.T) {
	cfg := testConfig(t)
	exporter, err := StartExporter(t.Context(), cfg)
	require.NoError(t, err)
	store := func(n uint64, release func()) {
		block, result := testBlock(n)
		require.NoError(t, exporter.StoreBlockResult(t.Context(), block, result, release))
	}
	released := 0
	for n := uint64(1); n <= 3; n++ {
		store(n, func() { released++ })
	}
	exporter.Close()
	require.Equal(t, 3, released)
	require.Equal(t, uint64(3), rowCounts(t, cfg)[tableBlocks])

	// A restarted exporter resumes after the archived blocks and skips any it is handed again.
	exporter, err = StartExporter(t.Context(), cfg)
	require.NoError(t, err)
	for n := uint64(2); n <= 4; n++ {
		store(n, func() {})
	}
	// A missing block ends a batch, so no commit spans it.
	store(6, func() {})
	exporter.Close()
	counts := rowCounts(t, cfg)
	require.Equal(t, uint64(5), counts[tableBlocks])
	require.Equal(t, uint64(10), counts[tableTxs])
	require.Equal(t, []uint64{1, 4, 6}, query(t, cfg, "SELECT start_block AS a FROM archive_commits ORDER BY a"))
	require.Equal(t, []uint64{3, 4, 6}, query(t, cfg, "SELECT end_block AS a FROM archive_commits ORDER BY a"))

	// A closed exporter drops blocks rather than failing them.
	dropped := false
	store(7, func() { dropped = true })
	require.True(t, dropped)
}
