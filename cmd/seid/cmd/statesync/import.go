package statesync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	sctypes "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
	ssarchive "github.com/sei-protocol/sei-chain/sei-db/statesync"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
)

const importStagingDirName = ".state-sync-import"

// Steps of import at which a stepHook is called, in the order import reaches them.
const (
	importStepChecked        = "import: home checked"
	importStepFlatKVCleared  = "import: FlatKV cleared"
	importStepRewritten      = "import: checkpoint rewritten"
	importStepMarked         = "import: marker written"
	importStepProofInstalled = "import: state sync proof installed"
	importStepAnchorStored   = "import: restore anchor stored"
	importStepInstalled      = "import: checkpoint installed"
)

// runImport imports the archive of src into the home of t.
func runImport(ctx context.Context, t target, src source, stderr io.Writer) error {
	return importWith(ctx, t, src, stderr, noStop)
}

// importWith is runImport with a hook that can stop it at each step.
func importWith(ctx context.Context, t target, src source, stderr io.Writer, stop stepHook) error {
	if t.mode == tmcfg.ModeValidator {
		return errors.New("import is for fullnodes; this home is configured as a validator")
	}
	lock, err := flatkv.LockRoot(t.layout.FlatKVDir)
	if err != nil {
		return fmt.Errorf("lock the FlatKV root (is a node running on this home?): %w", err)
	}
	defer func() { _ = lock.Unlock() }()
	if err := requireSameFilesystem(t.layout.PersistentStateDir, t.layout.FlatKVDir); err != nil {
		return err
	}

	staging := filepath.Join(t.layout.PersistentStateDir, importStagingDirName)
	if err := removeImportLeftovers(staging, t.layout); err != nil {
		return err
	}
	resume, err := checkHome(t.layout, lock, staging)
	if err != nil {
		return err
	}
	if err := stop(importStepChecked); err != nil {
		return err
	}

	in, err := src.open(ctx)
	if err != nil {
		return fmt.Errorf("open %s: %w", src.name, err)
	}
	defer func() { _ = in.Close() }()
	if err := os.Mkdir(staging, 0o750); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()

	start := time.Now()
	download := filepath.Join(staging, "download")
	m, proof, err := ssarchive.ReadArchive(ctx, ctxReader{ctx: ctx, r: in}, download, func(m *ssarchive.Manifest) error {
		return prepareHome(t, lock, src, m, resume, stop)
	})
	if err != nil {
		return fmt.Errorf("read %s: %w", src.name, err)
	}
	_, _ = fmt.Fprintf(stderr, "Downloaded the checkpoint at height %d in %s\n", m.Height, time.Since(start).Round(time.Millisecond))
	if err := checkDownload(download, m, proof); err != nil {
		return err
	}
	start = time.Now()
	checkpoint, err := rewriteCheckpoint(ctx, download, m, filepath.Join(staging, "flatkv"))
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stderr, "Rewrote %d rows, %d bytes in %s\n", m.RowCount, m.StateBytes, time.Since(start).Round(time.Millisecond))
	if err := stop(importStepRewritten); err != nil {
		return err
	}
	if err := os.RemoveAll(download); err != nil {
		return fmt.Errorf("remove the downloaded checkpoint: %w", err)
	}
	if err := installRestore(lock, t, m.Height, proof, checkpoint, stop); err != nil {
		return err
	}
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("remove staging dir: %w", err)
	}
	_, _ = fmt.Fprintf(stderr, "Imported height %d.\n", m.Height)
	return nil
}

// prepareHome checks the manifest of the archive against the home of t and src, removes the FlatKV
// data of an earlier import when resume is set, and checks that the home has room for the import.
func prepareHome(t target, lock *flatkv.RootLock, src source, m *ssarchive.Manifest, resume bool, stop stepHook) error {
	if m.ChainID != t.chainID {
		return fmt.Errorf("archive chain id %q does not match genesis chain id %q", m.ChainID, t.chainID)
	}
	if src.height != 0 && m.Height != src.height {
		return fmt.Errorf("archive is at height %d, want %d", m.Height, src.height)
	}
	if resume {
		if err := lock.Clear(); err != nil {
			return fmt.Errorf("clear the FlatKV data of an earlier import: %w", err)
		}
		if err := stop(importStepFlatKVCleared); err != nil {
			return err
		}
	}
	// The downloaded checkpoint and the rewritten one are on disk together.
	return requireFreeSpace(t.layout.PersistentStateDir, 2*checkpointSize(m))
}

func checkpointSize(m *ssarchive.Manifest) int64 {
	var size int64
	for _, f := range m.Files {
		size += f.Size
	}
	return size
}

// checkDownload returns an error unless the downloaded checkpoint matches m and proof decodes as
// the state sync proof file of the height of m.
func checkDownload(download string, m *ssarchive.Manifest, proof []byte) error {
	if err := ssarchive.VerifyCheckpoint(download, m); err != nil {
		return fmt.Errorf("verify the downloaded checkpoint: %w", err)
	}
	if _, err := statesyncproof.Decode(proof, atypes.RoadHeight(m.Height)); err != nil { //nolint:gosec // the manifest height is positive.
		return fmt.Errorf("decode the archived state sync proof file: %w", err)
	}
	return nil
}

// rewriteCheckpoint writes every row of the checkpoint in download into a new FlatKV store in root,
// and returns the checkpoint that the store writes at the height of m. It returns an error unless
// that checkpoint holds the stats of m.
func rewriteCheckpoint(ctx context.Context, download string, m *ssarchive.Manifest, root string) (string, error) {
	cfg := flatkvconfig.DefaultConfig()
	cfg.DataDir = root
	store, err := flatkv.NewCommitStore(ctx, cfg, nil)
	if err != nil {
		return "", fmt.Errorf("create FlatKV store for import: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = store.Close()
		}
	}()
	if err := store.LoadLatest(); err != nil {
		return "", fmt.Errorf("open FlatKV store for import: %w", err)
	}
	importer, err := store.Importer(m.Height)
	if err != nil {
		return "", fmt.Errorf("create FlatKV importer: %w", err)
	}
	_, err = flatkv.ExportSnapshotDir(ctx, download, m.Height, func(key, value []byte) error {
		// The importer keeps the node after AddNode returns, and ExportSnapshotDir reuses key and value.
		return importer.AddNode(&sctypes.SnapshotNode{Key: bytes.Clone(key), Value: bytes.Clone(value), Version: m.Height})
	})
	if err != nil {
		_ = importer.Abort(err)
		return "", fmt.Errorf("rewrite the downloaded checkpoint: %w", err)
	}
	if err := importer.Close(); err != nil {
		return "", fmt.Errorf("finish FlatKV import: %w", err)
	}
	closed = true
	if err := store.Close(); err != nil {
		return "", fmt.Errorf("close FlatKV store after import: %w", err)
	}
	checkpoint := filepath.Join(root, flatkv.SnapshotDirName(m.Height))
	if err := ssarchive.VerifyCheckpoint(checkpoint, m); err != nil {
		return "", fmt.Errorf("verify the rewritten checkpoint: %w", err)
	}
	return checkpoint, nil
}

// installRestore installs the rewritten checkpoint at height into the home of t. It writes the
// restore marker, the state sync proof file, the restore anchor and last the FlatKV checkpoint, so
// a home whose FlatKV is at height also holds the other three.
func installRestore(lock *flatkv.RootLock, t target, height int64, proof []byte, checkpoint string, stop stepHook) error {
	layout := t.layout
	if err := writeFileAtomic(layout.RestoreMarkerFile, []byte(strconv.FormatInt(height, 10)+"\n")); err != nil {
		return fmt.Errorf("write the restore marker: %w", err)
	}
	if err := stop(importStepMarked); err != nil {
		return err
	}
	if err := writeFileAtomic(layout.RestoreProofFile, proof); err != nil {
		return fmt.Errorf("install the state sync proof file: %w", err)
	}
	if err := stop(importStepProofInstalled); err != nil {
		return err
	}
	if err := node.StoreRestoreAnchor(layout, t.mode, t.storage, t.genDoc, height, proof); err != nil {
		return err
	}
	if err := stop(importStepAnchorStored); err != nil {
		return err
	}
	if err := lock.InstallSnapshot(checkpoint, height); err != nil {
		return fmt.Errorf("install the checkpoint: %w", err)
	}
	return stop(importStepInstalled)
}

// removeImportLeftovers removes the staging dir and the temporary files that an import which
// stopped before its cleanup left.
func removeImportLeftovers(staging string, layout node.AutobahnLayout) error {
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("remove the staging dir of an earlier import: %w", err)
	}
	for _, path := range []string{layout.RestoreMarkerFile, layout.RestoreProofFile} {
		if err := os.Remove(path + ".tmp"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove a temporary file of an earlier import: %w", err)
		}
	}
	return nil
}

// checkHome returns an error when the persistent state dir holds a file that import did not
// write, or FlatKV data without the restore marker. It reports whether the marker is present, in
// which case the FlatKV data is from an earlier import, which import replaces.
func checkHome(layout node.AutobahnLayout, lock *flatkv.RootLock, staging string) (resume bool, err error) {
	foreign, err := foreignFiles(layout, staging)
	if err != nil {
		return false, err
	}
	if len(foreign) > 0 {
		return false, fmt.Errorf("import needs a persistent state dir without other data; remove %s", strings.Join(foreign, ", "))
	}
	// Only import writes the marker, and it writes it before the FlatKV checkpoint. A node start
	// removes the marker before it commits a block, so the FlatKV data is import's own.
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
		return false, fmt.Errorf("the FlatKV root holds data that import did not write; remove %s and run import again", layout.FlatKVDir)
	}
	if _, err := os.Stat(layout.AppHashVaultDir); err == nil {
		return false, fmt.Errorf("the app-hash vault holds data that import did not write; remove %s and run import again", layout.AppHashVaultDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat the app-hash vault: %w", err)
	}
	return false, nil
}

// maxForeignFiles is the number of foreign files that checkHome names.
const maxForeignFiles = 5

// foreignFiles returns the first files under the persistent state dir that are not in the FlatKV
// root, the staging dir, the restore marker or the restore proof file.
func foreignFiles(layout node.AutobahnLayout, staging string) ([]string, error) {
	var foreign []string
	err := filepath.WalkDir(layout.PersistentStateDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case path == layout.FlatKVDir || path == layout.AppHashVaultDir || path == staging:
			return filepath.SkipDir
		case d.IsDir() || path == layout.RestoreMarkerFile || path == layout.RestoreProofFile:
			return nil
		}
		foreign = append(foreign, path)
		if len(foreign) == maxForeignFiles {
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read the persistent state dir: %w", err)
	}
	return foreign, nil
}

// requireSameFilesystem refuses when the persistent state dir psd, which holds the staging dir, and
// the FlatKV root flatKVDir are on different filesystems.
func requireSameFilesystem(psd, flatKVDir string) error {
	// The import installs the checkpoint with a rename from the staging dir into the FlatKV root.
	var a, b unix.Stat_t
	if err := unix.Stat(psd, &a); err != nil {
		return fmt.Errorf("stat %s: %w", psd, err)
	}
	if err := unix.Stat(flatKVDir, &b); err != nil {
		return fmt.Errorf("stat %s: %w", flatKVDir, err)
	}
	if a.Dev != b.Dev {
		return fmt.Errorf("import stages in %s, which is not on the filesystem of the FlatKV root %s; put both on one filesystem", psd, flatKVDir)
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
		return fmt.Errorf("import needs %d bytes, and %s has %d bytes free", size, dir, free)
	}
	return nil
}

// writeFileAtomic writes data to path through a fsynced temporary file.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // path is from the node layout.
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
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

// syncPath fsyncs the file or directory at p.
func syncPath(p string) error {
	f, err := os.Open(p) //nolint:gosec // p is a path under the node home.
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("fsync %s: %w", p, err)
	}
	return f.Close()
}
