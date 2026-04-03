package util

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Layr-Labs/eigenda/common"
	"github.com/stretchr/testify/require"
)

func TestNewFileLock(t *testing.T) {
	tempDir := t.TempDir()
	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	tests := []struct {
		name        string
		setup       func() string
		expectError bool
	}{
		{
			name: "successful lock creation",
			setup: func() string {
				return filepath.Join(tempDir, "test.lock")
			},
			expectError: false,
		},
		{
			name: "lock already exists with live process",
			setup: func() string {
				lockPath := filepath.Join(tempDir, "existing.lock")
				// Create an existing lock file with current process PID (which is alive)
				content := fmt.Sprintf("PID: %d\nTimestamp: 2023-01-01T00:00:00Z\n", os.Getpid())
				err := os.WriteFile(lockPath, []byte(content), 0644)
				require.NoError(t, err)
				return lockPath
			},
			expectError: true,
		},
		{
			name: "stale lock file gets overridden",
			setup: func() string {
				lockPath := filepath.Join(tempDir, "stale.lock")
				// Create a lock file with a PID that definitely doesn't exist
				// Use PID 999999 which is very unlikely to exist
				stalePID := 999999
				content := fmt.Sprintf("PID: %d\nTimestamp: 2023-01-01T00:00:00Z\n", stalePID)
				err := os.WriteFile(lockPath, []byte(content), 0644)
				require.NoError(t, err)
				return lockPath
			},
			expectError: false,
		},
		{
			name: "malformed lock file gets treated as existing",
			setup: func() string {
				lockPath := filepath.Join(tempDir, "malformed.lock")
				// Create a lock file without proper PID format
				err := os.WriteFile(lockPath, []byte("invalid content"), 0644)
				require.NoError(t, err)
				return lockPath
			},
			expectError: true,
		},
		{
			name: "invalid directory",
			setup: func() string {
				return filepath.Join(tempDir, "nonexistent", "test.lock")
			},
			expectError: true,
		},
		{
			name: "tilde expansion",
			setup: func() string {
				return "~/test.lock"
			},
			expectError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lockPath := tc.setup()

			lock, err := NewFileLock(logger, lockPath, false)

			if tc.expectError {
				require.Error(t, err)
				require.Nil(t, lock)
			} else {
				require.NoError(t, err)
				require.NotNil(t, lock)

				// Verify lock file was created
				_, err := os.Stat(lock.Path())
				require.NoError(t, err)

				// Verify lock file contains process info
				content, err := os.ReadFile(lock.Path())
				require.NoError(t, err)
				contentStr := string(content)
				require.Contains(t, contentStr, "PID:")
				require.Contains(t, contentStr, "Timestamp:")

				// Clean up
				lock.Release()
			}
		})
	}
}

func TestFileLockRelease(t *testing.T) {
	tempDir := t.TempDir()
	lockPath := filepath.Join(tempDir, "test.lock")

	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	// Create a lock
	lock, err := NewFileLock(logger, lockPath, false)
	require.NoError(t, err)
	require.NotNil(t, lock)

	// Verify lock file exists
	_, err = os.Stat(lockPath)
	require.NoError(t, err)

	// Release the lock
	lock.Release()

	// Verify lock file was removed
	_, err = os.Stat(lockPath)
	require.True(t, os.IsNotExist(err))

	// Try to release again (should not)
	lock.Release()
}

func TestFileLockPath(t *testing.T) {
	tempDir := t.TempDir()
	lockPath := filepath.Join(tempDir, "test.lock")

	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	lock, err := NewFileLock(logger, lockPath, false)
	require.NoError(t, err)
	defer lock.Release()

	// Path should be sanitized (absolute)
	returnedPath := lock.Path()
	require.True(t, filepath.IsAbs(returnedPath))
	require.True(t, strings.HasSuffix(returnedPath, "test.lock"))
}

func TestFileLockConcurrency(t *testing.T) {
	tempDir := t.TempDir()
	lockPath := filepath.Join(tempDir, "concurrent.lock")

	const numGoroutines = 10
	const duration = 50 * time.Millisecond

	var successCount int32
	var wg sync.WaitGroup
	results := make(chan bool, numGoroutines)

	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	// Launch multiple goroutines trying to acquire the same lock
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			lock, err := NewFileLock(logger, lockPath, false)
			if err != nil {
				results <- false
				return
			}

			// Hold the lock for a short time
			time.Sleep(duration)

			lock.Release()

			results <- true
		}(i)
	}

	wg.Wait()
	close(results)

	// Count successful lock acquisitions
	successCount = 0
	for success := range results {
		if success {
			successCount++
		}
	}

	// Only one goroutine should have successfully acquired the lock
	require.Equal(t, int32(1), successCount, "Only one goroutine should acquire the lock")
}

func TestDoubleRelease(t *testing.T) {
	tempDir := t.TempDir()

	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	lockPath := filepath.Join(tempDir, "double-release.lock")

	lock, err := NewFileLock(logger, lockPath, false)
	require.NoError(t, err)

	// First release should succeed
	lock.Release()

	// Second release should not panic
	lock.Release()
}

func TestFileLockDebugInfo(t *testing.T) {
	tempDir := t.TempDir()
	lockPath := filepath.Join(tempDir, "debug-test.lock")

	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	// Create first lock
	lock1, err := NewFileLock(logger, lockPath, false)
	require.NoError(t, err)

	// Try to create second lock - should fail with debug info
	lock2, err := NewFileLock(logger, lockPath, false)
	require.Error(t, err)
	require.Nil(t, lock2)

	// Error should contain debug information from existing lock
	require.Contains(t, err.Error(), "lock file already exists")
	require.Contains(t, err.Error(), "existing lock info:")
	require.Contains(t, err.Error(), "PID:")
	require.Contains(t, err.Error(), "Timestamp:")

	// Clean up
	lock1.Release()
}

func TestIsProcessAlive(t *testing.T) {
	tests := []struct {
		name     string
		pid      int
		expected bool
	}{
		{
			name:     "current process",
			pid:      os.Getpid(),
			expected: true,
		},
		{
			name:     "invalid pid zero",
			pid:      0,
			expected: false,
		},
		{
			name:     "invalid pid negative",
			pid:      -1,
			expected: false,
		},
		{
			name:     "nonexistent pid",
			pid:      999999, // Very unlikely to exist
			expected: false,
		},
		{
			name:     "init process",
			pid:      1,
			expected: true, // Init process should always exist on Unix systems
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsProcessAlive(tc.pid)
			require.Equal(t, tc.expected, result)
		})
	}
}

func TestParseLockFile(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		content     string
		expectedPID int
		expectError bool
	}{
		{
			name:        "valid lock file",
			content:     "PID: 12345\nTimestamp: 2023-01-01T00:00:00Z\n",
			expectedPID: 12345,
			expectError: false,
		},
		{
			name:        "lock file with extra whitespace",
			content:     "  PID: 67890  \n  Timestamp: 2023-01-01T00:00:00Z  \n",
			expectedPID: 67890,
			expectError: false,
		},
		{
			name:        "lock file missing PID",
			content:     "Timestamp: 2023-01-01T00:00:00Z\n",
			expectedPID: 0,
			expectError: true,
		},
		{
			name:        "lock file with invalid PID",
			content:     "PID: not-a-number\nTimestamp: 2023-01-01T00:00:00Z\n",
			expectedPID: 0,
			expectError: true,
		},
		{
			name:        "empty lock file",
			content:     "",
			expectedPID: 0,
			expectError: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lockPath := filepath.Join(tempDir, fmt.Sprintf("test-%s.lock", tc.name))
			err := os.WriteFile(lockPath, []byte(tc.content), 0644)
			require.NoError(t, err)

			pid, err := parseLockFile(lockPath)

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expectedPID, pid)
			}
		})
	}
}

func TestStaleLockRecovery(t *testing.T) {
	tempDir := t.TempDir()
	lockPath := filepath.Join(tempDir, "stale-recovery.lock")

	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	// Create a stale lock file with a definitely dead PID
	stalePID := 999999
	staleContent := fmt.Sprintf("PID: %d\nTimestamp: 2023-01-01T00:00:00Z\n", stalePID)
	err = os.WriteFile(lockPath, []byte(staleContent), 0644)
	require.NoError(t, err)

	// Verify the lock file exists
	_, err = os.Stat(lockPath)
	require.NoError(t, err)

	// Try to acquire the lock - should succeed by removing stale lock
	lock, err := NewFileLock(logger, lockPath, false)
	require.NoError(t, err)
	require.NotNil(t, lock)

	// Verify the lock file now has our PID
	content, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	require.Contains(t, string(content), fmt.Sprintf("PID: %d", os.Getpid()))

	// Clean up
	lock.Release()
}

func TestLockDirectoriesSuccessfulLocking(t *testing.T) {
	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	tempDir := t.TempDir()

	// Create multiple directories
	dir1 := filepath.Join(tempDir, "dir1")
	dir2 := filepath.Join(tempDir, "dir2")
	dir3 := filepath.Join(tempDir, "dir3")

	err = os.MkdirAll(dir1, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(dir2, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(dir3, 0755)
	require.NoError(t, err)

	directories := []string{dir1, dir2, dir3}
	lockFileName := "test.lock"

	// Lock all directories
	release, err := LockDirectories(logger, directories, lockFileName, false)
	require.NoError(t, err)
	require.NotNil(t, release)

	// Verify lock files were created in all directories
	for _, dir := range directories {
		lockPath := filepath.Join(dir, lockFileName)
		_, err := os.Stat(lockPath)
		require.NoError(t, err, "lock file should exist in %s", dir)

		// Verify lock file content
		content, err := os.ReadFile(lockPath)
		require.NoError(t, err)
		contentStr := string(content)
		require.Contains(t, contentStr, "PID:")
		require.Contains(t, contentStr, "Timestamp:")
	}

	// Release all locks
	release()

	// Verify all lock files were removed
	for _, dir := range directories {
		lockPath := filepath.Join(dir, lockFileName)
		_, err := os.Stat(lockPath)
		require.True(t, os.IsNotExist(err), "lock file should be removed from %s", dir)
	}
}

func TestLockDirectoriesFailureWhenLockExists(t *testing.T) {
	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	tempDir := t.TempDir()

	// Create multiple directories
	dir1 := filepath.Join(tempDir, "dir1")
	dir2 := filepath.Join(tempDir, "dir2")
	dir3 := filepath.Join(tempDir, "dir3")

	err = os.MkdirAll(dir1, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(dir2, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(dir3, 0755)
	require.NoError(t, err)

	lockFileName := "test.lock"

	// Create an existing lock in dir2
	existingLockPath := filepath.Join(dir2, lockFileName)
	content := fmt.Sprintf("PID: %d\nTimestamp: 2023-01-01T00:00:00Z\n", os.Getpid())
	err = os.WriteFile(existingLockPath, []byte(content), 0644)
	require.NoError(t, err)

	directories := []string{dir1, dir2, dir3}

	// Try to lock all directories - should fail
	release, err := LockDirectories(logger, directories, lockFileName, false)
	require.Error(t, err)
	require.Nil(t, release)
	require.Contains(t, err.Error(), "failed to acquire lock on directory")
	require.Contains(t, err.Error(), dir2)

	// Verify that no locks were left behind (all should be cleaned up on failure)
	lockPath1 := filepath.Join(dir1, lockFileName)
	_, err = os.Stat(lockPath1)
	require.True(t, os.IsNotExist(err), "lock file should not exist in %s after failure", dir1)

	lockPath3 := filepath.Join(dir3, lockFileName)
	_, err = os.Stat(lockPath3)
	require.True(t, os.IsNotExist(err), "lock file should not exist in %s after failure", dir3)

	// Clean up the existing lock
	err = os.Remove(existingLockPath)
	require.NoError(t, err)
}

func TestLockDirectoriesFailureWhenDirectoryDoesNotExist(t *testing.T) {
	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	tempDir := t.TempDir()

	// Create some directories but not all
	dir1 := filepath.Join(tempDir, "dir1")
	dir2 := filepath.Join(tempDir, "nonexistent")
	dir3 := filepath.Join(tempDir, "dir3")

	err = os.MkdirAll(dir1, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(dir3, 0755)
	require.NoError(t, err)

	directories := []string{dir1, dir2, dir3}
	lockFileName := "test.lock"

	// Try to lock all directories - should fail on nonexistent directory
	release, err := LockDirectories(logger, directories, lockFileName, false)
	require.Error(t, err)
	require.Nil(t, release)
	require.Contains(t, err.Error(), "failed to acquire lock on directory")
	require.Contains(t, err.Error(), dir2)

	// Verify that no locks were left behind
	lockPath1 := filepath.Join(dir1, lockFileName)
	_, err = os.Stat(lockPath1)
	require.True(t, os.IsNotExist(err), "lock file should not exist in %s after failure", dir1)

	lockPath3 := filepath.Join(dir3, lockFileName)
	_, err = os.Stat(lockPath3)
	require.True(t, os.IsNotExist(err), "lock file should not exist in %s after failure", dir3)
}

func TestLockDirectoriesEmptyList(t *testing.T) {
	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	directories := []string{}
	lockFileName := "test.lock"

	// Lock empty list should succeed
	release, err := LockDirectories(logger, directories, lockFileName, false)
	require.NoError(t, err)
	require.NotNil(t, release)

	// Release should not panic
	release()
}

func TestLockDirectoriesConcurrentAccessPrevention(t *testing.T) {
	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	tempDir := t.TempDir()

	// Create directories
	dir1 := filepath.Join(tempDir, "dir1")
	dir2 := filepath.Join(tempDir, "dir2")

	err = os.MkdirAll(dir1, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(dir2, 0755)
	require.NoError(t, err)

	directories := []string{dir1, dir2}
	lockFileName := "test.lock"

	// First process locks directories
	release1, err := LockDirectories(logger, directories, lockFileName, false)
	require.NoError(t, err)
	require.NotNil(t, release1)

	// Second process tries to lock same directories - should fail
	release2, err := LockDirectories(logger, directories, lockFileName, false)
	require.Error(t, err)
	require.Nil(t, release2)
	require.Contains(t, err.Error(), "failed to acquire lock on directory")

	// Release first lock
	release1()

	// Now second process should be able to lock
	release2, err = LockDirectories(logger, directories, lockFileName, false)
	require.NoError(t, err)
	require.NotNil(t, release2)

	// Clean up
	release2()
}

func TestLockDirectoriesStaleLockRecovery(t *testing.T) {
	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	tempDir := t.TempDir()

	// Create directories
	dir1 := filepath.Join(tempDir, "dir1")
	dir2 := filepath.Join(tempDir, "dir2")

	err = os.MkdirAll(dir1, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(dir2, 0755)
	require.NoError(t, err)

	lockFileName := "test.lock"

	// Create stale lock files with non-existent PIDs
	stalePID := 999999
	staleContent := fmt.Sprintf("PID: %d\nTimestamp: 2023-01-01T00:00:00Z\n", stalePID)

	staleLockPath1 := filepath.Join(dir1, lockFileName)
	err = os.WriteFile(staleLockPath1, []byte(staleContent), 0644)
	require.NoError(t, err)

	staleLockPath2 := filepath.Join(dir2, lockFileName)
	err = os.WriteFile(staleLockPath2, []byte(staleContent), 0644)
	require.NoError(t, err)

	directories := []string{dir1, dir2}

	// Should succeed by removing stale locks
	release, err := LockDirectories(logger, directories, lockFileName, false)
	require.NoError(t, err)
	require.NotNil(t, release)

	// Verify lock files now contain our PID
	for _, dir := range directories {
		lockPath := filepath.Join(dir, lockFileName)
		content, err := os.ReadFile(lockPath)
		require.NoError(t, err)
		require.Contains(t, string(content), fmt.Sprintf("PID: %d", os.Getpid()))
	}

	// Clean up
	release()
}
