package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type mapAppOpts map[string]interface{}

func (m mapAppOpts) Get(key string) interface{} {
	return m[key]
}

func TestReadReceiptConfigRejectsMisnamedBackendKey(t *testing.T) {
	_, err := ReadReceiptConfig(mapAppOpts{
		"receipt-store.backend": "pebbledb",
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "receipt-store.backend")
	require.ErrorContains(t, err, "receipt-store.rs-backend")
}

func TestReadReceiptConfigReadWriteMetrics(t *testing.T) {
	cfg, err := ReadReceiptConfig(mapAppOpts{
		"receipt-store.enable-read-write-metrics": true,
	})

	require.NoError(t, err)
	require.True(t, cfg.EnableReadWriteMetrics)
}

func TestReadReceiptConfigLogFilterParallelism(t *testing.T) {
	// Defaults when unset.
	cfg, err := ReadReceiptConfig(mapAppOpts{})
	require.NoError(t, err)
	require.Equal(t, DefaultReceiptLogFilterParallelism, cfg.LogFilterParallelism)

	// Override is read through.
	cfg, err = ReadReceiptConfig(mapAppOpts{
		"receipt-store.log-filter-parallelism": 32,
	})
	require.NoError(t, err)
	require.Equal(t, 32, cfg.LogFilterParallelism)
}
