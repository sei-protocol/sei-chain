package operations

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/hashlog"
	"github.com/stretchr/testify/require"
)

func hl(block uint64, version string, hashes map[string][]byte) *hashlog.HashLog {
	return &hashlog.HashLog{BlockNumber: block, Version: version, Hashes: hashes}
}

func TestRenderGetBlockTextSingle(t *testing.T) {
	var buf bytes.Buffer
	logs := []*hashlog.HashLog{hl(7, "v1", map[string][]byte{"root": {0xab}, "flatKV": {0xcd}})}
	require.NoError(t, renderGetBlock(&buf, 7, logs, false))

	out := buf.String()
	require.Contains(t, out, "block 7:")
	require.Contains(t, out, "version: v1")
	require.Contains(t, out, "flatKV: cd")
	require.Contains(t, out, "root: ab")
	require.NotContains(t, out, "record 1")
}

func TestRenderGetBlockTextMultipleRollback(t *testing.T) {
	var buf bytes.Buffer
	logs := []*hashlog.HashLog{
		hl(5, "v1", map[string][]byte{"root": {0x05}}),
		hl(5, "v1", map[string][]byte{"root": {0x99}}),
	}
	require.NoError(t, renderGetBlock(&buf, 5, logs, false))

	out := buf.String()
	require.Contains(t, out, "Block 5 has 2 records")
	require.Contains(t, out, "rollback")
	require.Contains(t, out, "record 1 (block 5):")
	require.Contains(t, out, "record 2 (block 5):")
	require.Contains(t, out, "root: 05")
	require.Contains(t, out, "root: 99")
}

func TestRenderGetBlockTextEmpty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, renderGetBlock(&buf, 42, nil, false))
	require.Contains(t, buf.String(), "No records for block 42.")
}

func TestRenderGetBlockTextNilHash(t *testing.T) {
	var buf bytes.Buffer
	logs := []*hashlog.HashLog{hl(1, "", map[string][]byte{"root": nil})}
	require.NoError(t, renderGetBlock(&buf, 1, logs, false))

	out := buf.String()
	require.Contains(t, out, "root: <none>")
	require.NotContains(t, out, "version:")
}

func TestRenderGetBlockJSON(t *testing.T) {
	var buf bytes.Buffer
	logs := []*hashlog.HashLog{hl(3, "v1", map[string][]byte{"root": {0x0a}, "flatKV": nil})}
	require.NoError(t, renderGetBlock(&buf, 3, logs, true))

	var got []hashLogJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Len(t, got, 1)
	require.Equal(t, uint64(3), got[0].BlockNumber)
	require.Equal(t, "v1", got[0].Version)
	require.NotNil(t, got[0].Hashes["root"])
	require.Equal(t, "0a", *got[0].Hashes["root"])
	// A nil hash must serialize to JSON null, distinguishable from an absent type.
	val, ok := got[0].Hashes["flatKV"]
	require.True(t, ok)
	require.Nil(t, val)
}

func TestRenderCompareTextIdentical(t *testing.T) {
	var buf bytes.Buffer
	result := compareResult{archiveA: "a", archiveB: "b", maxDiffs: -1}
	require.NoError(t, renderCompare(&buf, result, false))

	out := buf.String()
	require.Contains(t, out, "Comparing archive A (a) against archive B (b)")
	require.Contains(t, out, "Archives are identical over the compared range.")
}

func TestRenderCompareTextDiffs(t *testing.T) {
	var buf bytes.Buffer
	result := compareResult{
		archiveA: "a",
		archiveB: "b",
		ranged:   true,
		low:      1,
		high:     10,
		maxDiffs: 1,
		diffs: []*hashlog.HashLogPair{
			{
				// Side A has two records (rollback); side B has none for this block.
				HashesFromA: []*hashlog.HashLog{
					hl(4, "v1", map[string][]byte{"root": {0x04}}),
					hl(4, "v1", map[string][]byte{"root": {0x44}}),
				},
				HashesFromB: nil,
			},
		},
	}
	require.NoError(t, renderCompare(&buf, result, false))

	out := buf.String()
	require.Contains(t, out, "Restricted to blocks [1, 10]")
	require.Contains(t, out, "block 4 differs:")
	require.Contains(t, out, "archive A:")
	require.Contains(t, out, "record 1:")
	require.Contains(t, out, "record 2:")
	require.Contains(t, out, "archive B:")
	require.Contains(t, out, "<no records>")
	require.Contains(t, out, "1 differing block(s) reported.")
	// len(diffs) == maxDiffs, so the truncation warning must fire.
	require.Contains(t, out, "Output truncated at --max-diffs=1")
}

func TestRenderCompareJSON(t *testing.T) {
	var buf bytes.Buffer
	result := compareResult{
		archiveA: "a",
		archiveB: "b",
		maxDiffs: -1,
		diffs: []*hashlog.HashLogPair{
			{
				HashesFromA: []*hashlog.HashLog{hl(2, "v1", map[string][]byte{"root": {0x02}})},
				HashesFromB: []*hashlog.HashLog{hl(2, "v1", map[string][]byte{"root": {0xff}})},
			},
		},
	}
	require.NoError(t, renderCompare(&buf, result, true))

	var got []hashLogPairJSON
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))
	require.Len(t, got, 1)
	require.Equal(t, uint64(2), got[0].Block)
	require.Equal(t, "02", *got[0].HashesFromA[0].Hashes["root"])
	require.Equal(t, "ff", *got[0].HashesFromB[0].Hashes["root"])
}

// TestHashLogReadEndToEnd builds a real archive through the public hashlogger writer and reads it back through
// the same reader utility the CLI calls, exercising the full path the get-block/compare commands rely on.
func TestHashLogReadEndToEnd(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")

	// Archive A: blocks 1 and 2. Archive B: same, but block 2 deviates.
	writeHashArchive(t, dirA, "v1.2.3", map[uint64][]byte{1: {0x01}, 2: {0x02}})
	writeHashArchive(t, dirB, "v1.2.3", map[uint64][]byte{1: {0x01}, 2: {0xff}})

	logs, err := hashlog.ReadHashForBlock(dirA, 2)
	require.NoError(t, err)
	require.Len(t, logs, 1)

	var buf bytes.Buffer
	require.NoError(t, renderGetBlock(&buf, 2, logs, false))
	out := buf.String()
	require.Contains(t, out, "block 2:")
	require.Contains(t, out, "root: 02")
	require.Contains(t, out, "version: v1.2.3")

	diffs, err := hashlog.CompareHashes(dirA, dirB, -1)
	require.NoError(t, err)
	require.Len(t, diffs, 1)
	require.Equal(t, uint64(2), pairBlock(diffs[0]))
}

// writeHashArchive writes a "root" hash per block into a fresh archive directory using the public logger API,
// then closes it to flush everything to disk.
func writeHashArchive(t *testing.T, dir string, version string, blocks map[uint64][]byte) {
	t.Helper()
	cfg := hashlog.DefaultHashLoggerConfig(dir, version)
	cfg.HashTypes = []string{"root"}
	cfg.DisableChangesetHashing = true
	hashLogger, err := hashlog.NewHashLogger(cfg)
	require.NoError(t, err)
	for block, hash := range blocks {
		require.NoError(t, hashLogger.ReportHash(block, "root", hash))
	}
	require.NoError(t, hashLogger.Close())
}
