package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// extractOnce ensures we only extract the archive once per test run
var extractOnce sync.Once

// LoadStateTest loads a state test from a JSON file
func LoadStateTest(t testing.TB, filePath string) map[string]*StateTestJSON {
	file, err := os.Open(filepath.Clean(filePath))
	require.NoError(t, err, "failed to open test file")
	defer file.Close()

	var tests map[string]StateTestJSON
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&tests)
	require.NoError(t, err, "failed to decode test file")

	result := make(map[string]*StateTestJSON)
	for name, test := range tests {
		testCopy := test
		result[name] = &testCopy
	}
	return result
}

// LoadStateTestsFromDir loads all state tests from a directory
func LoadStateTestsFromDir(t testing.TB, dirPath string) map[string]*StateTestJSON {
	result := make(map[string]*StateTestJSON)

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}

		tests := LoadStateTest(t, path)
		for name, test := range tests {
			// Use relative path + test name as key to avoid collisions
			relPath, _ := filepath.Rel(dirPath, path)
			fullName := filepath.Join(relPath, name)
			result[fullName] = test
		}
		return nil
	})
	require.NoError(t, err, "failed to walk test directory")

	return result
}

// GetStateTestsPath returns the path to GeneralStateTests, extracting from archive if needed
func GetStateTestsPath(t testing.TB) string {
	dataPath := filepath.Join("data", "GeneralStateTests")
	if _, err := os.Stat(dataPath); err == nil {
		return dataPath
	}

	archivePath := filepath.Join("data", "fixtures_general_state_tests.tgz")
	if _, err := os.Stat(archivePath); err == nil {
		extractOnce.Do(func() {
			t.Log("Extracting test fixtures from archive...")
			extractArchive(t, archivePath, "data")
		})
		if _, err := os.Stat(dataPath); err == nil {
			return dataPath
		}
	}

	t.Skip("No test fixtures available")
	return ""
}

// extractArchive extracts a .tgz archive to the destination directory
func extractArchive(t testing.TB, archivePath, destDir string) {
	cmd := exec.Command("tar", "-xzf", archivePath, "-C", destDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Failed to extract test fixtures: %v\nOutput: %s", err, output)
	}
}
