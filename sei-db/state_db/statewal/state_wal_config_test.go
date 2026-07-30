package statewal

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConfigValidate(t *testing.T) {
	t.Run("default config is valid", func(t *testing.T) {
		require.NoError(t, DefaultConfig("/tmp/wal", "test").Validate())
	})

	t.Run("empty path is rejected", func(t *testing.T) {
		cfg := DefaultConfig("", "test")
		require.Error(t, cfg.Validate())
	})

	t.Run("empty name is rejected", func(t *testing.T) {
		cfg := DefaultConfig("/tmp/wal", "")
		require.Error(t, cfg.Validate())
	})

	t.Run("malformed name is rejected", func(t *testing.T) {
		cfg := DefaultConfig("/tmp/wal", "bad name!")
		require.Error(t, cfg.Validate())
	})

	t.Run("zero target file size is rejected", func(t *testing.T) {
		cfg := DefaultConfig("/tmp/wal", "test")
		cfg.TargetFileSize = 0
		require.Error(t, cfg.Validate())
	})

	t.Run("zero iterator prefetch size is rejected", func(t *testing.T) {
		cfg := DefaultConfig("/tmp/wal", "test")
		cfg.IteratorPrefetchSize = 0
		require.Error(t, cfg.Validate())
	})
}
