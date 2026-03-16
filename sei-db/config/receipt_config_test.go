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
