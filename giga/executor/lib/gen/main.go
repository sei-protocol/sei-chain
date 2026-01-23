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
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

const evmoneVersion = "0.12.0"

type Platform struct {
	Archive string
	Hash    string
	OS      string
	Arch    string
}

var platforms = []Platform{
	{
		Archive: "evmone-0.12.0-linux-x86_64.tar.gz",
		Hash:    "1c7b5eba0c8c3b3b2a7a05101e2d01a13a2f84b323989a29be66285dba4136ce",
		OS:      "linux",
		Arch:    "amd64",
	},
	{
		Archive: "evmone-0.12.0-darwin-arm64.tar.gz",
		Hash:    "e164e0d2b985cc1cca07b501538b2e804bf872d1d8d531f9241d518a886234a6",
		OS:      "darwin",
		Arch:    "arm64",
	},
}

// This program downloads evmone shared libraries for supported platforms.
//
// To upgrade evmone:
//  1. Visit https://github.com/ethereum/evmone/releases
//  2. Update evmoneVersion constant below
//  3. Update the Archive filenames and SHA256 hashes in the platforms slice
//  4. Run: go run download_evmone.go <output-dir>
func main() {
	if len(os.Args) != 2 {
		_, _ = fmt.Fprintf(os.Stderr, "Usage: %s <output-dir>\n", os.Args[0])
		os.Exit(1)
	}
	outDir := filepath.Clean(os.Args[1])

	// Create context that cancels on SIGINT or SIGTERM
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	for _, p := range platforms {
		if err := downloadAndExtract(ctx, p, outDir); err != nil {
			if ctx.Err() != nil {
				_, _ = fmt.Fprintf(os.Stderr, "\nInterrupted\n")
				os.Exit(1)
			}
			_, _ = fmt.Fprintf(os.Stderr, "Failed %s-%s: %v\n", p.OS, p.Arch, err)
			os.Exit(1)
		}
	}
	fmt.Println("All platforms downloaded successfully!")
}

func downloadAndExtract(ctx context.Context, p Platform, outDir string) error {
	url := fmt.Sprintf("https://github.com/ethereum/evmone/releases/download/v%s/%s", evmoneVersion, p.Archive)
	fmt.Printf("Downloading %s...\n", p.Archive)

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
	fmt.Printf("  Hash verified: %s-%s\n", p.OS, p.Arch)

	gzr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer func() { _ = gzr.Close() }()

	ext := map[string]string{"darwin": "dylib", "linux": "so"}[p.OS]
	libPattern := fmt.Sprintf("libevmone.%s.%s", evmoneVersion, ext)
	if p.OS == "linux" {
		libPattern = fmt.Sprintf("libevmone.so.%s", evmoneVersion)
	}

	tr := tar.NewReader(gzr)
	for ctx.Err() == nil {
		header, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("library not found in archive (looking for %s)", libPattern)
		}
		if err != nil {
			return fmt.Errorf("tar: %w", err)
		}

		if header.Typeflag != tar.TypeReg || !strings.HasSuffix(header.Name, libPattern) {
			continue
		}

		outName := fmt.Sprintf("libevmone.%s_%s_%s.%s", evmoneVersion, p.OS, p.Arch, ext)
		outPath := filepath.Join(outDir, outName)

		f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			return fmt.Errorf("create: %w", err)
		}

		if _, err := io.Copy(f, tr); err != nil {
			_ = f.Close()
			return fmt.Errorf("extract: %w", err)
		}

		if err := f.Close(); err != nil {
			return fmt.Errorf("close: %w", err)
		}

		fmt.Printf("  Extracted: %s\n", outPath)
		return nil
	}
	return ctx.Err()
}
