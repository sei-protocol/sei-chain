package operations

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
)

const (
	// toolCloneOwnerLockName is the flock-held marker file every tooling
	// clone carries. While the creating process lives, the lock is held and
	// the clone is off-limits; once the process dies (including SIGKILL,
	// where no deferred cleanup runs), the kernel releases the flock and the
	// next tool invocation can reap the directory.
	toolCloneOwnerLockName = ".seidb-tool-owner.lock"

	// staleUnmarkedCloneAge guards the marker-less case: a crash in the
	// window between MkdirTemp and the owner-lock creation, where liveness
	// cannot be probed. Age is a poor liveness signal for marked clones
	// (a mainnet-scale digest can legitimately run for hours), so it is
	// used only when no marker exists at all.
	staleUnmarkedCloneAge = 24 * time.Hour
)

// toolClone is a private, disposable copy of a live store's snapshot +
// changelog that seidb tooling operates on instead of the live directory.
// It lives inside the source dbDir (to share its filesystem for hardlinks),
// which is exactly why leaking one is costly: its hardlinks pin snapshot
// inodes, so the live node's snapshot pruning frees no disk space until the
// clone is removed.
type toolClone struct {
	dir       string
	ownerLock memiavl.FileLock

	// walRepaired records that validating the cloned changelog shrank it:
	// the byte-copy caught the live writer mid-append and the WAL open
	// repaired the torn tail inside the clone, costing the trailing record.
	walRepaired bool
}

// newToolClone reaps abandoned sibling clones, creates a fresh clone
// directory under dbDir with the given prefix, and marks it owned via flock
// before any expensive cloning starts.
//
// The prefix deliberately has no "-tmp" suffix: memiavl's removeTmpDirs
// deletes every "*-tmp" directory under its root when a node opens the DB
// read-write, and these clones must never be reaped by a process that cannot
// see whether the owning tool is still alive.
func newToolClone(dbDir, prefix string) (*toolClone, error) {
	sweepStaleToolClones(dbDir, prefix)

	dir, err := os.MkdirTemp(dbDir, prefix+"*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir under %s: %w", dbDir, err)
	}
	ownerLock, err := memiavl.LockFile(filepath.Join(dir, toolCloneOwnerLockName))
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("acquire clone owner lock in %s: %w", dir, err)
	}
	return &toolClone{dir: dir, ownerLock: ownerLock}, nil
}

// Remove releases the ownership lock and deletes the clone directory.
func (c *toolClone) Remove() error {
	if c == nil {
		return nil
	}
	if c.ownerLock != nil {
		_ = c.ownerLock.Unlock()
		_ = c.ownerLock.Destroy()
		c.ownerLock = nil
	}
	if c.dir == "" {
		return nil
	}
	if err := os.RemoveAll(c.dir); err != nil {
		return fmt.Errorf("cleanup temp dir: %w", err)
	}
	c.dir = ""
	return nil
}

// sweepStaleToolClones removes abandoned clone directories under dbDir whose
// owner is provably gone: either the owner flock is acquirable (the creating
// process died), or no marker exists and the directory is old enough that the
// mkdir-to-lock window cannot explain it. Clones whose lock is still held —
// a concurrently running tool — are left alone. Best-effort by design: a
// failed sweep must never block the read the tool was invoked for.
func sweepStaleToolClones(dbDir, prefix string) {
	entries, err := os.ReadDir(dbDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		dir := filepath.Join(dbDir, entry.Name())
		lockPath := filepath.Join(dir, toolCloneOwnerLockName)
		if _, err := os.Stat(lockPath); errors.Is(err, os.ErrNotExist) {
			if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > staleUnmarkedCloneAge {
				_ = os.RemoveAll(dir)
			}
			continue
		}
		lock, err := memiavl.LockFile(lockPath)
		if err != nil {
			// Lock held (owner alive) or unreadable — leave the clone alone.
			continue
		}
		_ = lock.Unlock()
		_ = lock.Destroy()
		_ = os.RemoveAll(dir)
	}
}

// changelogByteSize sums the sizes of the regular files in a cloned changelog
// directory. Comparing it before and after the WAL-coverage validation open
// detects a torn-tail repair inside the clone (the only mutation that open
// can perform), which callers surface as a warning for latest-height reads.
func changelogByteSize(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // best-effort size probe
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}
