package evmonly

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewValidatorStorageConfig(t *testing.T) {
	for _, receipts := range []bool{true, false} {
		storageConfig, err := NewValidatorStorageConfig(t.TempDir(), receipts)
		require.NoError(t, err)
		require.NotNil(t, storageConfig.FlatKVConfig)
		require.False(t, storageConfig.SSConfig.Enable)
		require.Equal(t, receipts, storageConfig.ReceiptDBConfig.Enable)
		require.NotNil(t, storageConfig.BlockDBConfig)
	}
}
