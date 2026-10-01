package memiavl

import (
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"github.com/sei-protocol/sei-chain/sei-db/wal"
)

// TreeChangelogRange is the snapshot a tree changelog replay starts from, and the version it reaches.
type TreeChangelogRange struct {
	// SnapshotDir is the snapshot directory the replay starts from.
	SnapshotDir string
	// SnapshotVersion is the version of the snapshot at SnapshotDir.
	SnapshotVersion int64
	// Version is the version the replay reaches.
	Version int64
}

// ReplayTreeChangelog passes to fn, in changelog order, every changeset of treeName that
// OpenDB(targetVersion) would apply on top of its snapshot. A targetVersion of 0 selects the
// current snapshot and the changelog tip. It loads no tree and never repairs the changelog.
//
// It refuses a changelog that starts above the first version after the snapshot, a changelog
// that ends below targetVersion, and an upgrade that adds, deletes, or renames treeName.
func ReplayTreeChangelog(
	dir string,
	targetVersion int64,
	treeName string,
	fn func(version int64, changeSet proto.ChangeSet) error,
) (TreeChangelogRange, error) {
	snapshotDir, err := treeChangelogSnapshotDir(dir, targetVersion)
	if err != nil {
		return TreeChangelogRange{}, err
	}
	metadata, err := readMetadata(snapshotDir)
	if err != nil {
		return TreeChangelogRange{}, fmt.Errorf("read snapshot metadata %s: %w", snapshotDir, err)
	}
	if metadata.InitialVersion < 0 || metadata.InitialVersion > math.MaxUint32 {
		return TreeChangelogRange{}, fmt.Errorf("invalid initial version: %d", metadata.InitialVersion)
	}
	r := TreeChangelogRange{
		SnapshotDir:     snapshotDir,
		SnapshotVersion: metadata.CommitInfo.Version,
		Version:         metadata.CommitInfo.Version,
	}

	stream, err := wal.NewChangelogWAL(utils.GetChangelogPath(dir), wal.Config{NoRepairOnOpen: true})
	if err != nil {
		return TreeChangelogRange{}, fmt.Errorf("open changelog: %w", err)
	}
	defer func() { _ = stream.Close() }()
	span, err := readChangelogSpan(stream)
	if err != nil {
		return TreeChangelogRange{}, err
	}

	startVersion := utils.NextVersion(r.SnapshotVersion, uint32(metadata.InitialVersion))
	endVersion, err := span.replayEnd(startVersion, targetVersion)
	if err != nil {
		return TreeChangelogRange{}, fmt.Errorf("replay above snapshot %d: %w", r.SnapshotVersion, err)
	}
	if endVersion < startVersion {
		return r, nil
	}
	if err := replayTreeChangeSets(stream, span, startVersion, endVersion, treeName, fn); err != nil {
		return TreeChangelogRange{}, err
	}
	r.Version = endVersion
	return r, nil
}

// treeChangelogSnapshotDir returns the snapshot directory OpenDB(targetVersion) loads.
func treeChangelogSnapshotDir(dir string, targetVersion int64) (string, error) {
	if targetVersion == 0 {
		name, err := os.Readlink(currentPath(dir))
		if err != nil {
			return "", fmt.Errorf("read current snapshot link: %w", err)
		}
		return filepath.Join(dir, name), nil
	}
	snapshotVersion, err := seekSnapshot(dir, targetVersion)
	if err != nil {
		return "", fmt.Errorf("fail to seek snapshot: %w", err)
	}
	return filepath.Join(dir, snapshotName(snapshotVersion)), nil
}

// changelogSpan is the version range a changelog holds.
type changelogSpan struct {
	// empty is true for a changelog with no entries; the other fields are then zero.
	empty        bool
	firstVersion int64
	lastVersion  int64
	// delta is the version of an entry minus its changelog index.
	delta int64
}

// readChangelogSpan returns the version range of stream.
func readChangelogSpan(stream wal.ChangelogWAL) (changelogSpan, error) {
	delta, hasEntries, err := computeWALIndexDelta(stream)
	if err != nil {
		return changelogSpan{}, fmt.Errorf("compute changelog index delta: %w", err)
	}
	if !hasEntries {
		return changelogSpan{empty: true}, nil
	}
	firstIndex, err := stream.FirstOffset()
	if err != nil {
		return changelogSpan{}, fmt.Errorf("read changelog first index: %w", err)
	}
	lastIndex, err := stream.LastOffset()
	if err != nil {
		return changelogSpan{}, fmt.Errorf("read changelog last index: %w", err)
	}
	if lastIndex < firstIndex {
		return changelogSpan{empty: true}, nil
	}
	// #nosec G115 -- changelog indexes are far below MaxInt64
	return changelogSpan{firstVersion: int64(firstIndex) + delta, lastVersion: int64(lastIndex) + delta, delta: delta}, nil
}

// replayEnd returns the last version to replay from startVersion for targetVersion, where 0 means
// the changelog tip. A result below startVersion means there is nothing to replay. It refuses a
// span that does not hold every version from startVersion to the result.
func (s changelogSpan) replayEnd(startVersion, targetVersion int64) (int64, error) {
	if targetVersion == 0 {
		if s.empty || s.lastVersion < startVersion {
			return startVersion - 1, nil
		}
		targetVersion = s.lastVersion
	}
	if targetVersion < startVersion {
		return targetVersion, nil
	}
	if s.empty || s.lastVersion < targetVersion {
		return 0, fmt.Errorf("changelog ends below version %d: %s", targetVersion, s)
	}
	if s.firstVersion > startVersion {
		return 0, fmt.Errorf("changelog starts above version %d: %s", startVersion, s)
	}
	return targetVersion, nil
}

func (s changelogSpan) String() string {
	if s.empty {
		return "the changelog is empty"
	}
	return fmt.Sprintf("the changelog holds versions %d to %d", s.firstVersion, s.lastVersion)
}

// replayTreeChangeSets passes the changesets of treeName in versions startVersion to endVersion
// to fn. It refuses an entry whose version does not match its index, and an upgrade of treeName.
func replayTreeChangeSets(
	stream wal.ChangelogWAL,
	span changelogSpan,
	startVersion, endVersion int64,
	treeName string,
	fn func(version int64, changeSet proto.ChangeSet) error,
) error {
	// #nosec G115 -- replayEnd placed both versions inside the span, so both indexes are positive
	startIndex, endIndex := uint64(startVersion-span.delta), uint64(endVersion-span.delta)
	return stream.Replay(startIndex, endIndex, func(index uint64, entry proto.ChangelogEntry) error {
		// #nosec G115 -- changelog indexes are far below MaxInt64
		if want := int64(index) + span.delta; entry.Version != want {
			return fmt.Errorf("changelog entry at index %d has version %d, want %d", index, entry.Version, want)
		}
		for _, upgrade := range entry.Upgrades {
			if upgrade.Name == treeName || upgrade.RenameFrom == treeName {
				return fmt.Errorf("changelog version %d upgrades tree %q (name=%q rename_from=%q delete=%t)",
					entry.Version, treeName, upgrade.Name, upgrade.RenameFrom, upgrade.Delete)
			}
		}
		for _, cs := range entry.Changesets {
			if cs.Name != treeName {
				continue
			}
			if err := fn(entry.Version, cs.Changeset); err != nil {
				return err
			}
		}
		return nil
	})
}
