package evm

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/config"
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

// DiscardStateAbove puts the closed store under dir on its newest snapshot at or below target when it
// holds any state above target, and reports the version its files hold once it returns. A store holding
// nothing above target is left alone, reported at the version it opens on, for a replay to carry it
// forward.
//
// earliestReplayableBlock is the first block the caller can replay, or 0 when it can replay none. A
// store that would land too low for that replay to carry it back to target is refused, and so is one
// above target with no way down at all. Neither refusal moves anything, so a caller that gets an error
// still has every snapshot it started with. The databases must be closed, and root is the store's
// snapshot directory.
//
// A store above target with no snapshot to land on has a second way down that SC does not: emptying it,
// which reconstructs it exactly when the caller can replay from block 1. A store already sitting at 0
// that the caller cannot replay from block 1 is left there to fill forward, rather than refused for
// being merely new.
func DiscardStateAbove(
	cfg config.StateStoreConfig, root string, target, earliestReplayableBlock int64,
) (landsOn int64, err error) {
	opensAt, highest, err := StoredVersions(cfg)
	if err != nil {
		return 0, fmt.Errorf("read the versions it holds: %w", err)
	}
	rebuildsFromEmpty := earliestReplayableBlock == 1

	// The highest version any one database records, not the version the store opens on: that one is the
	// lowest of them, so an interrupted rewind reads as merely behind while the rows above target
	// survive a replay that only writes forward.
	if highest <= target {
		if opensAt == 0 && !rebuildsFromEmpty {
			return 0, nil
		}
		if err := requireReplayable(opensAt, target, earliestReplayableBlock); err != nil {
			return 0, err
		}
		return opensAt, nil
	}

	// Sought before either route runs, so a store with nowhere to go is refused with its files still
	// where they are.
	base, found, err := snapshotAtOrBelow(root, target)
	if err != nil {
		return 0, err
	}
	if !found && !rebuildsFromEmpty {
		return 0, fmt.Errorf("it holds block %d and has no snapshot at or below %d, and no replay from "+
			"block 1 is available to rebuild it from", highest, target)
	}
	if !found {
		base = 0
	}
	if err := requireReplayable(base, target, earliestReplayableBlock); err != nil {
		return 0, err
	}
	if !found {
		if err := ResetClosedStore(cfg.EVMDBDirectory, root, cfg.SeparateEVMSubDBs); err != nil {
			return 0, err
		}
		return 0, nil
	}
	return RewindClosedStoreTo(cfg.EVMDBDirectory, root, cfg.SeparateEVMSubDBs, target)
}

// requireReplayable returns an error when a store landing on landsOn cannot be carried back up to
// target, because the caller's earliest replayable block is above the first one such a replay needs.
// earliestReplayableBlock is 0 when the caller can replay nothing.
func requireReplayable(landsOn, target, earliestReplayableBlock int64) error {
	if landsOn >= target {
		return nil
	}
	start := landsOn + 1
	if earliestReplayableBlock == 0 {
		return fmt.Errorf("it would land on version %d, so replay must start at block %d, but no blocks "+
			"are available to replay", landsOn, start)
	}
	if earliestReplayableBlock > start {
		return fmt.Errorf("it would land on version %d, so replay must start at block %d, but the "+
			"earliest block available is %d", landsOn, start, earliestReplayableBlock)
	}
	return nil
}

// StoredVersions reports where the closed store's databases sit: the version the store opens on, which
// is the lowest of them, and the highest version any one of them records. A directory that has never
// been written reads as 0.
//
// The two differ only after an interrupted commit, restore or reset, which leaves some databases
// holding a block the rest do not.
func StoredVersions(cfg config.StateStoreConfig) (opensAt, highest int64, err error) {
	if _, err := os.Stat(cfg.EVMDBDirectory); err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	store, err := NewEVMStateStore(cfg.EVMDBDirectory, cfg)
	if err != nil {
		return 0, 0, err
	}
	opensAt, highest = store.GetLatestVersion(), store.HighestDBVersion()
	if err := store.Close(); err != nil {
		return 0, 0, fmt.Errorf("close it again: %w", err)
	}
	return opensAt, highest, nil
}

// snapshotAtOrBelow returns the newest snapshot version under root at or below target, which is where
// RewindClosedStoreTo lands a store, and reports whether root has one.
//
// Version 0 is a snapshot like any other, which is why the answer is a version and a flag.
func snapshotAtOrBelow(root string, target int64) (version int64, found bool, err error) {
	versions, err := sssnapshot.ListSnapshotVersions(root)
	if err != nil {
		return 0, false, fmt.Errorf("list the EVM state store snapshots under %q: %w", root, err)
	}
	for _, candidate := range versions {
		if candidate <= target {
			version, found = candidate, true
		}
	}
	return version, found, nil
}

// DropSnapshotsAbove deletes every snapshot under root above target and repoints the current link at
// the newest one left. It leaves the store's databases alone, so a store already at or below target
// keeps the history it holds.
func DropSnapshotsAbove(root string, target int64) error {
	if _, err := sssnapshot.RewindTo(root, target); err != nil {
		return fmt.Errorf("remove EVM state store snapshots above %d: %w", target, err)
	}
	return nil
}

// RewindClosedStoreTo puts the files of the closed store under dir on the newest snapshot at or below
// target and reports that version, deleting the snapshots above it. The next open of that store lands
// on the reported version, with the blocks from there to target left for the caller to replay.
//
// It refuses a target that has no snapshot at or below it, and leaves the databases untouched. A
// caller whose store is already at or below the target must not call this. The databases under dir
// must be closed. root is the store's snapshot directory, and separateDBs its layout.
func RewindClosedStoreTo(dir, root string, separateDBs bool, target int64) (landed int64, err error) {
	if target < 1 {
		return 0, fmt.Errorf("rewind target %d is invalid: version 0 means no state, so there is nothing "+
			"to rewind to", target)
	}

	base, found, err := snapshotAtOrBelow(root, target)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, fmt.Errorf("cannot rewind the EVM state store to %d: no snapshot at or below target", target)
	}

	// Before the restore, not after: it is what decides which way an interrupted rewind points. The
	// databases still hold a version above the target until the restore lands, so a crash here leaves
	// the next rewind to redo it. Restoring first would leave the databases at base with the discarded
	// snapshots on disk, and nothing afterwards to say the branch they belong to was abandoned.
	if _, err := sssnapshot.RewindTo(root, target); err != nil {
		return 0, fmt.Errorf("remove EVM state store snapshots above %d: %w", target, err)
	}
	if err := restoreSnapshot(dir, root, separateDBs, base); err != nil {
		return 0, fmt.Errorf("restore EVM state store snapshot %d: %w", base, err)
	}
	logger.Info("EVM state store rewound a closed store to a snapshot", "version", base, "target", target)
	return base, nil
}

// ResetClosedStore empties the closed store under dir and deletes every snapshot under root, so the
// next open creates a store at version 0 and a replay rebuilds it from block 1. root is the store's
// snapshot directory, and separateDBs its layout.
//
// It is the route onto a target for a store above it with no snapshot to land on, which RewindClosedStoreTo
// refuses. The caller owns the WAL and so is the one that knows a replay from block 1 is available.
func ResetClosedStore(dir, root string, separateDBs bool) error {
	// Before the databases, for the reason RewindClosedStoreTo gives: they hold a version until they are
	// removed, so a crash in between leaves the next open to redo the reset rather than to land on a
	// snapshot from the branch this one abandoned.
	if _, err := sssnapshot.RewindTo(root, 0); err != nil {
		return fmt.Errorf("remove the EVM state store snapshots under %q: %w", root, err)
	}
	// Separate-DB mode empties them one at a time, so an interruption partway leaves the rest holding
	// the branch being discarded while the head, the lowest of them, reads as 0. HighestDBVersion is
	// what shows that, and is what a caller has to plan its next rewind from.
	for _, dbDir := range storeDBDirs(dir, separateDBs) {
		if err := removePebbleDir(dbDir); err != nil {
			return err
		}
	}
	logger.Info("EVM state store emptied for a replay to rebuild it", "dir", dir)
	return nil
}

// storeDBDirs returns the pebble directories a store of this layout keeps under dir.
func storeDBDirs(dir string, separateDBs bool) []string {
	if !separateDBs {
		return []string{dir}
	}
	storeTypes := AllEVMStoreTypes()
	dirs := make([]string, 0, len(storeTypes))
	for _, storeType := range storeTypes {
		dirs = append(dirs, subDBPath(dir, storeType))
	}
	return dirs
}

// removePebbleDir deletes dst along with anything an interrupted restore staged beside it.
//
// The leftovers go first: promoteInterruptedRestore moves one into an absent dst, so the other order
// leaves a window where a crash resurrects the store this is removing.
func removePebbleDir(dst string) error {
	for _, path := range []string{dst + restoreTmpSuffix, dst + restoreBakSuffix, dst} {
		if err := os.RemoveAll(path); err != nil {
			return fmt.Errorf("remove %q while emptying the EVM state store: %w", path, err)
		}
	}
	return nil
}

// restoreSnapshot replaces the databases under dir with the contents of the snapshot at version.
//
// A unified store is one directory, and the single window where an interruption leaves none is healed
// on the next open. Separate-DB mode replaces each sub-DB in turn, and an interruption partway leaves
// them on different branches: the head reads as the lowest of them, so the store looks merely behind,
// while a sub-DB the restore had not reached still holds rows above it that replaying forward cannot
// delete. HighestDBVersion is what shows those, and is what a caller has to plan its next rewind from.
func restoreSnapshot(dir, root string, separateDBs bool, version int64) error {
	if version < 1 {
		return fmt.Errorf("restore snapshot version %d is invalid: a rewind lands on a real snapshot", version)
	}
	src := filepath.Join(root, sssnapshot.SnapshotDirName(version))
	if !separateDBs {
		return replacePebbleDir(src, dir)
	}
	for _, storeType := range AllEVMStoreTypes() {
		if err := replacePebbleDir(subDBPath(src, storeType), subDBPath(dir, storeType)); err != nil {
			return err
		}
	}
	return nil
}

// replacePebbleDir swaps the contents of src into dst through a staged copy.
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
