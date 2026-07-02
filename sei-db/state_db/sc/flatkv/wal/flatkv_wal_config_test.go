package wal

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigValidate(t *testing.T) {
	t.Run("default config is valid", func(t *testing.T) {
		require.NoError(t, DefaultFlatKVWALConfig("/tmp/wal").Validate())
	})

	t.Run("empty path is rejected", func(t *testing.T) {
		cfg := DefaultFlatKVWALConfig("")
		require.Error(t, cfg.Validate())
	})

	t.Run("zero target file size is rejected", func(t *testing.T) {
		cfg := DefaultFlatKVWALConfig("/tmp/wal")
		cfg.TargetFileSize = 0
		require.Error(t, cfg.Validate())
	})
}
