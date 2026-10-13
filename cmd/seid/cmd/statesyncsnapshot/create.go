package statesyncsnapshot

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"

	cosmosversion "github.com/sei-protocol/sei-chain/sei-cosmos/version"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv"
	"github.com/sei-protocol/sei-chain/sei-tendermint/node"
)

const (
	createStagingDir = ".state-sync-create"
	pinAttempts      = 3
	// registryMargin covers the gap between the commit of a checkpoint and the mtime of its directory.
	registryMargin = time.Minute
)

// source is one file to pack: its archive path and where to read it.
type source struct {
	archivePath string
	diskPath    string
}

func runCreate(ctx context.Context, t target, out string, height int64, stdout, stderr io.Writer) error {
	stagingRoot := filepath.Join(t.layout.PersistentStateDir, createStagingDir)
	if err := os.MkdirAll(stagingRoot, 0o750); err != nil {
		return fmt.Errorf("create staging root: %w", err)
	}
	stage, err := os.MkdirTemp(stagingRoot, "create-")
	if err != nil {
		return fmt.Errorf("create staging dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()

	version, err := pinCheckpoint(t.layout.FlatKVDir, height, stage)
	if err != nil {
		return err
	}
	checkpointDir := flatkv.SnapshotDirName(version)
	pinned := filepath.Join(stage, flatkvPrefix, checkpointDir)
	if err := flatkv.VerifySnapshotDir(pinned, version); err != nil {
		return fmt.Errorf("verify the pinned checkpoint: %w", err)
	}
	if err := copyEpochFiles(t.layout.EpochsDir, filepath.Join(stage, epochsPrefix)); err != nil {
		return err
	}
	if err := requireRegistryCoversCheckpoint(t.layout, version); err != nil {
		return err
	}

	sources, err := collectSources(stage)
	if err != nil {
		return err
	}
	m, err := buildManifest(ctx, t.chainID, version, sources)
	if err != nil {
		return err
	}
	if out == "-" {
		if err := printHeight(stderr, version); err != nil {
			return err
		}
		return writeArchive(ctx, stdout, m, sources)
	}
	if err := writeArchiveFile(ctx, out, m, sources); err != nil {
		return err
	}
	return printHeight(stderr, version)
}

// pinCheckpoint hard-links the checkpoint at height (or the current one when height is 0)
// into stage/flatkv/ and returns its version. The current checkpoint can be pruned while
// it is pinned, so an unset height retries.
func pinCheckpoint(root string, height int64, stage string) (int64, error) {
	if err := os.MkdirAll(filepath.Join(stage, flatkvPrefix), 0o750); err != nil {
		return 0, err
	}
	var lastErr error
	for attempt := 0; attempt < pinAttempts; attempt++ {
		version := height
		if version == 0 {
			v, err := flatkv.ActiveSnapshotVersion(root)
			if err != nil {
				return 0, fmt.Errorf("find the current checkpoint (is there a checkpoint yet?): %w", err)
			}
			version = v
		}
		dst := filepath.Join(stage, flatkvPrefix, flatkv.SnapshotDirName(version))
		err := flatkv.PinSnapshot(root, version, dst)
		if err == nil {
			return version, nil
		}
		lastErr = err
		if height != 0 || !errors.Is(err, os.ErrNotExist) {
			break
		}
	}
	return 0, fmt.Errorf("pin the checkpoint: %w", lastErr)
}

// copyEpochFiles byte-copies the epoch registry files: the registry rewrites them in place,
// so a hard link would not keep their content.
func copyEpochFiles(src, dst string) error {
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read epoch dir %s: %w", src, err)
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".pb") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name())) //nolint:gosec // operator-owned node home.
		if err != nil {
			return fmt.Errorf("read epoch file: %w", err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), data, 0o600); err != nil {
			return fmt.Errorf("copy epoch file: %w", err)
		}
	}
	return nil
}

// requireRegistryCoversCheckpoint returns an error if an epoch registry file was written after the
// checkpoint at version. Such a registry can have dropped the checkpoint's epoch, and a node
// restored from it cannot verify the CommitQC at the checkpoint height.
func requireRegistryCoversCheckpoint(layout node.AutobahnLayout, version int64) error {
	cp, err := os.Stat(filepath.Join(layout.FlatKVDir, flatkv.SnapshotDirName(version)))
	if err != nil {
		return fmt.Errorf("stat checkpoint %d: %w", version, err)
	}
	entries, err := os.ReadDir(layout.EpochsDir)
	if err != nil {
		return fmt.Errorf("read epoch dir %s: %w", layout.EpochsDir, err)
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			return fmt.Errorf("stat epoch file %s: %w", e.Name(), err)
		}
		if info.Size() == 0 || !info.ModTime().After(cp.ModTime().Add(-registryMargin)) {
			continue
		}
		return fmt.Errorf("epoch file %s changed after checkpoint %d (mtime %s, checkpoint %s), so it may not hold the checkpoint's epoch; retry after the next checkpoint",
			e.Name(), version, info.ModTime().UTC().Format(time.RFC3339), cp.ModTime().UTC().Format(time.RFC3339))
	}
	return nil
}

// collectSources lists every regular file under stage except LOCK files, sorted by archive path.
func collectSources(stage string) ([]source, error) {
	var sources []source
	err := filepath.WalkDir(stage, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("%s is not a regular file", p)
		}
		if d.Name() == "LOCK" {
			return nil
		}
		rel, err := filepath.Rel(stage, p)
		if err != nil {
			return err
		}
		sources = append(sources, source{archivePath: filepath.ToSlash(rel), diskPath: p})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list files to pack: %w", err)
	}
	slices.SortFunc(sources, func(a, b source) int { return strings.Compare(a.archivePath, b.archivePath) })
	return sources, nil
}

func buildManifest(ctx context.Context, chainID string, height int64, sources []source) (*manifest, error) {
	m := &manifest{
		FormatVersion:    formatVersion,
		ChainID:          chainID,
		CheckpointHeight: height,
		CheckpointDir:    flatkv.SnapshotDirName(height),
		CreatedAt:        time.Now().UTC().Truncate(time.Second),
		ProducerVersion:  producerVersion(),
	}
	for _, s := range sources {
		size, sum, err := hashFile(ctx, s.diskPath)
		if err != nil {
			return nil, err
		}
		m.Files = append(m.Files, manifestFile{Path: s.archivePath, Size: size, SHA256: sum})
	}
	if err := m.validate(); err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	return m, nil
}

func producerVersion() string {
	if cosmosversion.Version == "" {
		return "unknown"
	}
	return cosmosversion.Version
}

func hashFile(ctx context.Context, p string) (int64, string, error) {
	f, err := os.Open(p) //nolint:gosec // operator-owned node home.
	if err != nil {
		return 0, "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, ctxReader{ctx: ctx, r: f})
	if err != nil {
		return 0, "", fmt.Errorf("hash %s: %w", p, err)
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

func writeArchiveFile(ctx context.Context, out string, m *manifest, sources []source) (err error) {
	partial := out + ".partial"
	f, err := os.OpenFile(partial, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) //nolint:gosec // operator-chosen path.
	if err != nil {
		return fmt.Errorf("create %s: %w", partial, err)
	}
	defer func() {
		if err != nil {
			_ = f.Close()
			_ = os.Remove(partial)
		}
	}()
	if err := writeArchive(ctx, f, m, sources); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(partial, out)
}

// writeArchive writes the manifest and then every source, in manifest order, as tar in zstd.
// It hashes each file again while it writes, so a file that changed since the manifest fails.
func writeArchive(ctx context.Context, w io.Writer, m *manifest, sources []source) error {
	zw, err := zstd.NewWriter(w, zstd.WithEncoderConcurrency(2))
	if err != nil {
		return err
	}
	tw := tar.NewWriter(zw)
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Name: manifestName, Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg, ModTime: m.CreatedAt}); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	for i, s := range sources {
		if err := writeEntry(ctx, tw, m.Files[i], s, m.CreatedAt); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}
	if err := zw.Close(); err != nil {
		return fmt.Errorf("close zstd: %w", err)
	}
	return nil
}

func writeEntry(ctx context.Context, tw *tar.Writer, mf manifestFile, s source, modTime time.Time) error {
	f, err := os.Open(s.diskPath) //nolint:gosec // operator-owned node home.
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := tw.WriteHeader(&tar.Header{Name: mf.Path, Mode: 0o600, Size: mf.Size, Typeflag: tar.TypeReg, ModTime: modTime}); err != nil {
		return err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tw, h), io.LimitReader(ctxReader{ctx: ctx, r: f}, mf.Size+1))
	if err != nil {
		return fmt.Errorf("pack %s: %w", mf.Path, err)
	}
	if n != mf.Size || hex.EncodeToString(h.Sum(nil)) != mf.SHA256 {
		return fmt.Errorf("pack %s: the file changed after the manifest was built", mf.Path)
	}
	return nil
}
