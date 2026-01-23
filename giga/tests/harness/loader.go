package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// extractOnce ensures we only extract the archive once per test run
var extractOnce sync.Once

// LoadStateTest loads a state test from a JSON file
func LoadStateTest(filePath string) (map[string]*StateTestJSON, error) {
	file, err := os.Open(filepath.Clean(filePath))
	defer file.Close()
	if err != nil {
		return nil, err
	}

	var tests map[string]StateTestJSON
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&tests)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*StateTestJSON)
	for name, test := range tests {
		testCopy := test
		result[name] = &testCopy
	}
	return result, nil
}

// LoadStateTestsFromDir loads all state tests from a directory
func LoadStateTestsFromDir(dirPath string) (map[string]*StateTestJSON, error) {
	result := make(map[string]*StateTestJSON)

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}

		tests, err := LoadStateTest(path)
		if err != nil {
			return err
		}

		for name, test := range tests {
			// Use relative path + test name as key to avoid collisions
			relPath, _ := filepath.Rel(dirPath, path)
			fullName := filepath.Join(relPath, name)
			result[fullName] = test
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// GetStateTestsPath returns the path to GeneralStateTests, extracting from archive if needed
func GetStateTestsPath() (string, error) {
	dataPath := filepath.Join("data", "GeneralStateTests")
	if _, err := os.Stat(dataPath); err == nil {
		return dataPath, nil
	}

	archivePath := filepath.Join("data", "fixtures_general_state_tests.tgz")
	_, err := os.Stat(archivePath)
	if err != nil {
		return "", err
	}

	extractOnce.Do(func() {
		err = extractArchive(archivePath, "data")
	})
	if err != nil {
		return "", err
	}

	_, err = os.Stat(dataPath)
	if err != nil {
		return "", nil
	}

	return dataPath, nil
}

// extractArchive extracts a .tgz archive to the destination directory
func extractArchive(archivePath, destDir string) error {
	cmd := exec.Command("tar", "-xzf", archivePath, "-C", destDir)
	if _, err := cmd.CombinedOutput(); err != nil {
		return err
	}

	return nil
}
