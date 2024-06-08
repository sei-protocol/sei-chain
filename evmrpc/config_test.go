package evmrpc_test

import (
	"testing"
	"time"

	"github.com/sei-protocol/sei-chain/evmrpc"
	"github.com/stretchr/testify/require"
)

type opts struct {
	httpEnabled             interface{}
	httpPort                interface{}
	wsEnabled               interface{}
	wsPort                  interface{}
	readTimeout             interface{}
	readHeaderTimeout       interface{}
	writeTimeout            interface{}
	idleTimeout             interface{}
	simulationGasLimit      interface{}
	simulationEVMTimeout    interface{}
	corsOrigins             interface{}
	wsOrigins               interface{}
	filterTimeout           interface{}
	checkTxTimeout          interface{}
	maxTxPoolTxs            interface{}
	slow                    interface{}
	denyList                interface{}
	maxLogNoBlock           interface{}
	maxBlocksForLog         interface{}
	maxSubscriptionsNewHead interface{}
	enableTestAPI           interface{}
}

func (o *opts) Get(k string) interface{} {
	if k == "evm.http_enabled" {
		return o.httpEnabled
	}
	if k == "evm.http_port" {
		return o.httpPort
	}
	if k == "evm.ws_enabled" {
		return o.wsEnabled
	}
	if k == "evm.ws_port" {
		return o.wsPort
	}
	if k == "evm.read_timeout" {
		return o.readTimeout
	}
	if k == "evm.read_header_timeout" {
		return o.readHeaderTimeout
	}
	if k == "evm.write_timeout" {
		return o.writeTimeout
	}
	if k == "evm.idle_timeout" {
		return o.idleTimeout
	}
	if k == "evm.simulation_gas_limit" {
		return o.simulationGasLimit
	}
	if k == "evm.simulation_evm_timeout" {
		return o.simulationEVMTimeout
	}
	if k == "evm.cors_origins" {
		return o.corsOrigins
	}
	if k == "evm.ws_origins" {
		return o.wsOrigins
	}
	if k == "evm.filter_timeout" {
		return o.filterTimeout
	}
	if k == "evm.checktx_timeout" {
		return o.checkTxTimeout
	}
	if k == "evm.max_tx_pool_txs" {
		return o.maxTxPoolTxs
	}
	if k == "evm.slow" {
		return o.slow
	}
	if k == "evm.deny_list" {
		return o.denyList
	}
	if k == "evm.max_log_no_block" {
		return o.maxLogNoBlock
	}
	if k == "evm.max_blocks_for_log" {
		return o.maxBlocksForLog
	}
	if k == "evm.max_subscriptions_new_head" {
		return o.maxSubscriptionsNewHead
	}
	if k == "evm.enable_test_api" {
		return o.enableTestAPI
	}
	panic("unknown key")
}

func TestReadConfig(t *testing.T) {
	goodOpts := opts{
		true,
		1,
		true,
		2,
		time.Duration(5),
		time.Duration(5),
		time.Duration(5),
		time.Duration(5),
		uint64(10),
		time.Duration(60),
		"",
		"",
		time.Duration(5),
		time.Duration(5),
		1000,
		false,
		make([]string, 0),
		20000,
		1000,
		10000,
		false,
	}
	_, err := evmrpc.ReadConfig(&goodOpts)
	require.Nil(t, err)
	badOpts := goodOpts
	badOpts.httpEnabled = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.httpPort = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.wsEnabled = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.wsPort = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.readTimeout = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.readHeaderTimeout = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.writeTimeout = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.idleTimeout = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.filterTimeout = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.simulationGasLimit = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.simulationEVMTimeout = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.corsOrigins = map[string]interface{}{}
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.wsOrigins = map[string]interface{}{}
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.checkTxTimeout = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.maxTxPoolTxs = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.slow = "bad"
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
	badOpts = goodOpts
	badOpts.denyList = map[string]interface{}{}
	_, err = evmrpc.ReadConfig(&badOpts)
	require.NotNil(t, err)
}
