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
	"strings"

	"github.com/klauspost/compress/zstd"
)

// archiveReader reads an archive stream once, in order.
type archiveReader struct {
	zr *zstd.Decoder
	tr *tar.Reader
}

// openArchive reads the manifest, which must be the first entry of the archive in r.
func openArchive(ctx context.Context, r io.Reader) (*archiveReader, *manifest, error) {
	zr, err := zstd.NewReader(ctxReader{ctx: ctx, r: r})
	if err != nil {
		return nil, nil, fmt.Errorf("open zstd: %w", err)
	}
	a := &archiveReader{zr: zr, tr: tar.NewReader(zr)}
	m, err := a.readManifest()
	if err != nil {
		a.Close()
		return nil, nil, err
	}
	return a, m, nil
}

// Close releases the decoder.
func (a *archiveReader) Close() {
	a.zr.Close()
}

func (a *archiveReader) readManifest() (*manifest, error) {
	hdr, err := a.tr.Next()
	if err != nil {
		return nil, fmt.Errorf("read the first archive entry: %w", err)
	}
	if hdr.Name != manifestName || hdr.Typeflag != tar.TypeReg {
		return nil, fmt.Errorf("the first archive entry is %q, want %s", hdr.Name, manifestName)
	}
	if hdr.Size > maxManifestSize {
		return nil, fmt.Errorf("the manifest is %d bytes, more than %d", hdr.Size, maxManifestSize)
	}
	data, err := io.ReadAll(a.tr)
	if err != nil {
		return nil, fmt.Errorf("read the manifest: %w", err)
	}
	return decodeManifest(data)
}

// extract writes every file that m lists into staging, which must exist and be empty. Each entry must
// be a regular file that m lists once, of the listed size and SHA-256, and the archive must hold every
// listed file. Files get mode 0600 and directories 0750, and every file and directory is fsynced.
func (a *archiveReader) extract(m *manifest, staging string) error {
	want := make(map[string]manifestFile, len(m.Files))
	for _, f := range m.Files {
		want[f.Path] = f
	}
	dirs := map[string]struct{}{staging: {}}
	for {
		hdr, err := a.tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("read the archive: %w", err)
		}
		mf, ok := want[hdr.Name]
		if !ok {
			return fmt.Errorf("archive entry %q is not listed in the manifest, or is repeated", hdr.Name)
		}
		delete(want, hdr.Name)
		if hdr.Typeflag != tar.TypeReg || hdr.Size != mf.Size {
			return fmt.Errorf("archive entry %q: type %q size %d, want a regular file of size %d", hdr.Name, hdr.Typeflag, hdr.Size, mf.Size)
		}
		dst := filepath.Join(staging, filepath.FromSlash(mf.Path))
		if err := mkdirAllTracked(filepath.Dir(dst), staging, dirs); err != nil {
			return err
		}
		if err := extractFile(a.tr, dst, mf); err != nil {
			return err
		}
	}
	if len(want) > 0 {
		missing := make([]string, 0, len(want))
		for p := range want {
			missing = append(missing, p)
		}
		slices.Sort(missing)
		return fmt.Errorf("the archive ends before %d listed files, the first is %q", len(missing), missing[0])
	}
	return syncDirsDeepestFirst(dirs)
}

// mkdirAllTracked creates dir under staging and records every directory it may have created.
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

// syncDirsDeepestFirst fsyncs every dir in dirs, each after the dirs below it.
func syncDirsDeepestFirst(dirs map[string]struct{}) error {
	ordered := make([]string, 0, len(dirs))
	for d := range dirs {
		ordered = append(ordered, d)
	}
	slices.SortFunc(ordered, func(a, b string) int {
		return strings.Count(b, string(filepath.Separator)) - strings.Count(a, string(filepath.Separator))
	})
	for _, d := range ordered {
		if err := syncPath(d); err != nil {
			return err
		}
	}
	return nil
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
