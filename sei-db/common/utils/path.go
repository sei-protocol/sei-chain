package utils

import "path/filepath"

func GetCommitStorePath(homePath string) string {
	return filepath.Join(homePath, "data", "committer.db")
}

func GetStateStorePath(homePath string, backend string) string {
	return filepath.Join(homePath, "data", backend)
}

func GetChangelogPath(commitStorePath string) string {
	return filepath.Join(commitStorePath, "changelog")
}
