package walrus

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// patchFile rewrites bytes of a file in place.
func patchFile(t *testing.T, path string, offset int, patch []byte) {
	t.Helper()

	contents, err := os.ReadFile(path) //nolint:gosec // the path is a test temporary directory
	require.NoError(t, err)
	require.LessOrEqual(t, offset+len(patch), len(contents), "the patch runs past the end of the file")
	copy(contents[offset:], patch)
	require.NoError(t, os.WriteFile(path, contents, 0o600))
}

// buildIndexedPod writes a pod holding one distinct key and returns its directory.
//
// One key makes the index's shape known: the first entry of the hash index, the first pointer, and the first
// key record all describe it, so a test can damage a named byte without first working out where the key
// landed under the pod's salt.
func buildIndexedPod(t *testing.T) (podDirectory string, hashPath string, versionPath string) {
	t.Helper()

	directory := t.TempDir()
	blocks := []Block{
		testBlock(1, testPair("alpha", "one", false)),
		testBlock(2, testPair("alpha", "two", false)),
		testBlock(3, testPair("alpha", "three", false)),
	}
	pod, err := newPodBuilder(directory, DefaultConfig(directory, "test", "evm")).Build(blocks)
	require.NoError(t, err)

	return pod.Directory,
		filepath.Join(pod.Directory, podHashIndexFileName),
		filepath.Join(pod.Directory, podVersionIndexFileName)
}

// versionRecordsStart reads a version index's header off disk and reports where the key records begin.
//
// The header is parsed from the bytes rather than by opening the index, so that a test about to rewrite the
// file is not also holding it open.
func versionRecordsStart(t *testing.T, path string) int {
	t.Helper()

	contents, err := os.ReadFile(path) //nolint:gosec // the path is a test temporary directory
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(contents), podVersionIndexHeaderSize)
	keyCount := binary.BigEndian.Uint64(contents[9:17])
	return podVersionIndexHeaderSize + int(keyCount)*indexPointerSize
}

// TestPodIndexRejectsCorruption checks that a damaged index fails rather than faulting or inventing an
// answer.
//
// The hash index is memory mapped, so a header that misdescribes it would have a search slicing past the
// mapping, which faults the process rather than returning an error. The version index is read rather than
// mapped, but every length it supplies still sizes a slice of what was read.
func TestPodIndexRejectsCorruption(t *testing.T) {
	// A pointer addressing a record past the end of the file.
	t.Run("record pointer out of range", func(t *testing.T) {
		podDirectory, _, versionPath := buildIndexedPod(t)

		patchFile(t, versionPath, podVersionIndexHeaderSize, binary.BigEndian.AppendUint64(nil, 1<<40))
		requireIndexRefuses(t, podDirectory, "alpha")
	})

	// A key length that claims more bytes than the record holds.
	t.Run("key length out of range", func(t *testing.T) {
		podDirectory, _, versionPath := buildIndexedPod(t)

		patchFile(t, versionPath, versionRecordsStart(t, versionPath),
			binary.BigEndian.AppendUint16(nil, 0xFFFF))
		requireIndexRefuses(t, podDirectory, "alpha")
	})

	// A version count that claims more versions than the file holds.
	t.Run("version count out of range", func(t *testing.T) {
		podDirectory, _, versionPath := buildIndexedPod(t)

		// The version count follows the key, whose length prefix says how long it is.
		recordsStart := versionRecordsStart(t, versionPath)
		contents, err := os.ReadFile(versionPath) //nolint:gosec // the path is a test temporary directory
		require.NoError(t, err)
		keyLength := int(binary.BigEndian.Uint16(contents[recordsStart:]))
		patchFile(t, versionPath, recordsStart+indexRecordKeyLengthSize+keyLength,
			binary.BigEndian.AppendUint32(nil, 1<<30))
		requireIndexRefuses(t, podDirectory, "alpha")
	})

	// A hash index header whose key count disagrees with the entries the file holds.
	t.Run("hash index key count disagrees", func(t *testing.T) {
		podDirectory, hashPath, _ := buildIndexedPod(t)

		patchFile(t, hashPath, 25, binary.BigEndian.AppendUint64(nil, 999_999))
		_, err := openPodIndex(podDirectory)
		require.ErrorContains(t, err, "bytes for")
	})

	// A version index header whose records offset disagrees with its key count.
	t.Run("version index records offset disagrees", func(t *testing.T) {
		podDirectory, _, versionPath := buildIndexedPod(t)

		patchFile(t, versionPath, 17, binary.BigEndian.AppendUint64(nil, 999_999))
		_, err := openPodIndex(podDirectory)
		require.ErrorContains(t, err, "its records at")
	})

	// A file too short to hold a header at all.
	t.Run("truncated", func(t *testing.T) {
		podDirectory, hashPath, _ := buildIndexedPod(t)

		require.NoError(t, os.Truncate(hashPath, 12))
		_, err := openPodIndex(podDirectory)
		require.ErrorContains(t, err, "truncated")
	})

	// Bytes that are not a hash index at all.
	t.Run("bad hash index magic", func(t *testing.T) {
		podDirectory, hashPath, _ := buildIndexedPod(t)

		patchFile(t, hashPath, 0, []byte("NOTAHASH"))
		_, err := openPodIndex(podDirectory)
		require.ErrorContains(t, err, "bad magic")
	})

	// Bytes that are not a version index at all.
	t.Run("bad version index magic", func(t *testing.T) {
		podDirectory, _, versionPath := buildIndexedPod(t)

		patchFile(t, versionPath, 0, []byte("NOTAVERS"))
		_, err := openPodIndex(podDirectory)
		require.ErrorContains(t, err, "bad magic")
	})
}

// requireIndexRefuses opens a damaged index and checks that searching it returns an error rather than
// faulting or inventing an answer.
func requireIndexRefuses(t *testing.T, podDirectory string, key string) {
	t.Helper()

	index, err := openPodIndex(podDirectory)
	if err != nil {
		return // Refused at open, which is just as good.
	}

	require.NotPanics(t, func() {
		_, _, _, _, err = index.FindNewest([]byte(key), 0, 100)
	})
	require.Error(t, err, "a corrupt index should report an error rather than answer")
}
