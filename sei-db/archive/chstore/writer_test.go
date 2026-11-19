package chstore

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

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/clickhouse"
)

// testConfig returns a config for a throwaway database on the ClickHouse server named by
// SEI_ARCHIVE_TEST_CLICKHOUSE, skipping the test when it is unset.
func testConfig(t *testing.T) clickhouse.Config {
	addr := os.Getenv("SEI_ARCHIVE_TEST_CLICKHOUSE")
	if addr == "" {
		t.Skip("SEI_ARCHIVE_TEST_CLICKHOUSE is not set")
	}
	cfg := clickhouse.Config{
		Addr:     addr,
		Database: "sei_archive_test",
		User:     "default",
		Password: os.Getenv("SEI_ARCHIVE_TEST_PASSWORD"),
	}
	exec(t, cfg, "default", "DROP DATABASE IF EXISTS "+cfg.Database+" SYNC")
	t.Cleanup(func() { exec(t, cfg, "default", "DROP DATABASE IF EXISTS "+cfg.Database+" SYNC") })
	return cfg
}

// exec runs stmt against database on cfg's server.
func exec(t *testing.T, cfg clickhouse.Config, database, stmt string) {
	cfg.Database = database
	client, err := clickhouse.Open(cfg, clickhouse.Options{})
	require.NoError(t, err)
	defer client.Close()
	require.NoError(t, client.Do(context.Background(), ch.Query{Body: stmt}))
}

// query returns the single UInt64 column a of the rows that body selects.
func query(t *testing.T, cfg clickhouse.Config, body string) []uint64 {
	client, err := clickhouse.Open(cfg, clickhouse.Options{})
	require.NoError(t, err)
	defer client.Close()
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

// testWriter returns a writer of the archive in cfg, closed when the test ends.
func testWriter(t *testing.T, cfg clickhouse.Config) *writer {
	w, err := newWriter(cfg)
	require.NoError(t, err)
	t.Cleanup(w.close)
	return w
}

// rowCounts returns the row count of every archive table.
func rowCounts(t *testing.T, cfg clickhouse.Config) map[string]uint64 {
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
		block := testBlock(n)
		require.NoError(t, b.add(&block))
	}
	return b
}

func TestWrite(t *testing.T) {
	cfg := testConfig(t)
	require.NoError(t, migrate(t.Context(), cfg))
	w := testWriter(t, cfg)

	next, err := w.nextBlock(t.Context())
	require.NoError(t, err)
	require.Zero(t, next)

	first := testBatch(t, 1, 3)
	require.NoError(t, w.write(t.Context(), first))
	require.NoError(t, w.write(t.Context(), testBatch(t, 4, 5)))

	next, err = w.nextBlock(t.Context())
	require.NoError(t, err)
	require.Equal(t, uint64(6), next)
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
		tableCommits:        4, // a pending and a committed row per batch
	}, rowCounts(t, cfg))

	require.Equal(t, []uint64{1, 0}, query(t, cfg,
		"SELECT toUInt64(executed) AS a FROM transactions_receipts WHERE block_number = 2 ORDER BY transaction_index"))
	// A transaction is found by its hash prefix.
	executed := testBlock(2).Txs[0].Receipt.TxHash
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
	w := testWriter(t, cfg)
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

func TestFailedWriteLeavesItsBatchPending(t *testing.T) {
	cfg := testConfig(t)
	require.NoError(t, migrate(t.Context(), cfg))
	w := testWriter(t, cfg)

	exec(t, cfg, cfg.Database, "DETACH TABLE blocks")
	defer exec(t, cfg, cfg.Database, "ATTACH TABLE blocks")
	start := time.Now()
	require.Error(t, w.write(t.Context(), testBatch(t, 1, 3)))
	require.Less(t, time.Since(start), 10*time.Second, "a failed table does not stall the others")
	_, err := w.nextBlock(t.Context())
	require.ErrorIs(t, err, errInterruptedBatch, "a partly written batch is not committed")
}

func TestStartExporterRefusesAnInterruptedBatch(t *testing.T) {
	cfg := testConfig(t)
	require.NoError(t, migrate(t.Context(), cfg))
	w := testWriter(t, cfg)
	require.NoError(t, w.write(t.Context(), testBatch(t, 1, 2)))

	// Blocks 3-4 stop after their pending row and one table, as a crash part way leaves them.
	b := testBatch(t, 3, 4)
	require.NoError(t, w.insertCommit(t.Context(), b.first, b.last, false))
	require.NoError(t, w.client.Insert(t.Context(), tableBlocks, b.blockRows.input()))

	_, err := StartExporter(t.Context(), cfg)
	require.ErrorIs(t, err, errInterruptedBatch)
	require.ErrorContains(t, err, "blocks 3-4")
}

func TestExporterWritesExecutedBlocks(t *testing.T) {
	cfg := testConfig(t)
	exporter, err := StartExporter(t.Context(), cfg)
	require.NoError(t, err)
	store := func(n uint64) {
		block := testBlock(n)
		require.NoError(t, exporter.ArchiveBlock(t.Context(), &block))
	}
	for n := uint64(1); n <= 3; n++ {
		store(n)
	}
	require.NoError(t, exporter.Close())
	require.Equal(t, uint64(3), rowCounts(t, cfg)[tableBlocks])

	// A restarted exporter expects the block after the archived ones and refuses one it holds.
	exporter, err = StartExporter(t.Context(), cfg)
	require.NoError(t, err)
	require.Equal(t, uint64(3), exporter.LatestBlock())
	block := testBlock(3)
	require.ErrorContains(t, exporter.ArchiveBlock(t.Context(), &block), "expects block 4")
	store(4)
	// A block that skips one is refused rather than archived.
	block = testBlock(6)
	require.ErrorContains(t, exporter.ArchiveBlock(t.Context(), &block), "expects block 5")
	require.NoError(t, exporter.Close())
	require.Equal(t, uint64(4), exporter.LatestBlock())
	counts := rowCounts(t, cfg)
	require.Equal(t, uint64(4), counts[tableBlocks])
	require.Equal(t, uint64(8), counts[tableTxs])
	// The archive's start at block 1, then the batches 1-3 and 4.
	require.Equal(t, []uint64{1, 1, 4}, query(t, cfg, "SELECT start_block AS a FROM archive_commits WHERE committed ORDER BY end_block"))
	require.Equal(t, []uint64{0, 3, 4}, query(t, cfg, "SELECT end_block AS a FROM archive_commits WHERE committed ORDER BY end_block"))

	// A closed exporter refuses blocks rather than dropping them.
	block = testBlock(5)
	require.ErrorContains(t, exporter.ArchiveBlock(t.Context(), &block), "closed")
}

func TestExporterStopsAtAFailedWrite(t *testing.T) {
	cfg := testConfig(t)
	exporter, err := StartExporter(t.Context(), cfg)
	require.NoError(t, err)
	exec(t, cfg, cfg.Database, "DETACH TABLE blocks")
	block := testBlock(1)
	require.NoError(t, exporter.ArchiveBlock(t.Context(), &block))
	require.ErrorContains(t, exporter.Close(), "insert blocks")
	exec(t, cfg, cfg.Database, "ATTACH TABLE blocks")

	// The failed batch stays pending, so the archive does not resume until it is removed.
	_, err = StartExporter(t.Context(), cfg)
	require.ErrorIs(t, err, errInterruptedBatch)
}

func TestEmptyArchiveRecordsItsStart(t *testing.T) {
	cfg := testConfig(t)
	exporter, err := StartExporter(t.Context(), cfg)
	require.NoError(t, err)
	require.Zero(t, exporter.LatestBlock())
	block := testBlock(5)
	require.NoError(t, exporter.ArchiveBlock(t.Context(), &block))
	// Recorded before the block is accepted, so a restart that loses the batch holding block 5
	// still expects block 5.
	require.Equal(t, []uint64{4}, query(t, cfg, "SELECT end_block AS a FROM archive_commits WHERE committed"))
	require.Equal(t, uint64(4), exporter.LatestBlock())
	require.NoError(t, exporter.Close())

	// An archive that holds only its start, as one whose first batch was lost.
	exec(t, cfg, cfg.Database, "DELETE FROM archive_commits WHERE end_block >= 5")
	exporter, err = StartExporter(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = exporter.Close() })
	require.Equal(t, uint64(4), exporter.LatestBlock())
	skipping := testBlock(6)
	require.ErrorContains(t, exporter.ArchiveBlock(t.Context(), &skipping), "expects block 5")
	require.NoError(t, exporter.ArchiveBlock(t.Context(), &block))
}

func TestArchiveStartingAtBlockOneExpectsIt(t *testing.T) {
	cfg := testConfig(t)
	require.NoError(t, migrate(t.Context(), cfg))
	require.NoError(t, testWriter(t, cfg).insertCommit(t.Context(), 1, 0, true))
	exporter, err := StartExporter(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = exporter.Close() })
	// Nothing to roll back to, but the archive still refuses to start anywhere but block 1.
	require.Zero(t, exporter.LatestBlock())
	block := testBlock(2)
	require.ErrorContains(t, exporter.ArchiveBlock(t.Context(), &block), "expects block 1")
}
