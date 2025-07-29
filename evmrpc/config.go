package evmrpc

import (
	"runtime"
	"time"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/spf13/cast"
)

// EVMRPC Config defines configurations for EVM RPC server on this node
type Config struct {
	// controls whether an HTTP EVM server is enabled
	HTTPEnabled bool `mapstructure:"http_enabled"`
	HTTPPort    int  `mapstructure:"http_port"`

	// controls whether a websocket server is enabled
	WSEnabled bool `mapstructure:"ws_enabled"`
	WSPort    int  `mapstructure:"ws_port"`

	// ReadTimeout is the maximum duration for reading the entire
	// request, including the body.
	//
	// Because ReadTimeout does not let Handlers make per-request
	// decisions on each request body's acceptable deadline or
	// upload rate, most users will prefer to use
	// ReadHeaderTimeout. It is valid to use them both.
	ReadTimeout time.Duration `mapstructure:"read_timeout"`

	// ReadHeaderTimeout is the amount of time allowed to read
	// request headers. The connection's read deadline is reset
	// after reading the headers and the Handler can decide what
	// is considered too slow for the body. If ReadHeaderTimeout
	// is zero, the value of ReadTimeout is used. If both are
	// zero, there is no timeout.
	ReadHeaderTimeout time.Duration `mapstructure:"read_header_timeout"`

	// WriteTimeout is the maximum duration before timing out
	// writes of the response. It is reset whenever a new
	// request's header is read. Like ReadTimeout, it does not
	// let Handlers make decisions on a per-request basis.
	WriteTimeout time.Duration `mapstructure:"write_timeout"`

	// IdleTimeout is the maximum amount of time to wait for the
	// next request when keep-alives are enabled. If IdleTimeout
	// is zero, the value of ReadTimeout is used. If both are
	// zero, ReadHeaderTimeout is used.
	IdleTimeout time.Duration `mapstructure:"idle_timeout"`

	// Maximum gas limit for simulation
	SimulationGasLimit uint64 `mapstructure:"simulation_gas_limit"`

	// Timeout for EVM call in simulation
	SimulationEVMTimeout time.Duration `mapstructure:"simulation_evm_timeout"`

	// list of CORS allowed origins, separated by comma
	CORSOrigins string `mapstructure:"cors_origins"`

	// list of WS origins, separated by comma
	WSOrigins string `mapstructure:"ws_origins"`

	// timeout for filters
	FilterTimeout time.Duration `mapstructure:"filter_timeout"`

	// checkTx timeout for sig verify
	CheckTxTimeout time.Duration `mapstructure:"checktx_timeout"`

	// max number of txs to pull from mempool
	MaxTxPoolTxs uint64 `mapstructure:"max_tx_pool_txs"`

	// controls whether to have txns go through one by one
	Slow bool `mapstructure:"slow"`

	// Deny list defines list of methods that EVM RPC should fail fast
	DenyList []string `mapstructure:"deny_list"`

	// max number of logs returned if block range is open-ended
	MaxLogNoBlock int64 `mapstructure:"max_log_no_block"`

	// max number of blocks to query logs for
	MaxBlocksForLog int64 `mapstructure:"max_blocks_for_log"`

	// max number of concurrent NewHead subscriptions
	MaxSubscriptionsNewHead uint64 `mapstructure:"max_subscriptions_new_head"`

	// test api enables certain override apis for integration test situations
	EnableTestAPI bool `mapstructure:"enable_test_api"`

	// MaxConcurrentTraceCalls defines the maximum number of concurrent debug_trace calls.
	// Set to 0 for unlimited.
	MaxConcurrentTraceCalls uint64 `mapstructure:"max_concurrent_trace_calls"`

	// MaxConcurrentSimulationCalls defines the maximum number of concurrent eth_call calls.
	// Set to 0 for unlimited.
	MaxConcurrentSimulationCalls int `mapstructure:"max_concurrent_simulation_calls"`

	// Max number of blocks allowed to look back for tracing
	MaxTraceLookbackBlocks int64 `mapstructure:"max_trace_lookback_blocks"`

	// Timeout for each trace call
	TraceTimeout time.Duration `mapstructure:"trace_timeout"`
}

var DefaultConfig = Config{
	HTTPEnabled:                  true,
	HTTPPort:                     8545,
	WSEnabled:                    true,
	WSPort:                       8546,
	ReadTimeout:                  rpc.DefaultHTTPTimeouts.ReadTimeout,
	ReadHeaderTimeout:            rpc.DefaultHTTPTimeouts.ReadHeaderTimeout,
	WriteTimeout:                 rpc.DefaultHTTPTimeouts.WriteTimeout,
	IdleTimeout:                  rpc.DefaultHTTPTimeouts.IdleTimeout,
	SimulationGasLimit:           10_000_000, // 10M
	SimulationEVMTimeout:         60 * time.Second,
	CORSOrigins:                  "*",
	WSOrigins:                    "*",
	FilterTimeout:                120 * time.Second,
	CheckTxTimeout:               5 * time.Second,
	MaxTxPoolTxs:                 1000,
	Slow:                         false,
	DenyList:                     make([]string, 0),
	MaxLogNoBlock:                10000,
	MaxBlocksForLog:              2000,
	MaxSubscriptionsNewHead:      10000,
	EnableTestAPI:                false,
	MaxConcurrentTraceCalls:      10,
	MaxConcurrentSimulationCalls: runtime.NumCPU(),
	MaxTraceLookbackBlocks:       10000,
	TraceTimeout:                 30 * time.Second,
}

const (
	flagHTTPEnabled                  = "evm.http_enabled"
	flagHTTPPort                     = "evm.http_port"
	flagWSEnabled                    = "evm.ws_enabled"
	flagWSPort                       = "evm.ws_port"
	flagReadTimeout                  = "evm.read_timeout"
	flagReadHeaderTimeout            = "evm.read_header_timeout"
	flagWriteTimeout                 = "evm.write_timeout"
	flagIdleTimeout                  = "evm.idle_timeout"
	flagSimulationGasLimit           = "evm.simulation_gas_limit"
	flagSimulationEVMTimeout         = "evm.simulation_evm_timeout"
	flagCORSOrigins                  = "evm.cors_origins"
	flagWSOrigins                    = "evm.ws_origins"
	flagFilterTimeout                = "evm.filter_timeout"
	flagMaxTxPoolTxs                 = "evm.max_tx_pool_txs"
	flagCheckTxTimeout               = "evm.checktx_timeout"
	flagSlow                         = "evm.slow"
	flagDenyList                     = "evm.deny_list"
	flagMaxLogNoBlock                = "evm.max_log_no_block"
	flagMaxBlocksForLog              = "evm.max_blocks_for_log"
	flagMaxSubscriptionsNewHead      = "evm.max_subscriptions_new_head"
	flagEnableTestAPI                = "evm.enable_test_api"
	flagMaxConcurrentTraceCalls      = "evm.max_concurrent_trace_calls"
	flagMaxConcurrentSimulationCalls = "evm.max_concurrent_simulation_calls"
	flagMaxTraceLookbackBlocks       = "evm.max_trace_lookback_blocks"
	flagTraceTimeout                 = "evm.trace_timeout"
)

func ReadConfig(opts servertypes.AppOptions) (Config, error) {
	cfg := DefaultConfig // copy
	var err error
	if v := opts.Get(flagHTTPEnabled); v != nil {
		if cfg.HTTPEnabled, err = cast.ToBoolE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagHTTPPort); v != nil {
		if cfg.HTTPPort, err = cast.ToIntE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagWSEnabled); v != nil {
		if cfg.WSEnabled, err = cast.ToBoolE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagWSPort); v != nil {
		if cfg.WSPort, err = cast.ToIntE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagReadTimeout); v != nil {
		if cfg.ReadTimeout, err = cast.ToDurationE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagReadHeaderTimeout); v != nil {
		if cfg.ReadHeaderTimeout, err = cast.ToDurationE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagWriteTimeout); v != nil {
		if cfg.WriteTimeout, err = cast.ToDurationE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagIdleTimeout); v != nil {
		if cfg.IdleTimeout, err = cast.ToDurationE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagSimulationGasLimit); v != nil {
		if cfg.SimulationGasLimit, err = cast.ToUint64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagSimulationEVMTimeout); v != nil {
		if cfg.SimulationEVMTimeout, err = cast.ToDurationE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagCORSOrigins); v != nil {
		if cfg.CORSOrigins, err = cast.ToStringE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagWSOrigins); v != nil {
		if cfg.WSOrigins, err = cast.ToStringE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagFilterTimeout); v != nil {
		if cfg.FilterTimeout, err = cast.ToDurationE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagCheckTxTimeout); v != nil {
		if cfg.CheckTxTimeout, err = cast.ToDurationE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagMaxTxPoolTxs); v != nil {
		if cfg.MaxTxPoolTxs, err = cast.ToUint64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagSlow); v != nil {
		if cfg.Slow, err = cast.ToBoolE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagDenyList); v != nil {
		if cfg.DenyList, err = cast.ToStringSliceE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagMaxLogNoBlock); v != nil {
		if cfg.MaxLogNoBlock, err = cast.ToInt64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagMaxBlocksForLog); v != nil {
		if cfg.MaxBlocksForLog, err = cast.ToInt64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagMaxSubscriptionsNewHead); v != nil {
		if cfg.MaxSubscriptionsNewHead, err = cast.ToUint64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagEnableTestAPI); v != nil {
		if cfg.EnableTestAPI, err = cast.ToBoolE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagMaxConcurrentTraceCalls); v != nil {
		if cfg.MaxConcurrentTraceCalls, err = cast.ToUint64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagMaxConcurrentSimulationCalls); v != nil {
		if cfg.MaxConcurrentSimulationCalls, err = cast.ToIntE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagMaxTraceLookbackBlocks); v != nil {
		if cfg.MaxTraceLookbackBlocks, err = cast.ToInt64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagTraceTimeout); v != nil {
		if cfg.TraceTimeout, err = cast.ToDurationE(v); err != nil {
			return cfg, err
		}
	}

	return cfg, nil
}
