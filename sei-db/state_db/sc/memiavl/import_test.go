package memiavl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	"github.com/stretchr/testify/require"
)

// TestTreeImporterRejectsBranchBeforeLeaves feeds a branch node before the two leaves it joins. The
// import must fail with an error, and Add must keep returning after the import has stopped, even once
// the node channel is full.
func TestTreeImporterRejectsBranchBeforeLeaves(t *testing.T) {
	prev := nodeChanSize
	nodeChanSize = 1
	t.Cleanup(func() { nodeChanSize = prev })

	imp := NewTreeImporter(context.Background(), t.TempDir(), 1)

	added := make(chan struct{})
	go func() {
		defer close(added)
		imp.Add(&types.SnapshotNode{Height: 1, Key: []byte("k"), Version: 1})
		for i := 0; i < 16; i++ {
			imp.Add(&types.SnapshotNode{Key: []byte(fmt.Sprintf("k%02d", i)), Value: []byte("v"), Version: 1})
		}
	}()
	select {
	case <-added:
	case <-time.After(30 * time.Second):
		t.Fatal("Add blocked after the import stopped")
	}

	require.ErrorContains(t, imp.Close(), "pending children")
	require.NoError(t, imp.Close(), "a second Close must be a no-op")
}

// TestMultiTreeImporterValidatesModuleName checks that AddModule only accepts
// plain directory names.
func TestMultiTreeImporterValidatesModuleName(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "db")
	require.NoError(t, os.Mkdir(dir, 0o750))

	mti, err := NewMultiTreeImporter(dir, 1)
	require.NoError(t, err)

	for _, name := range []string{"", ".", "..", "../../other", "a/b", "/abs", filepath.Join(root, "abs")} {
		require.ErrorContains(t, mti.AddModule(name), "invalid snapshot module name", "name %q", name)
	}
	require.Nil(t, mti.importer, "a rejected name must not start a tree import")

	require.NoError(t, mti.AddModule("bank"))
	require.NoError(t, mti.Close())

	_, err = os.Stat(filepath.Join(root, "other"))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(dir, snapshotName(1), "bank"))
	require.NoError(t, err)
}
