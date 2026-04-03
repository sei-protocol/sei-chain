package util

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"

	"github.com/Layr-Labs/eigenda/common"
	"github.com/stretchr/testify/require"
)

func TestSSHSession_NewSSHSession(t *testing.T) {
	t.Skip() // Docker build is flaky, need to fix prior to re-enabling

	t.Parallel()

	container := SetupSSHTestContainer(t, "")
	defer container.Cleanup()

	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	// Test successful connection
	session, err := NewSSHSession(
		logger,
		container.GetUser(),
		container.GetHost(),
		container.GetSSHPort(),
		container.GetPrivateKeyPath(),
		"",
		true)
	require.NoError(t, err)
	require.NotNil(t, session)
	defer func() { _ = session.Close() }()

	// Test with non-existent key
	_, err = NewSSHSession(
		logger,
		container.GetUser(),
		container.GetHost(),
		container.GetSSHPort(),
		"/nonexistent/key",
		"",
		false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "private key does not exist")

	// Test with wrong user
	_, err = NewSSHSession(
		logger,
		"wronguser",
		container.GetHost(),
		container.GetSSHPort(),
		container.GetPrivateKeyPath(),
		"",
		false)
	require.Error(t, err)
}

func TestSSHSession_Mkdirs(t *testing.T) {
	t.Skip() // Docker build is flaky, need to fix prior to re-enabling

	t.Parallel()

	dataDir := t.TempDir()

	container := SetupSSHTestContainer(t, dataDir)
	defer container.Cleanup()

	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	session, err := NewSSHSession(
		logger,
		container.GetUser(),
		container.GetHost(),
		container.GetSSHPort(),
		container.GetPrivateKeyPath(),
		"",
		true)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	// Test creating directory
	testDir := path.Join(container.GetDataDir(), "foo", "bar", "baz")
	err = session.Mkdirs(testDir)
	require.NoError(t, err)

	// Verify directories were created in the container workspace
	exists, err := Exists(path.Join(dataDir, "foo", "bar", "baz"))
	require.NoError(t, err)
	require.True(t, exists)

	// Recreating the same directory should not error.
	err = session.Mkdirs(testDir)
	require.NoError(t, err)
}

func TestSSHSession_FindFiles(t *testing.T) {
	t.Skip() // Docker build is flaky, need to fix prior to re-enabling

	t.Parallel()

	dataDir := t.TempDir()

	container := SetupSSHTestContainer(t, dataDir)
	defer container.Cleanup()

	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	session, err := NewSSHSession(
		logger,
		container.GetUser(),
		container.GetHost(),
		container.GetSSHPort(),
		container.GetPrivateKeyPath(),
		"",
		true)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	// Create a test subdirectory in the container's data directory
	testDir := path.Join(container.GetDataDir(), "search")
	err = session.Mkdirs(testDir)
	require.NoError(t, err)

	// Create test files via SSH instead of host filesystem to avoid permission issues
	// This ensures all files are created with proper container ownership
	_, _, err = session.Exec(fmt.Sprintf("echo 'test content' > %s/test.txt", testDir))
	require.NoError(t, err)
	_, _, err = session.Exec(fmt.Sprintf("echo 'log content' > %s/test.log", testDir))
	require.NoError(t, err)
	_, _, err = session.Exec(fmt.Sprintf("echo 'data content' > %s/other.dat", testDir))
	require.NoError(t, err)

	// Test finding files with specific extensions
	files, err := session.FindFiles(testDir, []string{".txt", ".log"})
	require.NoError(t, err)
	require.Len(t, files, 2)
	require.Contains(t, files, path.Join(testDir, "test.txt"))
	require.Contains(t, files, path.Join(testDir, "test.log"))

	// Test with non-existent directory
	files, err = session.FindFiles("/nonexistent", []string{".txt"})
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestSSHSession_Rsync(t *testing.T) {
	t.Skip() // Docker build is flaky, need to fix prior to re-enabling

	t.Parallel()

	// Create a temporary data directory for testing
	dataDir := t.TempDir()
	container := SetupSSHTestContainer(t, dataDir)
	defer container.Cleanup()

	logger, err := common.NewLogger(common.DefaultConsoleLoggerConfig())
	require.NoError(t, err)

	session, err := NewSSHSession(
		logger,
		container.GetUser(),
		container.GetHost(),
		container.GetSSHPort(),
		container.GetPrivateKeyPath(),
		"",
		true)
	require.NoError(t, err)
	defer func() { _ = session.Close() }()

	// Create local test file
	localFile := filepath.Join(container.GetTempDir(), "test_rsync.txt")
	testContent := []byte("This is test content for rsync")
	err = os.WriteFile(localFile, testContent, 0644)
	require.NoError(t, err)

	// Test rsync without throttling - sync to data directory
	remoteFile := filepath.Join(container.GetDataDir(), "remote_file.txt")
	err = session.Rsync(localFile, remoteFile, 0)
	require.NoError(t, err)

	// Verify file was transferred via the container workspace directory
	transferredFile := filepath.Join(dataDir, "remote_file.txt")
	transferredContent, err := os.ReadFile(transferredFile)
	require.NoError(t, err)
	require.Equal(t, testContent, transferredContent)

	// Test rsync with throttling
	localFile2 := filepath.Join(container.GetTempDir(), "test_rsync2.txt")
	throttledContent := []byte("throttled content")
	err = os.WriteFile(localFile2, throttledContent, 0644)
	require.NoError(t, err)

	remoteFile2 := filepath.Join(container.GetDataDir(), "throttled_file.txt")
	err = session.Rsync(localFile2, remoteFile2, 1.0) // 1MB/s throttle
	require.NoError(t, err)

	// Verify throttled file was transferred via the container workspace directory
	transferredFile2 := filepath.Join(dataDir, "throttled_file.txt")
	transferredContent2, err := os.ReadFile(transferredFile2)
	require.NoError(t, err)
	require.Equal(t, throttledContent, transferredContent2)
}
