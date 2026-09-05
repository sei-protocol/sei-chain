package walrus

import (
	"testing"

	"github.com/sei-protocol/sei-chain/sei-db/common/unit"
	"github.com/stretchr/testify/require"
)

func TestConfigValidation(t *testing.T) {
	require.NoError(t, DefaultConfig("/tmp/walrus", "instance", "evm").Validate())

	cases := map[string]func(config *Config){
		"path is required":       func(c *Config) { c.Path = "" },
		"must match":             func(c *Config) { c.Name = "not a valid name" },
		"store name is required": func(c *Config) { c.StoreName = "" },
		"greater than 0":         func(c *Config) { c.TargetPodSize = 0 },
		"addressable maximum":    func(c *Config) { c.TargetPodSize = 8 * unit.GB },
		"false positive rate":    func(c *Config) { c.BloomFalsePositiveRate = 1 },
		"retention blocks":       func(c *Config) { c.RetentionBlocks = 0 },
		"pod build concurrency":  func(c *Config) { c.PodBuildConcurrency = 0 },
	}
	for expected, breakIt := range cases {
		config := DefaultConfig("/tmp/walrus", "instance", "evm")
		breakIt(config)
		require.ErrorContains(t, config.Validate(), expected)
	}
}

func TestReadStatusNames(t *testing.T) {
	// The status names are metric label values, so a change here changes a dashboard.
	require.Equal(t, "found", ReadFound.String())
	require.Equal(t, "absent", ReadAbsent.String())
	require.Equal(t, "too_new", ReadTooNew.String())
	require.Equal(t, "too_old", ReadTooOld.String())
	require.Equal(t, "unknown", ReadStatus(200).String())
}

func TestNewRejectsAnInvalidConfig(t *testing.T) {
	config := DefaultConfig(t.TempDir(), "test", "evm")
	config.RetentionBlocks = 0

	_, err := New(config)
	require.ErrorContains(t, err, "invalid config")
}
