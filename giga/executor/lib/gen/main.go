package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

const evmoneVersion = "0.12.0"

type Platform struct {
	Archive string
	Hash    string // SHA256 hash of the archive
	LibHash string // SHA256 hash of the extracted library file
	OS      string
	Arch    string
	LibPath string
	Ext     string
}

var platforms = []Platform{
	{
		Archive: "evmone-0.12.0-linux-x86_64.tar.gz",
		Hash:    "1c7b5eba0c8c3b3b2a7a05101e2d01a13a2f84b323989a29be66285dba4136ce",
		LibHash: "0fec5d79f4c9a466bb680e8b0b9c770aea38f3dd6d2e4af23535c893d0d18d40",
		OS:      "linux",
		Arch:    "amd64",
		LibPath: "lib/libevmone.so.0.12.0",
		Ext:     "so",
	},
	{
		Archive: "evmone-0.12.0-darwin-arm64.tar.gz",
		Hash:    "e164e0d2b985cc1cca07b501538b2e804bf872d1d8d531f9241d518a886234a6",
		LibHash: "cb1c555b3849a0a6a9402bc907a9a7bfe14e1c08c483c488ec6ba5f19e986847",
		OS:      "darwin",
		Arch:    "arm64",
		LibPath: "lib/libevmone.0.12.0.dylib",
		Ext:     "dylib",
	},
}

// This program downloads evmone shared libraries for supported platforms.
//
// To upgrade evmone:
//  1. Visit https://github.com/ethereum/evmone/releases
//  2. Update evmoneVersion constant below
//  3. Update the Archive filenames and SHA256 hashes in the platforms slice
//  4. Update the LibHash values for extracted library files
//  5. Run: go run download_evmone.go <output-dir>
func main() {
	if len(os.Args) != 2 {
		log.Fatalf("Usage: %s <output-dir>\n", os.Args[0])
	}
	outDir := filepath.Clean(os.Args[1])

	// Create context that cancels on SIGINT or SIGTERM
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	for _, p := range platforms {
		if err := downloadAndExtract(ctx, p, outDir); err != nil {
			if ctx.Err() != nil {
				log.Fatal("Interrupted")
			}
			log.Fatalf("Failed %s-%s: %v\n", p.OS, p.Arch, err)
		}
	}
	log.Println("All platforms downloaded successfully!")
}

// libFileName returns the output filename for a platform's library
func (p Platform) libFileName() string {
	return fmt.Sprintf("libevmone.%s_%s_%s.%s", evmoneVersion, p.OS, p.Arch, p.Ext)
}

// checkExistingLib checks if the library file already exists with the correct hash.
// Returns true if the file exists and has the correct hash, false otherwise.
func checkExistingLib(p Platform, outDir string) (bool, error) {
	outPath := filepath.Clean(filepath.Join(outDir, p.libFileName()))

	f, err := os.Open(outPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("open existing file: %w", err)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, fmt.Errorf("hash existing file: %w", err)
	}

	actual := hex.EncodeToString(h.Sum(nil))
	return actual == p.LibHash, nil
}

func downloadAndExtract(ctx context.Context, p Platform, outDir string) error {
	outName := p.libFileName()
	outPath := filepath.Join(outDir, outName)

	// Check if file already exists with correct hash
	exists, err := checkExistingLib(p, outDir)
	if err != nil {
		return fmt.Errorf("check existing: %w", err)
	}
	if exists {
		log.Printf("Skipping %s (already exists with correct hash)\n", outName)
		return nil
	}

	url := fmt.Sprintf("https://github.com/ethereum/evmone/releases/download/v%s/%s", evmoneVersion, p.Archive)
	log.Printf("Downloading %s...\n", p.Archive)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read: %w", err)
	}

	// Check for cancellation after download
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Verify SHA-256 hash of the downloaded archive
	sum := sha256.Sum256(body)
	actual := hex.EncodeToString(sum[:])
	if actual != p.Hash {
		return fmt.Errorf("hash mismatch: expected %s, got %s", p.Hash, actual)
	}
	log.Printf("  Archive hash verified: %s-%s\n", p.OS, p.Arch)

	gzr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer func() { _ = gzr.Close() }()

	tr := tar.NewReader(gzr)
	for ctx.Err() == nil {
		header, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("library not found in archive (looking for %s)", p.LibPath)
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}
		if header.Typeflag == tar.TypeReg {
			fmt.Println(header.Name)
		}

		if header.Typeflag != tar.TypeReg || header.Name != p.LibPath {
			continue
		}

		f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644) //nolint:gosec
		if err != nil {
			return fmt.Errorf("create: %w", err)
		}

		const maxSize = 100 << 20 // 100MiB maximum copy
		if _, err := io.CopyN(f, tr, maxSize); err != nil && err != io.EOF {
			_ = f.Close()
			return fmt.Errorf("extract: %w", err)
		}

		if err := f.Close(); err != nil {
			return fmt.Errorf("close: %w", err)
		}

		log.Printf("  Extracted: %s\n", outPath)
		return nil
	}
	return ctx.Err()
}
