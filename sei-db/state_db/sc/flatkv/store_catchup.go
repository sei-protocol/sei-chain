package flatkv

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// walOffsetForVersion returns the WAL offset whose entry has the given version.
// Returns 0 if the WAL is empty or the version predates all WAL entries.
//
// Strategy: try the arithmetic shortcut (O(1) reads) first -- it works when
// each version maps 1:1 to a sequential offset. On mismatch, fall back to
// binary search (O(log N) reads) which handles gaps and batched versions.
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
	lastOff, err := s.changelog.LastOffset()
	if err != nil {
		return 0, fmt.Errorf("WAL last offset: %w", err)
	}
	if lastOff == 0 || firstOff > lastOff {
		return 0, nil
	}

	firstVer, err := s.walVersionAtOffset(firstOff)
	if err != nil {
		return 0, fmt.Errorf("read first WAL entry: %w", err)
	}
	if firstVer <= 0 || version < firstVer {
		return 0, nil
	}

	// Fast path: O(1) arithmetic guess.
	guess := firstOff + uint64(version-firstVer) //nolint:gosec // version >= firstVer checked above
	if guess >= firstOff && guess <= lastOff {
		if v, err := s.walVersionAtOffset(guess); err == nil && v == version {
			return guess, nil
		}
	}

	// Slow path: binary search over [firstOff, lastOff].
	lo, hi := firstOff, lastOff
	for lo <= hi {
		mid := lo + (hi-lo)/2
		v, err := s.walVersionAtOffset(mid)
		if err != nil {
			return 0, fmt.Errorf("WAL binary search at offset %d: %w", mid, err)
		}
		switch {
		case v == version:
			return mid, nil
		case v < version:
			lo = mid + 1
		default:
			if mid == 0 {
				break
			}
			hi = mid - 1
		}
	}
	return 0, fmt.Errorf("WAL version %d not found (range %d-%d)", version, firstOff, lastOff)
}

// walVersionAtOffset reads a single WAL entry and returns its version.
func (s *CommitStore) walVersionAtOffset(off uint64) (int64, error) {
	var ver int64
	err := s.changelog.Replay(off, off, func(_ uint64, entry proto.ChangelogEntry) error {
		ver = entry.Version
		return nil
	})
	return ver, err
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

	startOff := firstOff
	if s.committedVersion > 0 {
		if off, err := s.walOffsetForVersion(s.committedVersion + 1); err == nil && off > startOff {
			if off > lastOff {
				return nil
			}
			startOff = off
		}
	}

	// Bound end offset to avoid deserializing entries past the target:
	// O(target - snapshot) instead of O(WAL_size).
	endOff := lastOff
	if targetVersion > 0 {
		off, err := s.walOffsetForVersion(targetVersion)
		if err != nil {
			return fmt.Errorf("catchup: resolve WAL offset for target version %d: %w", targetVersion, err)
		}
		if off > 0 && off < endOff {
			endOff = off
		}
	}

	var replayed int
	err = s.changelog.Replay(startOff, endOff, func(_ uint64, entry proto.ChangelogEntry) error {
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
			// During catchup with Sync=false, per-entry batch commits can leave
			// data only in OS/page cache. Flush once before advancing global
			// metadata so global watermark won't get ahead of data durability.
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
