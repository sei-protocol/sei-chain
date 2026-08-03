package operations

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
)

// openedMemIAVL is a memiavl DB opened against a temp clone of the source.
type openedMemIAVL struct {
	*memiavl.DB
	tempDir string
}

func (o *openedMemIAVL) Close() error {
	var err error
	if o.DB != nil {
		err = o.DB.Close()
	}
	if o.tempDir != "" {
		if rmErr := os.RemoveAll(o.tempDir); rmErr != nil {
			if err != nil {
				return fmt.Errorf("%w; cleanup temp dir: %w", err, rmErr)
			}
			return fmt.Errorf("cleanup temp dir: %w", rmErr)
		}
	}
	return err
}

// openMemiAVLReplay opens memiavl at the given height (0 means latest) by
// replaying the changelog on top of the newest snapshot at or below it.
//
// It clones the source rather than using memiavl's ReadOnly option, which is
// weaker than it reads: ReadOnly skips the LOCK file but the changelog is still
// opened read-write, and that open "repairs" a torn tail record by truncating
// the segment. On a live node a torn tail is the writer mid-append, not
// corruption, so a read-only replay could truncate committed versions out from
// under a running seid. Repairing a torn tail in a private copy is harmless.
// This mirrors openFlatKVReadOnly.
func openMemiAVLReplay(dbDir string, height int64) (*openedMemIAVL, error) {
	tempDir, err := prepareMemIAVLToolingClone(dbDir, height)
	if err != nil {
		return nil, err
	}

	db, err := memiavl.OpenDB(height, memiavl.Options{
		Dir:      tempDir,
		ZeroCopy: true,
	})
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("open memiavl clone at version %d: %w", height, err)
	}
	opened := &openedMemIAVL{DB: db, tempDir: tempDir}

	// memiavl replays whatever the changelog holds and reports success even
	// when that falls short of the requested height. Every tail repair inside
	// the clone costs the trailing version, and a digest computed one version
	// early is indistinguishable from real divergence when comparing nodes.
	if reached := db.Version(); height > 0 && reached != height {
		err := fmt.Errorf("memiavl clone version mismatch: requested %d, reached %d "+
			"(changelog does not cover the target height)", height, reached)
		if closeErr := opened.Close(); closeErr != nil {
			return nil, errors.Join(err, fmt.Errorf("close clone: %w", closeErr))
		}
		return nil, err
	}

	return opened, nil
}

func prepareMemIAVLToolingClone(dbDir string, height int64) (string, error) {
	return retryToolingClone(dbDir, height, tryPrepareMemIAVLToolingClone)
}

// tryPrepareMemIAVLToolingClone mirrors tryPrepareFlatKVToolingClone: the two
// layouts are the same shape (current -> snapshot-N/, changelog/, LOCK) and both
// publish snapshots by rename and drop them wholesale, so the same
// hardlink-the-snapshot / byte-copy-the-changelog split applies.
func tryPrepareMemIAVLToolingClone(dbDir string, height int64) (string, error) {
	snapshotName, snapshotVersion, err := memiavl.SeekSnapshotDir(dbDir, height)
	if err != nil {
		return "", err
	}

	// The clone must sit inside dbDir to share a filesystem with the source
	// snapshot: dbDir is often its own mount point, so a sibling directory is
	// not enough and hardlinks would fail across the boundary.
	if err := os.MkdirAll(dbDir, 0o750); err != nil {
		return "", fmt.Errorf("ensure clone root %s: %w", dbDir, err)
	}
	// Not a "-tmp" suffix: memiavl's removeTmpDirs deletes every "*-tmp"
	// directory under its root when a node opens the DB read-write.
	tempDir, err := os.MkdirTemp(dbDir, ".seidb-memiavl-tool-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir under %s: %w", dbDir, err)
	}
	cleanup := func(err error) (string, error) {
		_ = os.RemoveAll(tempDir)
		return "", err
	}

	srcSnapshotDir := filepath.Join(dbDir, snapshotName)
	dstSnapshotDir := filepath.Join(tempDir, snapshotName)
	if err := cloneDirRecursive(srcSnapshotDir, dstSnapshotDir); err != nil {
		return cleanup(fmt.Errorf("clone snapshot %s: %w", snapshotName, err))
	}

	if err := os.Symlink(snapshotName, filepath.Join(tempDir, "current")); err != nil {
		return cleanup(fmt.Errorf("create current symlink: %w", err))
	}

	srcChangelogDir := filepath.Join(dbDir, "changelog")
	info, err := os.Stat(srcChangelogDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return cleanup(fmt.Errorf("stat changelog: %w", err))
	}
	if err == nil && !info.IsDir() {
		return cleanup(fmt.Errorf("changelog path is not a directory: %s", srcChangelogDir))
	}
	if err == nil {
		dstChangelogDir := filepath.Join(tempDir, "changelog")
		if err := copyDirRecursive(srcChangelogDir, dstChangelogDir); err != nil {
			return cleanup(fmt.Errorf("clone changelog: %w", err))
		}
		// A live writer can roll a new snapshot between our snapshot clone and
		// our changelog copy, then prune the changelog up to that newer
		// version — leaving a copy that no longer covers snapshotVersion+1 and
		// a catchup that would silently skip versions. Retryable.
		if err := verifyClonedWALCovers(dstChangelogDir, snapshotVersion); err != nil {
			return cleanup(err)
		}
	}

	return tempDir, nil
}
