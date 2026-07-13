package seiwal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// GetRange reports the range of record indices stored in the WAL directory at path, without constructing a
// live WAL instance. It first runs the standard recovery/sanity pass (the same one the constructor uses),
// which SEALS any unsealed file left behind by a prior session — so this function mutates the directory on
// disk even though its name suggests a read.
//
// NOT SAFE FOR CONCURRENT USE with a live WAL: it seals files that a running WAL instance owns, so it must
// only be called while no WAL is open on the same directory (e.g. offline, at startup before NewWAL).
// Callers must serialize it against any other GetRange/PruneAfter on the same directory.
func GetRange(path string) (ok bool, first uint64, last uint64, err error) {
	if err := recoverDirectory(path); err != nil {
		return false, 0, 0, fmt.Errorf("failed to recover WAL directory: %w", err)
	}
	sealedFiles, _, err := scanSealedFiles(path)
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
// recovery/sanity pass first (sealing any orphaned file), applies the rollback, then re-scans the result.
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
	if _, _, err := scanSealedFiles(path); err != nil {
		return fmt.Errorf("WAL is corrupt after pruning: %w", err)
	}
	return nil
}

// VerifyIntegrity reads every sealed file in the WAL directory at path and confirms that each record's CRC is
// intact and that each file's content exactly covers the index range its name promises. This is the expensive
// O(total sealed bytes) check that open (and GetRange/PruneAfter) deliberately skips.
//
// NOT SAFE FOR CONCURRENT USE with a live WAL on the same directory: it reads files a running WAL owns while
// they may still be changing.
func VerifyIntegrity(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		return fmt.Errorf("failed to read WAL directory %s: %w", path, err)
	}

	sealed := make([]parsedFileName, 0, len(entries))
	names := make(map[uint64]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		parsed, ok := parseFileName(entry.Name())
		if !ok || !parsed.sealed {
			continue
		}
		sealed = append(sealed, parsed)
		names[parsed.fileSeq] = entry.Name()
	}
	sort.Slice(sealed, func(i int, j int) bool { return sealed[i].fileSeq < sealed[j].fileSeq })

	var problems []error
	for i, parsed := range sealed {
		if i > 0 && parsed.fileSeq != sealed[i-1].fileSeq+1 {
			problems = append(problems, fmt.Errorf(
				"gap in sealed file sequence between %d and %d (a sealed file is missing)",
				sealed[i-1].fileSeq, parsed.fileSeq))
		}
		name := names[parsed.fileSeq]
		contents, err := readWalFile(filepath.Join(path, name))
		if err != nil {
			problems = append(problems, fmt.Errorf("failed to read sealed WAL file %s: %w", name, err))
			continue
		}
		if err := verifySealedContents(contents, parsed.fileSeq, parsed.firstIndex, parsed.lastIndex); err != nil {
			problems = append(problems, err)
		}
	}
	return errors.Join(problems...)
}
