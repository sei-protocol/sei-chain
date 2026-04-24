package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ResolveDBDir ---

func TestResolveDBDir_NewNode_Blockstore(t *testing.T) {
	base := t.TempDir()
	got := ResolveDBDir("blockstore", base)
	assert.Equal(t, filepath.Join(base, "tendermint"), got)
}

func TestResolveDBDir_NewNode_TxIndex(t *testing.T) {
	base := t.TempDir()
	got := ResolveDBDir("tx_index", base)
	assert.Equal(t, filepath.Join(base, "tendermint"), got)
}

func TestResolveDBDir_NewNode_State(t *testing.T) {
	base := t.TempDir()
	got := ResolveDBDir("state", base)
	assert.Equal(t, filepath.Join(base, "tendermint"), got)
}

func TestResolveDBDir_NewNode_Evidence(t *testing.T) {
	base := t.TempDir()
	got := ResolveDBDir("evidence", base)
	assert.Equal(t, filepath.Join(base, "tendermint"), got)
}

func TestResolveDBDir_NewNode_Peerstore(t *testing.T) {
	base := t.TempDir()
	got := ResolveDBDir("peerstore", base)
	assert.Equal(t, filepath.Join(base, "tendermint"), got)
}

func TestResolveDBDir_LegacyBlockstore(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, "blockstore.db"), 0755))

	got := ResolveDBDir("blockstore", base)
	assert.Equal(t, base, got, "should return base dir when legacy blockstore.db exists")
}

func TestResolveDBDir_LegacyState(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, "state.db"), 0755))

	got := ResolveDBDir("state", base)
	assert.Equal(t, base, got, "should return base dir when legacy state.db exists")
}

func TestResolveDBDir_LegacyTxIndex(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, "tx_index.db"), 0755))

	got := ResolveDBDir("tx_index", base)
	assert.Equal(t, base, got, "should return base dir when legacy tx_index.db exists")
}

func TestResolveDBDir_LegacyEvidence(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, "evidence.db"), 0755))

	got := ResolveDBDir("evidence", base)
	assert.Equal(t, base, got, "should return base dir when legacy evidence.db exists")
}

func TestResolveDBDir_LegacyPeerstore(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, "peerstore.db"), 0755))

	got := ResolveDBDir("peerstore", base)
	assert.Equal(t, base, got, "should return base dir when legacy peerstore.db exists")
}

func TestResolveDBDir_UnknownID(t *testing.T) {
	base := t.TempDir()
	got := ResolveDBDir("something_unknown", base)
	assert.Equal(t, base, got, "unknown DB IDs should fall through to base dir")
}

func TestResolveDBDir_NewDataAlreadyExists(t *testing.T) {
	base := t.TempDir()
	newDir := filepath.Join(base, "tendermint", "blockstore.db")
	require.NoError(t, os.MkdirAll(newDir, 0755))

	got := ResolveDBDir("blockstore", base)
	assert.Equal(t, filepath.Join(base, "tendermint"), got,
		"should use new path on subsequent runs when legacy is absent")
}

func TestResolveDBDir_BothExist_LegacyWins(t *testing.T) {
	base := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(base, "state.db"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(base, "tendermint", "state.db"), 0755))

	got := ResolveDBDir("state", base)
	assert.Equal(t, base, got, "legacy should win when both exist")
}

// Ensure that one DB having legacy data does not affect resolution of another DB.
func TestResolveDBDir_IndependentResolution(t *testing.T) {
	base := t.TempDir()
	// blockstore has legacy data, but state does not
	require.NoError(t, os.MkdirAll(filepath.Join(base, "blockstore.db"), 0755))

	gotBlock := ResolveDBDir("blockstore", base)
	gotState := ResolveDBDir("state", base)

	assert.Equal(t, base, gotBlock, "blockstore should resolve to legacy")
	assert.Equal(t, filepath.Join(base, "tendermint"), gotState, "state should resolve to new path")
}
