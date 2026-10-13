package statesyncsnapshot

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/klauspost/compress/zstd"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

const restoreStagingDir = ".state-sync-restore"

func runRestore(ctx context.Context, t target, from string, stdin io.Reader, stderr io.Writer) error {
	if t.mode == tmcfg.ModeValidator {
		return errors.New("restore is for fullnodes; this home is configured as a validator")
	}
	if err := os.MkdirAll(t.layout.FlatKVDir, 0o750); err != nil {
		return fmt.Errorf("create the FlatKV root: %w", err)
	}
	lock, err := flatkv.LockRoot(t.layout.FlatKVDir)
	if err != nil {
		return fmt.Errorf("lock the FlatKV root (is a node running on this home?): %w", err)
	}
	defer func() { _ = lock.Unlock() }()
	if err := requireFreshHome(t, lock); err != nil {
		return err
	}

	in, closeIn, err := openSource(from, stdin)
	if err != nil {
		return err
	}
	defer closeIn()

	staging := filepath.Join(t.layout.PersistentStateDir, restoreStagingDir)
	if err := os.RemoveAll(staging); err != nil {
		return fmt.Errorf("remove a stale staging dir: %w", err)
	}
	if err := os.MkdirAll(staging, 0o750); err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(staging) }()

	m, err := extractArchive(ctx, in, staging)
	if err != nil {
		return err
	}
	if m.ChainID != t.chainID {
		return fmt.Errorf("archive chain id %q does not match genesis chain id %q", m.ChainID, t.chainID)
	}
	checkpoint := filepath.Join(staging, flatkvPrefix, m.CheckpointDir)
	if err := flatkv.VerifySnapshotDir(checkpoint, m.CheckpointHeight); err != nil {
		return fmt.Errorf("verify the archived checkpoint: %w", err)
	}

	installed, err := installEpochFiles(filepath.Join(staging, epochsPrefix), t.layout.EpochsDir)
	if err != nil {
		removeAll(installed)
		return err
	}
	if err := lock.InstallSnapshot(checkpoint, m.CheckpointHeight); err != nil {
		removeAll(installed)
		return fmt.Errorf("install the checkpoint: %w", err)
	}
	if err := writeMarker(t.layout.RestoredMarker, m.CheckpointHeight); err != nil {
		return fmt.Errorf("write the restore marker: %w", err)
	}
	if err := printHeight(stderr, m.CheckpointHeight); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stderr, "Restored height %d. Start the node; on start it fetches the CommitQC that covers %d and syncs from there.\n", m.CheckpointHeight, m.CheckpointHeight)
	return nil
}

func openSource(from string, stdin io.Reader) (io.Reader, func(), error) {
	if from == "-" {
		return stdin, func() {}, nil
	}
	f, err := os.Open(from) //nolint:gosec // operator-chosen path.
	if err != nil {
		return nil, nil, err
	}
	return f, func() { _ = f.Close() }, nil
}

// requireFreshHome refuses a home that holds any Autobahn data or a restore marker.
func requireFreshHome(t target, lock *flatkv.RootLock) error {
	empty, err := lock.Empty()
	if err != nil {
		return err
	}
	if !empty {
		return fmt.Errorf("the FlatKV root %s is not empty", t.layout.FlatKVDir)
	}
	for _, dir := range []string{t.layout.BlockDBDir, t.layout.ReceiptStoreDir, t.layout.StateStoreDir, t.layout.HashVaultDir} {
		ok, err := dirEmptyOrMissing(dir)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%s is not empty; restore needs a fresh home", dir)
		}
	}
	entries, err := os.ReadDir(t.layout.EpochsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			return err
		}
		// The registry creates 0-byte files on every start.
		if info.Size() > 0 {
			return fmt.Errorf("epoch file %s is not empty; restore needs a fresh home", filepath.Join(t.layout.EpochsDir, e.Name()))
		}
	}
	if _, err := os.Stat(t.layout.RestoredMarker); err == nil {
		return fmt.Errorf("%s exists: a restore already completed on this home", t.layout.RestoredMarker)
	}
	return nil
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

// extractArchive reads the archive once, in order, into staging and returns its manifest.
// Every file is listed, unique, of the listed size and hash, and fsynced.
func extractArchive(ctx context.Context, r io.Reader, staging string) (*manifest, error) {
	zr, err := zstd.NewReader(ctxReader{ctx: ctx, r: r})
	if err != nil {
		return nil, fmt.Errorf("open zstd: %w", err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)

	hdr, err := tr.Next()
	if err != nil {
		return nil, fmt.Errorf("read the first archive entry: %w", err)
	}
	if hdr.Name != manifestName || hdr.Typeflag != tar.TypeReg || hdr.Size > maxManifestSize {
		return nil, fmt.Errorf("the first archive entry is %q, want %s", hdr.Name, manifestName)
	}
	data, err := io.ReadAll(io.LimitReader(tr, maxManifestSize))
	if err != nil {
		return nil, fmt.Errorf("read the manifest: %w", err)
	}
	m, err := decodeManifest(data)
	if err != nil {
		return nil, err
	}
	want := make(map[string]manifestFile, len(m.Files))
	for _, f := range m.Files {
		want[f.Path] = f
	}
	dirs := map[string]struct{}{staging: {}}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read the archive: %w", err)
		}
		mf, ok := want[hdr.Name]
		if !ok {
			return nil, fmt.Errorf("archive entry %q is not listed in the manifest, or is repeated", hdr.Name)
		}
		delete(want, hdr.Name)
		if hdr.Typeflag != tar.TypeReg || hdr.Size != mf.Size {
			return nil, fmt.Errorf("archive entry %q: type %c size %d, want a regular file of size %d", hdr.Name, hdr.Typeflag, hdr.Size, mf.Size)
		}
		dst := filepath.Join(staging, filepath.FromSlash(mf.Path))
		if err := mkdirAllTracked(filepath.Dir(dst), staging, dirs); err != nil {
			return nil, err
		}
		if err := extractFile(tr, dst, mf); err != nil {
			return nil, err
		}
	}
	if len(want) > 0 {
		missing := make([]string, 0, len(want))
		for p := range want {
			missing = append(missing, p)
		}
		slices.Sort(missing)
		return nil, fmt.Errorf("the archive ends before %d listed files, the first is %q", len(missing), missing[0])
	}
	// Deepest first, so a parent is synced after its children's entries exist.
	ordered := make([]string, 0, len(dirs))
	for d := range dirs {
		ordered = append(ordered, d)
	}
	slices.SortFunc(ordered, func(a, b string) int {
		return strings.Count(b, string(filepath.Separator)) - strings.Count(a, string(filepath.Separator))
	})
	for _, d := range ordered {
		if err := syncPath(d); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func mkdirAllTracked(dir, staging string, dirs map[string]struct{}) error {
	for d := dir; d != staging && strings.HasPrefix(d, staging); d = filepath.Dir(d) {
		if _, ok := dirs[d]; ok {
			break
		}
		dirs[d] = struct{}{}
	}
	return os.MkdirAll(dir, 0o750)
}

func extractFile(r io.Reader, dst string, mf manifestFile) (err error) {
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // dst is a validated manifest path under staging.
	if err != nil {
		return fmt.Errorf("create %s: %w", mf.Path, err)
	}
	defer func() {
		if cerr := f.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("close %s: %w", mf.Path, cerr)
		}
	}()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r, mf.Size+1))
	if err != nil {
		return fmt.Errorf("extract %s: %w", mf.Path, err)
	}
	if n != mf.Size {
		return fmt.Errorf("extract %s: size %d, want %d", mf.Path, n, mf.Size)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != mf.SHA256 {
		return fmt.Errorf("extract %s: sha256 %s, want %s", mf.Path, got, mf.SHA256)
	}
	return f.Sync()
}

// installEpochFiles moves the staged epoch files into dir and returns the paths it installed.
func installEpochFiles(staged, dir string) ([]string, error) {
	entries, err := os.ReadDir(staged)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("the archive has no epoch files")
	}
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	var installed []string
	for _, e := range entries {
		dst := filepath.Join(dir, e.Name())
		if err := os.Rename(filepath.Join(staged, e.Name()), dst); err != nil {
			return installed, fmt.Errorf("install epoch file %s: %w", e.Name(), err)
		}
		installed = append(installed, dst)
	}
	return installed, syncPath(dir)
}

func removeAll(paths []string) {
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

func writeMarker(path string, height int64) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // path from the node layout.
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
	return syncPath(filepath.Dir(path))
}

func syncPath(p string) error {
	f, err := os.Open(p) //nolint:gosec // path under the node home.
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("fsync %s: %w", p, err)
	}
	return f.Close()
}
