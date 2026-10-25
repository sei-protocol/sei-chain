package pebbledb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Every field the pebble options are built from is rejected when it holds a value pebble cannot be
// opened with, rather than being passed through to produce a database with a silently broken shape.
func TestValidateRejectsUnusableTuning(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(cfg *PebbleDBConfig)
		message string
	}{
		{
			name:    "zero block cache",
			mutate:  func(cfg *PebbleDBConfig) { cfg.BlockCacheSize = 0 },
			message: "block cache size must be positive",
		},
		{
			name:    "negative block cache",
			mutate:  func(cfg *PebbleDBConfig) { cfg.BlockCacheSize = -1 },
			message: "block cache size must be positive",
		},
		{
			name:    "no compaction concurrency",
			mutate:  func(cfg *PebbleDBConfig) { cfg.MaxConcurrentCompactions = 0 },
			message: "max concurrent compactions must be at least 1",
		},
		{
			name:    "zero memtable size",
			mutate:  func(cfg *PebbleDBConfig) { cfg.MemTableSize = 0 },
			message: "mem table size must be positive",
		},
		{
			name:    "single memtable",
			mutate:  func(cfg *PebbleDBConfig) { cfg.MemTableStopWritesThreshold = 1 },
			message: "mem table stop writes threshold must be at least 2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.DataDir = t.TempDir()
			require.NoError(t, cfg.Validate(), "the default config must be valid to start from")

			test.mutate(&cfg)
			err := cfg.Validate()
			require.Error(t, err)
			require.Contains(t, err.Error(), test.message)
		})
	}
}
