package disktable

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadBoundaryFileNonExistentFile(t *testing.T) {
	tempDir := t.TempDir()

	// Test loading lower bound file that doesn't exist
	lowerBoundary, err := LoadBoundaryFile(LowerBound, tempDir)
	require.NoError(t, err)
	require.NotNil(t, lowerBoundary)
	require.False(t, lowerBoundary.IsDefined())
	require.Equal(t, uint32(0), lowerBoundary.BoundaryIndex())

	// Test loading upper bound file that doesn't exist
	upperBoundary, err := LoadBoundaryFile(UpperBound, tempDir)
	require.NoError(t, err)
	require.NotNil(t, upperBoundary)
	require.False(t, upperBoundary.IsDefined())
	require.Equal(t, uint32(0), upperBoundary.BoundaryIndex())
}

func TestLoadBoundaryFileExistingFile(t *testing.T) {
	tempDir := t.TempDir()

	// Create a lower bound file
	lowerBoundPath := filepath.Join(tempDir, LowerBoundFileName)
	err := os.WriteFile(lowerBoundPath, []byte("123\n"), 0644)
	require.NoError(t, err)

	// Create an upper bound file
	upperBoundPath := filepath.Join(tempDir, UpperBoundFileName)
	err = os.WriteFile(upperBoundPath, []byte("456"), 0644)
	require.NoError(t, err)

	// Load lower bound file
	lowerBoundary, err := LoadBoundaryFile(LowerBound, tempDir)
	require.NoError(t, err)
	require.NotNil(t, lowerBoundary)
	require.True(t, lowerBoundary.IsDefined())
	require.Equal(t, uint32(123), lowerBoundary.BoundaryIndex())

	// Load upper bound file
	upperBoundary, err := LoadBoundaryFile(UpperBound, tempDir)
	require.NoError(t, err)
	require.NotNil(t, upperBoundary)
	require.True(t, upperBoundary.IsDefined())
	require.Equal(t, uint32(456), upperBoundary.BoundaryIndex())
}

func TestLoadBoundaryFileInvalidContent(t *testing.T) {
	tempDir := t.TempDir()

	// Create a file with invalid content
	boundaryPath := filepath.Join(tempDir, LowerBoundFileName)
	err := os.WriteFile(boundaryPath, []byte("not_a_number"), 0644)
	require.NoError(t, err)

	// Loading should fail
	_, err = LoadBoundaryFile(LowerBound, tempDir)
	require.Error(t, err)
}

func TestName(t *testing.T) {
	tempDir := t.TempDir()

	// Test lower bound file name
	lowerBoundary, err := LoadBoundaryFile(LowerBound, tempDir)
	require.NoError(t, err)
	require.Equal(t, LowerBoundFileName, lowerBoundary.Name())

	// Test upper bound file name
	upperBoundary, err := LoadBoundaryFile(UpperBound, tempDir)
	require.NoError(t, err)
	require.Equal(t, UpperBoundFileName, upperBoundary.Name())

	// Test nil boundary
	var nilBoundary *BoundaryFile
	require.Equal(t, "", nilBoundary.Name())
}

func TestPath(t *testing.T) {
	tempDir := t.TempDir()

	// Test lower bound file path
	lowerBoundary, err := LoadBoundaryFile(LowerBound, tempDir)
	require.NoError(t, err)
	expectedLowerPath := filepath.Join(tempDir, LowerBoundFileName)
	require.Equal(t, expectedLowerPath, lowerBoundary.Path())

	// Test upper bound file path
	upperBoundary, err := LoadBoundaryFile(UpperBound, tempDir)
	require.NoError(t, err)
	expectedUpperPath := filepath.Join(tempDir, UpperBoundFileName)
	require.Equal(t, expectedUpperPath, upperBoundary.Path())

	// Test nil boundary
	var nilBoundary *BoundaryFile
	require.Equal(t, "", nilBoundary.Path())
}

func TestUpdate(t *testing.T) {
	tempDir := t.TempDir()

	// Load boundary file (non-existent initially)
	boundary, err := LoadBoundaryFile(LowerBound, tempDir)
	require.NoError(t, err)
	require.False(t, boundary.IsDefined())

	// Update the boundary
	err = boundary.Update(42)
	require.NoError(t, err)
	require.True(t, boundary.IsDefined())
	require.Equal(t, uint32(42), boundary.BoundaryIndex())

	// Verify file was written
	expectedPath := filepath.Join(tempDir, LowerBoundFileName)
	content, err := os.ReadFile(expectedPath)
	require.NoError(t, err)
	require.Equal(t, "42\n", string(content))

	// Update again with different value
	err = boundary.Update(100)
	require.NoError(t, err)
	require.Equal(t, uint32(100), boundary.BoundaryIndex())

	// Verify file was updated
	content, err = os.ReadFile(expectedPath)
	require.NoError(t, err)
	require.Equal(t, "100\n", string(content))
}

func TestUpdateNilBoundary(t *testing.T) {
	var nilBoundary *BoundaryFile
	err := nilBoundary.Update(42)
	require.NoError(t, err) // Should not error on nil
}

func TestWrite(t *testing.T) {
	tempDir := t.TempDir()

	// Create boundary file
	boundary := &BoundaryFile{
		boundaryType:    LowerBound,
		parentDirectory: tempDir,
		defined:         true,
		boundaryIndex:   999,
	}

	// Write the file
	err := boundary.Write()
	require.NoError(t, err)

	// Verify file content
	expectedPath := filepath.Join(tempDir, LowerBoundFileName)
	content, err := os.ReadFile(expectedPath)
	require.NoError(t, err)
	require.Equal(t, "999\n", string(content))
}

func TestWriteNilBoundary(t *testing.T) {
	var nilBoundary *BoundaryFile
	err := nilBoundary.Write()
	require.NoError(t, err) // Should not error on nil
}

func TestIsDefined(t *testing.T) {
	tempDir := t.TempDir()

	// Test undefined boundary (newly loaded, no file exists)
	boundary, err := LoadBoundaryFile(LowerBound, tempDir)
	require.NoError(t, err)
	require.False(t, boundary.IsDefined())

	// Update to make it defined
	err = boundary.Update(50)
	require.NoError(t, err)
	require.True(t, boundary.IsDefined())

	// Test nil boundary
	var nilBoundary *BoundaryFile
	require.False(t, nilBoundary.IsDefined())
}

func TestBoundaryIndex(t *testing.T) {
	tempDir := t.TempDir()

	// Test undefined boundary
	boundary, err := LoadBoundaryFile(LowerBound, tempDir)
	require.NoError(t, err)
	require.Equal(t, uint32(0), boundary.BoundaryIndex())

	// Update and test defined boundary
	err = boundary.Update(789)
	require.NoError(t, err)
	require.Equal(t, uint32(789), boundary.BoundaryIndex())

	// Test nil boundary
	var nilBoundary *BoundaryFile
	require.Equal(t, uint32(0), nilBoundary.BoundaryIndex())
}

func TestSerialize(t *testing.T) {
	boundary := &BoundaryFile{
		boundaryType:    UpperBound,
		parentDirectory: "/tmp",
		defined:         true,
		boundaryIndex:   12345,
	}

	data := boundary.serialize()
	require.Equal(t, []byte("12345\n"), data)

	// Test nil boundary
	var nilBoundary *BoundaryFile
	require.Nil(t, nilBoundary.serialize())
}

func TestDeserialize(t *testing.T) {
	boundary := &BoundaryFile{
		boundaryType:    LowerBound,
		parentDirectory: "/tmp",
		defined:         false,
		boundaryIndex:   0,
	}

	// Test valid data
	err := boundary.deserialize([]byte("54321"))
	require.NoError(t, err)
	require.Equal(t, uint32(54321), boundary.boundaryIndex)

	// Test invalid data
	err = boundary.deserialize([]byte("invalid"))
	require.Error(t, err)

	// Test nil boundary
	var nilBoundary *BoundaryFile
	err = nilBoundary.deserialize([]byte("123"))
	require.NoError(t, err) // Should not error on nil
}

func TestRoundTrip(t *testing.T) {
	tempDir := t.TempDir()

	// Create and update a boundary file
	boundary, err := LoadBoundaryFile(LowerBound, tempDir)
	require.NoError(t, err)

	err = boundary.Update(98765)
	require.NoError(t, err)

	// Load the same file again and verify
	boundary2, err := LoadBoundaryFile(LowerBound, tempDir)
	require.NoError(t, err)
	require.True(t, boundary2.IsDefined())
	require.Equal(t, uint32(98765), boundary2.BoundaryIndex())
}
