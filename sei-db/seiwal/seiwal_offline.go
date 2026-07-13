package seiwal

import "fmt"

// GetRange reports the range of record indices stored in the WAL directory at path, without constructing a
// live WAL instance. It first runs the standard recovery/sanity pass (the same one the constructor uses),
// which SEALS any unsealed file left behind by a prior session and validates every sealed file — so this
// function mutates the directory on disk even though its name suggests a read.
//
// ok is false (and first/last are undefined) when the directory holds no records.
//
// NOT SAFE FOR CONCURRENT USE with a live WAL: it seals and validates files that a running WAL instance
// owns, so it must only be called while no WAL is open on the same directory (e.g. offline, at startup
// before NewWAL). Callers must serialize it against any other GetRange/PruneAfter on the same directory.
func GetRange(path string) (ok bool, first uint64, last uint64, err error) {
	if err := recoverDirectory(path); err != nil {
		return false, 0, 0, fmt.Errorf("failed to recover WAL directory: %w", err)
	}
	sealedFiles, _, err := scanAndValidate(path)
	if err != nil {
		return false, 0, 0, err
	}
	front, ok := sealedFiles.TryPeekFront()
	if !ok {
		return false, 0, 0, nil
	}
	back, _ := sealedFiles.TryPeekBack()
	return true, front.firstIndex, back.lastIndex, nil
}

// PruneAfter deletes every record with an index greater than highestIndexToKeep from the WAL directory at
// path — the offline rollback operation — without constructing a live WAL instance. It runs the standard
// recovery/sanity pass first (sealing any orphaned file), applies the rollback, then re-validates the result.
//
// This mirrors PruneBefore on the other end of the log: PruneBefore(lowestIndexToKeep) keeps indices >= its
// argument, while PruneAfter(highestIndexToKeep) keeps indices <= its argument. A crash mid-prune leaves a
// contiguous prefix of records, never a gap.
//
// NOT SAFE FOR CONCURRENT USE with a live WAL: it seals, rewrites, and removes files that a running WAL
// instance owns, so it must only be called while no WAL is open on the same directory (e.g. offline, at
// startup before NewWAL). Callers must serialize it against any other GetRange/PruneAfter on the same
// directory.
func PruneAfter(path string, highestIndexToKeep uint64) error {
	if err := recoverDirectory(path); err != nil {
		return fmt.Errorf("failed to recover WAL directory: %w", err)
	}
	if err := rollbackDirectory(path, highestIndexToKeep); err != nil {
		return fmt.Errorf("failed to prune WAL entries after index %d: %w", highestIndexToKeep, err)
	}
	if _, _, err := scanAndValidate(path); err != nil {
		return fmt.Errorf("WAL is corrupt after pruning: %w", err)
	}
	return nil
}
