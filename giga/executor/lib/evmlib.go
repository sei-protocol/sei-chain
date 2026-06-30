package lib

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/ethereum/evmc/v12/bindings/go/evmc"
)

//go:generate go run ./gen/main.go .

// libDirEnv lets operators point at the directory that holds the trusted,
// integrity-verified evmone shared library. It must be an absolute path to a
// root-owned, non-writable directory.
const libDirEnv = "SEI_EVMONE_LIB_DIR"

// installLibDir is the canonical location the evmone library is installed to
// in release images (see Dockerfile, which copies it alongside the other
// native libraries that are loaded from /usr/lib).
const installLibDir = "/usr/lib"

// InitEvmoneVM initializes the EVMC VM by loading the platform-specific evmone
// library from a trusted, absolute path.
//
// Resolution order (first existing file wins):
//  1. $SEI_EVMONE_LIB_DIR (operator override)
//  2. /usr/lib (release install location)
//  3. the source-tree directory (local development and tests)
//
// The file's SHA-256 digest is verified against the digest pinned for this
// platform before it is handed to the dynamic linker. Passing an absolute
// path to evmc.Load avoids the dynamic linker's search path
// (LD_LIBRARY_PATH, ld.so.cache, default dirs), so the library cannot be
// substituted by planting a file earlier in the loader's search order.
//
// It does not verify that the loaded version is compatible with evmc version.
func InitEvmoneVM() (*evmc.VM, error) {
	libPath, err := resolveLibPath()
	if err != nil {
		return nil, err
	}

	if err := verifyLibDigest(libPath); err != nil {
		return nil, err
	}

	vm, err := evmc.Load(libPath)
	if err != nil {
		return nil, fmt.Errorf("evmc.Load(%q): %w", libPath, err)
	}

	return vm, nil
}

// resolveLibPath returns the absolute path of the platform library, choosing
// the first candidate directory that actually contains it.
func resolveLibPath() (string, error) {
	dirs := make([]string, 0, 3)
	if dir := os.Getenv(libDirEnv); dir != "" {
		if !filepath.IsAbs(dir) {
			return "", fmt.Errorf("%s must be an absolute path, got %q", libDirEnv, dir)
		}
		dirs = append(dirs, dir)
	}
	dirs = append(dirs, installLibDir)
	if _, srcFile, _, ok := runtime.Caller(0); ok {
		dirs = append(dirs, filepath.Dir(srcFile))
	}

	for _, dir := range dirs {
		candidate := filepath.Join(dir, libName)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("evmone library %q not found in any of %v", libName, dirs)
}

// verifyLibDigest computes the SHA-256 of the file at path and compares it
// against the digest pinned for this platform, mirroring the integrity check
// the generator performs when downloading the library (see gen/main.go).
func verifyLibDigest(path string) error {
	f, err := os.Open(filepath.Clean(path)) //nolint:gosec // path resolved from trusted, fixed locations
	if err != nil {
		return fmt.Errorf("open evmone library %q: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash evmone library %q: %w", path, err)
	}

	if actual := hex.EncodeToString(h.Sum(nil)); actual != libSHA256 {
		return fmt.Errorf("evmone library %q digest mismatch: expected %s, got %s", path, libSHA256, actual)
	}

	return nil
}
