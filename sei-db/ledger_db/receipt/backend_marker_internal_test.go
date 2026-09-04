package receipt

import (
	"os"
	"path/filepath"
	"testing"

	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/stretchr/testify/require"
)

// openBackend builds a receipt store of the named backend in dir and closes it again, leaving whatever
// that backend puts on disk — including its ownership marker.
func openBackend(t *testing.T, dir string, backend string) {
	t.Helper()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = backend
	cfg.DBDirectory = dir
	store, err := NewReceiptStore(cfg, nil)
	require.NoError(t, err)
	require.NoError(t, store.Close())
}

// writeRawMarker replaces dir's marker with arbitrary contents, standing in for a marker written by a
// build that disagrees with this one.
func writeRawMarker(t *testing.T, dir string, contents string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(dir, receiptBackendMarkerFileName), []byte(contents), 0o600))
}

func TestBackendTypeRoundTrip(t *testing.T) {
	dir := t.TempDir()

	backend, found, err := readBackendType(dir)
	require.NoError(t, err)
	require.False(t, found, "an unmarked directory must report no owner rather than erroring")
	require.Empty(t, backend)

	require.NoError(t, recordBackendType(dir, receiptBackendLittIdx))

	backend, found, err = readBackendType(dir)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, receiptBackendLittIdx, backend)
}

// TestBackendTypeRecordCreatesDirectory verifies that recording a type works on a store directory that
// does not exist yet, which is the state of every freshly configured node.
func TestBackendTypeRecordCreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "receipt")
	require.NoError(t, recordBackendType(dir, receiptBackendPebble))

	backend, found, err := readBackendType(dir)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, receiptBackendPebble, backend)
}

func TestBackendTypeRecordIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, recordBackendType(dir, receiptBackendLittIdx))
	require.NoError(t, recordBackendType(dir, receiptBackendLittIdx))
}

// TestBackendTypeRecordRefusesAnotherOwner verifies that recording a mismatched type fails and leaves the
// existing marker in place, so the directory still reports its real owner afterwards.
func TestBackendTypeRecordRefusesAnotherOwner(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, recordBackendType(dir, receiptBackendPebble))

	err := recordBackendType(dir, receiptBackendLittIdx)
	require.ErrorContains(t, err, receiptBackendPebble)
	require.ErrorContains(t, err, receiptBackendLittIdx)

	backend, found, err := readBackendType(dir)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, receiptBackendPebble, backend)
}

func TestBackendTypeRequire(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, requireBackendType(dir, receiptBackendLittIdx), "an unmarked directory must pass")

	require.NoError(t, recordBackendType(dir, receiptBackendLittIdx))
	require.NoError(t, requireBackendType(dir, receiptBackendLittIdx))
	require.Error(t, requireBackendType(dir, receiptBackendPebble))
}

// TestBackendTypeRejectsUnreadableMarkers verifies that a marker this build cannot reason about is an
// error rather than being treated as absent, which would defeat the check.
func TestBackendTypeRejectsUnreadableMarkers(t *testing.T) {
	for name, contents := range map[string]string{
		"empty":            "",
		"one line":         "1",
		"three lines":      "1\nlittidx\nextra",
		"unparsed version": "v1\nlittidx",
		"future version":   "2\nlittidx",
		"unknown backend":  "1\nrocksdb",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeRawMarker(t, dir, contents)

			_, found, err := readBackendType(dir)
			require.Error(t, err)
			require.False(t, found)
			require.Error(t, requireBackendType(dir, receiptBackendLittIdx))
		})
	}
}

// TestNewReceiptStoreRefusesAnotherBackendsDirectory verifies that opening a store directory as the wrong
// backend fails loudly instead of presenting the operator with a silently empty store.
func TestNewReceiptStoreRefusesAnotherBackendsDirectory(t *testing.T) {
	littDir := t.TempDir()
	openBackend(t, littDir, receiptBackendLittIdx)
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = receiptBackendPebble
	cfg.DBDirectory = littDir
	_, err := NewReceiptStore(cfg, nil)
	require.ErrorContains(t, err, receiptBackendLittIdx)

	pebbleDir := t.TempDir()
	openBackend(t, pebbleDir, receiptBackendPebble)
	cfg = dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = receiptBackendLittIdx
	cfg.DBDirectory = pebbleDir
	_, err = NewReceiptStore(cfg, nil)
	require.ErrorContains(t, err, receiptBackendPebble)
}

// TestNewReceiptStoreAdoptsUnmarkedDirectory verifies the backwards-compatibility case: a store written
// before markers existed opens as configured, is stamped on the way through, and reopens afterwards.
func TestNewReceiptStoreAdoptsUnmarkedDirectory(t *testing.T) {
	dir := t.TempDir()
	openBackend(t, dir, receiptBackendLittIdx)
	require.NoError(t, os.Remove(filepath.Join(dir, receiptBackendMarkerFileName)))

	openBackend(t, dir, receiptBackendLittIdx)

	backend, found, err := readBackendType(dir)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, receiptBackendLittIdx, backend)

	openBackend(t, dir, receiptBackendLittIdx)
}

// TestNewReceiptStoreRejectsUnsupportedBackendBeforeRecording verifies that an unsupported backend is
// refused without stamping the directory, which would otherwise leave a marker no build can read.
func TestNewReceiptStoreRejectsUnsupportedBackendBeforeRecording(t *testing.T) {
	dir := t.TempDir()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = "rocksdb"
	cfg.DBDirectory = dir
	_, err := NewReceiptStore(cfg, nil)
	require.ErrorContains(t, err, "unsupported receipt store backend")

	_, found, err := readBackendType(dir)
	require.NoError(t, err)
	require.False(t, found, "a refused backend must not record a type")
}

// TestNewReceiptStoreRejectsBadConfigBeforeRecording verifies that a config the pebble backend refuses is
// rejected without stamping the directory, so correcting the config to littidx is not then refused by a
// marker left behind by the failed attempt.
func TestNewReceiptStoreRejectsBadConfigBeforeRecording(t *testing.T) {
	dir := t.TempDir()
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = receiptBackendPebble
	cfg.DBDirectory = dir
	cfg.ExternalPruning = true
	_, err := NewReceiptStore(cfg, nil)
	require.ErrorContains(t, err, "does not support external pruning")

	_, found, err := readBackendType(dir)
	require.NoError(t, err)
	require.False(t, found, "a rejected config must not record a type")

	cfg.Backend = receiptBackendLittIdx
	store, err := NewReceiptStore(cfg, nil)
	require.NoError(t, err, "the corrected config must not be refused by the failed attempt")
	require.NoError(t, store.Close())
}

// TestOfflineRefusesNonLittIdxStore verifies that the offline entry points refuse a store they cannot
// read, and refuse it before creating littidx's subdirectories inside it.
func TestOfflineRefusesNonLittIdxStore(t *testing.T) {
	dir := t.TempDir()
	openBackend(t, dir, receiptBackendPebble)

	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.DBDirectory = dir
	require.Equal(t, receiptBackendPebble, normalizeReceiptBackend(cfg.Backend), "default backend assumption")

	_, _, _, err := GetRange(cfg)
	require.ErrorContains(t, err, receiptBackendLittIdx)
	require.ErrorContains(t, PruneAfter(cfg, 0), receiptBackendLittIdx)

	// A littidx-configured caller pointed at this directory is refused by the marker rather than the
	// config, which is the mistake the config check cannot see.
	cfg.Backend = receiptBackendLittIdx
	_, _, _, err = GetRange(cfg)
	require.ErrorContains(t, err, receiptBackendPebble)
	require.ErrorContains(t, PruneAfter(cfg, 0), receiptBackendPebble)

	for _, subdirectory := range []string{littValuesDirName, littIndexDirName} {
		_, err := os.Stat(filepath.Join(dir, subdirectory))
		require.Truef(t, os.IsNotExist(err), "%s must not have been created in a refused store", subdirectory)
	}
}
