package bench

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/bench/wrappers"
)

func TestGenerateOpWriteSetShapes(t *testing.T) {
	const kpb, blocks = 4, 3

	t.Run("insert has no warmup", func(t *testing.T) {
		ws, warmup, err := GenerateOpWriteSet(OpSweepSpec{
			Op: OpInsert, KeyKind: WriteKindStorage, ValueBytes: 32,
			KeysPerBlock: kpb, TimedBlocks: blocks,
		})
		require.NoError(t, err)
		require.Equal(t, 0, warmup)
		require.Len(t, ws.Blocks, blocks)
		for _, blk := range ws.Blocks {
			require.Len(t, blk.Writes, kpb)
			for _, w := range blk.Writes {
				require.False(t, w.Delete)
			}
		}
	})

	t.Run("update seeds then overwrites the same keys", func(t *testing.T) {
		ws, warmup, err := GenerateOpWriteSet(OpSweepSpec{
			Op: OpUpdate, KeyKind: WriteKindStorage, ValueBytes: 32,
			KeysPerBlock: kpb, TimedBlocks: blocks,
		})
		require.NoError(t, err)
		require.Equal(t, 1, warmup)
		require.Len(t, ws.Blocks, blocks+1)
		require.Len(t, ws.Blocks[0].Writes, kpb*blocks, "warm-up seeds every timed key")

		// Every timed-block key must appear in the warm-up block (overwrite,
		// not insert).
		seeded := map[string]bool{}
		for _, w := range ws.Blocks[0].Writes {
			seeded[w.Address+"/"+w.Slot] = true
			require.False(t, w.Delete)
		}
		for _, blk := range ws.Blocks[1:] {
			for _, w := range blk.Writes {
				require.True(t, seeded[w.Address+"/"+w.Slot], "timed key must be pre-seeded")
				require.False(t, w.Delete)
			}
		}
	})

	t.Run("delete seeds then deletes", func(t *testing.T) {
		ws, warmup, err := GenerateOpWriteSet(OpSweepSpec{
			Op: OpDelete, KeyKind: WriteKindStorage, ValueBytes: 32,
			KeysPerBlock: kpb, TimedBlocks: blocks,
		})
		require.NoError(t, err)
		require.Equal(t, 1, warmup)
		for _, blk := range ws.Blocks[1:] {
			for _, w := range blk.Writes {
				require.True(t, w.Delete)
			}
		}
	})
}

func TestReplayWriteSetSampledSkipsWarmup(t *testing.T) {
	ws, warmup, err := GenerateOpWriteSet(OpSweepSpec{
		Op: OpUpdate, KeyKind: WriteKindStorage, ValueBytes: 32,
		KeysPerBlock: 8, TimedBlocks: 5,
	})
	require.NoError(t, err)

	for _, backend := range []wrappers.DBType{wrappers.MemIAVL, wrappers.FlatKV} {
		t.Run(string(backend), func(t *testing.T) {
			wrapper, err := OpenReplayWrapper(t.Context(), backend, t.TempDir())
			require.NoError(t, err)
			defer func() { require.NoError(t, wrapper.Close()) }()

			samples, err := ReplayWriteSetSampled(wrapper, ws, warmup)
			require.NoError(t, err)
			require.Len(t, samples.ApplyNs, 5, "one sample per timed block, warm-up excluded")
			require.Len(t, samples.CommitNs, 5)
			require.Equal(t, 8, samples.KeysPerBlock)
			for _, ns := range samples.CommitNs {
				require.Positive(t, ns)
			}
		})
	}
}

func TestPercentile(t *testing.T) {
	s := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	require.InDelta(t, 50, percentile(s, 0.5), 10)
	require.Equal(t, 100.0, percentile(s, 0.99))
	require.Equal(t, 10.0, percentile(s, 0.0))
	require.Zero(t, percentile(nil, 0.5))
}

// TestOpTypeSweep runs the full insert/update/delete grid on both backends and
// writes a CSV of per-key apply/commit percentiles. It is skipped unless
// OP_SWEEP_CSV names an output path, so it never runs in CI. Tune the grid with
// OP_SWEEP_KEYS_PER_BLOCK and OP_SWEEP_BLOCKS.
//
// This variant runs on an empty temp DB, so it measures the relative op
// asymmetry only. For mainnet-sized absolute cost, run it against a copy of a
// real data directory on the cluster (a follow-up wires OpenReplayWrapper to
// the real memiavl/flatkv paths); the empty-DB numbers understate memiavl cost
// because the tree is shallow.
func TestOpTypeSweep(t *testing.T) {
	csvPath := os.Getenv("OP_SWEEP_CSV")
	if csvPath == "" {
		t.Skip("set OP_SWEEP_CSV=/path/out.csv to run the op-type sweep")
	}
	keysPerBlock := envInt("OP_SWEEP_KEYS_PER_BLOCK", 100)
	blocks := envInt("OP_SWEEP_BLOCKS", 50)

	f, err := os.Create(csvPath)
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	w := csv.NewWriter(f)
	defer w.Flush()
	require.NoError(t, w.Write([]string{
		"backend", "op", "key_kind", "value_bytes", "keys_per_block", "timed_blocks",
		"apply_p50_ns_per_key", "apply_p99_ns_per_key",
		"commit_p50_ns_per_key", "commit_p99_ns_per_key",
	}))

	backends := []wrappers.DBType{wrappers.MemIAVL, wrappers.FlatKV}
	ops := []OpKind{OpInsert, OpUpdate, OpDelete}
	for _, backend := range backends {
		for _, op := range ops {
			spec := OpSweepSpec{
				Op: op, KeyKind: WriteKindStorage, ValueBytes: 32,
				KeysPerBlock: keysPerBlock, TimedBlocks: blocks,
			}
			ws, warmup, err := GenerateOpWriteSet(spec)
			require.NoError(t, err)

			wrapper, err := OpenReplayWrapper(t.Context(), backend, t.TempDir())
			require.NoError(t, err)
			samples, err := ReplayWriteSetSampled(wrapper, ws, warmup)
			require.NoError(t, wrapper.Close())
			require.NoError(t, err)

			kpb := float64(samples.KeysPerBlock)
			row := []string{
				string(backend), string(op), spec.KeyKind, strconv.Itoa(spec.ValueBytes),
				strconv.Itoa(keysPerBlock), strconv.Itoa(blocks),
				perKey(percentile(samples.ApplyNs, 0.5), kpb),
				perKey(percentile(samples.ApplyNs, 0.99), kpb),
				perKey(percentile(samples.CommitNs, 0.5), kpb),
				perKey(percentile(samples.CommitNs, 0.99), kpb),
			}
			require.NoError(t, w.Write(row))
			fmt.Printf("[OpSweep] %s %s: commit p50=%.0f ns/key p99=%.0f ns/key\n",
				backend, op,
				percentile(samples.CommitNs, 0.5)/kpb, percentile(samples.CommitNs, 0.99)/kpb)
		}
	}
	fmt.Printf("[OpSweep] wrote %s\n", csvPath)
}

func perKey(blockNs, keysPerBlock float64) string {
	if keysPerBlock == 0 {
		return "0"
	}
	return strconv.FormatFloat(blockNs/keysPerBlock, 'f', 2, 64)
}

func envInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}
