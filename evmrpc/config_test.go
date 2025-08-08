package evmrpc

import (
	"testing"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestReadConfig(t *testing.T) {
	viper.Set("evm_rpc.rpc_address", "127.0.0.1:9545")
	cfg := ReadConfig()
	require.Equal(t, "127.0.0.1:9545", cfg.RPCAddress)
}
