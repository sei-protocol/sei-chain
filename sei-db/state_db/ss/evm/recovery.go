package evm

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	sssnapshot "github.com/sei-protocol/sei-chain/sei-db/state_db/ss/snapshot"
)

// Suffixes of the directories a snapshot restore stages beside the one it replaces.
const (
	restoreTmpSuffix = ".restore-tmp"
	restoreBakSuffix = ".restore-bak"
)

// ApplyReplayedBlock applies one WAL block's EVM changesets and moves this store's head to that block.
//
// It is the per-block step of a replay, not the replay itself: the range to cover, and the check that
// the WAL still holds it, belong to the caller that owns the WAL.
func (s *EVMStateStore) ApplyReplayedBlock(block int64, changesets []*proto.NamedChangeSet) error {
	evmChangesets := filterEVMChangesets(changesets)
	if len(evmChangesets) == 0 {
		return s.SetLatestVersion(block)
	}
	if err := s.ApplyChangesetSync(block, evmChangesets); err != nil {
		return err
	}
	// A sync apply stamps the version marker only on the databases the block routed to, and the head is
	// the minimum across all of them, so the rest are moved to the same block here.
	return s.SetLatestVersion(block)
}

// RewindToSnapshotAtOrBelow rewinds this store to the newest snapshot at or below version and reports
// the version it landed on, discarding snapshots above that point. It needs no WAL: it moves only
// between snapshot boundaries, and replaying forward from the version it returns is the caller's to do.
//
// The store must have a snapshot at or below version, which is the one way this differs from the state
// commit store's rewind: SS restores from its own snapshots and has no base to fall back on.
func (s *EVMStateStore) RewindToSnapshotAtOrBelow(version int64) (int64, error) {
	if version <= 0 {
		return 0, fmt.Errorf("invalid rollback target %d", version)
	}
	base, err := s.rollbackBaseVersion(version)
	if err != nil {
		return 0, err
	}

	// A publish in flight reads and stamps the databases this is about to close and replace.
	resume := s.quiesceCheckpoints()
	defer resume()

	if err := s.closeDBs(); err != nil {
		return 0, fmt.Errorf("close EVM state store before rewinding to snapshot %d: %w", base, err)
	}
	// Before the restore, not after: it is what decides which way an interrupted rewind points. The
	// databases still hold a version above the target until the restore lands, so a crash here leaves
	// the next rewind to redo it. Restoring first would leave the databases at base and the discarded
	// snapshots on disk, and a store already at the target is one the next rewind skips.
	if err := s.snapshotMgr.RemoveSnapshotsAbove(version); err != nil {
		return 0, fmt.Errorf("remove snapshots above %d: %w", version, err)
	}
	if err := s.restoreSnapshot(base); err != nil {
		return 0, fmt.Errorf("restore snapshot %d: %w", base, err)
	}
	if err := s.openDBs(); err != nil {
		return 0, fmt.Errorf("reopen EVM state store after rewinding to snapshot %d: %w", base, err)
	}
	s.rewindLastOffered(base)
	return base, nil
}

func (s *EVMStateStore) rollbackBaseVersion(target int64) (int64, error) {
	if s.snapshotMgr == nil {
		return 0, fmt.Errorf("no snapshot at or below the target")
	}
	versions, err := s.snapshotMgr.Versions()
	if err != nil {
		return 0, fmt.Errorf("list snapshots: %w", err)
	}
	var base int64
	for _, version := range versions {
		if version <= target {
			base = version
		}
	}
	if base == 0 {
		return 0, fmt.Errorf("no snapshot at or below the target")
	}
	return base, nil
}

// restoreSnapshot replaces this store's databases with the contents of the snapshot at version.
//
// A unified store is one directory, and the single window where an interruption leaves none is healed
// on the next open. Separate-DB mode replaces each sub-DB in turn, and an interruption partway leaves
// them on different branches with no recovery: the head reads as the lowest of them, so the store looks
// merely behind, and replaying forward cannot delete the rows an untouched sub-DB holds above it. That
// mode is off by default.
func (s *EVMStateStore) restoreSnapshot(version int64) error {
	src := filepath.Join(s.snapshotMgr.Root(), sssnapshot.SnapshotDirName(version))
	if s.separateDBs {
		for _, storeType := range AllEVMStoreTypes() {
			if err := replacePebbleDir(subDBPath(src, storeType), subDBPath(s.dir, storeType)); err != nil {
				return err
			}
		}
		return nil
	}
	return replacePebbleDir(src, s.dir)
}

func replacePebbleDir(src, dst string) error {
	tmp := dst + restoreTmpSuffix
	bak := dst + restoreBakSuffix
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := os.RemoveAll(bak); err != nil {
		return err
	}
	if err := utils.ClonePebbleDir(src, tmp); err != nil {
		return err
	}
	if err := os.Rename(dst, bak); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return err
	}
	return os.RemoveAll(bak)
}

// healInterruptedRestore puts dst back when a restore was interrupted between the two renames that
// swap the new copy in, and clears whatever that restore left beside it.
//
// An absent dst is otherwise indistinguishable from a store that has never been written: the open
// creates an empty one, the head reads as 0, and a catch-up stamps its target over almost no state.
// The staged copy is preferred over the displaced one, since landing on the snapshot is what the
// interrupted rewind was for.
func healInterruptedRestore(dst string) error {
	if err := promoteInterruptedRestore(dst); err != nil {
		return err
	}
	// Each leftover is a full copy of the store, and only a later restore of this same directory would
	// clear it. A node that crashed once and never rewinds again would carry it forever.
	for _, leftover := range []string{dst + restoreTmpSuffix, dst + restoreBakSuffix} {
		if err := os.RemoveAll(leftover); err != nil {
			return fmt.Errorf("remove %q left by an interrupted snapshot restore: %w", leftover, err)
		}
	}
	return nil
}

// promoteInterruptedRestore moves a copy left by an interrupted restore into dst, and does nothing
// when dst is already there.
func promoteInterruptedRestore(dst string) error {
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		// Present, or unreadable for a reason opening it will report.
		return nil
	}
	for _, leftover := range []string{dst + restoreTmpSuffix, dst + restoreBakSuffix} {
		if _, err := os.Stat(leftover); err != nil {
			continue
		}
		if err := os.Rename(leftover, dst); err != nil {
			return fmt.Errorf("promote %q left by an interrupted snapshot restore: %w", leftover, err)
		}
		logger.Info("promoted a directory left by an interrupted snapshot restore",
			"from", leftover, "to", dst)
		return nil
	}
	return nil
}
