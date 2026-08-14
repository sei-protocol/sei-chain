package config

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/rpc"
	"github.com/sei-protocol/sei-chain/ratelimiter"
	servertypes "github.com/sei-protocol/sei-chain/sei-cosmos/server/types"
	"github.com/sei-protocol/sei-chain/sei-db/ledger_db/receipt"
	"github.com/spf13/cast"
)

const (
	// WorkerBatchSize is the number of blocks processed in each batch.
	// Used in filter.go for batch processing of block queries.
	WorkerBatchSize = 100

	// DefaultWorkerQueueSize is the default size of the task queue.
	// This represents the number of tasks (not blocks) that can be queued.
	// Total capacity = DefaultWorkerQueueSize * WorkerBatchSize blocks
	// Example: 1000 tasks * 100 blocks/task = 100,000 blocks can be buffered
	//
	// Memory footprint estimate:
	// - Queue channel overhead: ~8KB (1000 * 8 bytes per channel slot)
	// - Each task closure: ~24 bytes
	// - Total queue memory: ~32KB (negligible)
	// Note: Actual memory usage depends on block data processed by workers
	DefaultWorkerQueueSize = 1000

	// MaxWorkerPoolSize caps the number of workers to prevent excessive
	// goroutine creation on high-core machines. Tasks are primarily I/O bound
	// (fetching and processing block logs), so 2x CPU cores can be excessive.
	MaxWorkerPoolSize = 64

	TraceTracer4Byte    = "4byteTracer"
	TraceTracerCall     = "callTracer"
	TraceTracerFlatCall = "flatCallTracer"
	TraceTracerMux      = "muxTracer"
	TraceTracerNoop     = "noopTracer"
	TraceTracerPrestate = "prestateTracer"
)

var nativeTraceTracers = map[string]struct{}{
	TraceTracer4Byte:    {},
	TraceTracerCall:     {},
	TraceTracerFlatCall: {},
	TraceTracerMux:      {},
	TraceTracerNoop:     {},
	TraceTracerPrestate: {},
}

// DefaultTraceAllowedTracers returns the native debug tracers allowed by default.
func DefaultTraceAllowedTracers() []string {
	return []string{
		TraceTracerCall,
		TraceTracerPrestate,
		TraceTracerFlatCall,
		TraceTracer4Byte,
		TraceTracerNoop,
		TraceTracerMux,
	}
}

// IsNativeTraceTracer reports whether name is a registered native geth tracer
// name that can be safely allowlisted without enabling request-supplied JS.
func IsNativeTraceTracer(name string) bool {
	_, ok := nativeTraceTracers[name]
	return ok
}

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

	// Enable simulation before broadcasting EVM RPC sendRawTransaction.
	EnableSimulation bool `mapstructure:"enable_simulation"`

	// Deny list defines list of methods that EVM RPC should fail fast
	DenyList []string `mapstructure:"deny_list"`

	// max number of logs a single eth_getLogs query may match before it errors,
	// for both bounded and open-ended block ranges (a non-positive value falls
	// back to DefaultMaxLogLimit)
	MaxLogNoBlock int64 `mapstructure:"max_log_no_block"`

	// max estimated heap bytes of matched logs a single eth_getLogs query may
	// materialize before it errors (a non-positive value falls back to the
	// receipt store default)
	MaxLogBytes int64 `mapstructure:"max_log_bytes"`

	// max number of blocks to query logs for
	MaxBlocksForLog int64 `mapstructure:"max_blocks_for_log"`

	// max number of calls allowed in an eth_estimateGasAfterCalls request
	MaxEstimateGasCalls int `mapstructure:"max_estimate_gas_calls"`

	// max number of accounts allowed in a single state override
	MaxStateOverrideAccounts int `mapstructure:"max_state_override_accounts"`

	// max number of storage slots allowed per account in a state override
	MaxStateOverrideSlots int `mapstructure:"max_state_override_slots"`

	// max number of concurrent NewHead subscriptions
	MaxSubscriptionsNewHead uint64 `mapstructure:"max_subscriptions_new_head"`

	// max number of concurrent logs subscriptions
	MaxSubscriptionsLogs uint64 `mapstructure:"max_subscriptions_logs"`

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

	// MaxTraceStructLogBytes bounds the retained struct-logger output (in bytes) per
	// traced transaction on the default debug_trace* endpoints
	// (debug_traceCall/traceTransaction/traceBlock*), guarding against quadratic memory
	// growth from traces that read many distinct storage slots. The bound is per
	// transaction, not per RPC call: geth builds a fresh struct logger for each tx, so a
	// debug_traceBlock* call over N transactions retains up to N times this value (and the
	// parallelized path holds several concurrent traces live). Set to 0 for unlimited
	// (matches upstream geth behavior).
	MaxTraceStructLogBytes uint64 `mapstructure:"max_trace_struct_log_bytes"`

	// TraceAllowedTracers lists native debug tracer names that callers may
	// request with TraceConfig.Tracer. The default struct logger remains
	// available when Tracer is omitted. Request-supplied JavaScript tracers are
	// never allowed through this list.
	TraceAllowedTracers []string `mapstructure:"trace_allowed_tracers"`

	// TraceAllowJSTracers permits callers to supply JavaScript tracer source in
	// TraceConfig.Tracer. This executes request-supplied code in-process and is
	// disabled by default.
	TraceAllowJSTracers bool `mapstructure:"trace_allow_js_tracers"`

	// EnableParallelizedBlockTrace enables the parallelized default debug_traceBlock* path.
	EnableParallelizedBlockTrace bool `mapstructure:"enable_parallelized_block_trace"`

	// RPCStatsInterval for how often to report stats
	RPCStatsInterval time.Duration `mapstructure:"rpc_stats_interval"`

	// WorkerPoolSize defines the number of workers in the worker pool.
	// Set to 0 to use default: min(64, runtime.NumCPU() * 2)
	WorkerPoolSize int `mapstructure:"worker_pool_size"`

	// WorkerQueueSize defines the size of the task queue in the worker pool.
	// Set to 0 to use default: 1000
	WorkerQueueSize int `mapstructure:"worker_queue_size"`

	// EnabledLegacySeiApis lists which gated sei_* JSON-RPC methods are allowed on the EVM HTTP endpoint.
	// Set in app.toml [evm] as enabled_legacy_sei_apis (see ReadConfig and ConfigTemplate defaults).
	EnabledLegacySeiApis []string `mapstructure:"enabled_legacy_sei_apis"`

	// TraceBakeEnabled runs a background worker that re-executes each
	// committed block and caches the trace JSON at <home>/data/trace_db.
	// debug_trace* serves from cache on hit. RPC nodes only.
	TraceBakeEnabled      bool     `mapstructure:"trace_bake_enabled"`
	TraceBakeWorkers      int      `mapstructure:"trace_bake_workers"`       // re-execution goroutines (default 1)
	TraceBakeQueueSize    int      `mapstructure:"trace_bake_queue_size"`    // in-flight height queue (default 4096)
	TraceBakeTracers      []string `mapstructure:"trace_bake_tracers"`       // tracers to bake (default ["callTracer"])
	TraceBakeWindowBlocks int64    `mapstructure:"trace_bake_window_blocks"` // rolling prune window; 0 disables

	// TraceBakeUseSnapshot captures an in-memory memiavl snapshot at
	// EndBlock and uses it as the state backend for the baker, bypassing
	// SS-pebble. Requires MemiavlOnly write mode; falls back transparently.
	TraceBakeUseSnapshot    bool  `mapstructure:"trace_bake_use_snapshot"`
	TraceBakeSnapshotWindow int64 `mapstructure:"trace_bake_snapshot_window"` // recent snapshots to keep (default 64)

	// IPRateLimitRPS is the per-IP sustained request rate in requests/second.
	// Zero disables the token bucket (no HTTP 429 rejections). When
	// rate_limiting_enabled is true, the admission middleware still runs: bodies
	// are parsed and oversize/malformed requests are rejected before dispatch.
	IPRateLimitRPS float64 `mapstructure:"ip_rate_limit_rps"`

	// IPRateLimitBurst is the maximum per-IP burst size. Zero disables the token
	// bucket (same effect as ip_rate_limit_rps = 0) and does not bypass the
	// admission middleware when rate_limiting_enabled is true. Should be at least
	// batch_request_limit because the rate limiter charges one token per batch element.
	IPRateLimitBurst int `mapstructure:"ip_rate_limit_burst"`

	// RateLimitingEnabled is the master switch for the rate-limit admission
	// middleware on the EVM HTTP plane. When false, requests bypass method
	// extraction and all rejections from that layer (HTTP 400/413/429).
	RateLimitingEnabled bool `mapstructure:"rate_limiting_enabled"`

	// TrustedProxyCIDRs lists CIDRs whose X-Forwarded-For headers are trusted when
	// resolving the client IP for rate limiting. Empty means trust no proxy.
	TrustedProxyCIDRs []string `mapstructure:"trusted_proxy_cidrs"`

	// BatchRequestLimit is the maximum number of requests allowed in a single
	// JSON-RPC batch (HTTP and WebSocket). Set to 0 to disable the limit.
	BatchRequestLimit int `mapstructure:"batch_request_limit"`

	// BatchResponseMaxSize is the maximum number of bytes returned from a
	// batched JSON-RPC call (HTTP and WebSocket). Set to 0 to disable the limit.
	BatchResponseMaxSize int `mapstructure:"batch_response_max_size"`

	// MaxRequestBodyBytes is the maximum size, in bytes, of a single HTTP (:8545)
	// or WebSocket (:8546) JSON-RPC request body/frame. HTTP requests larger than
	// this are rejected (HTTP 413) before the body is buffered or JSON-decoded,
	// including at the rate limiter method-extraction layer. WebSocket frames
	// exceeding this limit close the connection with WebSocket close code 1009
	// (no JSON-RPC error response). Oversize WS rejections are recorded on
	// evmrpc_requests_rejected_total{protocol="ws",reason="oversize"}. 0 uses the
	// go-ethereum default (5 MiB). Upgrade note: WS previously used a hardcoded
	// 10 MiB frame cap. With default config both planes now use 5 MiB.
	MaxRequestBodyBytes int64 `mapstructure:"max_request_body_bytes"`

	// MaxConcurrentRequestBytes bounds the total size, in bytes, of HTTP and
	// WebSocket JSON-RPC request bodies admitted for processing concurrently.
	// HTTP (:8545) and WebSocket (:8546) each get an independent budget, so peak
	// in-flight request bytes process-wide can reach 2× this value (e.g. 256 MiB
	// when set to the 128 MiB default). HTTP charges the budget incrementally as
	// body bytes are read, bounding what a slow/stalled upload can pin to about
	// one read batch rather than the full declared Content-Length; requests that
	// would exceed the budget mid-read are rejected (HTTP 429). WebSocket blocks
	// until budget frees or WSAdmissionTimeout elapses; on timeout the peer
	// receives JSON-RPC error -32005 and the connection is closed (active
	// subscriptions are dropped with the connection). Set to 0 to disable the
	// limit on either protocol.
	MaxConcurrentRequestBytes int64 `mapstructure:"max_concurrent_request_bytes"`

	// BodyReadIdleTimeout is the maximum idle time allowed between body chunks
	// while reading an HTTP JSON-RPC request. Stalled body reads are cut with
	// HTTP 408 and release any byte budget held so far. Zero disables the
	// per-chunk idle guard (http.Server ReadTimeout remains the backstop).
	BodyReadIdleTimeout time.Duration `mapstructure:"body_read_idle_timeout"`

	// WSAdmissionTimeout bounds how long a WebSocket connection waits for
	// concurrent-byte budget to free before the next frame is read or committed.
	// When the wait expires the peer receives JSON-RPC error -32005
	// ("timed out waiting for concurrent request-byte budget") and the connection
	// is closed. Zero or negative values use the go-ethereum default (30s).
	WSAdmissionTimeout time.Duration `mapstructure:"ws_admission_timeout"`

	// MaxOpenConnections caps the number of simultaneously accepted connections
	// on the EVM HTTP and WebSocket listeners. The limit is applied per listener
	// (HTTP and WS each get their own budget). Excess connections block in the
	// accept queue until an active connection closes. Zero disables the limit.
	MaxOpenConnections int `mapstructure:"max_open_connections"`
}

const defaultBatchRequestLimit = 1000

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
	EnableSimulation:             true,
	DenyList:                     make([]string, 0),
	MaxLogNoBlock:                10000,
	MaxLogBytes:                  receipt.DefaultMaxLogBytes,
	MaxBlocksForLog:              2000,
	MaxEstimateGasCalls:          100,
	MaxStateOverrideAccounts:     100,
	MaxStateOverrideSlots:        1000,
	MaxSubscriptionsNewHead:      10000,
	MaxSubscriptionsLogs:         1000,
	EnableTestAPI:                false,
	MaxConcurrentTraceCalls:      10,
	MaxConcurrentSimulationCalls: runtime.NumCPU(),
	MaxTraceLookbackBlocks:       10000,
	TraceTimeout:                 30 * time.Second,
	MaxTraceStructLogBytes:       32 * 1024 * 1024, // 32 MiB
	TraceAllowedTracers:          DefaultTraceAllowedTracers(),
	TraceAllowJSTracers:          false,
	EnableParallelizedBlockTrace: false,
	RPCStatsInterval:             10 * time.Second,
	WorkerPoolSize:               min(MaxWorkerPoolSize, runtime.NumCPU()*2), // Default: min(64, CPU cores × 2)
	WorkerQueueSize:              DefaultWorkerQueueSize,                     // Default: 1000 tasks
	EnabledLegacySeiApis: []string{
		"sei_getSeiAddress",
		"sei_getEVMAddress",
		"sei_getCosmosTx",
	},
	TraceBakeEnabled:          false,
	TraceBakeWorkers:          1,
	TraceBakeQueueSize:        4096,
	TraceBakeTracers:          []string{"callTracer"},
	TraceBakeWindowBlocks:     0,
	TraceBakeUseSnapshot:      false,
	TraceBakeSnapshotWindow:   64,
	IPRateLimitRPS:            200,
	IPRateLimitBurst:          defaultBatchRequestLimit,
	RateLimitingEnabled:       false,
	TrustedProxyCIDRs:         nil,
	BatchRequestLimit:         defaultBatchRequestLimit,
	BatchResponseMaxSize:      25 * 1000 * 1000,  // 25MB
	MaxRequestBodyBytes:       5 * 1024 * 1024,   // 5 MiB (matches go-ethereum rpc default body limit)
	MaxConcurrentRequestBytes: 128 * 1024 * 1024, // 128 MiB of request bodies admitted concurrently
	WSAdmissionTimeout:        30 * time.Second,  // matches go-ethereum rpc defaultWSAdmissionTimeout
	MaxOpenConnections:        2000,
	BodyReadIdleTimeout:       10 * time.Second,
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
	flagEnableSimulation             = "evm.enable_simulation"
	flagDenyList                     = "evm.deny_list"
	flagMaxLogNoBlock                = "evm.max_log_no_block"
	flagMaxLogBytes                  = "evm.max_log_bytes"
	flagMaxBlocksForLog              = "evm.max_blocks_for_log"
	flagMaxEstimateGasCalls          = "evm.max_estimate_gas_calls"
	flagMaxStateOverrideAccounts     = "evm.max_state_override_accounts"
	flagMaxStateOverrideSlots        = "evm.max_state_override_slots"
	flagMaxSubscriptionsNewHead      = "evm.max_subscriptions_new_head"
	flagMaxSubscriptionsLogs         = "evm.max_subscriptions_logs"
	flagEnableTestAPI                = "evm.enable_test_api"
	flagMaxConcurrentTraceCalls      = "evm.max_concurrent_trace_calls"
	flagMaxConcurrentSimulationCalls = "evm.max_concurrent_simulation_calls"
	flagMaxTraceLookbackBlocks       = "evm.max_trace_lookback_blocks"
	flagTraceTimeout                 = "evm.trace_timeout"
	flagMaxTraceStructLogBytes       = "evm.max_trace_struct_log_bytes"
	flagTraceAllowedTracers          = "evm.trace_allowed_tracers"
	flagTraceAllowJSTracers          = "evm.trace_allow_js_tracers"
	flagEnableParallelizedBlockTrace = "evm.enable_parallelized_block_trace"
	flagRPCStatsInterval             = "evm.rpc_stats_interval"
	flagWorkerPoolSize               = "evm.worker_pool_size"
	flagWorkerQueueSize              = "evm.worker_queue_size"
	flagEVMLegacySeiApis             = "evm.enabled_legacy_sei_apis"
	flagTraceBakeEnabled             = "evm.trace_bake_enabled"
	flagTraceBakeWorkers             = "evm.trace_bake_workers"
	flagTraceBakeQueueSize           = "evm.trace_bake_queue_size"
	flagTraceBakeTracers             = "evm.trace_bake_tracers"
	flagTraceBakeWindowBlocks        = "evm.trace_bake_window_blocks"
	flagTraceBakeUseSnapshot         = "evm.trace_bake_use_snapshot"
	flagTraceBakeSnapshotWindow      = "evm.trace_bake_snapshot_window"
	flagIPRateLimitRPS               = "evm.ip_rate_limit_rps"
	flagIPRateLimitBurst             = "evm.ip_rate_limit_burst"
	flagRateLimitingEnabled          = "evm.rate_limiting_enabled"
	flagTrustedProxyCIDRs            = "evm.trusted_proxy_cidrs"
	flagBatchRequestLimit            = "evm.batch_request_limit"
	flagBatchResponseMaxSize         = "evm.batch_response_max_size"
	flagMaxRequestBodyBytes          = "evm.max_request_body_bytes"
	flagMaxConcurrentRequestBytes    = "evm.max_concurrent_request_bytes"
	flagWSAdmissionTimeout           = "evm.ws_admission_timeout"
	flagMaxOpenConnections           = "evm.max_open_connections"
	flagBodyReadIdleTimeout          = "evm.body_read_idle_timeout"
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
	if v := opts.Get(flagEnableSimulation); v != nil {
		if cfg.EnableSimulation, err = cast.ToBoolE(v); err != nil {
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
	if v := opts.Get(flagMaxLogBytes); v != nil {
		if cfg.MaxLogBytes, err = cast.ToInt64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagMaxBlocksForLog); v != nil {
		if cfg.MaxBlocksForLog, err = cast.ToInt64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagMaxEstimateGasCalls); v != nil {
		if cfg.MaxEstimateGasCalls, err = cast.ToIntE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagMaxStateOverrideAccounts); v != nil {
		if cfg.MaxStateOverrideAccounts, err = cast.ToIntE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagMaxStateOverrideSlots); v != nil {
		if cfg.MaxStateOverrideSlots, err = cast.ToIntE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagMaxSubscriptionsNewHead); v != nil {
		if cfg.MaxSubscriptionsNewHead, err = cast.ToUint64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagMaxSubscriptionsLogs); v != nil {
		if cfg.MaxSubscriptionsLogs, err = cast.ToUint64E(v); err != nil {
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
	if v := opts.Get(flagMaxTraceStructLogBytes); v != nil {
		if cfg.MaxTraceStructLogBytes, err = cast.ToUint64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagTraceAllowedTracers); v != nil {
		if cfg.TraceAllowedTracers, err = cast.ToStringSliceE(v); err != nil {
			return cfg, err
		}
	}
	if cfg.TraceAllowedTracers, err = normalizeNativeTracerNames(flagTraceAllowedTracers, cfg.TraceAllowedTracers); err != nil {
		return cfg, err
	}
	if v := opts.Get(flagTraceAllowJSTracers); v != nil {
		if cfg.TraceAllowJSTracers, err = cast.ToBoolE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagEnableParallelizedBlockTrace); v != nil {
		if cfg.EnableParallelizedBlockTrace, err = cast.ToBoolE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagRPCStatsInterval); v != nil {
		if cfg.RPCStatsInterval, err = cast.ToDurationE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagWorkerPoolSize); v != nil {
		if cfg.WorkerPoolSize, err = cast.ToIntE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagWorkerQueueSize); v != nil {
		if cfg.WorkerQueueSize, err = cast.ToIntE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagEVMLegacySeiApis); v != nil {
		if cfg.EnabledLegacySeiApis, err = cast.ToStringSliceE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagTraceBakeEnabled); v != nil {
		if cfg.TraceBakeEnabled, err = cast.ToBoolE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagTraceBakeWorkers); v != nil {
		if cfg.TraceBakeWorkers, err = cast.ToIntE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagTraceBakeQueueSize); v != nil {
		if cfg.TraceBakeQueueSize, err = cast.ToIntE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagTraceBakeTracers); v != nil {
		if cfg.TraceBakeTracers, err = cast.ToStringSliceE(v); err != nil {
			return cfg, err
		}
	}
	// The baker feeds these names straight into geth tracing, so hold them to
	// the same native-only rule as request-supplied tracers: a typo'd name must
	// fail at startup instead of being evaluated as JS source on every block.
	if cfg.TraceBakeTracers, err = normalizeNativeTracerNames(flagTraceBakeTracers, cfg.TraceBakeTracers); err != nil {
		return cfg, err
	}
	if v := opts.Get(flagTraceBakeWindowBlocks); v != nil {
		if cfg.TraceBakeWindowBlocks, err = cast.ToInt64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagTraceBakeUseSnapshot); v != nil {
		if cfg.TraceBakeUseSnapshot, err = cast.ToBoolE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagTraceBakeSnapshotWindow); v != nil {
		if cfg.TraceBakeSnapshotWindow, err = cast.ToInt64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagIPRateLimitRPS); v != nil {
		if cfg.IPRateLimitRPS, err = cast.ToFloat64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagIPRateLimitBurst); v != nil {
		if cfg.IPRateLimitBurst, err = cast.ToIntE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagRateLimitingEnabled); v != nil {
		if cfg.RateLimitingEnabled, err = cast.ToBoolE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagTrustedProxyCIDRs); v != nil {
		if cfg.TrustedProxyCIDRs, err = cast.ToStringSliceE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagBatchRequestLimit); v != nil {
		if cfg.BatchRequestLimit, err = cast.ToIntE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagBatchResponseMaxSize); v != nil {
		if cfg.BatchResponseMaxSize, err = cast.ToIntE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagMaxRequestBodyBytes); v != nil {
		if cfg.MaxRequestBodyBytes, err = cast.ToInt64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagMaxConcurrentRequestBytes); v != nil {
		if cfg.MaxConcurrentRequestBytes, err = cast.ToInt64E(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagWSAdmissionTimeout); v != nil {
		if cfg.WSAdmissionTimeout, err = cast.ToDurationE(v); err != nil {
			return cfg, err
		}
	}
	if v := opts.Get(flagMaxOpenConnections); v != nil {
		if cfg.MaxOpenConnections, err = cast.ToIntE(v); err != nil {
			return cfg, err
		}
		if cfg.MaxOpenConnections < 0 {
			return cfg, fmt.Errorf("%s must be >= 0 (0 disables the limit), got %d", flagMaxOpenConnections, cfg.MaxOpenConnections)
		}
	}
	if v := opts.Get(flagBodyReadIdleTimeout); v != nil {
		if cfg.BodyReadIdleTimeout, err = cast.ToDurationE(v); err != nil {
			return cfg, err
		}
		if cfg.BodyReadIdleTimeout < 0 {
			return cfg, fmt.Errorf("%s must be >= 0 (0 disables the idle guard), got %s", flagBodyReadIdleTimeout, cfg.BodyReadIdleTimeout)
		}
	}
	if cfg.RateLimitingEnabled && cfg.IPRateLimitBurst > 0 && cfg.BatchRequestLimit > 0 &&
		cfg.IPRateLimitBurst < cfg.BatchRequestLimit {
		return cfg, fmt.Errorf(
			"%s (%d) must be >= %s (%d): the rate limiter charges one token per batch element, "+
				"so a lower burst would permanently reject any full-size batch",
			flagIPRateLimitBurst, cfg.IPRateLimitBurst, flagBatchRequestLimit, cfg.BatchRequestLimit,
		)
	}
	return cfg, nil
}

func normalizeNativeTracerNames(flagName string, names []string) ([]string, error) {
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed == "" {
			return nil, fmt.Errorf("%s entries must not be empty", flagName)
		}
		if !IsNativeTraceTracer(trimmed) {
			return nil, fmt.Errorf("%s contains non-native tracer %q", flagName, trimmed)
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out, nil
}

// RateLimiterConfig builds the ratelimiter.Config used by EVM JSON-RPC admission.
func (c Config) RateLimiterConfig() ratelimiter.Config {
	return ratelimiter.Config{
		RPS:               c.IPRateLimitRPS,
		Burst:             c.IPRateLimitBurst,
		TrustedProxyCIDRs: c.TrustedProxyCIDRs,
	}
}

// ConfigTemplate defines the TOML configuration template for EVM RPC
const ConfigTemplate = `
###############################################################################
###                            EVM Configuration                            ###
###############################################################################

[evm]
# controls whether an HTTP EVM server is enabled
http_enabled = {{ .EVM.HTTPEnabled }}
http_port = {{ .EVM.HTTPPort }}

# controls whether a websocket server is enabled
ws_enabled = {{ .EVM.WSEnabled }}
ws_port = {{ .EVM.WSPort }}

# ReadTimeout is the maximum duration for reading the entire
# request, including the body.
# Because ReadTimeout does not let Handlers make per-request
# decisions on each request body's acceptable deadline or
# upload rate, most users will prefer to use
# ReadHeaderTimeout. It is valid to use them both.
read_timeout = "{{ .EVM.ReadTimeout }}"

# ReadHeaderTimeout is the amount of time allowed to read
# request headers. The connection's read deadline is reset
# after reading the headers and the Handler can decide what
# is considered too slow for the body. If ReadHeaderTimeout
# is zero, the value of ReadTimeout is used. If both are
# zero, there is no timeout.
read_header_timeout = "{{ .EVM.ReadHeaderTimeout }}"

# WriteTimeout is the maximum duration before timing out
# writes of the response. It is reset whenever a new
# request's header is read. Like ReadTimeout, it does not
# let Handlers make decisions on a per-request basis.
write_timeout = "{{ .EVM.WriteTimeout }}"

# IdleTimeout is the maximum amount of time to wait for the
# next request when keep-alives are enabled. If IdleTimeout
# is zero, the value of ReadTimeout is used. If both are
# zero, ReadHeaderTimeout is used.
idle_timeout = "{{ .EVM.IdleTimeout }}"

# Maximum gas limit for simulation
simulation_gas_limit = {{ .EVM.SimulationGasLimit }}

# Timeout for EVM call in simulation
simulation_evm_timeout = "{{ .EVM.SimulationEVMTimeout }}"

# list of CORS allowed origins, separated by comma
cors_origins = "{{ .EVM.CORSOrigins }}"

# list of WS origins, separated by comma
ws_origins = "{{ .EVM.WSOrigins }}"

# timeout for filters
filter_timeout = "{{ .EVM.FilterTimeout }}"

# checkTx timeout for sig verify
checktx_timeout = "{{ .EVM.CheckTxTimeout }}"

# controls whether to have txns go through one by one
slow = {{ .EVM.Slow }}

# Enables simulation before broadcasting EVM RPC sendRawTransaction.
enable_simulation = {{ .EVM.EnableSimulation }}

# Deny list defines list of methods that EVM RPC should fail fast, e.g ["debug_traceBlockByNumber"]
deny_list = {{ .EVM.DenyList }}

# Legacy sei_* JSON-RPC (EVM HTTP only - not Cosmos REST on 1317).
#
# DEPRECATION: The sei_* JSON-RPC surface is deprecated and scheduled for removal. Do not
# build new integrations on them; use eth_* / debug_* and documented replacements. HTTP 200;
# gate errors use standard JSON-RPC error encoding (see evmrpc/AGENTS.md). Successful allowlisted
# responses are unchanged; nodes may set HTTP header Sei-Legacy-RPC-Deprecation (see AGENTS.md).
#
# Only methods listed in enabled_legacy_sei_apis are allowed. Init defaults enable the three
# address/Cosmos helpers; uncomment optional lines below to enable more legacy methods.
enabled_legacy_sei_apis = [
{{- range .EVM.EnabledLegacySeiApis }}
  "{{ . }}",
{{- end }}

  # Optional legacy methods - uncomment to enable (same deprecation applies):
  # "sei_associate",
  # "sei_getBlockByHash",
  # "sei_getBlockByHashExcludeTraceFail",
  # "sei_getBlockByNumber",
  # "sei_getBlockByNumberExcludeTraceFail",
  # "sei_getBlockReceipts",
  # "sei_getBlockTransactionCountByHash",
  # "sei_getBlockTransactionCountByNumber",
  # "sei_getEvmTx",
  # "sei_getFilterChanges",
  # "sei_getFilterLogs",
  # "sei_getLogs",
  # "sei_getTransactionByBlockHashAndIndex",
  # "sei_getTransactionByBlockNumberAndIndex",
  # "sei_getTransactionByHash",
  # "sei_getTransactionCount",
  # "sei_getTransactionErrorByHash",
  # "sei_getTransactionReceipt",
  # "sei_getTransactionReceiptExcludeTraceFail",
  # "sei_getVMError",
  # "sei_newBlockFilter",
  # "sei_newFilter",
  # "sei_sign",
  # "sei_uninstallFilter",
]

# max number of logs a single eth_getLogs query may match before it errors,
# for both bounded and open-ended block ranges
max_log_no_block = {{ .EVM.MaxLogNoBlock }}

# max estimated heap bytes of matched logs a single eth_getLogs query may return
max_log_bytes = {{ .EVM.MaxLogBytes }}

# max number of blocks to query logs for
max_blocks_for_log = {{ .EVM.MaxBlocksForLog }}

# max number of calls allowed in an eth_estimateGasAfterCalls request
max_estimate_gas_calls = {{ .EVM.MaxEstimateGasCalls }}

# max number of accounts allowed in a single state override
max_state_override_accounts = {{ .EVM.MaxStateOverrideAccounts }}

# max number of storage slots allowed per account in a state override
max_state_override_slots = {{ .EVM.MaxStateOverrideSlots }}

# max number of concurrent NewHead subscriptions
max_subscriptions_new_head = {{ .EVM.MaxSubscriptionsNewHead }}

# max number of concurrent logs subscriptions
max_subscriptions_logs = {{ .EVM.MaxSubscriptionsLogs }}

# MaxConcurrentTraceCalls defines the maximum number of concurrent debug_trace calls.
# Set to 0 for unlimited.
max_concurrent_trace_calls = {{ .EVM.MaxConcurrentTraceCalls }}

# Max number of blocks allowed to look back for tracing
# Set to -1 for unlimited lookback, which is useful for archive nodes.
max_trace_lookback_blocks = {{ .EVM.MaxTraceLookbackBlocks }}

# Timeout for each trace call
trace_timeout = "{{ .EVM.TraceTimeout }}"

# MaxTraceStructLogBytes bounds the retained struct-logger output (in bytes) per traced
# transaction on the default debug_trace* endpoints, guarding against quadratic memory growth
# from traces that read many distinct storage slots. The bound is per transaction, not per RPC
# call: a debug_traceBlock* call over N transactions retains up to N times this value (and the
# parallelized path holds several concurrent traces live). Set to 0 for unlimited (matches
# upstream geth behavior).
max_trace_struct_log_bytes = {{ .EVM.MaxTraceStructLogBytes }}

# Native debug tracers that may be requested with TraceConfig.Tracer. The default
# struct logger remains available when Tracer is omitted. Request-supplied
# JavaScript tracer source is disabled and cannot be enabled through this list.
# Set to [] to disable all named tracers.
trace_allowed_tracers = [{{- range $i, $t := .EVM.TraceAllowedTracers }}{{- if $i }}, {{ end }}"{{ $t }}"{{- end }}]

# Allow request-supplied JavaScript tracer source in TraceConfig.Tracer. This
# executes untrusted code in-process; keep disabled on public/default RPC nodes.
# Enabling this does not widen trace_allowed_tracers: native tracer names must
# still be listed there to be usable.
trace_allow_js_tracers = {{ .EVM.TraceAllowJSTracers }}

# Enable the parallelized default debug_traceBlock* path.
enable_parallelized_block_trace = {{ .EVM.EnableParallelizedBlockTrace }}

# WorkerPoolSize defines the number of workers in the worker pool.
# Default: min(64, CPU cores × 2). Capped at 64 to prevent excessive goroutines on high-core machines.
# Set to 0 to use the default.
worker_pool_size = {{ .EVM.WorkerPoolSize }}

# WorkerQueueSize defines the size of the task queue in the worker pool.
# Default: 1000 tasks. Set to 0 to use the default.
worker_queue_size = {{ .EVM.WorkerQueueSize }}

# TraceBakeEnabled, when true, runs a background worker that re-executes
# each committed block with the configured tracers and stores the result
# to <home>/data/trace_db. debug_traceTransaction with a bakeable
# tracer config (callTracer / prestateTracer / flatCallTracer) returns
# from cache on hit. Recommended for RPC nodes only; default false.
trace_bake_enabled = {{ .EVM.TraceBakeEnabled }}

# Number of re-execution worker goroutines (default 1).
trace_bake_workers = {{ .EVM.TraceBakeWorkers }}

# Bounded in-flight height queue. Drops on full so consensus never blocks.
trace_bake_queue_size = {{ .EVM.TraceBakeQueueSize }}

# Which tracers to bake per block; must be native tracer names (validated at startup).
trace_bake_tracers = [{{- range $i, $t := .EVM.TraceBakeTracers }}{{- if $i }}, {{ end }}"{{ $t }}"{{- end }}]

# Rolling cache window: prune blocks older than (latest - this).
# 0 disables pruning (cache grows forever).
trace_bake_window_blocks = {{ .EVM.TraceBakeWindowBlocks }}

# TraceBakeUseSnapshot, when true, uses in-memory memiavl snapshots as the
# state backend for trace baking when the store backend supports snapshots.
# Watch these metrics when enabling on a high-throughput node:
#   - memiavl_mem_node_total_size / memiavl_num_of_mem_node: rise if held
#     snapshots are pinning too many COW nodes; lower the window or drop the
#     memiavl snapshot interval.
#   - trace baker dropped/baked counters: dropped > 0 or baked lagging chain
#     tip means the baker is falling behind.
trace_bake_use_snapshot = {{ .EVM.TraceBakeUseSnapshot }}

# Number of recent memiavl snapshots to retain for trace baking.
trace_bake_snapshot_window = {{ .EVM.TraceBakeSnapshotWindow }}

# ip_rate_limit_rps is the per-IP sustained request rate in requests/second.
# Set to 0 to disable per-IP throttling (no HTTP 429). Does not bypass the
# admission middleware; set rate_limiting_enabled = false for a full bypass.
ip_rate_limit_rps = {{ .EVM.IPRateLimitRPS }}

# ip_rate_limit_burst is the maximum per-IP burst above the sustained rate.
# Set to 0 to disable per-IP throttling (same effect as ip_rate_limit_rps = 0).
# Must be at least batch_request_limit when both are positive and rate_limiting_enabled
# is true (one token is charged per batch element) - enforced at startup.
ip_rate_limit_burst = {{ .EVM.IPRateLimitBurst }}

# rate_limiting_enabled is the master switch for the rate-limit admission
# middleware on the EVM HTTP plane (:8545). When false, requests bypass method
# extraction and all rejections from that layer (HTTP 400/413/429).
rate_limiting_enabled = {{ .EVM.RateLimitingEnabled }}

# trusted_proxy_cidrs lists CIDRs whose X-Forwarded-For headers are trusted when
# resolving the client IP for rate limiting. Empty means trust no proxy — set
# this to your ingress/LB CIDRs when the node sits behind a reverse proxy.
# Do NOT use the full RFC-1918 private ranges unless every address in them is
# your own controlled infrastructure.
trusted_proxy_cidrs = [{{- range $i, $c := .EVM.TrustedProxyCIDRs }}{{- if $i }}, {{ end }}"{{ $c }}"{{- end }}]

# batch_request_limit is the maximum number of requests allowed in a single
# JSON-RPC batch (HTTP and WebSocket). Set to 0 to disable the limit.
batch_request_limit = {{ .EVM.BatchRequestLimit }}

# batch_response_max_size is the maximum number of bytes returned from a
# batched JSON-RPC call (HTTP and WebSocket). Set to 0 to disable the limit.
batch_response_max_size = {{ .EVM.BatchResponseMaxSize }}

# max_request_body_bytes is the maximum size, in bytes, of a single HTTP (:8545)
# or WebSocket (:8546) JSON-RPC request/frame. HTTP larger requests are rejected
# (HTTP 413) before decode, including at the rate limiter method-extraction
# layer. WS frames exceeding this limit close the connection with WebSocket
# close code 1009 (no JSON-RPC error). Set to 0 to use the default (5 MiB).
# Upgrade note: WS previously used a hardcoded 10 MiB frame cap; if WS clients
# send frames in the 5-10 MiB range, set max_request_body_bytes = 10485760
# (10 MiB) in app.toml.
max_request_body_bytes = {{ .EVM.MaxRequestBodyBytes }}

# max_concurrent_request_bytes bounds total request bytes admitted concurrently
# on HTTP (:8545) and WebSocket (:8546). Each protocol gets an independent
# budget, so peak in-flight bytes process-wide can reach 2× this value. HTTP
# charges the budget incrementally as body bytes are read, bounding what a
# slow/stalled upload can pin to about one read batch rather than the full
# declared Content-Length; requests that would exceed the budget mid-read are
# rejected (HTTP 429). WS blocks until budget frees or ws_admission_timeout
# elapses; on timeout the peer gets JSON-RPC error -32005 and the connection
# closes. Set to 0 to disable on either protocol.
max_concurrent_request_bytes = {{ .EVM.MaxConcurrentRequestBytes }}

# ws_admission_timeout bounds how long a WebSocket connection waits for
# concurrent-byte budget before the next frame is read or committed. On expiry
# the peer receives JSON-RPC error -32005 and the connection closes. Zero or
# negative values use the go-ethereum default (30s).
ws_admission_timeout = "{{ .EVM.WSAdmissionTimeout }}"

# body_read_idle_timeout is the maximum idle time allowed between body chunks
# while reading an HTTP JSON-RPC request. Stalled body reads return HTTP 408 and
# release any byte budget held so far. Set to 0 to disable (ReadTimeout remains
# the whole-request backstop).
body_read_idle_timeout = "{{ .EVM.BodyReadIdleTimeout }}"

# max_open_connections caps the number of simultaneously accepted connections on
# the EVM HTTP and WebSocket listeners. Set to 0 to disable the limit.
max_open_connections = {{ .EVM.MaxOpenConnections }}

`
