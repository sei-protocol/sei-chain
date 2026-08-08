package operations

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
)

// TestSweepStaleToolClones pins the reaping policy for abandoned tooling
// clones: a clone whose owner lock is acquirable was orphaned by a dead
// process (SIGKILL runs no deferred cleanup) and must be removed, a clone
// whose lock is held belongs to a live tool and must survive, and a
// marker-less clone is removed only once it is old enough that the
// mkdir-to-lock window cannot explain it.
func TestSweepStaleToolClones(t *testing.T) {
	dbDir := t.TempDir()
	prefix := ".seidb-flatkv-tool-"

	staleUnmarked := filepath.Join(dbDir, prefix+"stale-unmarked")
	require.NoError(t, os.Mkdir(staleUnmarked, 0o750))
	old := time.Now().Add(-2 * staleUnmarkedCloneAge)
	require.NoError(t, os.Chtimes(staleUnmarked, old, old))

	freshUnmarked := filepath.Join(dbDir, prefix+"fresh-unmarked")
	require.NoError(t, os.Mkdir(freshUnmarked, 0o750))

	held := filepath.Join(dbDir, prefix+"held")
	require.NoError(t, os.Mkdir(held, 0o750))
	heldLock, err := memiavl.LockFile(filepath.Join(held, toolCloneOwnerLockName))
	require.NoError(t, err)

	orphaned := filepath.Join(dbDir, prefix+"orphaned")
	require.NoError(t, os.Mkdir(orphaned, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(orphaned, toolCloneOwnerLockName), nil, 0o600))

	otherPrefix := filepath.Join(dbDir, ".seidb-memiavl-tool-orphaned")
	require.NoError(t, os.Mkdir(otherPrefix, 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(otherPrefix, toolCloneOwnerLockName), nil, 0o600))

	sweepStaleToolClones(dbDir, prefix)

	require.NoDirExists(t, staleUnmarked, "old marker-less clone must be reaped")
	require.DirExists(t, freshUnmarked, "fresh marker-less clone may still be mid-creation")
	require.DirExists(t, held, "a held owner lock proves the owning tool is alive")
	require.NoDirExists(t, orphaned, "an acquirable owner lock proves the owner died")
	require.DirExists(t, otherPrefix, "the sweep must not touch other clone families")

	require.NoError(t, heldLock.Unlock())
	sweepStaleToolClones(dbDir, prefix)
	require.NoDirExists(t, held, "once the owner releases the lock the clone is reapable")
}

// TestNewToolCloneOwnershipLifecycle checks that a live clone defends itself
// against a concurrent sweep and that Remove releases everything.
func TestNewToolCloneOwnershipLifecycle(t *testing.T) {
	dbDir := t.TempDir()
	prefix := ".seidb-memiavl-tool-"

	clone, err := newToolClone(dbDir, prefix)
	require.NoError(t, err)
	require.DirExists(t, clone.dir)
	require.FileExists(t, filepath.Join(clone.dir, toolCloneOwnerLockName))

	sweepStaleToolClones(dbDir, prefix)
	require.DirExists(t, clone.dir, "an owned clone must survive a concurrent sweep")

	require.NoError(t, clone.Remove())
	require.NoDirExists(t, clone.dir)
	require.NoError(t, clone.Remove(), "Remove must be idempotent")
}
