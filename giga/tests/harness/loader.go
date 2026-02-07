package harness

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// extractOnce ensures we only extract the archive once per test run
var extractOnce sync.Once

// extractBlockchainOnce ensures we only extract the blockchain tests archive once per test run
var extractBlockchainOnce sync.Once

// LoadStateTest loads a state test from a JSON file
func LoadStateTest(filePath string) (map[string]*StateTestJSON, error) {
	file, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var tests map[string]StateTestJSON
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&tests)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*StateTestJSON)
	names := make([]string, 0, len(tests))
	for name := range tests {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		test := tests[name]
		result[name] = &test
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

		names := make([]string, 0, len(tests))
		for name := range tests {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			// Use relative path + test name as key to avoid collisions
			relPath, _ := filepath.Rel(dirPath, path)
			fullName := filepath.Join(relPath, name)
			result[fullName] = tests[name]
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

	var extractErr error
	extractOnce.Do(func() {
		extractErr = extractArchive(archivePath, "data")
	})
	if extractErr != nil {
		return "", extractErr
	}

	// Always verify the directory exists after sync.Once completes,
	// regardless of whether this was the first invocation
	if _, err = os.Stat(dataPath); err != nil {
		return "", err
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

// ============================================================================
// BlockchainTests Loaders
// ============================================================================

// LoadBlockchainTest loads a blockchain test from a JSON file
func LoadBlockchainTest(filePath string) (map[string]*BlockchainTestJSON, error) {
	file, err := os.Open(filepath.Clean(filePath))
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var tests map[string]BlockchainTestJSON
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&tests)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*BlockchainTestJSON)
	names := make([]string, 0, len(tests))
	for name := range tests {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		test := tests[name]
		result[name] = &test
	}
	return result, nil
}

// LoadBlockchainTestsFromDir loads all blockchain tests from a directory
func LoadBlockchainTestsFromDir(dirPath string) (map[string]*BlockchainTestJSON, error) {
	result := make(map[string]*BlockchainTestJSON)

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip bcEIP4844-blobtransactions directory (blob txs not supported)
			if info.Name() == "bcEIP4844-blobtransactions" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(info.Name(), ".json") {
			return nil
		}

		tests, err := LoadBlockchainTest(path)
		if err != nil {
			return err
		}

		names := make([]string, 0, len(tests))
		for name := range tests {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			// Use relative path + test name as key to avoid collisions
			relPath, _ := filepath.Rel(dirPath, path)
			fullName := filepath.Join(relPath, name)
			result[fullName] = tests[name]
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// GetBlockchainTestsPath returns the path to BlockchainTests, extracting from archive if needed
func GetBlockchainTestsPath() (string, error) {
	dataPath := filepath.Join("data", "BlockchainTests")
	if _, err := os.Stat(dataPath); err == nil {
		return dataPath, nil
	}

	archivePath := filepath.Join("data", "fixtures_blockchain_tests.tgz")
	_, err := os.Stat(archivePath)
	if err != nil {
		return "", err
	}

	var extractErr error
	extractBlockchainOnce.Do(func() {
		extractErr = extractArchive(archivePath, "data")
	})
	if extractErr != nil {
		return "", extractErr
	}

	// Always verify the directory exists after sync.Once completes,
	// regardless of whether this was the first invocation
	if _, err = os.Stat(dataPath); err != nil {
		return "", err
	}

	return dataPath, nil
}
