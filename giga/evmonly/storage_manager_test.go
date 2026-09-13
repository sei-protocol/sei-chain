package evmonly

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewValidatorStorageConfig(t *testing.T) {
	storageConfig, err := NewValidatorStorageConfig(t.TempDir())
	require.NoError(t, err)
	require.NotNil(t, storageConfig.FlatKVConfig)
	require.False(t, storageConfig.SSConfig.Enable)
	require.True(t, storageConfig.ReceiptDBConfig.Enable)
	require.NotNil(t, storageConfig.BlockDBConfig)
}
