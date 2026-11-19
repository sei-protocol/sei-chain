package statesync

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
	"time"

	"golang.org/x/sys/unix"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	flatkvconfig "github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/statesyncproof"
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
)

const importStagingDirName = ".state-sync-import"

// Steps of import at which a stepHook is called, in the order import reaches them.
const (
	importStepChecked        = "import: home checked"
	importStepVaultCleared   = "import: app-hash vault cleared"
	importStepFlatKVCleared  = "import: FlatKV cleared"
	importStepImported       = "import: FlatKV imported"
	importStepMarked         = "import: marker written"
	importStepProofInstalled = "import: state sync proof installed"
	importStepAnchorStored   = "import: restore anchor stored"
	importStepInstalled      = "import: checkpoint installed"
)

// runImport imports the archive at from, or on stdin when from is "-", into the home of t.
func runImport(ctx context.Context, t target, from string, stdin io.Reader, stderr io.Writer) error {
	return importWith(ctx, t, from, stdin, stderr, noStop)
}

// importWith is runImport with a hook that can stop it at each step.
func importWith(ctx context.Context, t target, from string, stdin io.Reader, stderr io.Writer, stop stepHook) error {
	if t.mode == tmcfg.ModeValidator {
		return errors.New("import is for fullnodes; this home is configured as a validator")
	}
	lock, err := flatkv.LockRoot(t.layout.FlatKVDir)
	if err != nil {
		return fmt.Errorf("lock the FlatKV root (is a node running on this home?): %w", err)
	}
	defer func() { _ = lock.Unlock() }()

	staging := filepath.Join(t.layout.PersistentStateDir, importStagingDirName)
	if err := removeImportLeftovers(staging, t.layout); err != nil {
		return err
	}
	resume, err := checkHome(t.layout, lock)
	if err != nil {
		return err
	}
	if err := stop(importStepChecked); err != nil {
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
	if resume {
		// The vault accepts the restore anchor only as the first record of an empty vault, so the
		// anchor of the earlier import goes before the new one is stored.
		if err := clearEarlierImport(t.layout, lock, stop); err != nil {
			return err
		}
	}
	if err := requireFreeSpace(t.layout.PersistentStateDir, m.StateBytes); err != nil {
		return err
	}
	if err := os.Mkdir(staging, 0o750); err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(staging) }()
	staged, err := stageArchive(ctx, archive, m, t.epochGenesis, staging, stderr)
	if err != nil {
		return err
	}
	if err := stop(importStepImported); err != nil {
		return err
	}
	if err := installRestore(lock, t, m, staged, staging, stop); err != nil {
		return err
	}
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("remove staging dir: %w", err)
	}

	if err := printHeight(stderr, m.CheckpointHeight); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stderr, "Imported height %d.\n", m.CheckpointHeight)
	return nil
}

// installRestore installs the checkpoint of m from staging into the home of t. It writes the pending
// restore marker, the state sync proof file, the restore anchor and last the FlatKV checkpoint, so a
// home whose FlatKV is at the height of m also holds the other three.
func installRestore(lock *flatkv.RootLock, t target, m *manifest, staged stagedArchive, staging string, stop stepHook) error {
	if err := writeFileAtomic(t.layout.PendingRestoreFile, []byte(strconv.FormatInt(m.CheckpointHeight, 10)+"\n")); err != nil {
		return fmt.Errorf("write the pending restore marker: %w", err)
	}
	if err := stop(importStepMarked); err != nil {
		return err
	}
	if err := writeFileAtomic(t.layout.RestoreProofFile, staged.proof); err != nil {
		return fmt.Errorf("install the state sync proof file: %w", err)
	}
	if err := stop(importStepProofInstalled); err != nil {
		return err
	}
	if err := node.StoreRestoreAnchor(t.layout, t.epochGenesis, m.CheckpointHeight, staged.appHash); err != nil {
		return err
	}
	if err := stop(importStepAnchorStored); err != nil {
		return err
	}
	if err := installArchive(lock, staging, m); err != nil {
		return err
	}
	return stop(importStepInstalled)
}

// removeImportLeftovers removes the staging dir and the temporary files that an import which
// stopped before its cleanup left.
func removeImportLeftovers(staging string, layout node.AutobahnLayout) error {
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("remove the staging dir of an earlier import: %w", err)
	}
	for _, path := range []string{layout.PendingRestoreFile, layout.RestoreProofFile} {
		if err := os.Remove(path + ".tmp"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove a temporary file of an earlier import: %w", err)
		}
	}
	return nil
}

// checkHome returns an error if the home has block, receipt, state or hash log data, or has FlatKV
// data or app hashes without the pending restore marker. It reports whether the marker is present,
// in which case that data is from an earlier import and import replaces it.
func checkHome(layout node.AutobahnLayout, lock *flatkv.RootLock) (resume bool, err error) {
	stores := append(slices.Clone(layout.BlockDBDirs), layout.ReceiptStoreDir, layout.StateStoreDir, layout.AppHashLogDir)
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
		return false, fmt.Errorf("import needs a home without block, receipt, state or hash log data; remove %s", strings.Join(nonEmpty, ", "))
	}

	// Only import writes the marker, before it replaces the vault and FlatKV, and the seed deletes
	// it only after it writes block data. With an empty block store the marker means both are
	// import's own, or import stopped before it installed them and may retry.
	if _, err := os.Stat(layout.PendingRestoreFile); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat the pending restore marker: %w", err)
	}
	flatKVEmpty, err := lock.Empty()
	if err != nil {
		return false, err
	}
	if !flatKVEmpty {
		nonEmpty = append(nonEmpty, layout.FlatKVDir)
	}
	vaultEmpty, err := dirEmptyOrMissing(layout.AppHashVaultDir)
	if err != nil {
		return false, err
	}
	if !vaultEmpty {
		nonEmpty = append(nonEmpty, layout.AppHashVaultDir)
	}
	if len(nonEmpty) > 0 {
		return false, fmt.Errorf("this home holds Autobahn state that import did not write; remove %s and run import again",
			strings.Join(nonEmpty, ", "))
	}
	return false, nil
}

// removeDir removes dir and everything under it, and syncs its parent.
func removeDir(dir string) error {
	if _, err := os.Lstat(dir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return syncPath(filepath.Dir(dir))
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

// checkManifest refuses an archive of another chain.
func checkManifest(m *manifest, t target) error {
	if m.ChainID != t.chainID {
		return fmt.Errorf("archive chain id %q does not match genesis chain id %q", m.ChainID, t.chainID)
	}
	return nil
}

// requireFreeSpace refuses when the filesystem of dir has less than size bytes free. It is a lower
// limit: the import also writes a WAL and compacts.
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

// stagedArchive is what import installs from an archive that stageArchive checked.
type stagedArchive struct {
	// proof is the state sync proof file of the checkpoint.
	proof []byte
	// appHash is the app hash of the checkpoint that the AppQC in proof signs.
	appHash [32]byte
}

// stageArchive imports the archive into staging, opens the epoch registry of that checkpoint, and
// verifies the state sync proof file of the checkpoint against that registry.
func stageArchive(
	ctx context.Context,
	archive *archiveReader,
	m *manifest,
	genesis node.EpochGenesis,
	staging string,
	stderr io.Writer,
) (stagedArchive, error) {
	proof, err := readArchivedProof(archive, m.CheckpointHeight)
	if err != nil {
		return stagedArchive{}, err
	}
	imported, err := importArchivedRows(ctx, archive, m, filepath.Join(staging, flatkvPrefix))
	if err != nil {
		return stagedArchive{}, err
	}
	_, _ = fmt.Fprintf(stderr, "FlatKV import complete: %d keys, %d bytes in %s\n",
		m.RowCount, m.StateBytes, imported.Round(time.Millisecond))
	checkpoint := filepath.Join(staging, flatkvPrefix, flatkv.SnapshotDirName(m.CheckpointHeight))
	if err := flatkv.VerifySnapshotDir(checkpoint, m.CheckpointHeight); err != nil {
		return stagedArchive{}, fmt.Errorf("verify the imported checkpoint: %w", err)
	}
	if err := requireImportedStats(checkpoint, m); err != nil {
		return stagedArchive{}, err
	}
	epochs, err := node.OpenCheckpointEpochs(genesis, checkpoint, m.CheckpointHeight)
	if err != nil {
		return stagedArchive{}, fmt.Errorf("open the epoch registry: %w", err)
	}
	appHash, err := node.VerifyStateSyncProof(epochs, m.CheckpointHeight, proof)
	if err != nil {
		return stagedArchive{}, fmt.Errorf("verify the archived state sync proof file: %w", err)
	}
	return stagedArchive{proof: proof, appHash: appHash}, nil
}

// requireImportedStats returns an error unless the metadata of the imported checkpoint records the
// row count, byte count and state hash of m.
func requireImportedStats(checkpoint string, m *manifest) error {
	got, err := flatkv.SnapshotDirStats(checkpoint, m.CheckpointHeight)
	if err != nil {
		return fmt.Errorf("read the stats of the imported checkpoint: %w", err)
	}
	if want := m.stats(); got != want {
		return fmt.Errorf("the imported checkpoint holds %s, the manifest records %s", formatStats(got), formatStats(want))
	}
	return nil
}

// readArchivedProof returns the archived state sync proof file of height, after it checks that the
// file decodes as the road block and AppQC of height.
func readArchivedProof(archive *archiveReader, height int64) ([]byte, error) {
	data, err := archive.readProof(height)
	if err != nil {
		return nil, err
	}
	if _, err := statesyncproof.Decode(data, atypes.RoadIndex(height)); err != nil { //nolint:gosec // heights are positive.
		return nil, fmt.Errorf("decode the archived state sync proof file: %w", err)
	}
	return data, nil
}

func importArchivedRows(ctx context.Context, archive *archiveReader, m *manifest, root string) (time.Duration, error) {
	start := time.Now()
	cfg := flatkvconfig.DefaultConfig()
	cfg.DataDir = root
	store, err := flatkv.NewCommitStore(ctx, cfg, nil)
	if err != nil {
		return 0, fmt.Errorf("create FlatKV store for import: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = store.Close()
		}
	}()
	if err := store.LoadLatest(); err != nil {
		return 0, fmt.Errorf("open FlatKV store for import: %w", err)
	}
	importer, err := store.Importer(m.CheckpointHeight)
	if err != nil {
		return 0, fmt.Errorf("create FlatKV importer: %w", err)
	}
	if err := archive.importRows(m, importer); err != nil {
		_ = importer.Abort(err)
		return 0, err
	}
	if err := importer.Close(); err != nil {
		return 0, fmt.Errorf("finish FlatKV import: %w", err)
	}
	if err := store.Close(); err != nil {
		return 0, fmt.Errorf("close FlatKV store after import: %w", err)
	}
	closed = true
	return time.Since(start), nil
}

// clearEarlierImport removes the app hashes and then the FlatKV data of an earlier import.
func clearEarlierImport(layout node.AutobahnLayout, lock *flatkv.RootLock, stop stepHook) error {
	if err := removeDir(layout.AppHashVaultDir); err != nil {
		return fmt.Errorf("clear the app-hash vault of an earlier import: %w", err)
	}
	if err := stop(importStepVaultCleared); err != nil {
		return err
	}
	if err := lock.Clear(); err != nil {
		return fmt.Errorf("clear the FlatKV data of an earlier import: %w", err)
	}
	if err := stop(importStepFlatKVCleared); err != nil {
		return err
	}
	return nil
}

// installArchive moves the staged checkpoint into place.
func installArchive(lock *flatkv.RootLock, staging string, m *manifest) error {
	if err := lock.InstallSnapshot(filepath.Join(staging, flatkvPrefix, flatkv.SnapshotDirName(m.CheckpointHeight)), m.CheckpointHeight); err != nil {
		return fmt.Errorf("install the checkpoint: %w", err)
	}
	return nil
}
