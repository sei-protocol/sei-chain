package statesyncsnapshot

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
)

// encoderConcurrency bounds the zstd encoder goroutines, so packing does not take every core of a
// running node.
const encoderConcurrency = 2

// source is one file to pack: its archive path and where to read it.
type source struct {
	archivePath string
	diskPath    string
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

// buildManifest hashes every source and returns the manifest that lists them.
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
	f, err := os.Open(p) //nolint:gosec // p is a file under the staging dir.
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

// writeArchive writes the manifest and then every source in manifest order, as tar in zstd. It hashes
// each file again while it writes, so a file that changed after the manifest was built fails.
func writeArchive(ctx context.Context, w io.Writer, m *manifest, sources []source) error {
	zw, err := zstd.NewWriter(w, zstd.WithEncoderConcurrency(encoderConcurrency))
	if err != nil {
		return err
	}
	defer func() { _ = zw.Close() }()
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
	f, err := os.Open(s.diskPath) //nolint:gosec // diskPath is a file under the staging dir.
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
