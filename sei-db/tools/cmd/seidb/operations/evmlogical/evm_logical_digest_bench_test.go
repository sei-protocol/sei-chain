package evmlogical

import (
	"bufio"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/common/keys"
)

// benchDigestRowsEnv overrides the number of storage rows in the benchmark kvs file.
const benchDigestRowsEnv = "SEI_EVM_DIGEST_BENCH_ROWS"

// writeBenchKVSFile writes a memiavl EVM snapshot kvs file of storage rows, one per slot of
// 1,000 contracts, in ascending key order, and returns its directory.
func writeBenchKVSFile(b *testing.B, rows int) string {
	b.Helper()
	dir := b.TempDir()
	f, err := os.Create(filepath.Join(dir, "kvs"))
	require.NoError(b, err)
	w := bufio.NewWriterSize(f, 1<<20)
	var lenbuf [4]byte
	writeField := func(field []byte) {
		binary.LittleEndian.PutUint32(lenbuf[:], uint32(len(field))) //nolint:gosec
		_, _ = w.Write(lenbuf[:])
		_, _ = w.Write(field)
	}
	const contracts = 1000
	perContract := (rows + contracts - 1) / contracts
	value := bytesOfLen(32, 0x5A)
	for c := 0; c < contracts && rows > 0; c++ {
		contract := make([]byte, keys.AddressLen)
		binary.BigEndian.PutUint32(contract[keys.AddressLen-4:], uint32(c)) //nolint:gosec
		for s := 0; s < perContract && rows > 0; s++ {
			slot := make([]byte, 32)
			binary.BigEndian.PutUint64(slot[24:], uint64(s)) //nolint:gosec
			writeField(keys.BuildEVMKey(keys.EVMKeyStorage, append(append([]byte{}, contract...), slot...)))
			writeField(value)
			rows--
		}
	}
	require.NoError(b, w.Flush())
	require.NoError(b, f.Close())
	return dir
}

func benchDigestRows(b *testing.B) int {
	b.Helper()
	rows := 1_000_000
	if v := os.Getenv(benchDigestRowsEnv); v != "" {
		n, err := strconv.Atoi(v)
		require.NoError(b, err, benchDigestRowsEnv)
		rows = n
	}
	return rows
}

// BenchmarkMemiavlSemanticDigestSnapshotScan measures the snapshot kvs scan and the semantic
// digest of its rows.
func BenchmarkMemiavlSemanticDigestSnapshotScan(b *testing.B) {
	rows := benchDigestRows(b)
	dir := writeBenchKVSFile(b, rows)
	saved := digestOut
	b.Cleanup(func() { digestOut = saved })
	digestOut = digestSink{prose: io.Discard, jsonReport: io.Discard}
	kvs, err := openMemiavlSnapshotKVs(dir)
	require.NoError(b, err)
	b.Cleanup(func() { _ = kvs.Close() })
	scan := func(fn func(rawKey, rawVal []byte) error) error { return scanMemiavlSnapshotEVMLeaves(kvs, fn) }

	b.ReportAllocs()
	for b.Loop() {
		require.NoError(b, runMemiavlSemanticDigest(digestPrintContext{}, "bench", "bench total leaves", nil, scan))
	}
	b.ReportMetric(float64(rows)*float64(b.N)/b.Elapsed().Seconds(), "rows/s")
}
