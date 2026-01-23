package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// SkipList defines tests and categories to skip during state test execution
type SkipList struct {
	Tests      map[string]string `json:"skipped_tests"`      // testName -> reason
	Categories []string          `json:"skipped_categories"` // entire categories to skip
}

// LoadSkipList loads the skip list from the data directory
func LoadSkipList() (*SkipList, error) {
	skipListPath := filepath.Join("data", "skip_list.json")
	return LoadSkipListFromPath(skipListPath)
}

// LoadSkipListFromPath loads a skip list from a specific path
func LoadSkipListFromPath(path string) (*SkipList, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty skip list if file doesn't exist
			return &SkipList{
				Tests:      make(map[string]string),
				Categories: []string{},
			}, nil
		}
		return nil, err
	}
	defer file.Close()

	var sl SkipList
	decoder := json.NewDecoder(file)
	err = decoder.Decode(&sl)
	if err != nil {
		return nil, err
	}

	// Initialize maps if nil
	if sl.Tests == nil {
		sl.Tests = make(map[string]string)
	}
	if sl.Categories == nil {
		sl.Categories = []string{}
	}

	return &sl, nil
}

// ShouldSkip checks if a test should be skipped
// Returns whether to skip and the reason
func (sl *SkipList) ShouldSkip(category, testName string) (skip bool, reason string) {
	// Check if entire category is skipped
	for _, cat := range sl.Categories {
		if cat == category {
			return true, "category skipped"
		}
	}

	// Check specific test by full name (category/testName)
	fullName := category + "/" + testName
	if r, ok := sl.Tests[fullName]; ok {
		return true, r
	}

	// Also check with just the test name (for backward compatibility)
	if r, ok := sl.Tests[testName]; ok {
		return true, r
	}

	// Check for partial matches - allows skipping by short name like "category/testBase"
	// which will match "category/testBase.json/..." style full test names
	for skipPattern, r := range sl.Tests {
		// Pattern format: "category/shortName" should match if testName contains shortName
		parts := strings.SplitN(skipPattern, "/", 2)
		if len(parts) == 2 && parts[0] == category {
			shortName := parts[1]
			// Match if the test name starts with the short name (before any .json or /)
			if strings.HasPrefix(testName, shortName+".json") ||
				strings.HasPrefix(testName, shortName+"/") ||
				testName == shortName {
				return true, r
			}
		}
	}

	return false, ""
}

// SkippedCount returns the number of tests that would be skipped
func (sl *SkipList) SkippedCount() int {
	return len(sl.Tests)
}

// SkippedCategoriesCount returns the number of skipped categories
func (sl *SkipList) SkippedCategoriesCount() int {
	return len(sl.Categories)
}

// IsCategorySkipped checks if an entire category is skipped
func (sl *SkipList) IsCategorySkipped(category string) bool {
	for _, cat := range sl.Categories {
		if cat == category {
			return true
		}
	}
	return false
}

// GetSkipReason returns the skip reason for a test, or empty string if not skipped
func (sl *SkipList) GetSkipReason(category, testName string) string {
	_, reason := sl.ShouldSkip(category, testName)
	return reason
}

// NormalizeTestName normalizes a test name by removing leading directory components
// that match known patterns (e.g., "stExample/test.json/testName" -> "stExample/testName")
func NormalizeTestName(fullName string) (category string, testName string) {
	parts := strings.Split(fullName, "/")
	if len(parts) >= 1 {
		category = parts[0]
	}
	if len(parts) >= 2 {
		// Remove .json extension if present in any part
		var cleanParts []string
		for i, p := range parts[1:] {
			if strings.HasSuffix(p, ".json") {
				// Skip the json filename part, use remaining parts
				continue
			}
			cleanParts = append(cleanParts, p)
			// If this is the last part, include index if present
			if i == len(parts)-2 {
				break
			}
		}
		testName = strings.Join(cleanParts, "/")
	}
	return category, testName
}
