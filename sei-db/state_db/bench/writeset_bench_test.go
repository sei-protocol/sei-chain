package bench

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

// BenchmarkWriteSetReplay replays a captured write-set file against both
// storage backends, timing ApplyChangeSets and Commit separately.
//
// Inputs (environment variables):
//
//	TRACE_PATH     path to a prestateTracer diffMode JSON file (the raw
//	               {"pre","post"} result or a whole JSON-RPC response),
//	               converted on the fly; takes precedence over WRITESET_PATH.
//	WRITESET_PATH  path to a write-set JSON file (see writeset.go). Raw
//	               tracer output is not accepted here; use TRACE_PATH.
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
			// Accumulate across iterations and report once: b.ReportMetric keeps
			// only the last value for a given unit, so reporting inside the loop
			// would surface a single iteration instead of an average over b.N.
			var totalApply, totalCommit time.Duration
			var totalKeys int
			for range b.N {
				result := runWriteSetReplay(b, backend, ws)
				totalApply += result.ApplyDuration
				totalCommit += result.CommitDuration
				totalKeys += result.Keys
			}
			if totalKeys == 0 {
				return
			}
			keys := float64(totalKeys)
			b.ReportMetric(totalApply.Seconds()/keys*1e9, "apply_ns/key")
			b.ReportMetric(totalCommit.Seconds()/keys*1e9, "commit_ns/key")
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

func runWriteSetReplay(b *testing.B, backend wrappers.DBType, ws *WriteSet) ReplayResult {
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

	fmt.Printf("[Replay %s] blocks=%d keys=%d apply=%s commit=%s\n",
		backend, result.Blocks, result.Keys, result.ApplyDuration, result.CommitDuration)
	return result
}
