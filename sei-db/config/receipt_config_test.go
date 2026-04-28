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
		"receipt-store.backend": "parquet",
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "receipt-store.backend")
	require.ErrorContains(t, err, "receipt-store.rs-backend")
}

func TestReadReceiptConfigAcceptsParquetV2Backend(t *testing.T) {
	cfg, err := ReadReceiptConfig(mapAppOpts{
		"receipt-store.rs-backend": " parquet_v2 ",
	})

	require.NoError(t, err)
	require.Equal(t, "parquet_v2", cfg.Backend)
}

func TestReadReceiptConfigBackendErrorListsParquetV2(t *testing.T) {
	_, err := ReadReceiptConfig(mapAppOpts{
		"receipt-store.rs-backend": "rocksdb",
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "parquet_v2")
}

func TestReadReceiptConfigTxIndexBackendOverride(t *testing.T) {
	cfg, err := ReadReceiptConfig(mapAppOpts{
		"receipt-store.tx-index-backend": "",
	})

	require.NoError(t, err)
	require.Equal(t, "", cfg.TxIndexBackend)
}

func TestReadReceiptConfigAcceptsPebbleDBTxIndexBackend(t *testing.T) {
	cfg, err := ReadReceiptConfig(mapAppOpts{
		"receipt-store.tx-index-backend": "pebbledb",
	})

	require.NoError(t, err)
	require.Equal(t, ReceiptTxIndexBackendPebble, cfg.TxIndexBackend)
}

func TestReadReceiptConfigUnknownTxIndexBackendDefaultsToNone(t *testing.T) {
	cfg, err := ReadReceiptConfig(mapAppOpts{
		"receipt-store.tx-index-backend": "rocksdb",
	})

	require.NoError(t, err)
	require.Equal(t, ReceiptTxIndexBackendNone, cfg.TxIndexBackend)
}
