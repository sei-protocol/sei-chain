package config

import (
	"bytes"
	"strings"
	"testing"
	"text/template"

	"github.com/stretchr/testify/require"
)

// TestStateCommitConfigTemplate verifies that all field paths in the TOML template
// are valid and can be resolved against the actual Config struct.
// This catches issues where config struct fields are renamed/moved but the template isn't updated.
func TestStateCommitConfigTemplate(t *testing.T) {
	// Create a config struct that mirrors how it's used in the server config
	type TemplateConfig struct {
		StateCommit StateCommitConfig
		StateStore  StateStoreConfig
	}

	cfg := TemplateConfig{
		StateCommit: DefaultStateCommitConfig(),
		StateStore:  DefaultStateStoreConfig(),
	}

	// Parse and execute the StateCommit template
	tmpl, err := template.New("sc").Parse(StateCommitConfigTemplate)
	require.NoError(t, err, "Failed to parse StateCommitConfigTemplate")

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, cfg)
	require.NoError(t, err, "Failed to execute StateCommitConfigTemplate - field path mismatch detected")

	output := buf.String()

	// Verify key config values are present in output
	require.Contains(t, output, "[state-commit]", "Missing state-commit section")
	require.Contains(t, output, "sc-enable = true", "Missing or incorrect sc-enable")
	require.Contains(t, output, `sc-write-mode = "cosmos_only"`, "Missing or incorrect sc-write-mode")
	require.Contains(t, output, `sc-read-mode = "cosmos_only"`, "Missing or incorrect sc-read-mode")

	// Verify MemIAVLConfig fields are accessible
	require.Contains(t, output, "sc-async-commit-buffer =", "Missing sc-async-commit-buffer")
	require.Contains(t, output, "sc-keep-recent =", "Missing sc-keep-recent")
	require.Contains(t, output, "sc-snapshot-interval =", "Missing sc-snapshot-interval")
	require.Contains(t, output, "sc-snapshot-min-time-interval =", "Missing sc-snapshot-min-time-interval")
	require.Contains(t, output, "sc-snapshot-prefetch-threshold =", "Missing sc-snapshot-prefetch-threshold")
	require.Contains(t, output, "sc-snapshot-write-rate-mbps =", "Missing sc-snapshot-write-rate-mbps")
	require.Contains(t, output, "sc-historical-proof-max-inflight = 1", "Missing or incorrect sc-historical-proof-max-inflight")
	require.Contains(t, output, "sc-historical-proof-rate-limit = 1", "Missing or incorrect sc-historical-proof-rate-limit")
	require.Contains(t, output, "sc-historical-proof-burst = 1", "Missing or incorrect sc-historical-proof-burst")

	// sc-snapshot-writer-limit is intentionally removed from template (hardcoded to 4)
	// but old configs with this field still parse fine via mapstructure
	require.NotContains(t, output, "sc-snapshot-writer-limit", "sc-snapshot-writer-limit should not be in template")
}

// TestStateStoreConfigTemplate verifies that all field paths in the StateStore TOML template
// are valid and can be resolved against the actual Config struct.
func TestStateStoreConfigTemplate(t *testing.T) {
	type TemplateConfig struct {
		StateCommit StateCommitConfig
		StateStore  StateStoreConfig
	}

	cfg := TemplateConfig{
		StateCommit: DefaultStateCommitConfig(),
		StateStore:  DefaultStateStoreConfig(),
	}

	// Parse and execute the StateStore template
	tmpl, err := template.New("ss").Parse(StateStoreConfigTemplate)
	require.NoError(t, err, "Failed to parse StateStoreConfigTemplate")

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, cfg)
	require.NoError(t, err, "Failed to execute StateStoreConfigTemplate - field path mismatch detected")

	output := buf.String()

	// Verify key config values are present
	require.Contains(t, output, "[state-store]", "Missing state-store section")
	require.Contains(t, output, "ss-enable = true", "Missing or incorrect ss-enable")
	require.Contains(t, output, `ss-backend = "pebbledb"`, "Missing or incorrect ss-backend")
	require.Contains(t, output, "ss-async-write-buffer =", "Missing ss-async-write-buffer")
	require.Contains(t, output, "ss-keep-recent =", "Missing ss-keep-recent")
	require.Contains(t, output, "ss-prune-interval =", "Missing ss-prune-interval")
	require.Contains(t, output, "ss-import-num-workers =", "Missing ss-import-num-workers")
}

// TestDefaultConfigTemplate verifies the combined template works correctly
func TestDefaultConfigTemplate(t *testing.T) {
	type TemplateConfig struct {
		StateCommit  StateCommitConfig
		StateStore   StateStoreConfig
		ReceiptStore ReceiptStoreConfig
	}

	cfg := TemplateConfig{
		StateCommit:  DefaultStateCommitConfig(),
		StateStore:   DefaultStateStoreConfig(),
		ReceiptStore: DefaultReceiptStoreConfig(),
	}

	tmpl, err := template.New("default").Parse(DefaultConfigTemplate)
	require.NoError(t, err, "Failed to parse DefaultConfigTemplate")

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, cfg)
	require.NoError(t, err, "Failed to execute DefaultConfigTemplate")

	output := buf.String()

	// All sections should be present
	require.Contains(t, output, "[state-commit]")
	require.Contains(t, output, "[state-store]")
	require.Contains(t, output, "[receipt-store]")
}

// TestWriteModeValues verifies WriteMode enum values match template output
func TestWriteModeValues(t *testing.T) {
	tests := []struct {
		mode     WriteMode
		expected string
	}{
		{CosmosOnlyWrite, "cosmos_only"},
		{DualWrite, "dual_write"},
		{SplitWrite, "split_write"},
	}

	for _, tc := range tests {
		t.Run(string(tc.mode), func(t *testing.T) {
			require.Equal(t, tc.expected, string(tc.mode))
			require.True(t, tc.mode.IsValid())
		})
	}

	// Test invalid mode
	require.False(t, WriteMode("invalid").IsValid())
}

// TestReadModeValues verifies ReadMode enum values match template output
func TestReadModeValues(t *testing.T) {
	tests := []struct {
		mode     ReadMode
		expected string
	}{
		{CosmosOnlyRead, "cosmos_only"},
		{EVMFirstRead, "evm_first"},
		{SplitRead, "split_read"},
	}

	for _, tc := range tests {
		t.Run(string(tc.mode), func(t *testing.T) {
			require.Equal(t, tc.expected, string(tc.mode))
			require.True(t, tc.mode.IsValid())
		})
	}

	// Test invalid mode
	require.False(t, ReadMode("invalid").IsValid())
}

// TestParseWriteMode verifies string to WriteMode conversion
func TestParseWriteMode(t *testing.T) {
	tests := []struct {
		input    string
		expected WriteMode
		hasError bool
	}{
		{"cosmos_only", CosmosOnlyWrite, false},
		{"dual_write", DualWrite, false},
		{"split_write", SplitWrite, false},
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			mode, err := ParseWriteMode(tc.input)
			if tc.hasError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected, mode)
			}
		})
	}
}

// TestParseReadMode verifies string to ReadMode conversion
func TestParseReadMode(t *testing.T) {
	tests := []struct {
		input    string
		expected ReadMode
		hasError bool
	}{
		{"cosmos_only", CosmosOnlyRead, false},
		{"evm_first", EVMFirstRead, false},
		{"split_read", SplitRead, false},
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			mode, err := ParseReadMode(tc.input)
			if tc.hasError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected, mode)
			}
		})
	}
}

// TestStateCommitConfigValidate verifies config validation works
func TestStateCommitConfigValidate(t *testing.T) {
	tests := []struct {
		name      string
		writeMode WriteMode
		readMode  ReadMode
		hasError  bool
	}{
		{"valid cosmos_only", CosmosOnlyWrite, CosmosOnlyRead, false},
		{"valid dual_write", DualWrite, EVMFirstRead, false},
		{"invalid write mode", WriteMode("invalid"), CosmosOnlyRead, true},
		{"invalid read mode", CosmosOnlyWrite, ReadMode("invalid"), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultStateCommitConfig()
			cfg.WriteMode = tc.writeMode
			cfg.ReadMode = tc.readMode

			err := cfg.Validate()
			if tc.hasError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestTemplateFieldPathsExist is a comprehensive test that extracts all template
// field paths and verifies they exist in the config struct. This catches typos
// and renamed fields.
func TestTemplateFieldPathsExist(t *testing.T) {
	type TemplateConfig struct {
		StateCommit  StateCommitConfig
		StateStore   StateStoreConfig
		ReceiptStore ReceiptStoreConfig
	}

	cfg := TemplateConfig{
		StateCommit:  DefaultStateCommitConfig(),
		StateStore:   DefaultStateStoreConfig(),
		ReceiptStore: DefaultReceiptStoreConfig(),
	}

	templates := []struct {
		name     string
		template string
	}{
		{"StateCommitConfigTemplate", StateCommitConfigTemplate},
		{"StateStoreConfigTemplate", StateStoreConfigTemplate},
		{"ReceiptStoreConfigTemplate", ReceiptStoreConfigTemplate},
	}

	for _, tt := range templates {
		t.Run(tt.name, func(t *testing.T) {
			tmpl, err := template.New(tt.name).Parse(tt.template)
			require.NoError(t, err, "Template parse error indicates syntax issue")

			var buf bytes.Buffer
			err = tmpl.Execute(&buf, cfg)
			require.NoError(t, err, "Template execution failed - likely a field path doesn't exist in Config struct")

			// Ensure output doesn't contain "<no value>" which indicates missing fields
			output := buf.String()
			require.False(t, strings.Contains(output, "<no value>"),
				"Template contains fields that resolved to <no value>, indicating missing struct fields")
		})
	}
}
