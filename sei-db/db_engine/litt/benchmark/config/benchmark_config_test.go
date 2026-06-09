package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadConfig(t *testing.T) {
	// Create a temporary directory for the test
	tempDir := t.TempDir()

	testConfigJSON := `{
		"MetadataDirectory": "/test/dir",
		"MaximumWriteThroughputMB": 20.0,
		"ValueSizeMB": 3.0,
		"BatchSizeMB": 15
	}`

	testConfigPath := filepath.Join(tempDir, "test-config.json")
	err := os.WriteFile(testConfigPath, []byte(testConfigJSON), 0644)
	require.NoError(t, err)

	// Expected config for comparison
	expectedConfig := &BenchmarkConfig{
		MetadataDirectory:        "/test/dir",
		MaximumWriteThroughputMB: 20.0,
		ValueSizeMB:              3.0,
		BatchSizeMB:              15,
	}

	// Test loading the config
	loadedConfig, err := LoadConfig(testConfigPath)
	require.NoError(t, err)
	require.Equal(t, expectedConfig.MetadataDirectory, loadedConfig.MetadataDirectory)
	require.Equal(t, expectedConfig.MaximumWriteThroughputMB, loadedConfig.MaximumWriteThroughputMB)
	require.Equal(t, expectedConfig.ValueSizeMB, loadedConfig.ValueSizeMB)
	require.Equal(t, expectedConfig.BatchSizeMB, loadedConfig.BatchSizeMB)

	// Test loading a non-existent file
	_, err = LoadConfig("/non/existent/path.json")
	require.Error(t, err)

	// Test that unknown fields cause an error
	unknownFieldConfig := []byte(`{
		"MetadataDirectory": "/test/dir",
		"MaximumWriteThroughputMB": 20.0,
		"UnknownField": "this field doesn't exist in the struct"
	}`)

	unknownFieldPath := filepath.Join(tempDir, "unknown-field.json")
	err = os.WriteFile(unknownFieldPath, unknownFieldConfig, 0644)
	require.NoError(t, err)

	_, err = LoadConfig(unknownFieldPath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown field")
}
