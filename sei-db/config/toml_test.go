package config

import (
	"bytes"
	"strings"
	"testing"
	"text/template"

	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/types"
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

	// The FlatKV section header is kept, but no FlatKV configs are exposed yet.
	require.Contains(t, output, "[state-commit.flatkv]", "Missing FlatKV section")
	flatKVSection := output[strings.Index(output, "[state-commit.flatkv]"):]
	require.NotContains(t, flatKVSection, "fsync", "FlatKV fsync should not be exposed in app.toml")
	require.NotContains(t, flatKVSection, "async-write-buffer", "FlatKV async-write-buffer should not be exposed in app.toml")
	require.NotContains(t, flatKVSection, "snapshot-interval", "FlatKV snapshot-interval should not be exposed in app.toml")
	require.NotContains(t, flatKVSection, "snapshot-keep-recent", "FlatKV snapshot-keep-recent should not be exposed in app.toml")
	require.NotContains(t, flatKVSection, "enable-read-write-metrics", "FlatKV read/write metrics flag should not be exposed in app.toml")

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
	require.Contains(t, output, "ss-enable-read-write-metrics = false", "Missing state-store read/write metrics flag")
	require.Contains(t, output, `evm-ss-db-directory = ""`, "Missing evm-ss-db-directory")
	require.Contains(t, output, `evm-ss-split = false`, "Missing or incorrect evm-ss-split")
	require.Contains(t, output, "evm-ss-separate-dbs = false", "Missing or incorrect evm-ss-separate-dbs")
}

// TestReceiptStoreConfigTemplate verifies that all field paths in the receipt-store TOML template
// are valid and can be resolved against the actual config struct.
func TestReceiptStoreConfigTemplate(t *testing.T) {
	type TemplateConfig struct {
		ReceiptStore ReceiptStoreConfig
	}

	cfg := TemplateConfig{
		ReceiptStore: DefaultReceiptStoreConfig(),
	}

	tmpl, err := template.New("receipt").Parse(ReceiptStoreConfigTemplate)
	require.NoError(t, err, "Failed to parse ReceiptStoreConfigTemplate")

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, cfg)
	require.NoError(t, err, "Failed to execute ReceiptStoreConfigTemplate - field path mismatch detected")

	output := buf.String()

	require.Contains(t, output, "[receipt-store]", "Missing receipt-store section")
	require.Contains(t, output, `rs-backend = "pebbledb"`, "Missing or incorrect rs-backend")
	require.Contains(t, output, `db-directory = ""`, "Missing or incorrect db-directory")
	require.Contains(t, output, "async-write-buffer =", "Missing async-write-buffer")
	require.Contains(t, output, "prune-interval-seconds =", "Missing prune-interval-seconds")
	require.Contains(t, output, "enable-read-write-metrics = false", "Missing receipt read/write metrics flag")
	require.NotContains(t, output, "keep-recent", "keep-recent should not be in receipt-store template (controlled by min-retain-blocks)")
	require.Contains(t, output, `Applies only when rs-backend = "pebbledb"`, "Missing pebble-only async-write-buffer note")
	require.NotContains(t, output, "use-default-comparer", "use-default-comparer should not be in receipt-store template")
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
	require.Contains(t, output, "async-write-buffer =")
	require.Contains(t, output, "prune-interval-seconds =")
}

// TestWriteModeValues verifies WriteMode enum values match template output
func TestWriteModeValues(t *testing.T) {
	tests := []struct {
		mode     types.WriteMode
		expected string
	}{
		{types.MemiavlOnly, "memiavl_only"},
		{types.MigrateEVM, "migrate_evm"},
		{types.EVMMigrated, "evm_migrated"},
		{types.MigrateAllButBank, "migrate_all_but_bank"},
		{types.AllMigratedButBank, "all_migrated_but_bank"},
		{types.MigrateBank, "migrate_bank"},
		{types.FlatKVOnly, "flatkv_only"},
		{types.TestOnlyDualWrite, "test_only_dual_write"},
		{types.Auto, "auto"},
	}

	for _, tc := range tests {
		t.Run(string(tc.mode), func(t *testing.T) {
			require.Equal(t, tc.expected, string(tc.mode))
			require.True(t, tc.mode.IsValid())
		})
	}

	require.False(t, types.WriteMode("invalid").IsValid())
}

// TestParseWriteMode verifies string to WriteMode conversion
func TestParseWriteMode(t *testing.T) {
	tests := []struct {
		input    string
		expected types.WriteMode
		hasError bool
	}{
		{"memiavl_only", types.MemiavlOnly, false},
		{"migrate_evm", types.MigrateEVM, false},
		{"evm_migrated", types.EVMMigrated, false},
		{"migrate_all_but_bank", types.MigrateAllButBank, false},
		{"all_migrated_but_bank", types.AllMigratedButBank, false},
		{"migrate_bank", types.MigrateBank, false},
		{"flatkv_only", types.FlatKVOnly, false},
		{"test_only_dual_write", types.TestOnlyDualWrite, false},
		{"auto", types.Auto, false},
		{"invalid", "", true},
		{"", "", true},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			mode, err := types.ParseWriteMode(tc.input)
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
		writeMode types.WriteMode
		hasError  bool
	}{
		{"valid memiavl_only", types.MemiavlOnly, false},
		{"valid test_only_dual_write", types.TestOnlyDualWrite, false},
		{"valid evm_migrated", types.EVMMigrated, false},
		{"valid flatkv_only", types.FlatKVOnly, false},
		{"valid auto", types.Auto, false},
		{"invalid write mode", types.WriteMode("invalid"), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultStateCommitConfig()
			cfg.WriteMode = tc.writeMode

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
