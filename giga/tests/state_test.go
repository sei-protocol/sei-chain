package giga_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/sei-protocol/sei-chain/giga/tests/harness"
	"github.com/stretchr/testify/require"
)

// TestSummary tracks test results for summary reporting
type TestSummary struct {
	mu sync.Mutex

	// Per-category stats
	CategoryStats map[string]*CategoryStats

	// Global failure tracking by type
	FailuresByType map[FailureType][]string

	// Total counts
	TotalTests   int
	TotalPassed  int
	TotalFailed  int
	TotalSkipped int
}

// CategoryStats tracks stats for a single category
type CategoryStats struct {
	Total   int
	Passed  int
	Failed  int
	Skipped int
	Failures []FailureRecord
}

// FailureRecord records a single test failure
type FailureRecord struct {
	TestName    string
	FailureType FailureType
	Message     string
}

// NewTestSummary creates a new test summary tracker
func NewTestSummary() *TestSummary {
	return &TestSummary{
		CategoryStats:  make(map[string]*CategoryStats),
		FailuresByType: make(map[FailureType][]string),
	}
}

// RecordPass records a passing test
func (ts *TestSummary) RecordPass(category, testName string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.TotalTests++
	ts.TotalPassed++

	if ts.CategoryStats[category] == nil {
		ts.CategoryStats[category] = &CategoryStats{}
	}
	ts.CategoryStats[category].Total++
	ts.CategoryStats[category].Passed++
}

// RecordSkip records a skipped test
func (ts *TestSummary) RecordSkip(category, testName, reason string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.TotalTests++
	ts.TotalSkipped++

	if ts.CategoryStats[category] == nil {
		ts.CategoryStats[category] = &CategoryStats{}
	}
	ts.CategoryStats[category].Total++
	ts.CategoryStats[category].Skipped++
}

// RecordFailure records a failed test
func (ts *TestSummary) RecordFailure(category, testName string, failureType FailureType, message string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	ts.TotalTests++
	ts.TotalFailed++

	if ts.CategoryStats[category] == nil {
		ts.CategoryStats[category] = &CategoryStats{}
	}
	cs := ts.CategoryStats[category]
	cs.Total++
	cs.Failed++
	cs.Failures = append(cs.Failures, FailureRecord{
		TestName:    testName,
		FailureType: failureType,
		Message:     message,
	})

	fullName := category + "/" + testName
	ts.FailuresByType[failureType] = append(ts.FailuresByType[failureType], fullName)
}

// PrintSummary prints the test summary to the test log
func (ts *TestSummary) PrintSummary(t *testing.T) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	var sb strings.Builder
	sb.WriteString("\n=== State Test Summary ===\n\n")

	// Sort categories for consistent output
	categories := make([]string, 0, len(ts.CategoryStats))
	for cat := range ts.CategoryStats {
		categories = append(categories, cat)
	}
	sort.Strings(categories)

	// Per-category summary
	for _, cat := range categories {
		stats := ts.CategoryStats[cat]
		sb.WriteString(fmt.Sprintf("Category: %s\n", cat))
		sb.WriteString(fmt.Sprintf("  Total: %d\n", stats.Total))
		sb.WriteString(fmt.Sprintf("  Passed: %d\n", stats.Passed))
		sb.WriteString(fmt.Sprintf("  Failed: %d\n", stats.Failed))
		sb.WriteString(fmt.Sprintf("  Skipped: %d\n", stats.Skipped))
		sb.WriteString("\n")
	}

	// Failures by type
	if len(ts.FailuresByType) > 0 {
		sb.WriteString("Failures by type:\n")
		// Sort failure types for consistent output
		types := make([]FailureType, 0, len(ts.FailuresByType))
		for ft := range ts.FailuresByType {
			types = append(types, ft)
		}
		sort.Slice(types, func(i, j int) bool {
			return string(types[i]) < string(types[j])
		})

		for _, ft := range types {
			sb.WriteString(fmt.Sprintf("  %s: %d\n", ft, len(ts.FailuresByType[ft])))
		}
		sb.WriteString("\n")

		// List failed tests (limit to first 50 to avoid huge output)
		sb.WriteString("Failed tests:\n")
		failedCount := 0
		for _, cat := range categories {
			stats := ts.CategoryStats[cat]
			for _, f := range stats.Failures {
				if failedCount >= 50 {
					sb.WriteString(fmt.Sprintf("  ... and %d more failures\n", ts.TotalFailed-50))
					break
				}
				sb.WriteString(fmt.Sprintf("  - %s/%s: %s (%s)\n", cat, f.TestName, f.FailureType, f.Message))
				failedCount++
			}
			if failedCount >= 50 {
				break
			}
		}
	}

	// Overall summary
	sb.WriteString(fmt.Sprintf("\n=== Overall: %d total, %d passed, %d failed, %d skipped ===\n",
		ts.TotalTests, ts.TotalPassed, ts.TotalFailed, ts.TotalSkipped))

	t.Log(sb.String())
}

// Global test summary (shared across subtests)
var globalSummary = NewTestSummary()

// TestGigaVsV2_StateTests runs state tests comparing Giga vs V2 execution
//
// Usage: STATE_TEST_DIR=stChainId go test -v -run TestGigaVsV2_StateTests ./giga/tests/...
// Usage with test name filter: STATE_TEST_DIR=stExample STATE_TEST_NAME=add11 go test -v -run TestGigaVsV2_StateTests ./giga/tests/...
func TestGigaVsV2_StateTests(t *testing.T) {
	stateTestsPath, err := harness.GetStateTestsPath()
	require.NoError(t, err, "failed to get path to state tests")

	// Load skip list
	skipList, err := harness.LoadSkipList()
	require.NoError(t, err, "failed to load skip list")

	// Allow filtering to specific directory via STATE_TEST_DIR env var
	specificDir := os.Getenv("STATE_TEST_DIR")

	// Allow filtering to specific test name via STATE_TEST_NAME env var
	specificTestName := os.Getenv("STATE_TEST_NAME")

	var testDirs []string
	if specificDir != "" {
		// Run only the specified directory
		testDirs = []string{specificDir}
	} else {
		// Run all directories
		entries, err := os.ReadDir(stateTestsPath)
		require.NoError(t, err, "failed to read state tests directory")
		for _, entry := range entries {
			if entry.IsDir() {
				testDirs = append(testDirs, entry.Name())
			}
		}
	}

	// Reset global summary for this run
	globalSummary = NewTestSummary()

	// Print summary at the end
	t.Cleanup(func() {
		globalSummary.PrintSummary(t)
	})

	if specificTestName != "" {
		t.Logf("Filtering to tests matching: %s", specificTestName)
	}

	for _, dir := range testDirs {
		// Check if entire category is skipped
		if skipList.IsCategorySkipped(dir) {
			t.Logf("Skipping category %s (in skip list)", dir)
			continue
		}

		dirPath := filepath.Join(stateTestsPath, dir)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			t.Logf("Skipping %s - directory not found", dir)
			continue
		}

		tests, err := harness.LoadStateTestsFromDir(dirPath)
		require.NoError(t, err, "failed to load state tests from %s", dirPath)

		for testName, st := range tests {
			// Filter by test name if specified
			if specificTestName != "" && !strings.Contains(testName, specificTestName) {
				continue
			}

			// Run each subtest for Cancun fork (most recent stable)
			cancunPosts, ok := st.Post["Cancun"]
			if !ok {
				// Try other forks
				for fork := range st.Post {
					cancunPosts = st.Post[fork]
					break
				}
			}

			for i, post := range cancunPosts {
				subtestName := testName
				if len(cancunPosts) > 1 {
					subtestName = fmt.Sprintf("%s/%d", testName, i)
				}

				// Check skip list
				if shouldSkip, reason := skipList.ShouldSkip(dir, subtestName); shouldSkip {
					t.Run(subtestName, func(t *testing.T) {
						globalSummary.RecordSkip(dir, subtestName, reason)
						t.Skipf("Skipped: %s", reason)
					})
					continue
				}

				// Capture variables for closure
				category := dir
				testNameCopy := subtestName
				stCopy := st
				postCopy := post

				t.Run(subtestName, func(t *testing.T) {
					result := runStateTestComparisonWithResult(t, stCopy, postCopy)
					if result.Passed {
						globalSummary.RecordPass(category, testNameCopy)
					} else {
						globalSummary.RecordFailure(category, testNameCopy, result.FailureType, result.Message)
					}
				})
			}
		}
	}
}
