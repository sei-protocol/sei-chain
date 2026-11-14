package util

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNewFileLock(t *testing.T) {
	tempDir := t.TempDir()
	logger := slog.Default()

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
			name: "lock held by another FileLock",
			setup: func() string {
				lockPath := filepath.Join(tempDir, "existing.lock")
				holder, err := NewFileLock(logger, lockPath, false)
				require.NoError(t, err)
				t.Cleanup(holder.Release)
				return lockPath
			},
			expectError: true,
		},
		{
			name: "lock file with this process's PID that no lock holds",
			setup: func() string {
				// A restarted container process often has the PID that its stopped predecessor recorded.
				lockPath := filepath.Join(tempDir, "own-pid.lock")
				content := fmt.Sprintf("PID: %d\nTimestamp: 2023-01-01T00:00:00Z\n", os.Getpid())
				err := os.WriteFile(lockPath, []byte(content), 0644)
				require.NoError(t, err)
				return lockPath
			},
			expectError: false,
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
			name: "malformed lock file that no lock holds gets overridden",
			setup: func() string {
				lockPath := filepath.Join(tempDir, "malformed.lock")
				// Create a lock file without proper PID format
				err := os.WriteFile(lockPath, []byte("invalid content"), 0644)
				require.NoError(t, err)
				return lockPath
			},
			expectError: false,
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

	logger := slog.Default()

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

	logger := slog.Default()

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

	var successCount int32
	var wg sync.WaitGroup
	results := make(chan bool, numGoroutines)

	// The holder keeps the lock until every goroutine has made its attempt.
	var attempted sync.WaitGroup
	attempted.Add(numGoroutines)

	logger := slog.Default()

	// Launch multiple goroutines trying to acquire the same lock
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			lock, err := NewFileLock(logger, lockPath, false)
			attempted.Done()
			if err != nil {
				results <- false
				return
			}

			attempted.Wait()
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

	logger := slog.Default()

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

	logger := slog.Default()

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

// lockHolderEnv names the lock file that TestFileLockReleasedWhenHolderProcessDies asks its child process to hold.
const lockHolderEnv = "LITT_FILE_LOCK_HOLDER_PATH"

func TestFileLockReleasedWhenHolderProcessDies(t *testing.T) {
	if lockPath := os.Getenv(lockHolderEnv); lockPath != "" {
		holdLockUntilKilled(lockPath)
		return
	}

	lockPath := filepath.Join(t.TempDir(), "holder.lock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestFileLockReleasedWhenHolderProcessDies$") //nolint:gosec // re-runs this test binary
	cmd.Env = append(os.Environ(), lockHolderEnv+"="+lockPath)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	waitForHolder(t, stdout)

	_, err = NewFileLock(slog.Default(), lockPath, false)
	require.ErrorContains(t, err, "another process holds it")

	require.NoError(t, cmd.Process.Kill())
	_ = cmd.Wait()
	content, err := os.ReadFile(lockPath)
	require.NoError(t, err)
	require.Contains(t, string(content), fmt.Sprintf("PID: %d", cmd.Process.Pid), "the killed holder leaves its lock file")

	lock, err := NewFileLock(slog.Default(), lockPath, false)
	require.NoError(t, err)
	lock.Release()
}

// holdLockUntilKilled takes the lock at lockPath, reports it on stdout, and never returns.
func holdLockUntilKilled(lockPath string) {
	if _, err := NewFileLock(slog.Default(), lockPath, false); err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
	fmt.Println("locked")
	time.Sleep(time.Hour)
	os.Exit(1)
}

// waitForHolder reads the child's stdout until it reports that it holds the lock.
func waitForHolder(t *testing.T, stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		line := scanner.Text()
		require.False(t, strings.HasPrefix(line, "error:"), line)
		if line == "locked" {
			return
		}
	}
	t.Fatalf("holder process exited without taking the lock: %v", scanner.Err())
}

func TestFileLockExclusionUnderChurn(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "churn.lock")
	logger := slog.Default()

	var holders, acquisitions atomic.Int32
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 500 {
				lock, err := NewFileLock(logger, lockPath, false)
				if err != nil {
					continue
				}
				acquisitions.Add(1)
				if n := holders.Add(1); n != 1 {
					t.Errorf("%d FileLocks hold %s at once", n, lockPath)
				}
				time.Sleep(10 * time.Microsecond)
				holders.Add(-1)
				lock.Release()
			}
		}()
	}
	wg.Wait()
	require.Positive(t, acquisitions.Load())
}

func TestStaleLockRecovery(t *testing.T) {
	tempDir := t.TempDir()
	lockPath := filepath.Join(tempDir, "stale-recovery.lock")

	logger := slog.Default()

	// Create a stale lock file with a definitely dead PID
	stalePID := 999999
	staleContent := fmt.Sprintf("PID: %d\nTimestamp: 2023-01-01T00:00:00Z\n", stalePID)
	err := os.WriteFile(lockPath, []byte(staleContent), 0644)
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
	logger := slog.Default()

	tempDir := t.TempDir()

	// Create multiple directories
	dir1 := filepath.Join(tempDir, "dir1")
	dir2 := filepath.Join(tempDir, "dir2")
	dir3 := filepath.Join(tempDir, "dir3")

	err := os.MkdirAll(dir1, 0755)
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
	logger := slog.Default()

	tempDir := t.TempDir()

	// Create multiple directories
	dir1 := filepath.Join(tempDir, "dir1")
	dir2 := filepath.Join(tempDir, "dir2")
	dir3 := filepath.Join(tempDir, "dir3")

	err := os.MkdirAll(dir1, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(dir2, 0755)
	require.NoError(t, err)
	err = os.MkdirAll(dir3, 0755)
	require.NoError(t, err)

	lockFileName := "test.lock"

	// Hold an existing lock in dir2
	existingLock, err := NewFileLock(logger, filepath.Join(dir2, lockFileName), false)
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
	existingLock.Release()
}

func TestLockDirectoriesFailureWhenDirectoryDoesNotExist(t *testing.T) {
	logger := slog.Default()

	tempDir := t.TempDir()

	// Create some directories but not all
	dir1 := filepath.Join(tempDir, "dir1")
	dir2 := filepath.Join(tempDir, "nonexistent")
	dir3 := filepath.Join(tempDir, "dir3")

	err := os.MkdirAll(dir1, 0755)
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
	logger := slog.Default()

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
	logger := slog.Default()

	tempDir := t.TempDir()

	// Create directories
	dir1 := filepath.Join(tempDir, "dir1")
	dir2 := filepath.Join(tempDir, "dir2")

	err := os.MkdirAll(dir1, 0755)
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
	logger := slog.Default()

	tempDir := t.TempDir()

	// Create directories
	dir1 := filepath.Join(tempDir, "dir1")
	dir2 := filepath.Join(tempDir, "dir2")

	err := os.MkdirAll(dir1, 0755)
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
