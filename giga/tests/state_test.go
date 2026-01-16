package giga_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sei-protocol/sei-chain/giga/tests/harness"
	"github.com/stretchr/testify/require"
)

// TestGigaVsV2_StateTests runs state tests comparing Giga vs V2 execution
func TestGigaVsV2_StateTests(t *testing.T) {
	stateTestsPath := harness.GetStateTestsPath(t)

	// Allow filtering to specific directory via STATE_TEST_DIR env var
	specificDir := os.Getenv("STATE_TEST_DIR")

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

	for _, dir := range testDirs {
		dirPath := filepath.Join(stateTestsPath, dir)
		if _, err := os.Stat(dirPath); os.IsNotExist(err) {
			t.Logf("Skipping %s - directory not found", dir)
			continue
		}

		tests := harness.LoadStateTestsFromDir(t, dirPath)
		for testName, st := range tests {
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
					subtestName = testName + "/" + string(rune('0'+i))
				}

				t.Run(subtestName, func(t *testing.T) {
					runStateTestComparison(t, st, post)
				})
			}
		}
	}
}
