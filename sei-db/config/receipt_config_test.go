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

func TestReadReceiptConfigEnable(t *testing.T) {
	// An app.toml written before this key existed carries no rs-enable, and such a node must keep
	// the store it already has rather than losing its receipt history to an absent key.
	cfg, err := ReadReceiptConfig(mapAppOpts{})
	require.NoError(t, err)
	require.True(t, cfg.Enable)

	// Override is read through, which is the only way to turn the store off.
	cfg, err = ReadReceiptConfig(mapAppOpts{
		"receipt-store.rs-enable": false,
	})
	require.NoError(t, err)
	require.False(t, cfg.Enable)
}

func TestReadReceiptConfigRewardPercentiles(t *testing.T) {
	// Unset leaves it empty; the littidx backend applies receipt.DefaultRewardPercentiles itself.
	cfg, err := ReadReceiptConfig(mapAppOpts{})
	require.NoError(t, err)
	require.Empty(t, cfg.RewardPercentiles)

	// A TOML float array decodes (via Viper) as []interface{} of float64.
	cfg, err = ReadReceiptConfig(mapAppOpts{
		"receipt-store.rs-reward-percentiles": []interface{}{0.0, 50.0, 100.0},
	})
	require.NoError(t, err)
	require.Equal(t, []float64{0, 50, 100}, cfg.RewardPercentiles)
}

func TestReadReceiptConfigRewardPercentilesRejectsOutOfRange(t *testing.T) {
	_, err := ReadReceiptConfig(mapAppOpts{
		"receipt-store.rs-reward-percentiles": []interface{}{-1.0, 50.0},
	})
	require.ErrorContains(t, err, "between 0 and 100")
}

func TestReadReceiptConfigRewardPercentilesRejectsDuplicates(t *testing.T) {
	_, err := ReadReceiptConfig(mapAppOpts{
		"receipt-store.rs-reward-percentiles": []interface{}{50.0, 50.0},
	})
	require.ErrorContains(t, err, "duplicate")
}

func TestReceiptStoreConfigRewardPercentilesTOML(t *testing.T) {
	require.Equal(t, "[]", ReceiptStoreConfig{}.RewardPercentilesTOML())
	require.Equal(t, "[0, 50, 100]",
		ReceiptStoreConfig{RewardPercentiles: []float64{0, 50, 100}}.RewardPercentilesTOML())
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
