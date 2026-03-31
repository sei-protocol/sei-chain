package utils

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathExists(t *testing.T) {
	dir := t.TempDir()
	assert.True(t, PathExists(dir))
	assert.False(t, PathExists(filepath.Join(dir, "nonexistent")))

	f := filepath.Join(dir, "file.txt")
	require.NoError(t, os.WriteFile(f, []byte("hi"), 0644))
	assert.True(t, PathExists(f))
}

// --- GetCommitStorePath ---

func TestGetCommitStorePath_NewNode(t *testing.T) {
	home := t.TempDir()
	got := GetCommitStorePath(home)
	assert.Equal(t, filepath.Join(home, "data", "state_commit", "memiavl"), got)
}

func TestGetCommitStorePath_LegacyExists(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, "data", "committer.db")
	require.NoError(t, os.MkdirAll(legacy, 0755))

	got := GetCommitStorePath(home)
	assert.Equal(t, legacy, got)
}

// --- GetFlatKVPath ---

func TestGetFlatKVPath_NewNode(t *testing.T) {
	home := t.TempDir()
	got := GetFlatKVPath(home)
	assert.Equal(t, filepath.Join(home, "data", "state_commit", "flatkv"), got)
}

func TestGetFlatKVPath_LegacyExists(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, "data", "flatkv")
	require.NoError(t, os.MkdirAll(legacy, 0755))

	got := GetFlatKVPath(home)
	assert.Equal(t, legacy, got)
}

// --- GetStateStorePath ---

func TestGetStateStorePath_NewNode_Pebble(t *testing.T) {
	home := t.TempDir()
	got := GetStateStorePath(home, "pebbledb")
	assert.Equal(t, filepath.Join(home, "data", "state_store", "cosmos_ss", "pebbledb"), got)
}

func TestGetStateStorePath_NewNode_RocksDB(t *testing.T) {
	home := t.TempDir()
	got := GetStateStorePath(home, "rocksdb")
	assert.Equal(t, filepath.Join(home, "data", "state_store", "cosmos_ss", "rocksdb"), got)
}

func TestGetStateStorePath_LegacyExists(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, "data", "pebbledb")
	require.NoError(t, os.MkdirAll(legacy, 0755))

	got := GetStateStorePath(home, "pebbledb")
	assert.Equal(t, legacy, got)
}

func TestGetStateStorePath_LegacyForDifferentBackend(t *testing.T) {
	home := t.TempDir()
	// Legacy rocksdb dir exists but we ask for pebbledb — no legacy match
	require.NoError(t, os.MkdirAll(filepath.Join(home, "data", "rocksdb"), 0755))

	got := GetStateStorePath(home, "pebbledb")
	assert.Equal(t, filepath.Join(home, "data", "state_store", "cosmos_ss", "pebbledb"), got)
}

// --- GetEVMStateStorePath ---

func TestGetEVMStateStorePath_NewNode(t *testing.T) {
	home := t.TempDir()
	got := GetEVMStateStorePath(home)
	assert.Equal(t, filepath.Join(home, "data", "state_store", "evm_ss"), got)
}

func TestGetEVMStateStorePath_LegacyExists(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, "data", "evm_ss")
	require.NoError(t, os.MkdirAll(legacy, 0755))

	got := GetEVMStateStorePath(home)
	assert.Equal(t, legacy, got)
}

// --- GetReceiptStorePath ---

func TestGetReceiptStorePath_NewNode(t *testing.T) {
	home := t.TempDir()
	got := GetReceiptStorePath(home)
	assert.Equal(t, filepath.Join(home, "data", "ledger", "receipt.db"), got)
}

func TestGetReceiptStorePath_LegacyExists(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, "data", "receipt.db")
	require.NoError(t, os.MkdirAll(legacy, 0755))

	got := GetReceiptStorePath(home)
	assert.Equal(t, legacy, got)
}

// --- GetChangelogPath (unchanged, but verify) ---

func TestGetChangelogPath(t *testing.T) {
	assert.Equal(t, "/foo/bar/changelog", GetChangelogPath("/foo/bar"))
}

// --- Edge: new path already has data (second run of new node) ---

func TestGetCommitStorePath_NewDataAlreadyExists(t *testing.T) {
	home := t.TempDir()
	newPath := filepath.Join(home, "data", "state_commit", "memiavl")
	require.NoError(t, os.MkdirAll(newPath, 0755))

	got := GetCommitStorePath(home)
	assert.Equal(t, newPath, got, "should use new path when legacy is absent even if new path already exists")
}

func TestGetStateStorePath_NewDataAlreadyExists(t *testing.T) {
	home := t.TempDir()
	newPath := filepath.Join(home, "data", "state_store", "cosmos_ss", "pebbledb")
	require.NoError(t, os.MkdirAll(newPath, 0755))

	got := GetStateStorePath(home, "pebbledb")
	assert.Equal(t, newPath, got, "should use new path when legacy is absent even if new path already exists")
}

// --- Edge: both legacy and new exist (legacy wins) ---

func TestGetCommitStorePath_BothExist(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, "data", "committer.db")
	require.NoError(t, os.MkdirAll(legacy, 0755))
	newPath := filepath.Join(home, "data", "state_commit", "memiavl")
	require.NoError(t, os.MkdirAll(newPath, 0755))

	got := GetCommitStorePath(home)
	assert.Equal(t, legacy, got, "legacy should take precedence when both exist")
}

func TestGetReceiptStorePath_BothExist(t *testing.T) {
	home := t.TempDir()
	legacy := filepath.Join(home, "data", "receipt.db")
	require.NoError(t, os.MkdirAll(legacy, 0755))
	newPath := filepath.Join(home, "data", "ledger", "receipt.db")
	require.NoError(t, os.MkdirAll(newPath, 0755))

	got := GetReceiptStorePath(home)
	assert.Equal(t, legacy, got, "legacy should take precedence when both exist")
}
