package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultDataLakeConfigIsValid(t *testing.T) {
	cfg := DefaultDataLakeConfig("r2://sei-lake?account_id=abc123")
	require.NoError(t, cfg.Validate())
	require.Equal(t, "r2://sei-lake?account_id=abc123", cfg.BucketURL)
}

func TestGigaStorageConfigValidatesTheDataLakeConfig(t *testing.T) {
	cfg := defaultGigaConfig(t)
	lakeConfig := DefaultDataLakeConfig("")
	cfg.DataLakeConfig = &lakeConfig
	require.ErrorContains(t, cfg.Validate(), "data lake config is invalid")
}
