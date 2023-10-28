package utils

import "path/filepath"

func GetMemIavlDBPath(homePath string) string {
	return filepath.Join(homePath, "data", "memiavl.db")
}

func GetChangelogPath(dbPath string) string {
	return filepath.Join(dbPath, "changelog")
}
