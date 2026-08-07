package operations

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/memiavl"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

// openedMemIAVL is a memiavl DB opened against a temp clone of the source.
type openedMemIAVL struct {
	*memiavl.DB
	clone *toolClone
}

func (o *openedMemIAVL) Close() error {
	var err error
	if o.DB != nil {
		err = o.DB.Close()
	}
	if rmErr := o.clone.Remove(); rmErr != nil {
		if err != nil {
			return fmt.Errorf("%w; %w", err, rmErr)
		}
		return rmErr
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
//
// height=0 ("latest") is best-effort on a live node: the clone is a
// consistent committed prefix as of the copy instant, and a torn-tail repair
// inside the clone (surfaced as a warning) can land it one version behind the
// source tip. There is no target version to check against, so the report's
// printed version line is the authoritative record of what was digested;
// cross-node comparisons should always use an explicit common --height.
func openMemiAVLReplay(dbDir string, height int64) (*openedMemIAVL, error) {
	clone, err := prepareMemIAVLToolingClone(dbDir, height)
	if err != nil {
		return nil, err
	}
	warnIfCloneRepaired(clone, "memiavl", height)

	db, err := memiavl.OpenDB(height, memiavl.Options{
		Dir:      clone.dir,
		ZeroCopy: true,
	})
	if err != nil {
		_ = clone.Remove()
		return nil, fmt.Errorf("open memiavl clone at version %d: %w", height, err)
	}
	opened := &openedMemIAVL{DB: db, clone: clone}

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

func prepareMemIAVLToolingClone(dbDir string, height int64) (*toolClone, error) {
	return retryToolingClone(dbDir, height, tryPrepareMemIAVLToolingClone)
}

// tryPrepareMemIAVLToolingClone mirrors tryPrepareFlatKVToolingClone: the two
// layouts are the same shape (current -> snapshot-N/, changelog/, LOCK) and both
// publish snapshots by rename and drop them wholesale, so the same
// hardlink-the-snapshot / byte-copy-the-changelog split applies.
func tryPrepareMemIAVLToolingClone(dbDir string, height int64) (*toolClone, error) {
	snapshotName, snapshotVersion, err := memiavl.SeekSnapshotName(dbDir, height)
	if err != nil {
		return nil, err
	}

	// The clone must sit inside dbDir to share a filesystem with the source
	// snapshot: dbDir is often its own mount point, so a sibling directory is
	// not enough and hardlinks would fail across the boundary.
	// SeekSnapshotName already read dbDir, so it is known to exist.
	clone, err := newToolClone(dbDir, ".seidb-memiavl-tool-")
	if err != nil {
		return nil, err
	}
	cleanup := func(err error) (*toolClone, error) {
		_ = clone.Remove()
		return nil, err
	}

	srcSnapshotDir := filepath.Join(dbDir, snapshotName)
	dstSnapshotDir := filepath.Join(clone.dir, snapshotName)
	if err := cloneDirRecursive(srcSnapshotDir, dstSnapshotDir); err != nil {
		return cleanup(fmt.Errorf("clone snapshot %s: %w", snapshotName, err))
	}

	if err := os.Symlink(snapshotName, filepath.Join(clone.dir, "current")); err != nil {
		return cleanup(fmt.Errorf("create current symlink: %w", err))
	}

	// The version parsed from the snapshot directory name is not enough to
	// know which changelog version catchup resumes from: memiavl bootstraps
	// every DB as snapshot-0 even when it was initialized with
	// SetInitialVersion(N), in which case the first changelog entry is
	// version N, not 1. Read the initial version from the cloned snapshot's
	// metadata (immune to source pruning — the files are hard links) and
	// derive the successor the same way memiavl itself does.
	metadata, err := memiavl.ReadMetadata(dstSnapshotDir)
	if err != nil {
		return cleanup(fmt.Errorf("read cloned snapshot metadata: %w", err))
	}
	if metadata.InitialVersion < 0 || metadata.InitialVersion > math.MaxUint32 {
		return cleanup(fmt.Errorf("cloned snapshot has invalid initial version: %d", metadata.InitialVersion))
	}
	firstNeeded := utils.NextVersion(snapshotVersion, uint32(metadata.InitialVersion))

	srcChangelogDir := filepath.Join(dbDir, "changelog")
	info, err := os.Stat(srcChangelogDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return cleanup(fmt.Errorf("stat changelog: %w", err))
	}
	if err == nil && !info.IsDir() {
		return cleanup(fmt.Errorf("changelog path is not a directory: %s", srcChangelogDir))
	}
	if err == nil {
		dstChangelogDir := filepath.Join(clone.dir, "changelog")
		if err := copyDirRecursive(srcChangelogDir, dstChangelogDir); err != nil {
			return cleanup(fmt.Errorf("clone changelog: %w", err))
		}
		// A live writer can roll a new snapshot between our snapshot clone and
		// our changelog copy, then prune the changelog up to that newer
		// version — leaving a copy that no longer covers the snapshot's
		// successor and a catchup that would silently skip versions. Retryable.
		sizeBefore := changelogByteSize(dstChangelogDir)
		if err := verifyClonedMemIAVLWALCovers(dstChangelogDir, snapshotVersion, firstNeeded); err != nil {
			return cleanup(err)
		}
		clone.walRepaired = changelogByteSize(dstChangelogDir) < sizeBefore
	}

	return clone, nil
}

// verifyClonedMemIAVLWALCovers is the memiavl counterpart to
// verifyClonedFlatKVWALCovers. memiavl still stores its changelog in the
// offset-indexed changelog WAL, where the offset says nothing about the
// version, so the range has to be read by replaying the first and last entries
// rather than from sealed file names as the block-keyed state WAL allows.
func verifyClonedMemIAVLWALCovers(dstChangelogDir string, snapshotVersion, firstNeeded int64) error {
	walLog, err := wal.NewChangelogWAL(dstChangelogDir, wal.Config{})
	if err != nil {
		return fmt.Errorf("open cloned changelog for validation: %w", err)
	}
	defer func() { _ = walLog.Close() }()

	firstOff, err := walLog.FirstOffset()
	if err != nil {
		return fmt.Errorf("cloned changelog first offset: %w", err)
	}
	lastOff, err := walLog.LastOffset()
	if err != nil {
		return fmt.Errorf("cloned changelog last offset: %w", err)
	}
	if firstOff == 0 || lastOff == 0 || firstOff > lastOff {
		return nil
	}

	firstVer, err := readWALEntryVersion(walLog, firstOff)
	if err != nil {
		return fmt.Errorf("read first cloned changelog entry: %w", err)
	}
	lastVer, err := readWALEntryVersion(walLog, lastOff)
	if err != nil {
		return fmt.Errorf("read last cloned changelog entry: %w", err)
	}
	return checkClonedWALCoverage(firstVer, lastVer, snapshotVersion, firstNeeded)
}

func readWALEntryVersion(walLog wal.ChangelogWAL, off uint64) (int64, error) {
	var ver int64
	err := walLog.Replay(off, off, func(_ uint64, entry proto.ChangelogEntry) error {
		ver = entry.Version
		return nil
	})
	return ver, err
}
