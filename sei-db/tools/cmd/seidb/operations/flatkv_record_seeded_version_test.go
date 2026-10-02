package operations

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/stretchr/testify/require"
)

func TestRecordFlatKVSeededVersion(t *testing.T) {
	homeDir := t.TempDir()
	store := newTestFlatKVStoreAtHome(t, homeDir)
	require.NoError(t, store.SetInitialVersion(11))
	require.NoError(t, store.Close())
	require.NoError(t, os.Remove(filepath.Join(utils.GetFlatKVPath(homeDir), "SEEDED_VERSION")))

	require.ErrorContains(t, recordFlatKVSeededVersion(context.Background(), homeDir, 11), "above the store's version 10")
	require.NoError(t, recordFlatKVSeededVersion(context.Background(), homeDir, 10))

	store = newTestFlatKVStoreAtHome(t, homeDir)
	defer func() { require.NoError(t, store.Close()) }()
	seeded, ok := store.SeededVersion()
	require.True(t, ok)
	require.Equal(t, int64(10), seeded)
}
