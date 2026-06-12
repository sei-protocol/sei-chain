package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrIfNotWritableFile(t *testing.T) {
	// Setup
	tempDir := t.TempDir()

	// Test cases
	tests := []struct {
		name             string
		setup            func() string
		expectedExists   bool
		expectedSize     int64
		expectError      bool
		expectedErrorMsg string
	}{
		{
			name: "existing file with correct permissions",
			setup: func() string {
				path := filepath.Join(tempDir, "test-file")
				err := os.WriteFile(path, []byte("test data"), 0600)
				require.NoError(t, err)
				return path
			},
			expectedExists: true,
			expectedSize:   9, // "test data" is 9 bytes
			expectError:    false,
		},
		{
			name: "non-existent file with writable parent",
			setup: func() string {
				return filepath.Join(tempDir, "non-existent-file")
			},
			expectedExists: false,
			expectedSize:   -1,
			expectError:    false,
		},
		{
			name: "non-existent file with non-existent parent",
			setup: func() string {
				return filepath.Join(tempDir, "non-existent-dir", "non-existent-file")
			},
			expectedExists:   false,
			expectedSize:     -1,
			expectError:      true,
			expectedErrorMsg: "parent directory",
		},
		{
			name: "existing file is a directory",
			setup: func() string {
				path := filepath.Join(tempDir, "test-dir")
				err := os.Mkdir(path, 0755)
				require.NoError(t, err)
				return path
			},
			expectedExists:   false,
			expectedSize:     -1,
			expectError:      true,
			expectedErrorMsg: "is a directory",
		},
	}

	// Run tests
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.setup()
			exists, size, err := ErrIfNotWritableFile(path)

			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedErrorMsg)
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tc.expectedExists, exists)
			require.Equal(t, tc.expectedSize, size)
		})
	}
}

func TestExists(t *testing.T) {
	// Setup
	tempDir := t.TempDir()
	existingFile := filepath.Join(tempDir, "existing-file")
	err := os.WriteFile(existingFile, []byte("test"), 0600)
	require.NoError(t, err)

	nonExistentFile := filepath.Join(tempDir, "non-existent-file")

	// Test cases
	tests := []struct {
		name        string
		path        string
		expected    bool
		expectError bool
	}{
		{
			name:        "existing file",
			path:        existingFile,
			expected:    true,
			expectError: false,
		},
		{
			name:        "non-existent file",
			path:        nonExistentFile,
			expected:    false,
			expectError: false,
		},
	}

	// Run tests
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			exists, err := Exists(tc.path)

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, tc.expected, exists)
		})
	}
}

func TestErrIfNotWritableDirectory(t *testing.T) {
	// Setup
	tempDir := t.TempDir()

	// Create a non-writable directory (0500 = read & execute, no write)
	nonWritableDir := filepath.Join(tempDir, "non-writable-dir")
	err := os.Mkdir(nonWritableDir, 0500)
	require.NoError(t, err)

	// Create a writable directory
	writableDir := filepath.Join(tempDir, "writable-dir")
	err = os.Mkdir(writableDir, 0700)
	require.NoError(t, err)

	// Create a regular file
	regularFile := filepath.Join(tempDir, "regular-file")
	err = os.WriteFile(regularFile, []byte("test"), 0600)
	require.NoError(t, err)

	// Test cases
	tests := []struct {
		name        string
		path        string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "writable directory",
			path:        writableDir,
			expectError: false,
		},
		{
			name:        "non-writable directory",
			path:        nonWritableDir,
			expectError: true,
			errorMsg:    "not writable",
		},
		{
			name:        "regular file",
			path:        regularFile,
			expectError: true,
			errorMsg:    "is not a directory",
		},
		{
			name:        "non-existent directory with writable parent",
			path:        filepath.Join(writableDir, "non-existent"),
			expectError: false,
		},
	}

	// Run tests
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ErrIfNotWritableDirectory(tc.path)

			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errorMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}

	// Cleanup special permissions
	err = os.Chmod(nonWritableDir, 0700)
	require.NoError(t, err)
}

func TestEnsureParentDirExists(t *testing.T) {
	// Setup
	tempDir := t.TempDir()

	// Create a non-writable directory (0500 = read & execute, no write)
	nonWritableDir := filepath.Join(tempDir, "non-writable-dir")
	err := os.Mkdir(nonWritableDir, 0500)
	require.NoError(t, err)

	// Create a test file
	testFile := filepath.Join(tempDir, "test-file")
	err = os.WriteFile(testFile, []byte("test"), 0600)
	require.NoError(t, err)

	// Test cases
	tests := []struct {
		name        string
		path        string
		expectError bool
		errorMsg    string
	}{
		{
			name:        "parent exists and is writable",
			path:        filepath.Join(tempDir, "new-file"),
			expectError: false,
		},
		{
			name:        "multi-level parent doesn't exist",
			path:        filepath.Join(tempDir, "new-dir", "subdir", "new-file"),
			expectError: false,
		},
		{
			name:        "parent exists but is a file",
			path:        filepath.Join(testFile, "impossible"),
			expectError: true,
			errorMsg:    "is not a directory",
		},
	}

	// Run tests
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := EnsureParentDirectoryExists(tc.path, false)

			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errorMsg)
			} else {
				require.NoError(t, err)

				// Verify the parent directory was created if needed
				parentDir := filepath.Dir(tc.path)
				exists, err := Exists(parentDir)
				require.NoError(t, err)
				require.True(t, exists)
			}
		})
	}

	// Cleanup special permissions
	err = os.Chmod(nonWritableDir, 0700)
	require.NoError(t, err)
}

func TestCopyRegularFile(t *testing.T) {
	// Setup
	tempDir := t.TempDir()

	// Create a source file with specific content, permissions, and time
	sourceFile := filepath.Join(tempDir, "source-file")
	content := []byte("test content")
	err := os.WriteFile(sourceFile, content, 0640)
	require.NoError(t, err)

	// Test cases
	tests := []struct {
		name        string
		destPath    string
		expectError bool
	}{
		{
			name:        "copy to a new file",
			destPath:    filepath.Join(tempDir, "dest-file"),
			expectError: false,
		},
		{
			name:        "overwrite existing file",
			destPath:    filepath.Join(tempDir, "existing-file"),
			expectError: false,
		},
		{
			name:        "copy to a new subdirectory",
			destPath:    filepath.Join(tempDir, "subdir", "dest-file"),
			expectError: false,
		},
	}

	// Run tests
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// If testing overwrite, create the file first
			if tc.name == "overwrite existing file" {
				err := os.WriteFile(tc.destPath, []byte("original content"), 0600)
				require.NoError(t, err)
			}

			err := CopyRegularFile(sourceFile, tc.destPath, false)

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)

				// Check content
				destContent, err := os.ReadFile(tc.destPath)
				require.NoError(t, err)
				require.Equal(t, content, destContent)
			}
		})
	}
}

func TestEnsureDirectoryExists(t *testing.T) {
	// Setup
	tempDir := t.TempDir()

	// Create a regular file
	regularFile := filepath.Join(tempDir, "regular-file")
	err := os.WriteFile(regularFile, []byte("test"), 0600)
	require.NoError(t, err)

	// Test cases
	tests := []struct {
		name        string
		dirPath     string
		setup       func(path string)
		expectError bool
		errorMsg    string
	}{
		{
			name:        "directory doesn't exist",
			dirPath:     filepath.Join(tempDir, "new-dir"),
			setup:       func(path string) {},
			expectError: false,
		},
		{
			name:    "directory already exists",
			dirPath: filepath.Join(tempDir, "existing-dir"),
			setup: func(path string) {
				err := os.Mkdir(path, 0755)
				require.NoError(t, err)
			},
			expectError: false,
		},
		{
			name:        "path exists but is a file",
			dirPath:     regularFile,
			setup:       func(path string) {},
			expectError: true,
			errorMsg:    "is not a directory",
		},
	}

	// Run tests
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.setup(tc.dirPath)

			err := EnsureDirectoryExists(tc.dirPath, false)

			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errorMsg)
			} else {
				require.NoError(t, err)

				// Verify the directory exists
				info, err := os.Stat(tc.dirPath)
				require.NoError(t, err)
				require.True(t, info.IsDir())

				// If we created a new directory, verify the mode
				if tc.name == "directory doesn't exist" {
					// Note: mode comparison can be tricky due to umask and OS differences
					// So we just check that it's writable
					require.True(t, info.Mode()&0200 != 0, "Directory should be writable")
				}
			}
		})
	}

	// Clean up non-writable directory
	nonWritableDir := filepath.Join(tempDir, "non-writable-dir")
	if _, err := os.Stat(nonWritableDir); err == nil {
		err = os.Chmod(nonWritableDir, 0700)
		require.NoError(t, err)
	}
}

func TestEnsureParentDirectoryExists(t *testing.T) {
	testDir := t.TempDir()

	directoryPath := filepath.Join(testDir, "foo", "bar", "baz")
	filePath := filepath.Join(directoryPath, "data.txt")

	err := EnsureParentDirectoryExists(filePath, false)
	require.NoError(t, err, "failed to create directory")

	exists, err := Exists(directoryPath)
	require.NoError(t, err, "failed to check if directory exists")
	require.True(t, exists, "directory does not exist")

	// Utility should not have created the file, just the parent.
	exists, err = Exists(filePath)
	require.NoError(t, err, "failed to check if file 1exists")
	require.False(t, exists, "file should not exist")

	// Calling the same method again should not cause an error.
	err = EnsureParentDirectoryExists(filePath, false)
	require.NoError(t, err)
}

func TestAtomicWrite(t *testing.T) {
	// Setup
	tempDir := t.TempDir()

	// Test cases
	tests := []struct {
		name        string
		setup       func() (string, []byte)
		expectError bool
		errorMsg    string
	}{
		{
			name: "write to new file",
			setup: func() (string, []byte) {
				path := filepath.Join(tempDir, "new-file.txt")
				data := []byte("test content")
				return path, data
			},
			expectError: false,
		},
		{
			name: "overwrite existing file",
			setup: func() (string, []byte) {
				path := filepath.Join(tempDir, "existing-file.txt")
				// Create existing file with different content
				err := os.WriteFile(path, []byte("old content"), 0644)
				require.NoError(t, err)
				data := []byte("new content")
				return path, data
			},
			expectError: false,
		},
		{
			name: "write to subdirectory",
			setup: func() (string, []byte) {
				subDir := filepath.Join(tempDir, "subdir")
				err := os.Mkdir(subDir, 0755)
				require.NoError(t, err)
				path := filepath.Join(subDir, "file.txt")
				data := []byte("content in subdirectory")
				return path, data
			},
			expectError: false,
		},
		{
			name: "write with empty data",
			setup: func() (string, []byte) {
				path := filepath.Join(tempDir, "empty-file.txt")
				data := []byte("")
				return path, data
			},
			expectError: false,
		},
		{
			name: "write to non-existent parent directory",
			setup: func() (string, []byte) {
				path := filepath.Join(tempDir, "non-existent-dir", "file.txt")
				data := []byte("content")
				return path, data
			},
			expectError: true,
			errorMsg:    "failed to create swap file",
		},
		{
			name: "write with large data",
			setup: func() (string, []byte) {
				path := filepath.Join(tempDir, "large-file.txt")
				// Create 1MB of data
				data := make([]byte, 1024*1024)
				for i := range data {
					data[i] = byte(i % 256)
				}
				return path, data
			},
			expectError: false,
		},
	}

	// Run tests
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path, data := tc.setup()
			swapPath := path + SwapFileExtension

			// Ensure swap file doesn't exist before test
			_, err := os.Stat(swapPath)
			require.True(t, os.IsNotExist(err), "Swap file should not exist before test")

			err = AtomicWrite(path, data, true)

			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errorMsg)

				// Verify that the destination file wasn't created or modified
				if tc.name == "overwrite existing file" {
					// Original file should still have old content
					content, err := os.ReadFile(path)
					require.NoError(t, err)
					require.Equal(t, "old content", string(content))
				}
			} else {
				require.NoError(t, err)

				// Verify the file was written correctly
				content, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, data, content)

				// Verify the swap file was cleaned up
				_, err = os.Stat(swapPath)
				require.True(t, os.IsNotExist(err), "Swap file should be cleaned up after successful write")

				// Verify file permissions are reasonable (at least owner readable/writable)
				info, err := os.Stat(path)
				require.NoError(t, err)
				require.True(t, info.Mode()&0600 != 0, "File should be readable and writable by owner")
			}
		})
	}
}

func TestAtomicWriteSwapFileCleanup(t *testing.T) {
	// Test that swap files are properly cleaned up even if something goes wrong
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "test-file.txt")
	swapPath := path + SwapFileExtension
	data := []byte("test content")

	// Simulate a scenario where swap file might be left behind
	// by creating a swap file manually first
	err := os.WriteFile(swapPath, []byte("old swap content"), 0644)
	require.NoError(t, err)

	// Verify swap file exists
	_, err = os.Stat(swapPath)
	require.NoError(t, err)

	// Now run AtomicWrite - it should overwrite the swap file and clean up
	err = AtomicWrite(path, data, true)
	require.NoError(t, err)

	// Verify the target file has the correct content
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, data, content)

	// Verify the swap file was cleaned up
	_, err = os.Stat(swapPath)
	require.True(t, os.IsNotExist(err), "Swap file should be cleaned up")
}

func TestAtomicWritePreservesOtherFiles(t *testing.T) {
	// Test that AtomicWrite doesn't interfere with other files in the same directory
	tempDir := t.TempDir()

	// Create some existing files
	file1 := filepath.Join(tempDir, "file1.txt")
	file2 := filepath.Join(tempDir, "file2.txt")
	targetFile := filepath.Join(tempDir, "target.txt")

	err := os.WriteFile(file1, []byte("content1"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(file2, []byte("content2"), 0644)
	require.NoError(t, err)

	// Perform atomic write on target file
	targetData := []byte("target content")
	err = AtomicWrite(targetFile, targetData, true)
	require.NoError(t, err)

	// Verify all files have correct content
	content1, err := os.ReadFile(file1)
	require.NoError(t, err)
	require.Equal(t, "content1", string(content1))

	content2, err := os.ReadFile(file2)
	require.NoError(t, err)
	require.Equal(t, "content2", string(content2))

	targetContent, err := os.ReadFile(targetFile)
	require.NoError(t, err)
	require.Equal(t, targetData, targetContent)
}

func TestAtomicRename(t *testing.T) {
	// Setup
	tempDir := t.TempDir()

	// Test cases
	tests := []struct {
		name        string
		setup       func() (string, string)
		expectError bool
		errorMsg    string
	}{
		{
			name: "rename file in same directory",
			setup: func() (string, string) {
				oldPath := filepath.Join(tempDir, "old-name.txt")
				newPath := filepath.Join(tempDir, "new-name.txt")
				err := os.WriteFile(oldPath, []byte("test content"), 0644)
				require.NoError(t, err)
				return oldPath, newPath
			},
			expectError: false,
		},
		{
			name: "rename file to different directory",
			setup: func() (string, string) {
				subDir := filepath.Join(tempDir, "subdir")
				err := os.Mkdir(subDir, 0755)
				require.NoError(t, err)

				oldPath := filepath.Join(tempDir, "file.txt")
				newPath := filepath.Join(subDir, "moved-file.txt")
				err = os.WriteFile(oldPath, []byte("content to move"), 0644)
				require.NoError(t, err)
				return oldPath, newPath
			},
			expectError: false,
		},
		{
			name: "overwrite existing file",
			setup: func() (string, string) {
				oldPath := filepath.Join(tempDir, "source.txt")
				newPath := filepath.Join(tempDir, "target.txt")

				// Create source file
				err := os.WriteFile(oldPath, []byte("source content"), 0644)
				require.NoError(t, err)

				// Create target file that will be overwritten
				err = os.WriteFile(newPath, []byte("target content"), 0644)
				require.NoError(t, err)

				return oldPath, newPath
			},
			expectError: false,
		},
		{
			name: "rename non-existent file",
			setup: func() (string, string) {
				oldPath := filepath.Join(tempDir, "non-existent.txt")
				newPath := filepath.Join(tempDir, "new.txt")
				return oldPath, newPath
			},
			expectError: true,
			errorMsg:    "failed to rename file",
		},
		{
			name: "rename to non-existent directory",
			setup: func() (string, string) {
				oldPath := filepath.Join(tempDir, "existing.txt")
				newPath := filepath.Join(tempDir, "non-existent-dir", "file.txt")
				err := os.WriteFile(oldPath, []byte("content"), 0644)
				require.NoError(t, err)
				return oldPath, newPath
			},
			expectError: true,
			errorMsg:    "failed to rename file",
		},
		{
			name: "rename directory",
			setup: func() (string, string) {
				oldDir := filepath.Join(tempDir, "old-dir")
				newDir := filepath.Join(tempDir, "new-dir")

				err := os.Mkdir(oldDir, 0755)
				require.NoError(t, err)

				// Add a file inside the directory
				err = os.WriteFile(filepath.Join(oldDir, "file.txt"), []byte("dir content"), 0644)
				require.NoError(t, err)

				return oldDir, newDir
			},
			expectError: false,
		},
		{
			name: "rename with same source and destination",
			setup: func() (string, string) {
				path := filepath.Join(tempDir, "same-file.txt")
				err := os.WriteFile(path, []byte("content"), 0644)
				require.NoError(t, err)
				return path, path
			},
			expectError: false, // os.Rename typically succeeds for same path
		},
	}

	// Run tests
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			oldPath, newPath := tc.setup()

			// Store original content if file exists
			var originalContent []byte
			var originalInfo os.FileInfo
			if info, err := os.Stat(oldPath); err == nil {
				if !info.IsDir() {
					originalContent, err = os.ReadFile(oldPath)
					require.NoError(t, err)
				}
				originalInfo = info
			}

			err := AtomicRename(oldPath, newPath, true)

			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errorMsg)

				// Verify original file still exists (rename failed)
				if originalInfo != nil {
					_, err := os.Stat(oldPath)
					if tc.errorMsg == "failed to rename file" {
						require.NoError(t, err, "Original file should still exist after failed rename")
					}
				}
			} else {
				require.NoError(t, err)

				// Verify the rename was successful
				if tc.name != "rename with same source and destination" {
					// Old path should not exist
					_, err := os.Stat(oldPath)
					require.True(t, os.IsNotExist(err), "Old path should not exist after successful rename")
				}

				// New path should exist
				newInfo, err := os.Stat(newPath)
				require.NoError(t, err, "New path should exist after successful rename")

				// Verify content and properties if it was a file
				if originalInfo != nil && !originalInfo.IsDir() {
					if tc.name != "rename with same source and destination" {
						// Check content preservation
						newContent, err := os.ReadFile(newPath)
						require.NoError(t, err)
						require.Equal(t, originalContent, newContent, "File content should be preserved")
					}

					// Check that it's still a file
					require.False(t, newInfo.IsDir(), "Renamed file should still be a file")
				} else if originalInfo != nil && originalInfo.IsDir() {
					// Check that it's still a directory
					require.True(t, newInfo.IsDir(), "Renamed directory should still be a directory")

					// Check that directory contents are preserved
					if tc.name == "rename directory" {
						fileContent, err := os.ReadFile(filepath.Join(newPath, "file.txt"))
						require.NoError(t, err)
						require.Equal(t, "dir content", string(fileContent))
					}
				}
			}
		})
	}
}

func TestAtomicRenamePreservesPermissions(t *testing.T) {
	// Test that file permissions are preserved during atomic rename
	tempDir := t.TempDir()

	oldPath := filepath.Join(tempDir, "source.txt")
	newPath := filepath.Join(tempDir, "dest.txt")

	// Create file with specific permissions
	err := os.WriteFile(oldPath, []byte("test content"), 0640)
	require.NoError(t, err)

	// Get original permissions
	originalInfo, err := os.Stat(oldPath)
	require.NoError(t, err)

	// Perform atomic rename
	err = AtomicRename(oldPath, newPath, true)
	require.NoError(t, err)

	// Verify permissions are preserved
	newInfo, err := os.Stat(newPath)
	require.NoError(t, err)
	require.Equal(t, originalInfo.Mode(), newInfo.Mode(), "File permissions should be preserved")
}

func TestAtomicRenameWithSymlink(t *testing.T) {
	tempDir := t.TempDir()

	// Create a target file
	targetFile := filepath.Join(tempDir, "target.txt")
	err := os.WriteFile(targetFile, []byte("target content"), 0644)
	require.NoError(t, err)

	// Create a symlink
	oldLink := filepath.Join(tempDir, "old-link")
	err = os.Symlink(targetFile, oldLink)
	require.NoError(t, err)

	// Rename the symlink
	newLink := filepath.Join(tempDir, "new-link")
	err = AtomicRename(oldLink, newLink, true)
	require.NoError(t, err)

	// Verify the symlink was renamed and still points to the same target
	linkTarget, err := os.Readlink(newLink)
	require.NoError(t, err)
	require.Equal(t, targetFile, linkTarget)

	// Verify old symlink no longer exists
	_, err = os.Stat(oldLink)
	require.True(t, os.IsNotExist(err))
}

const mixedSwapFilesTestName = "delete swap files in directory with mixed files"

func TestDeleteOrphanedSwapFiles(t *testing.T) {
	// Setup
	tempDir := t.TempDir()

	// Test cases
	tests := []struct {
		name        string
		setup       func() string
		expectError bool
		errorMsg    string
	}{
		{
			name: mixedSwapFilesTestName,
			setup: func() string {
				testDir := filepath.Join(tempDir, "mixed-files")
				err := os.Mkdir(testDir, 0755)
				require.NoError(t, err)

				// Create regular files
				err = os.WriteFile(filepath.Join(testDir, "regular1.txt"), []byte("content1"), 0644)
				require.NoError(t, err)
				err = os.WriteFile(filepath.Join(testDir, "regular2.log"), []byte("content2"), 0644)
				require.NoError(t, err)

				// Create swap files
				err = os.WriteFile(filepath.Join(testDir, "file1.txt"+SwapFileExtension), []byte("swap1"), 0644)
				require.NoError(t, err)
				err = os.WriteFile(filepath.Join(testDir, "file2.log"+SwapFileExtension), []byte("swap2"), 0644)
				require.NoError(t, err)
				err = os.WriteFile(filepath.Join(testDir, "orphaned"+SwapFileExtension), []byte("orphaned"), 0644)
				require.NoError(t, err)

				// Create a subdirectory (should be ignored)
				subDir := filepath.Join(testDir, "subdir")
				err = os.Mkdir(subDir, 0755)
				require.NoError(t, err)

				// Create a swap file in subdirectory (should not be deleted by this call)
				err = os.WriteFile(filepath.Join(subDir, "nested"+SwapFileExtension), []byte("nested"), 0644)
				require.NoError(t, err)

				return testDir
			},
			expectError: false,
		},
		{
			name: "empty directory",
			setup: func() string {
				testDir := filepath.Join(tempDir, "empty-dir")
				err := os.Mkdir(testDir, 0755)
				require.NoError(t, err)
				return testDir
			},
			expectError: false,
		},
		{
			name: "directory with only swap files",
			setup: func() string {
				testDir := filepath.Join(tempDir, "only-swap")
				err := os.Mkdir(testDir, 0755)
				require.NoError(t, err)

				// Create only swap files
				err = os.WriteFile(filepath.Join(testDir, "swap1"+SwapFileExtension), []byte("content1"), 0644)
				require.NoError(t, err)
				err = os.WriteFile(filepath.Join(testDir, "swap2"+SwapFileExtension), []byte("content2"), 0644)
				require.NoError(t, err)

				return testDir
			},
			expectError: false,
		},
		{
			name: "directory with no swap files",
			setup: func() string {
				testDir := filepath.Join(tempDir, "no-swap")
				err := os.Mkdir(testDir, 0755)
				require.NoError(t, err)

				// Create only regular files
				err = os.WriteFile(filepath.Join(testDir, "file1.txt"), []byte("content1"), 0644)
				require.NoError(t, err)
				err = os.WriteFile(filepath.Join(testDir, "file2.log"), []byte("content2"), 0644)
				require.NoError(t, err)

				return testDir
			},
			expectError: false,
		},
		{
			name: "non-existent directory",
			setup: func() string {
				return filepath.Join(tempDir, "non-existent")
			},
			expectError: true,
			errorMsg:    "failed to read directory",
		},
		{
			name: "path is a file not directory",
			setup: func() string {
				filePath := filepath.Join(tempDir, "not-a-dir.txt")
				err := os.WriteFile(filePath, []byte("content"), 0644)
				require.NoError(t, err)
				return filePath
			},
			expectError: true,
			errorMsg:    "failed to read directory",
		},
	}

	// Run tests
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dirPath := tc.setup()

			// Count files before deletion for verification
			var beforeFiles []string
			if entries, err := os.ReadDir(dirPath); err == nil {
				for _, entry := range entries {
					if !entry.IsDir() {
						beforeFiles = append(beforeFiles, entry.Name())
					}
				}
			}

			err := DeleteOrphanedSwapFiles(dirPath)

			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errorMsg)
			} else {
				require.NoError(t, err)

				// Verify that all swap files were deleted
				entries, err := os.ReadDir(dirPath)
				require.NoError(t, err)

				var afterFiles []string
				var afterSwapFiles []string
				for _, entry := range entries {
					if !entry.IsDir() {
						afterFiles = append(afterFiles, entry.Name())
						if filepath.Ext(entry.Name()) == SwapFileExtension {
							afterSwapFiles = append(afterSwapFiles, entry.Name())
						}
					}
				}

				// No swap files should remain
				require.Empty(t, afterSwapFiles, "All swap files should be deleted")

				// Regular files should remain unchanged
				var beforeRegularFiles []string
				var afterRegularFiles []string
				for _, file := range beforeFiles {
					if filepath.Ext(file) != SwapFileExtension {
						beforeRegularFiles = append(beforeRegularFiles, file)
					}
				}
				for _, file := range afterFiles {
					if filepath.Ext(file) != SwapFileExtension {
						afterRegularFiles = append(afterRegularFiles, file)
					}
				}
				require.ElementsMatch(t, beforeRegularFiles, afterRegularFiles, "Regular files should be unchanged")

				// Verify subdirectories are not affected
				if tc.name == mixedSwapFilesTestName {
					subDirPath := filepath.Join(dirPath, "subdir")
					subEntries, err := os.ReadDir(subDirPath)
					require.NoError(t, err)
					require.Len(t, subEntries, 1, "Subdirectory should still contain its swap file")
					require.Equal(t, "nested"+SwapFileExtension, subEntries[0].Name())
				}
			}
		})
	}
}

func TestDeleteOrphanedSwapFilesPermissions(t *testing.T) {
	// Test behavior with permission issues
	tempDir := t.TempDir()

	// Create a directory with swap files
	testDir := filepath.Join(tempDir, "perm-test")
	err := os.Mkdir(testDir, 0755)
	require.NoError(t, err)

	// Create a swap file
	swapFile := filepath.Join(testDir, "test"+SwapFileExtension)
	err = os.WriteFile(swapFile, []byte("content"), 0644)
	require.NoError(t, err)

	// Make the directory read-only (no write permissions)
	err = os.Chmod(testDir, 0555) // read + execute only
	require.NoError(t, err)

	// Attempt to delete swap files should fail
	err = DeleteOrphanedSwapFiles(testDir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to remove swap file")

	// Restore permissions for cleanup
	err = os.Chmod(testDir, 0755)
	require.NoError(t, err)
}

func TestSanitizePath(t *testing.T) {
	// Get the current working directory and home directory for test expectations
	cwd, err := os.Getwd()
	require.NoError(t, err)

	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	// Test cases
	tests := []struct {
		name           string
		input          string
		expectedResult func() string // Function to compute expected result
		expectError    bool
		errorMsg       string
	}{
		{
			name:  "tilde expansion - home directory only",
			input: "~",
			expectedResult: func() string {
				return homeDir
			},
			expectError: false,
		},
		{
			name:  "tilde expansion - home directory with subdirectory",
			input: "~/Documents/test.txt",
			expectedResult: func() string {
				return filepath.Join(homeDir, "Documents/test.txt")
			},
			expectError: false,
		},
		{
			name:  "tilde expansion - home directory with nested subdirectories",
			input: "~/Documents/Projects/test-project/file.txt",
			expectedResult: func() string {
				return filepath.Join(homeDir, "Documents/Projects/test-project/file.txt")
			},
			expectError: false,
		},
		{
			name:  "absolute path - no changes needed",
			input: "/usr/local/bin/test",
			expectedResult: func() string {
				return "/usr/local/bin/test"
			},
			expectError: false,
		},
		{
			name:  "relative path - converted to absolute",
			input: "test-file.txt",
			expectedResult: func() string {
				return filepath.Join(cwd, "test-file.txt")
			},
			expectError: false,
		},
		{
			name:  "relative path with subdirectory",
			input: "subdir/test-file.txt",
			expectedResult: func() string {
				return filepath.Join(cwd, "subdir/test-file.txt")
			},
			expectError: false,
		},
		{
			name:  "path with redundant elements",
			input: "/usr/local/../local/bin/./test",
			expectedResult: func() string {
				return "/usr/local/bin/test"
			},
			expectError: false,
		},
		{
			name:  "path with current directory reference",
			input: "./test-file.txt",
			expectedResult: func() string {
				return filepath.Join(cwd, "test-file.txt")
			},
			expectError: false,
		},
		{
			name:  "path with parent directory reference",
			input: "../test-file.txt",
			expectedResult: func() string {
				return filepath.Join(filepath.Dir(cwd), "test-file.txt")
			},
			expectError: false,
		},
		{
			name:  "empty path",
			input: "",
			expectedResult: func() string {
				return cwd
			},
			expectError: false,
		},
		{
			name:  "path with multiple slashes",
			input: "/usr//local///bin/test",
			expectedResult: func() string {
				return "/usr/local/bin/test"
			},
			expectError: false,
		},
		{
			name:  "tilde in middle of path - not expanded",
			input: "/path/to/~user/file.txt",
			expectedResult: func() string {
				return "/path/to/~user/file.txt"
			},
			expectError: false,
		},
		{
			name:  "complex relative path with redundant elements",
			input: "./subdir/../another/./file.txt",
			expectedResult: func() string {
				return filepath.Join(cwd, "another/file.txt")
			},
			expectError: false,
		},
		{
			name:  "tilde with complex path",
			input: "~/Documents/../Downloads/./file.txt",
			expectedResult: func() string {
				return filepath.Join(homeDir, "Downloads/file.txt")
			},
			expectError: false,
		},
	}

	// Run tests
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := SanitizePath(tc.input)

			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errorMsg)
			} else {
				require.NoError(t, err)
				expected := tc.expectedResult()
				require.Equal(t, expected, result)

				// Verify the result is an absolute path
				require.True(t, filepath.IsAbs(result), "Result should be an absolute path")

				// Verify the path is clean (no redundant elements)
				require.Equal(t, filepath.Clean(result), result, "Result should be clean")
			}
		})
	}
}

func TestIsSymlink(t *testing.T) {
	testDir := t.TempDir()

	nonExistentPath := "non-existent-file.txt"
	isSymlink, err := IsSymlink(nonExistentPath)
	require.NoError(t, err)
	require.False(t, isSymlink, "Non-existent file should not be a symlink")
	err = ErrIfSymlink(nonExistentPath)
	require.NoError(t, err, "Non-existent file should not be a symlink")

	regularFilePath := filepath.Join(testDir, "file.txt")
	err = os.WriteFile(regularFilePath, []byte("test content"), 0644)
	require.NoError(t, err)
	isSymlink, err = IsSymlink(regularFilePath)
	require.NoError(t, err)
	require.False(t, isSymlink, "Regular file should not be a symlink")
	err = ErrIfSymlink(regularFilePath)
	require.NoError(t, err, "Regular file should not raise an error for being a symlink")

	isSymlink, err = IsSymlink(testDir)
	require.NoError(t, err)
	require.False(t, isSymlink, "Directory should not be a symlink")
	err = ErrIfSymlink(testDir)
	require.NoError(t, err, "Directory should not raise an error for being a symlink")

	symlinkToRegularFilePath := filepath.Join(testDir, "link-to-file.txt")
	err = os.Symlink(regularFilePath, symlinkToRegularFilePath)
	require.NoError(t, err)
	isSymlink, err = IsSymlink(symlinkToRegularFilePath)
	require.NoError(t, err)
	require.True(t, isSymlink, "Symlink to regular file should be detected as symlink")
	err = ErrIfSymlink(symlinkToRegularFilePath)
	require.Error(t, err, "Symlink to regular file should raise an error")

	symlinkToTestDirPath := filepath.Join(testDir, "link-to-dir")
	err = os.Symlink(testDir, symlinkToTestDirPath)
	require.NoError(t, err)
	isSymlink, err = IsSymlink(symlinkToTestDirPath)
	require.NoError(t, err)
	require.True(t, isSymlink, "Symlink to directory should be detected as symlink")
	err = ErrIfSymlink(symlinkToTestDirPath)
	require.Error(t, err, "Symlink to directory should raise an error")
}

// It's hard to know if the sync methods are actually doing what they should be doing. But at the very least,
// ensure that they don't crash.
func TestSync(t *testing.T) {
	testDir := t.TempDir()

	err := SyncPath(testDir)
	require.NoError(t, err, "SyncPath should not return an error")

	nestedDir := filepath.Join(testDir, "nested")
	err = os.Mkdir(nestedDir, 0755)
	require.NoError(t, err, "Creating nested directory should not return an error")
	err = SyncParentPath(nestedDir)
	require.NoError(t, err, "SyncParentPath should not return an error")

	regularFilePath := filepath.Join(testDir, "file.txt")
	err = os.WriteFile(regularFilePath, []byte("test content"), 0644)
	require.NoError(t, err, "Creating regular file should not return an error")
	err = SyncPath(regularFilePath)
	require.NoError(t, err, "SyncPath should not return an error")
}

func TestErrIfExists(t *testing.T) {
	testDir := t.TempDir()
	err := os.MkdirAll(testDir, 0755)
	require.NoError(t, err, "Failed to create test directory")

	err = ErrIfExists(testDir)
	require.Error(t, err)
	err = ErrIfNotExists(testDir)
	require.NoError(t, err, "Expected no error for existing directory")

	fooPath := filepath.Join(testDir, "foo")
	barPath := filepath.Join(testDir, "bar.txt")

	err = ErrIfExists(fooPath)
	require.NoError(t, err)
	err = ErrIfNotExists(fooPath)
	require.Error(t, err, "Expected error for non-existing directory")

	err = ErrIfExists(barPath)
	require.NoError(t, err)
	err = ErrIfNotExists(barPath)
	require.Error(t, err, "Expected error for non-existing file")

	err = os.MkdirAll(fooPath, 0755)
	require.NoError(t, err)

	err = os.WriteFile(barPath, []byte("test content"), 0644)
	require.NoError(t, err)

	err = ErrIfExists(fooPath)
	require.Error(t, err, "Expected error for existing directory")
	err = ErrIfNotExists(fooPath)
	require.NoError(t, err, "Expected no error for existing directory")

	err = ErrIfExists(barPath)
	require.Error(t, err, "Expected error for existing file")
	err = ErrIfNotExists(barPath)
	require.NoError(t, err, "Expected no error for existing file")
}

func TestDeepDelete(t *testing.T) {
	directory := t.TempDir()

	// Attempt to delete a non-existent path
	err := DeepDelete(filepath.Join(directory, "non-existent"))
	require.Error(t, err)

	// Delete an empty directory
	emptyDir := filepath.Join(directory, "empty-dir")
	err = os.Mkdir(emptyDir, 0755)
	require.NoError(t, err, "Failed to create empty directory")
	exists, err := Exists(emptyDir)
	require.NoError(t, err, "Failed to check if empty directory exists")
	require.True(t, exists, "Empty directory should exist")
	err = DeepDelete(emptyDir)
	require.NoError(t, err, "Failed to delete empty directory")
	exists, err = Exists(emptyDir)
	require.NoError(t, err, "Failed to check if empty directory exists after deletion")
	require.False(t, exists, "Empty directory should not exist after deletion")

	// Delete a regular file
	filePath := filepath.Join(directory, "file.txt")
	err = os.WriteFile(filePath, []byte("test content"), 0644)
	require.NoError(t, err, "Failed to create regular file")
	exists, err = Exists(filePath)
	require.NoError(t, err, "Failed to check if regular file exists")
	require.True(t, exists, "Regular file should exist before deletion")
	err = DeepDelete(filePath)
	require.NoError(t, err, "Failed to delete regular file")
	exists, err = Exists(filePath)
	require.NoError(t, err, "Failed to check if regular file exists after deletion")
	require.False(t, exists, "Regular file should not exist after deletion")

	// Attempt to delete a non-empty directory
	nonEmptyDir := filepath.Join(directory, "non-empty-dir")
	err = os.Mkdir(nonEmptyDir, 0755)
	require.NoError(t, err, "Failed to create non-empty directory")
	subFilePath := filepath.Join(nonEmptyDir, "subfile.txt")
	err = os.WriteFile(subFilePath, []byte("subfile content"), 0644)
	require.NoError(t, err, "Failed to create subfile in non-empty directory")
	exists, err = Exists(nonEmptyDir)
	require.NoError(t, err, "Failed to check if non-empty directory exists")
	require.True(t, exists, "Non-empty directory should exist before deletion")
	err = DeepDelete(nonEmptyDir)
	require.Error(t, err, "Expected error for non-empty directory")
	exists, err = Exists(nonEmptyDir)
	require.NoError(t, err, "Failed to check if non-empty directory exists after deletion attempt")
	require.True(t, exists, "Non-empty directory should still exist after deletion attempt")

	// Delete a symlink that points to a file
	targetFile := filepath.Join(directory, "target.txt")
	symlinkPath := filepath.Join(directory, "symlink-to-file")
	err = os.WriteFile(targetFile, []byte("target content"), 0644)
	require.NoError(t, err, "Failed to create target file for symlink")
	err = os.Symlink(targetFile, symlinkPath)
	require.NoError(t, err, "Failed to create symlink to file")
	exists, err = Exists(symlinkPath)
	require.NoError(t, err, "Failed to check if symlink to file exists")
	require.True(t, exists, "Symlink to file should exist before deletion")
	err = DeepDelete(symlinkPath)
	require.NoError(t, err, "Failed to delete symlink to file")
	exists, err = Exists(symlinkPath)
	require.NoError(t, err, "Failed to check if symlink to file exists after deletion")
	require.False(t, exists, "Symlink to file should not exist after deletion")
	exists, err = Exists(targetFile)
	require.NoError(t, err, "Failed to check if original file exists after deleting symlink")
	require.False(t, exists, "Original file should not exist after deleting symlink")

	// Delete a symlink that points to a directory
	dirToLink := filepath.Join(directory, "dir-to-link")
	err = os.Mkdir(dirToLink, 0755)
	require.NoError(t, err, "Failed to create directory for symlink")
	symlinkDirPath := filepath.Join(directory, "symlink-to-dir")
	err = os.Symlink(dirToLink, symlinkDirPath)
	require.NoError(t, err, "Failed to create symlink to directory")
	exists, err = Exists(symlinkDirPath)
	require.NoError(t, err, "Failed to check if symlink to directory exists")
	require.True(t, exists, "Symlink to directory should exist before deletion")
	err = DeepDelete(symlinkDirPath)
	require.NoError(t, err, "Failed to delete symlink to directory")
	exists, err = Exists(symlinkDirPath)
	require.NoError(t, err, "Failed to check if symlink to directory exists after deletion")
	require.False(t, exists, "Symlink to directory should not exist after deletion")
	exists, err = Exists(dirToLink)
	require.NoError(t, err, "Failed to check if original directory exists after deleting symlink")
	require.False(t, exists, "Original directory should not exist after deleting symlink")

	// Delete a symlink that points to a non-empty directory
	nonEmptyDirForSymlink := filepath.Join(directory, "non-empty-dir-for-symlink")
	err = os.Mkdir(nonEmptyDirForSymlink, 0755)
	require.NoError(t, err, "Failed to create non-empty directory for symlink")
	subFileForSymlink := filepath.Join(nonEmptyDirForSymlink, "subfile-for-symlink.txt")
	err = os.WriteFile(subFileForSymlink, []byte("subfile content for symlink"), 0644)
	require.NoError(t, err, "Failed to create subfile in non-empty directory for symlink")
	symlinkNonEmptyDirPath := filepath.Join(directory, "symlink-to-non-empty-dir")
	err = os.Symlink(nonEmptyDirForSymlink, symlinkNonEmptyDirPath)
	require.NoError(t, err, "Failed to create symlink to non-empty directory")
	exists, err = Exists(symlinkNonEmptyDirPath)
	require.NoError(t, err, "Failed to check if symlink to non-empty directory exists")
	require.True(t, exists, "Symlink to non-empty directory should exist before deletion")
	err = DeepDelete(symlinkNonEmptyDirPath)
	require.Error(t, err, "Expected error due to non-empty directory")
	exists, err = Exists(symlinkNonEmptyDirPath)
	require.NoError(t, err, "Failed to check if symlink to non-empty directory exists after deletion")
	require.True(t, exists, "Symlink to non-empty directory should exist after failed deletion")
	exists, err = Exists(nonEmptyDirForSymlink)
	require.NoError(t, err, "Failed to check if original non-empty directory exists after deleting symlink")
	require.True(t, exists, "Original non-empty directory should still exist after failed deletion")
}

func TestIsDirectory(t *testing.T) {
	testDir := t.TempDir()

	// non-existent path
	nonExistentPath := filepath.Join(testDir, "non-existent-dir")
	isDir, err := IsDirectory(nonExistentPath)
	require.NoError(t, err, "IsDirectory should not return an error for non-existent path")
	require.False(t, isDir, "Non-existent path should not be a directory")

	// path is a file
	filePath := filepath.Join(testDir, "file.txt")
	err = os.WriteFile(filePath, []byte("test content"), 0644)
	require.NoError(t, err, "Failed to create test file")
	isDir, err = IsDirectory(filePath)
	require.NoError(t, err, "IsDirectory should not return an error for file path")
	require.False(t, isDir, "File path should not be a directory")

	// path is a directory
	dirPath := filepath.Join(testDir, "test-dir")
	err = os.Mkdir(dirPath, 0755)
	require.NoError(t, err, "Failed to create test directory")
	isDir, err = IsDirectory(dirPath)
	require.NoError(t, err, "IsDirectory should not return an error for directory path")
	require.True(t, isDir, "Directory path should be recognized as a directory")
}
