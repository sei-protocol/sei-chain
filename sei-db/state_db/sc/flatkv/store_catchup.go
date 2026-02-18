package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// walOffsetForVersion returns the WAL offset corresponding to version.
// Returns 0 if the WAL is empty or the version predates all WAL entries.
// The mapping relies on the invariant that each version produces exactly one
// WAL entry and offsets are sequential.
func (s *CommitStore) walOffsetForVersion(version int64) (uint64, error) {
	if s.changelog == nil {
		return 0, fmt.Errorf("changelog not open")
	}
	firstOff, err := s.changelog.FirstOffset()
	if err != nil {
		return 0, fmt.Errorf("WAL first offset: %w", err)
	}
	if firstOff == 0 {
		return 0, nil
	}
	var firstVersion int64
	if err := s.changelog.Replay(firstOff, firstOff, func(_ uint64, entry proto.ChangelogEntry) error {
		firstVersion = entry.Version
		return nil
	}); err != nil {
		return 0, fmt.Errorf("read first WAL entry: %w", err)
	}
	if firstVersion <= 0 || version < firstVersion {
		return 0, nil
	}
	return firstOff + uint64(version-firstVersion), nil
}

// catchup replays WAL entries from the current committedVersion up to (and
// including) targetVersion. If targetVersion <= 0, replay continues to the
// end of the WAL.
//
// Each replayed entry runs through ApplyChangeSets (which updates
// workingLtHash) and commitBatches (which persists to the per-DB PebbleDBs).
// After all entries are replayed, global metadata is flushed once.
func (s *CommitStore) catchup(targetVersion int64) error {
	if s.changelog == nil {
		return fmt.Errorf("catchup: changelog not open")
	}

	firstOff, err := s.changelog.FirstOffset()
	if err != nil {
		return fmt.Errorf("catchup: first offset: %w", err)
	}
	lastOff, err := s.changelog.LastOffset()
	if err != nil {
		return fmt.Errorf("catchup: last offset: %w", err)
	}

	if lastOff == 0 || firstOff > lastOff {
		return nil
	}

	// Compute optimal start offset using the version-to-offset mapping so
	// we skip WAL entries that are already applied rather than scanning from
	// the beginning.
	startOff := firstOff
	if s.committedVersion > 0 {
		if off, err := s.walOffsetForVersion(s.committedVersion + 1); err == nil && off > startOff {
			if off > lastOff {
				return nil
			}
			startOff = off
		}
	}

	var replayed int
	err = s.changelog.Replay(startOff, lastOff, func(_ uint64, entry proto.ChangelogEntry) error {
		if entry.Version <= s.committedVersion {
			return nil
		}
		if targetVersion > 0 && entry.Version > targetVersion {
			return nil
		}

		if err := s.ApplyChangeSets(entry.Changesets); err != nil {
			return fmt.Errorf("catchup apply v%d: %w", entry.Version, err)
		}
		if err := s.commitBatches(entry.Version); err != nil {
			return fmt.Errorf("catchup commit v%d: %w", entry.Version, err)
		}

		s.committedVersion = entry.Version
		s.committedLtHash = s.workingLtHash.Clone()
		s.clearPendingWrites()

		replayed++
		if replayed%1000 == 0 {
			s.log.Info("FlatKV catchup progress", "replayed", replayed, "version", entry.Version)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("catchup replay: %w", err)
	}

	if replayed > 0 {
		if !s.config.Fsync {
			if err := s.flushAllDBs(); err != nil {
				return fmt.Errorf("catchup flush: %w", err)
			}
		}
		if err := s.commitGlobalMetadata(s.committedVersion, s.committedLtHash); err != nil {
			return fmt.Errorf("catchup global meta: %w", err)
		}
		s.log.Info("FlatKV catchup complete", "replayed", replayed, "version", s.committedVersion)
	}

	return nil
}
