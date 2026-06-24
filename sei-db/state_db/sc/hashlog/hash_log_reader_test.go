package hashlog

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeArchive writes a set of logs into a single sealed file in dir, creating dir if needed.
func writeArchive(t *testing.T, dir string, index uint64, version string, hashTypes []string, logs []*HashLog) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file, err := newHashLogFile(dir, index, version, hashTypes)
	require.NoError(t, err)
	for _, log := range logs {
		require.NoError(t, file.write(log))
	}
	require.NoError(t, file.close())
}

func log(block uint64, hashes map[string][]byte) *HashLog {
	return &HashLog{BlockNumber: block, Hashes: hashes}
}

func TestReadHashForBlockMultipleAfterRollback(t *testing.T) {
	dir := t.TempDir()
	hashTypes := []string{"root"}
	// First timeline: blocks 1-2.
	writeArchive(t, dir, 0, "v1", hashTypes, []*HashLog{
		log(1, map[string][]byte{"root": {0x01}}),
		log(2, map[string][]byte{"root": {0x02}}),
	})
	// Second timeline (rollback): block 1 re-executed with a different hash.
	writeArchive(t, dir, 1, "v1", hashTypes, []*HashLog{
		log(1, map[string][]byte{"root": {0x99}}),
	})

	results, err := ReadHashForBlock(dir, 1)
	require.NoError(t, err)
	require.Len(t, results, 2)
	require.Equal(t, []byte{0x01}, results[0].Hashes["root"])
	require.Equal(t, []byte{0x99}, results[1].Hashes["root"])

	none, err := ReadHashForBlock(dir, 99)
	require.NoError(t, err)
	require.Empty(t, none)
}

func TestCompareHashesFindsDeviations(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")
	hashTypes := []string{"root"}

	writeArchive(t, dirA, 0, "v1", hashTypes, []*HashLog{
		log(1, map[string][]byte{"root": {0x01}}),
		log(2, map[string][]byte{"root": {0x02}}),
		log(3, map[string][]byte{"root": {0x03}}),
	})
	writeArchive(t, dirB, 0, "v1", hashTypes, []*HashLog{
		log(1, map[string][]byte{"root": {0x01}}),
		log(2, map[string][]byte{"root": {0xFF}}), // deviates
		log(3, map[string][]byte{"root": {0x03}}),
	})

	diffs, err := CompareHashes(dirA, dirB, -1)
	require.NoError(t, err)
	require.Len(t, diffs, 1)
	require.Equal(t, uint64(2), diffs[0].HashesFromA[0].BlockNumber)
	require.Equal(t, []byte{0x02}, diffs[0].HashesFromA[0].Hashes["root"])
	require.Equal(t, []byte{0xFF}, diffs[0].HashesFromB[0].Hashes["root"])
}

func TestCompareHashesIdentical(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")
	hashTypes := []string{"root"}
	logs := []*HashLog{
		log(1, map[string][]byte{"root": {0x01}}),
		log(2, map[string][]byte{"root": {0x02}}),
	}
	writeArchive(t, dirA, 0, "v1", hashTypes, logs)
	writeArchive(t, dirB, 0, "v1", hashTypes, logs)

	diffs, err := CompareHashes(dirA, dirB, -1)
	require.NoError(t, err)
	require.Empty(t, diffs)
}

func TestCompareHashesRespectsMaxDiffCount(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")
	hashTypes := []string{"root"}

	writeArchive(t, dirA, 0, "v1", hashTypes, []*HashLog{
		log(1, map[string][]byte{"root": {0x01}}),
		log(2, map[string][]byte{"root": {0x02}}),
		log(3, map[string][]byte{"root": {0x03}}),
	})
	writeArchive(t, dirB, 0, "v1", hashTypes, []*HashLog{
		log(1, map[string][]byte{"root": {0xA1}}),
		log(2, map[string][]byte{"root": {0xA2}}),
		log(3, map[string][]byte{"root": {0xA3}}),
	})

	diffs, err := CompareHashes(dirA, dirB, 2)
	require.NoError(t, err)
	require.Len(t, diffs, 2, "should stop at maxDiffCount")
	// Returned lowest-first.
	require.Equal(t, uint64(1), diffs[0].HashesFromA[0].BlockNumber)
	require.Equal(t, uint64(2), diffs[1].HashesFromA[0].BlockNumber)
}

func TestCompareHashesStreamsAcrossManyFiles(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")
	hashTypes := []string{"root"}

	// Spread blocks 1..30 across many single-block files in each archive, identical except block 17 in B.
	for block := uint64(1); block <= 30; block++ {
		valueA := []byte{byte(block)}
		valueB := []byte{byte(block)}
		if block == 17 {
			valueB = []byte{0xFF}
		}
		writeArchive(t, dirA, block, "v1", hashTypes,
			[]*HashLog{log(block, map[string][]byte{"root": valueA})})
		writeArchive(t, dirB, block, "v1", hashTypes,
			[]*HashLog{log(block, map[string][]byte{"root": valueB})})
	}

	diffs, err := CompareHashes(dirA, dirB, -1)
	require.NoError(t, err)
	require.Len(t, diffs, 1)
	require.Equal(t, uint64(17), diffs[0].HashesFromA[0].BlockNumber)
}

func TestCompareHashesOverlappingRollbackFile(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")
	hashTypes := []string{"root"}

	// Archive A: a base file covering blocks 1..10, plus a later rollback file re-covering blocks 5..7.
	// Both files are simultaneously "active" while the cursor is in [5,7], exercising overlap handling.
	writeArchive(t, dirA, 0, "v1", hashTypes, []*HashLog{
		log(5, map[string][]byte{"root": {0x05}}),
		log(6, map[string][]byte{"root": {0x06}}),
		log(7, map[string][]byte{"root": {0x07}}),
	})
	writeArchive(t, dirA, 1, "v1", hashTypes, []*HashLog{
		log(5, map[string][]byte{"root": {0x55}}),
		log(6, map[string][]byte{"root": {0x66}}),
	})

	// Archive B records block 5 only once, so block 5 has a different number of occurrences -> a deviation.
	writeArchive(t, dirB, 0, "v1", hashTypes, []*HashLog{
		log(5, map[string][]byte{"root": {0x05}}),
		log(6, map[string][]byte{"root": {0x06}}),
		log(7, map[string][]byte{"root": {0x07}}),
	})
	writeArchive(t, dirB, 1, "v1", hashTypes, []*HashLog{
		log(6, map[string][]byte{"root": {0x66}}),
	})

	diffs, err := CompareHashes(dirA, dirB, -1)
	require.NoError(t, err)
	require.Len(t, diffs, 1)
	require.Equal(t, uint64(5), diffs[0].HashesFromA[0].BlockNumber)
	require.Len(t, diffs[0].HashesFromA, 2, "block 5 appears in both of A's files")
	require.Len(t, diffs[0].HashesFromB, 1, "block 5 appears in one of B's files")
}

func TestCompareHashesInRangeRestrictsToWindow(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")
	hashTypes := []string{"root"}

	// Blocks 1..30 in single-block files, identical except deviations at 5, 17, and 25.
	deviant := map[uint64]bool{5: true, 17: true, 25: true}
	for block := uint64(1); block <= 30; block++ {
		valueB := []byte{byte(block)}
		if deviant[block] {
			valueB = []byte{0xFF}
		}
		writeArchive(t, dirA, block, "v1", hashTypes,
			[]*HashLog{log(block, map[string][]byte{"root": {byte(block)}})})
		writeArchive(t, dirB, block, "v1", hashTypes,
			[]*HashLog{log(block, map[string][]byte{"root": valueB})})
	}

	// Zooming into [10, 20] must surface only the block-17 deviation.
	diffs, err := CompareHashesInRange(dirA, dirB, 10, 20, -1)
	require.NoError(t, err)
	require.Len(t, diffs, 1)
	require.Equal(t, uint64(17), diffs[0].HashesFromA[0].BlockNumber)

	// The full comparison still finds all three, confirming the window is what narrowed the result.
	all, err := CompareHashes(dirA, dirB, -1)
	require.NoError(t, err)
	require.Len(t, all, 3)
}

func TestCompareHashesInRangeClampsAndValidates(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")
	hashTypes := []string{"root"}
	writeArchive(t, dirA, 0, "v1", hashTypes, []*HashLog{
		log(5, map[string][]byte{"root": {0x05}}),
		log(6, map[string][]byte{"root": {0x06}}),
	})
	writeArchive(t, dirB, 0, "v1", hashTypes, []*HashLog{
		log(5, map[string][]byte{"root": {0x05}}),
		log(6, map[string][]byte{"root": {0xFF}}), // block 6 deviates
	})

	// A window wider than the data is clamped to what's present and still finds the deviation.
	diffs, err := CompareHashesInRange(dirA, dirB, 0, 1_000_000, -1)
	require.NoError(t, err)
	require.Len(t, diffs, 1)
	require.Equal(t, uint64(6), diffs[0].HashesFromA[0].BlockNumber)

	// A window entirely outside the data yields nothing.
	none, err := CompareHashesInRange(dirA, dirB, 100, 200, -1)
	require.NoError(t, err)
	require.Empty(t, none)

	// An inverted range is rejected.
	_, err = CompareHashesInRange(dirA, dirB, 10, 5, -1)
	require.Error(t, err)
}

// TestArchiveReaderDoesNotAliasRowsInMultiRowFile guards against a per-iteration pointer-aliasing
// bug in loadFile. A single sealed file holds three distinct rows; each block must read back its own
// block number and hash. If loadFile stored one shared *HashLog reused across loop iterations, every
// block would read back as the final row (block 3 / {0x03}), failing the first assertion.
func TestArchiveReaderDoesNotAliasRowsInMultiRowFile(t *testing.T) {
	dir := t.TempDir()
	hashTypes := []string{"root"}
	writeArchive(t, dir, 0, "v1", hashTypes, []*HashLog{
		log(1, map[string][]byte{"root": {0x01}}),
		log(2, map[string][]byte{"root": {0x02}}),
		log(3, map[string][]byte{"root": {0x03}}),
	})

	r, err := newArchiveReader(dir)
	require.NoError(t, err)

	// at requires non-decreasing block numbers across calls; 1,2,3 satisfies that.
	for block := uint64(1); block <= 3; block++ {
		got, err := r.at(block)
		require.NoError(t, err)
		require.Len(t, got, 1)
		require.Equal(t, block, got[0].BlockNumber,
			"block %d read back as block %d (rows aliased to the final row?)", block, got[0].BlockNumber)
		require.Equal(t, []byte{byte(block)}, got[0].Hashes["root"],
			"block %d returned the wrong hash (rows aliased to the final row?)", block)
	}
}

func TestCompareHashesDifferentTypeSets(t *testing.T) {
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")

	// Archive A records only "root"; archive B records "root" and "flatKV".
	writeArchive(t, dirA, 0, "v1", []string{"root"}, []*HashLog{
		log(1, map[string][]byte{"root": {0x01}}),
	})
	writeArchive(t, dirB, 0, "v1", []string{"root", "flatKV"}, []*HashLog{
		log(1, map[string][]byte{"root": {0x01}, "flatKV": {0x07}}),
	})

	// The extra "flatKV" hash on side B (absent on A) counts as a deviation.
	diffs, err := CompareHashes(dirA, dirB, -1)
	require.NoError(t, err)
	require.Len(t, diffs, 1)
}
