package bench

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

// BenchmarkWriteSetReplay replays a captured write-set file against both
// storage backends, timing ApplyChangeSets and Commit separately.
//
// Inputs (environment variables):
//
//	WRITESET_PATH  path to a write-set JSON file (see writeset.go), or a raw
//	               prestateTracer diffMode result / JSON-RPC response, which
//	               is converted on the fly (TRACE_PATH takes precedence).
//	TRACE_PATH     path to a prestateTracer diffMode JSON file to convert.
//	SNAPSHOT_PATH  optional state sync snapshot chunks directory imported
//	               before the timed region (same as the other benchmarks).
//
// Example:
//
//	TRACE_PATH=/tmp/sstore_trace.json go test ./sei-db/state_db/bench \
//	  -run '^$' -bench '^BenchmarkWriteSetReplay$' -benchtime=5x
func BenchmarkWriteSetReplay(b *testing.B) {
	ws := loadBenchWriteSet(b)

	for _, backend := range []wrappers.DBType{wrappers.MemIAVL, wrappers.FlatKV} {
		b.Run(string(backend), func(b *testing.B) {
			for range b.N {
				runWriteSetReplay(b, backend, ws)
			}
		})
	}
}

func loadBenchWriteSet(b *testing.B) *WriteSet {
	if tracePath := os.Getenv("TRACE_PATH"); tracePath != "" {
		converted, err := ConvertPrestateDiffFile(tracePath)
		require.NoError(b, err)
		if converted.SkippedBalanceChanges > 0 {
			b.Logf("skipped %d balance change(s): bank-module replay is out of scope",
				converted.SkippedBalanceChanges)
		}
		return converted.WriteSet
	}
	if wsPath := os.Getenv("WRITESET_PATH"); wsPath != "" {
		ws, err := LoadWriteSet(wsPath)
		require.NoError(b, err)
		return ws
	}
	b.Skip("set TRACE_PATH or WRITESET_PATH to run the write-set replay benchmark")
	return nil
}

func runWriteSetReplay(b *testing.B, backend wrappers.DBType, ws *WriteSet) {
	b.StopTimer()
	dbDir := b.TempDir()
	wrapper, err := OpenReplayWrapper(b.Context(), backend, dbDir)
	require.NoError(b, err)
	defer func() {
		require.NoError(b, wrapper.Close())
	}()

	if snapshotPath := os.Getenv("SNAPSHOT_PATH"); snapshotPath != "" {
		snapshotHeight, err := parseSnapshotHeight(snapshotPath)
		require.NoError(b, err)
		importer, err := wrapper.Importer(snapshotHeight)
		require.NoError(b, err)
		require.NoError(b, importSnapshot(snapshotPath, importer))
		require.NoError(b, wrapper.LoadVersion(0))
	}

	b.StartTimer()
	result, err := ReplayWriteSet(wrapper, ws)
	b.StopTimer()
	require.NoError(b, err)

	keys := float64(result.Keys)
	if keys == 0 {
		return
	}
	b.ReportMetric(result.ApplyDuration.Seconds()/keys*1e9, "apply_ns/key")
	b.ReportMetric(result.CommitDuration.Seconds()/keys*1e9, "commit_ns/key")
	fmt.Printf("[Replay %s] blocks=%d keys=%d apply=%s commit=%s\n",
		backend, result.Blocks, result.Keys, result.ApplyDuration, result.CommitDuration)
}
