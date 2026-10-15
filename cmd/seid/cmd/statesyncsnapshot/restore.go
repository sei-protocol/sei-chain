package statesyncsnapshot

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
)

const restoreStagingDirName = ".state-sync-restore"

// Steps of restore at which a stepHook is called.
const (
	restoreStepChecked         = "restore: home checked"
	restoreStepExtracted       = "restore: archive extracted"
	restoreStepMarked          = "restore: marker written"
	restoreStepFlatKVCleared   = "restore: FlatKV cleared"
	restoreStepEpochsInstalled = "restore: epoch files installed"
	restoreStepInstalled       = "restore: checkpoint installed"
)

// runRestore restores the archive at from, or on stdin when from is "-", into the home of t.
func runRestore(ctx context.Context, t target, from string, stdin io.Reader, stderr io.Writer) error {
	return restoreWith(ctx, t, from, stdin, stderr, noStop)
}

// restoreWith is runRestore with a hook that can stop it at each step.
func restoreWith(ctx context.Context, t target, from string, stdin io.Reader, stderr io.Writer, stop stepHook) error {
	if t.mode == tmcfg.ModeValidator {
		return errors.New("restore is for fullnodes; this home is configured as a validator")
	}
	lock, err := flatkv.LockRoot(t.layout.FlatKVDir)
	if err != nil {
		return fmt.Errorf("lock the FlatKV root (is a node running on this home?): %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	staging := filepath.Join(t.layout.PersistentStateDir, restoreStagingDirName)
	if err := removeRestoreLeftovers(staging, t.layout.RestoreMarkerFile); err != nil {
		return err
	}
	resume, err := checkHome(t.layout, lock)
	if err != nil {
		return err
	}
	if err := stop(restoreStepChecked); err != nil {
		return err
	}

	in, closeIn, err := openSource(from, stdin)
	if err != nil {
		return err
	}
	defer closeIn()
	archive, m, err := openArchive(ctx, in)
	if err != nil {
		return err
	}
	defer archive.Close()
	if err := checkManifest(m, t); err != nil {
		return err
	}
	if err := requireFreeSpace(t.layout.PersistentStateDir, m.totalSize()); err != nil {
		return err
	}
	if err := os.Mkdir(staging, 0o750); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()
	if err := archive.extract(m, staging); err != nil {
		return err
	}
	checkpoint := filepath.Join(staging, flatkvPrefix, m.CheckpointDir)
	if err := flatkv.VerifySnapshotDir(checkpoint, m.CheckpointHeight); err != nil {
		return fmt.Errorf("verify the archived checkpoint: %w", err)
	}
	if err := stop(restoreStepExtracted); err != nil {
		return err
	}

	// From the marker on, a restore that stops leaves a home that refuses to start until restore runs
	// again, never a home that starts without seeding.
	if err := writeMarker(t.layout.RestoreMarkerFile, m.CheckpointHeight); err != nil {
		return fmt.Errorf("write the restore marker: %w", err)
	}
	if err := stop(restoreStepMarked); err != nil {
		return err
	}
	if resume {
		if err := clearEarlierRestore(lock, t.layout.EpochRegistryFiles, stop); err != nil {
			return err
		}
	}
	if err := installArchive(lock, t.layout.EpochRegistryFiles, staging, m, stop); err != nil {
		return err
	}
	if err := stop(restoreStepInstalled); err != nil {
		return err
	}
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("remove staging dir: %w", err)
	}

	if err := printHeight(stderr, m.CheckpointHeight); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stderr, "Restored height %d. Start the node; on start it fetches the CommitQC that covers %d and syncs from there.\n",
		m.CheckpointHeight, m.CheckpointHeight)
	return nil
}

// removeRestoreLeftovers removes the staging dir and the temporary marker that a restore which stopped
// before its cleanup left.
func removeRestoreLeftovers(staging, marker string) error {
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("remove the staging dir of an earlier restore: %w", err)
	}
	if err := os.Remove(marker + ".tmp"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove the temporary marker of an earlier restore: %w", err)
	}
	return nil
}

// checkHome returns an error if the home has block, receipt or state data, or has FlatKV or epoch data
// without the restore marker. It reports whether the marker is present, in which case the FlatKV and
// epoch data are from an earlier restore and restore replaces them.
func checkHome(layout node.AutobahnLayout, lock *flatkv.RootLock) (resume bool, err error) {
	stores := append(slices.Clone(layout.BlockDBDirs), layout.ReceiptStoreDir, layout.StateStoreDir, layout.StateStoreSnapshotDir, layout.HashVaultDir)
	var nonEmpty []string
	for _, dir := range stores {
		empty, err := dirEmptyOrMissing(dir)
		if err != nil {
			return false, err
		}
		if !empty {
			nonEmpty = append(nonEmpty, dir)
		}
	}
	if len(nonEmpty) > 0 {
		return false, fmt.Errorf("restore needs a home without block, receipt or state data; remove %s", strings.Join(nonEmpty, ", "))
	}

	// Only restore writes the marker, and the seed removes it only after it writes the block store, so
	// with an empty block store the marker means the FlatKV and epoch data are restore's own.
	if _, err := os.Stat(layout.RestoreMarkerFile); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat the restore marker: %w", err)
	}
	empty, err := lock.Empty()
	if err != nil {
		return false, err
	}
	if !empty {
		nonEmpty = append(nonEmpty, layout.FlatKVDir)
	}
	for _, p := range layout.EpochRegistryFiles {
		info, err := os.Stat(p)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, fmt.Errorf("stat epoch file: %w", err)
		}
		// The registry creates 0-byte files on every start.
		if info.Size() > 0 {
			nonEmpty = append(nonEmpty, p)
		}
	}
	if len(nonEmpty) > 0 {
		return false, fmt.Errorf("this home holds Autobahn state that restore did not write; remove %s and run restore again",
			strings.Join(nonEmpty, ", "))
	}
	return false, nil
}

func dirEmptyOrMissing(dir string) (bool, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

func openSource(from string, stdin io.Reader) (io.Reader, func(), error) {
	if from == "-" {
		return stdin, func() {}, nil
	}
	f, err := os.Open(from) //nolint:gosec // from is the operator's path.
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
}

// checkManifest refuses an archive of another chain, or one whose epoch files are not the ones this
// home keeps.
func checkManifest(m *manifest, t target) error {
	if m.ChainID != t.chainID {
		return fmt.Errorf("archive chain id %q does not match genesis chain id %q", m.ChainID, t.chainID)
	}
	want := make([]string, 0, len(t.layout.EpochRegistryFiles))
	for _, p := range t.layout.EpochRegistryFiles {
		want = append(want, filepath.Base(p))
	}
	got := m.epochFileNames()
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		return fmt.Errorf("archive epoch files %v, want %v", got, want)
	}
	return nil
}

// requireFreeSpace refuses when the filesystem of dir has less than size bytes free.
func requireFreeSpace(dir string, size int64) error {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return fmt.Errorf("statfs %s: %w", dir, err)
	}
	free := st.Bavail * uint64(st.Bsize) //nolint:gosec // block size is positive.
	if uint64(size) > free {             //nolint:gosec // size is a sum of validated non-negative sizes.
		return fmt.Errorf("the archive holds %d bytes, and %s has %d bytes free", size, dir, free)
	}
	return nil
}

// writeMarker writes height to path through a fsynced temporary file.
func writeMarker(path string, height int64) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // path is from the node layout.
	if err != nil {
		return err
	}
	if _, err := f.WriteString(strconv.FormatInt(height, 10) + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return syncPath(filepath.Dir(path))
}

// clearEarlierRestore removes the FlatKV and epoch data of an earlier restore. FlatKV goes first, so a
// stop between the two leaves app height 0, never a FlatKV checkpoint without its epoch files.
func clearEarlierRestore(lock *flatkv.RootLock, epochFiles []string, stop stepHook) error {
	if err := lock.Clear(); err != nil {
		return fmt.Errorf("clear the FlatKV data of an earlier restore: %w", err)
	}
	if err := stop(restoreStepFlatKVCleared); err != nil {
		return err
	}
	if err := removeFiles(epochFiles); err != nil {
		return fmt.Errorf("remove the epoch files of an earlier restore: %w", err)
	}
	return nil
}

// installArchive moves the staged epoch files, then the staged checkpoint, into place. On failure it
// removes the epoch files it moved; InstallSnapshot undoes its own move.
func installArchive(lock *flatkv.RootLock, epochFiles []string, staging string, m *manifest, stop stepHook) (err error) {
	defer func() {
		if err != nil {
			_ = removeFiles(epochFiles)
		}
	}()
	if err := installEpochFiles(filepath.Join(staging, epochsPrefix), epochFiles); err != nil {
		return err
	}
	if err := stop(restoreStepEpochsInstalled); err != nil {
		return err
	}
	if err := lock.InstallSnapshot(filepath.Join(staging, flatkvPrefix, m.CheckpointDir), m.CheckpointHeight); err != nil {
		return fmt.Errorf("install the checkpoint: %w", err)
	}
	return nil
}

// installEpochFiles renames each staged epoch file over its path in files.
func installEpochFiles(staged string, files []string) error {
	if len(files) == 0 {
		return nil
	}
	dir := filepath.Dir(files[0])
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	for _, p := range files {
		if err := os.Rename(filepath.Join(staged, filepath.Base(p)), p); err != nil {
			return fmt.Errorf("install epoch file: %w", err)
		}
	}
	return syncPath(dir)
}

// removeFiles removes each file in files that exists, then fsyncs their directories.
func removeFiles(files []string) error {
	dirs := map[string]struct{}{}
	for _, p := range files {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		dirs[filepath.Dir(p)] = struct{}{}
	}
	for d := range dirs {
		if err := syncPath(d); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
