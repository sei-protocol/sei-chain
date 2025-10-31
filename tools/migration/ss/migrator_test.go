package ss

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewMigratorCacheSize(t *testing.T) {
	m := NewMigrator(nil, nil, 12345)
	require.Equal(t, 12345, m.cacheSize)
}
