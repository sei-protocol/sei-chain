package walrus

import (
	"encoding/binary"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// indexRecordsStart reads a pod index's header off disk and reports where the key records begin.
//
// The header is parsed from the bytes rather than by opening the index, because opening it maps the file and
// a test that is about to rewrite that file should not be holding a mapping over it.
func indexRecordsStart(t *testing.T, path string) int {
	t.Helper()

	contents, err := os.ReadFile(path) //nolint:gosec // the path is a test temporary directory
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(contents), podIndexHeaderSize)
	keyCount := binary.BigEndian.Uint64(contents[25:33])
	return podIndexHeaderSize + int(keyCount)*indexSlotSize
}

// patchIndex rewrites bytes of a pod's index file in place.
func patchIndex(t *testing.T, path string, offset int, patch []byte) {
	t.Helper()

	contents, err := os.ReadFile(path) //nolint:gosec // the path is a test temporary directory
	require.NoError(t, err)
	require.LessOrEqual(t, offset+len(patch), len(contents), "the patch runs past the end of the file")
	copy(contents[offset:], patch)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
}

// buildIndexedPod writes a small pod and returns the path of its index.
func buildIndexedPod(t *testing.T) (directory string, indexPath string) {
	t.Helper()

	directory = t.TempDir()
	blocks := []Block{
		testBlock(1, testPair("alpha", "one", false)),
		testBlock(2, testPair("bravo", "two", false)),
		testBlock(3, testPair("alpha", "three", false)),
	}
	pod, err := newPodBuilder(directory, DefaultConfig(directory, "test", "evm")).Build(blocks)
	require.NoError(t, err)
	return directory, pod.Info.IndexPath(directory)
}

// TestPodIndexRejectsCorruption checks that a damaged index fails rather than faulting.
//
// The index is memory mapped, so every length and offset the file supplies is used to slice that mapping. A
// value the file made up would walk off the end, which faults the process instead of returning an error, so
// the bounds checks are what stand between a corrupt file and a crash.
func TestPodIndexRejectsCorruption(t *testing.T) {
	// A slot pointing past the end of the key records.
	t.Run("record offset out of range", func(t *testing.T) {
		_, indexPath := buildIndexedPod(t)

		// The slots begin after the header; a slot's record offset is its last four bytes.
		patchIndex(t, indexPath, podIndexHeaderSize+8, binary.BigEndian.AppendUint32(nil, 1<<30))
		requireIndexRefuses(t, indexPath, "alpha")
	})

	// A key length that claims more bytes than the record holds.
	t.Run("key length out of range", func(t *testing.T) {
		_, indexPath := buildIndexedPod(t)

		recordsStart := indexRecordsStart(t, indexPath)
		patchIndex(t, indexPath, recordsStart, binary.BigEndian.AppendUint16(nil, 0xFFFF))
		requireIndexRefuses(t, indexPath, "alpha")
	})

	// A version count that claims more versions than the file holds.
	t.Run("version count out of range", func(t *testing.T) {
		_, indexPath := buildIndexedPod(t)

		recordsStart := indexRecordsStart(t, indexPath)

		// The version count follows the key, whose length prefix says how long it is.
		contents, err := os.ReadFile(indexPath) //nolint:gosec // the path is a test temporary directory
		require.NoError(t, err)
		keyLength := int(binary.BigEndian.Uint16(contents[recordsStart:]))
		patchIndex(t, indexPath, recordsStart+2+keyLength, binary.BigEndian.AppendUint32(nil, 1<<30))
		requireIndexRefuses(t, indexPath, "alpha")
	})

	// A header whose records offset disagrees with its key count.
	t.Run("header records offset disagrees", func(t *testing.T) {
		_, indexPath := buildIndexedPod(t)

		patchIndex(t, indexPath, 33, binary.BigEndian.AppendUint64(nil, 999_999))
		_, err := openPodIndex(indexPath)
		require.ErrorContains(t, err, "its records at")
	})

	// A file too short to hold a header at all.
	t.Run("truncated", func(t *testing.T) {
		_, indexPath := buildIndexedPod(t)

		require.NoError(t, os.Truncate(indexPath, 12))
		_, err := openPodIndex(indexPath)
		require.ErrorContains(t, err, "truncated")
	})

	// Bytes that are not an index at all.
	t.Run("bad magic", func(t *testing.T) {
		_, indexPath := buildIndexedPod(t)

		patchIndex(t, indexPath, 0, []byte("NOTANIDX"))
		_, err := openPodIndex(indexPath)
		require.ErrorContains(t, err, "bad magic")
	})
}

// requireIndexRefuses opens a damaged index and checks that searching it returns an error rather than
// faulting or inventing an answer.
func requireIndexRefuses(t *testing.T, indexPath string, key string) {
	t.Helper()

	index, err := openPodIndex(indexPath)
	if err != nil {
		return // Refused at open, which is just as good.
	}

	require.NotPanics(t, func() {
		_, _, _, _, err = index.FindNewest([]byte(key), 0, 100)
	})
	require.Error(t, err, "a corrupt index should report an error rather than answer")
}
