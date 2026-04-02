package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func GetCommitStorePath(homePath string) string {
	return filepath.Join(homePath, "data", "committer.db")
}

func GetStateStorePath(homePath string, backend string) string {
	return filepath.Join(homePath, "data", backend)
}

func GetChangelogPath(dbPath string) string {
	return filepath.Join(dbPath, "changelog")
}

// ResolveAndCreateDir expands ~ to the home directory, resolves the path to
// an absolute path, and creates the directory if it doesn't exist.
func ResolveAndCreateDir(dir string) (string, error) {
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("failed to get home directory: %w", err)
		}
		if dir == "~" {
			dir = home
		} else {
			dir = filepath.Join(home, dir[2:])
		}
	}
	if dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return "", fmt.Errorf("failed to create directory: %w", err)
		}
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("failed to resolve absolute path: %w", err)
	}
	return abs, nil
}
