package receipt

import (
	"os"
	"path/filepath"
	"testing"

	dbconfig "github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/stretchr/testify/require"
)

// foreignBackend names a backend this package does not open, standing in for a directory another store
// owns.
const foreignBackend = "littidx"

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

	require.NoError(t, recordBackendType(dir, receiptBackendPebble))

	backend, found, err = readBackendType(dir)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, receiptBackendPebble, backend)
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
	require.NoError(t, recordBackendType(dir, receiptBackendPebble))
	require.NoError(t, recordBackendType(dir, receiptBackendPebble))
}

// TestBackendTypeRecordRefusesAnotherOwner verifies that recording a mismatched type fails and leaves the
// existing marker in place, so the directory still reports its real owner afterwards.
func TestBackendTypeRecordRefusesAnotherOwner(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, recordBackendType(dir, receiptBackendPebble))

	err := recordBackendType(dir, foreignBackend)
	require.ErrorContains(t, err, receiptBackendPebble)
	require.ErrorContains(t, err, foreignBackend)

	backend, found, err := readBackendType(dir)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, receiptBackendPebble, backend)
}

func TestBackendTypeRequire(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, requireBackendType(dir, receiptBackendPebble), "an unmarked directory must pass")

	require.NoError(t, recordBackendType(dir, receiptBackendPebble))
	require.NoError(t, requireBackendType(dir, receiptBackendPebble))
	require.Error(t, requireBackendType(dir, foreignBackend))
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
			require.Error(t, requireBackendType(dir, receiptBackendPebble))
		})
	}
}

// TestNewReceiptStoreRefusesAnotherBackendsDirectory verifies that opening a store directory as the wrong
// backend fails loudly instead of presenting the operator with a silently empty store.
func TestNewReceiptStoreRefusesAnotherBackendsDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, recordBackendType(dir, foreignBackend))
	cfg := dbconfig.DefaultReceiptStoreConfig()
	cfg.Backend = receiptBackendPebble
	cfg.DBDirectory = dir
	_, err := NewReceiptStore(cfg, nil)
	require.ErrorContains(t, err, foreignBackend)
}

// TestNewReceiptStoreAdoptsUnmarkedDirectory verifies the backwards-compatibility case: a store written
// before markers existed opens as configured, is stamped on the way through, and reopens afterwards.
func TestNewReceiptStoreAdoptsUnmarkedDirectory(t *testing.T) {
	dir := t.TempDir()
	openBackend(t, dir, receiptBackendPebble)
	require.NoError(t, os.Remove(filepath.Join(dir, receiptBackendMarkerFileName)))

	openBackend(t, dir, receiptBackendPebble)

	backend, found, err := readBackendType(dir)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, receiptBackendPebble, backend)

	openBackend(t, dir, receiptBackendPebble)
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
// rejected without stamping the directory, so the corrected config is not then refused by a marker left
// behind by the failed attempt.
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

	cfg.ExternalPruning = false
	store, err := NewReceiptStore(cfg, nil)
	require.NoError(t, err, "the corrected config must not be refused by the failed attempt")
	require.NoError(t, store.Close())
}
