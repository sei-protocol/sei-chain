package upgradetest

import (
	"bufio"
	"fmt"
	"go/build/constraint"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// A TestFile is one version-specific app upgrade test and its build tag.
type TestFile struct {
	Name string
	Tag  string
}

var testFileName = regexp.MustCompile(`^upgrade_v\d+_test\.go$`)

// TestFiles returns every version-specific upgrade test in root.
func TestFiles(root string) ([]TestFile, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var files []TestFile
	for _, entry := range entries {
		if entry.IsDir() || !testFileName.MatchString(entry.Name()) {
			continue
		}
		file, err := ReadTestFile(root, entry.Name())
		if err != nil {
			return nil, err
		}
		files = append(files, file)
	}
	return files, nil
}

// ReadTestFile returns the version-specific upgrade test named under root.
func ReadTestFile(root, name string) (TestFile, error) {
	tag, err := declaredTag(filepath.Join(root, name))
	if err != nil {
		return TestFile{}, err
	}
	return TestFile{Name: name, Tag: tag}, nil
}

// ExpectedTag returns the build tag implied by the file's name.
func (f TestFile) ExpectedTag() string {
	return strings.TrimSuffix(f.Name, "_test.go")
}

// declaredTag returns the single build tag a Go file is constrained by, and the
// empty string when the file declares no constraint or a compound one.
func declaredTag(path string) (string, error) {
	file, err := os.Open(path) //nolint:gosec // path is a Go file listed from the app directory
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()

	lines := bufio.NewScanner(file)
	for lines.Scan() {
		line := strings.TrimSpace(lines.Text())
		if strings.HasPrefix(line, "package ") {
			return "", nil
		}
		if !constraint.IsGoBuild(line) {
			continue
		}
		expr, err := constraint.Parse(line)
		if err != nil {
			return "", fmt.Errorf("%s: %w", path, err)
		}
		if tag, ok := expr.(*constraint.TagExpr); ok {
			return tag.Tag, nil
		}
		return "", nil
	}
	return "", lines.Err()
}
