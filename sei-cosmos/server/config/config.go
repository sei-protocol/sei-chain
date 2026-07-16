package config

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
	"github.com/sei-protocol/sei-chain/sei-cosmos/telemetry"
	sdk "github.com/sei-protocol/sei-chain/sei-cosmos/types"
	sdkerrors "github.com/sei-protocol/sei-chain/sei-cosmos/types/errors"
	"github.com/sei-protocol/sei-chain/sei-db/config"
	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
	"github.com/spf13/viper"
)

const (
	// DefaultMinGasPrices defines the default minimum gas prices
	DefaultMinGasPrices = "0.01usei"

	// DefaultGRPCAddress defines the default address to bind the gRPC server to.
	DefaultGRPCAddress = "0.0.0.0:9090"

	// DefaultGRPCWebAddress defines the default address to bind the gRPC-web server to.
	DefaultGRPCWebAddress = "0.0.0.0:9091"

	// DefaultGRPCWebMaxOpenConnections defines the default maximum number of
	// simultaneous open connections for the gRPC-web server.
	DefaultGRPCWebMaxOpenConnections = 1000

	// DefaultGRPCMaxOpenConnections defines the default maximum number of
	// simultaneous open connections for the gRPC server. 0 means unlimited.
	DefaultGRPCMaxOpenConnections = 1000

	// DefaultGRPCMaxRecvMsgSize defines the default maximum message size in bytes
	// that the gRPC server can receive (4 MB), mirroring gRPC's own default.
	DefaultGRPCMaxRecvMsgSize = 4 * 1024 * 1024

	// DefaultGRPCMaxConnectionIdle is the default duration after which an idle
	// connection (one with no in-flight RPCs) is closed via GoAway. It is bounded
	// by default to reclaim abandoned connection slots — which matter now that the
	// listener is capped by MaxOpenConnections — while staying long enough not to
	// churn clients that query on a shorter cadence. The real DoS bound is the
	// connection cap; this only reaps dormant connections. 0 means infinity.
	DefaultGRPCMaxConnectionIdle = 5 * time.Minute

	// The remaining keepalive defaults below mirror gRPC's own defaults, so they
	// are opt-in for operators and do not change behavior unless configured.

	// DefaultGRPCMaxConnectionAge is the default maximum duration a connection may
	// exist before it is closed. 0 means infinity.
	DefaultGRPCMaxConnectionAge = time.Duration(0)

	// DefaultGRPCMaxConnectionAgeGrace is the default additive period after
	// MaxConnectionAge during which the connection is forcibly closed. 0 means infinity.
	DefaultGRPCMaxConnectionAgeGrace = time.Duration(0)

	// DefaultGRPCKeepaliveTime is the default interval after which, if the server
	// sees no activity, it pings the client to check liveness.
	DefaultGRPCKeepaliveTime = 2 * time.Hour

	// DefaultGRPCKeepaliveTimeout is the default duration the server waits for a
	// keepalive ping ack before closing the connection.
	DefaultGRPCKeepaliveTimeout = 20 * time.Second

	// DefaultGRPCKeepaliveMinTime is the default minimum interval a client must
	// wait between keepalive pings; pings more frequent than this are penalized.
	DefaultGRPCKeepaliveMinTime = 5 * time.Minute

	// DefaultGRPCKeepalivePermitWithoutStream defines whether the server allows
	// keepalive pings even when there are no active streams.
	DefaultGRPCKeepalivePermitWithoutStream = false

	// DefaultOccEanbled defines whether to use OCC for tx processing
	DefaultOccEnabled = true
)

var (
	// DefaultConcurrencyWorkers defines the default workers to use for concurrent transactions
	// Set to 2x CPU cores, capped between [10, 128]
	// - 2x CPU: In practice, goroutines often block on IO/network, so having more workers
	//   than CPU cores keeps CPUs busy. Load tests show only 60-70% CPU usage with 500 workers
	//   processing ~1500 txs/block, suggesting IO-bound workload benefits from oversubscription.
	// - Min 10: Minimum viable parallelism for transaction processing
	// - Max 128: Prevents unbounded goroutine creation on high-core machines. While 500 workers
	//   worked in tests, 128 provides sufficient parallelism without excessive scheduler overhead.
	DefaultConcurrencyWorkers = getConcurrencyWorkers()
)

// getConcurrencyWorkers returns the default number of concurrency workers
func getConcurrencyWorkers() int {
	workers := runtime.NumCPU() * 2
	return max(10, min(workers, 128))
}

// BaseConfig defines the server's basic configuration
type BaseConfig struct {
	// The minimum gas prices a validator is willing to accept for processing a
	// transaction. A transaction's fees must meet the minimum of any denomination
	// specified in this config (e.g. 0.25token1;0.0001token2).
	MinGasPrices string `mapstructure:"minimum-gas-prices"`

	Pruning           string `mapstructure:"pruning"`
	PruningKeepRecent string `mapstructure:"pruning-keep-recent"`
	PruningKeepEvery  string `mapstructure:"pruning-keep-every"`
	PruningInterval   string `mapstructure:"pruning-interval"`

	// HaltHeight contains a non-zero block height at which a node will gracefully
	// halt and shutdown that can be used to assist upgrades and testing.
	//
	// Note: Commitment of state will be attempted on the corresponding block.
	HaltHeight uint64 `mapstructure:"halt-height"`

	// HaltTime contains a non-zero minimum block time (in Unix seconds) at which
	// a node will gracefully halt and shutdown that can be used to assist
	// upgrades and testing.
	//
	// Note: Commitment of state will be attempted on the corresponding block.
	HaltTime uint64 `mapstructure:"halt-time"`

	// MinRetainBlocks defines the minimum block height offset from the current
	// block being committed, such that blocks past this offset may be pruned
	// from Tendermint. It is used as part of the process of determining the
	// ResponseCommit.RetainHeight value during ABCI Commit. A value of 0 indicates
	// that no blocks should be pruned.
	//
	// This configuration value is only responsible for pruning Tendermint blocks.
	// It has no bearing on application state pruning which is determined by the
	// "pruning-*" configurations.
	//
	// Note: Tendermint block pruning is dependant on this parameter in conunction
	// with the unbonding (safety threshold) period, state pruning and state sync
	// snapshot parameters to determine the correct minimum value of
	// ResponseCommit.RetainHeight.
	MinRetainBlocks uint64 `mapstructure:"min-retain-blocks"`

	// InterBlockCache enables inter-block caching.
	InterBlockCache bool `mapstructure:"inter-block-cache"`

	// IndexEvents defines the set of events in the form {eventType}.{attributeKey},
	// which informs Tendermint what to index. If empty, all events will be indexed.
	IndexEvents []string `mapstructure:"index-events"`

	// CompactionInterval sets (in seconds) the interval between forced levelDB
	// compaction. A value of 0 means no forced levelDB
	CompactionInterval uint64 `mapstructure:"compaction-interval"`

	// ConcurrencyWorkers defines the number of workers to use for concurrent
	// transaction execution. A value of -1 means unlimited workers.  Default value is 10.
	ConcurrencyWorkers int `mapstructure:"concurrency-workers"`
	// Whether to enable optimistic concurrency control for tx execution, default is true
	OccEnabled bool `mapstructure:"occ-enabled"`
}

// APIConfig defines the API listener configuration.
type APIConfig struct {
	// Enable defines if the API server should be enabled.
	Enable bool `mapstructure:"enable"`

	// Swagger defines if swagger documentation should automatically be registered.
	Swagger bool `mapstructure:"swagger"`

	// EnableUnsafeCORS defines if CORS should be enabled (unsafe - use it at your own risk)
	EnableUnsafeCORS bool `mapstructure:"enabled-unsafe-cors"`

	// Address defines the API server to listen on
	Address string `mapstructure:"address"`

	// MaxOpenConnections defines the number of maximum open connections
	MaxOpenConnections uint `mapstructure:"max-open-connections"`

	// RPCReadTimeout defines the Tendermint RPC read timeout (in seconds)
	RPCReadTimeout uint `mapstructure:"rpc-read-timeout"`

	// RPCWriteTimeout defines the Tendermint RPC write timeout (in seconds)
	RPCWriteTimeout uint `mapstructure:"rpc-write-timeout"`

	// RPCMaxBodyBytes defines the Tendermint maximum response body (in bytes)
	RPCMaxBodyBytes uint `mapstructure:"rpc-max-body-bytes"`

	// TODO: TLS/Proxy configuration.
	//
	// Ref: https://github.com/cosmos/cosmos-sdk/issues/6420
}

// RosettaConfig defines the Rosetta API listener configuration.
type RosettaConfig struct {
	// Address defines the API server to listen on
	Address string `mapstructure:"address"`

	// Blockchain defines the blockchain name
	// defaults to DefaultBlockchain
	Blockchain string `mapstructure:"blockchain"`

	// Network defines the network name
	Network string `mapstructure:"network"`

	// Retries defines the maximum number of retries
	// rosetta will do before quitting
	Retries int `mapstructure:"retries"`

	// Enable defines if the API server should be enabled.
	Enable bool `mapstructure:"enable"`

	// Offline defines if the server must be run in offline mode
	Offline bool `mapstructure:"offline"`
}

// GRPCConfig defines configuration for the gRPC server.
type GRPCConfig struct {
	// Enable defines if the gRPC server should be enabled.
	Enable bool `mapstructure:"enable"`

	// Address defines the API server to listen on
	Address string `mapstructure:"address"`

	// MaxRecvMsgSize defines the maximum message size in bytes the server can
	// receive. It bounds per-request memory allocation before the rate limiter
	// fires. Defaults to 4 MB.
	MaxRecvMsgSize int `mapstructure:"max-recv-msg-size"`

	// MaxOpenConnections defines the maximum number of simultaneous open
	// connections. 0 means unlimited.
	MaxOpenConnections uint `mapstructure:"max-open-connections"`

	// MaxConnectionIdle is the duration after which an idle connection is closed.
	// 0 means infinity.
	MaxConnectionIdle time.Duration `mapstructure:"max-connection-idle"`

	// MaxConnectionAge is the maximum duration a connection may exist before it
	// is closed (a jitter is added by gRPC). 0 means infinity.
	MaxConnectionAge time.Duration `mapstructure:"max-connection-age"`

	// MaxConnectionAgeGrace is an additive period after MaxConnectionAge during
	// which the connection is forcibly closed. 0 means infinity.
	MaxConnectionAgeGrace time.Duration `mapstructure:"max-connection-age-grace"`

	// KeepaliveTime is the interval after which, if the server sees no activity,
	// it pings the client to check liveness.
	KeepaliveTime time.Duration `mapstructure:"keepalive-time"`

	// KeepaliveTimeout is the duration the server waits for a keepalive ping ack
	// before closing the connection.
	KeepaliveTimeout time.Duration `mapstructure:"keepalive-timeout"`

	// KeepaliveMinTime is the minimum interval a client must wait between
	// keepalive pings; clients pinging more frequently are penalized.
	KeepaliveMinTime time.Duration `mapstructure:"keepalive-min-time"`

	// KeepalivePermitWithoutStream defines whether the server allows keepalive
	// pings even when there are no active streams.
	KeepalivePermitWithoutStream bool `mapstructure:"keepalive-permit-without-stream"`
}

// GRPCWebConfig defines configuration for the gRPC-web server.
type GRPCWebConfig struct {
	// Enable defines if the gRPC-web should be enabled.
	Enable bool `mapstructure:"enable"`

	// Address defines the gRPC-web server to listen on
	Address string `mapstructure:"address"`

	// EnableUnsafeCORS defines if CORS should be enabled (unsafe - use it at your own risk)
	EnableUnsafeCORS bool `mapstructure:"enable-unsafe-cors"`

	// MaxOpenConnections defines the maximum number of simultaneous open connections. 0 means unlimited.
	MaxOpenConnections uint `mapstructure:"max-open-connections"`
}

// StateSyncConfig defines the state sync snapshot configuration.
type StateSyncConfig struct {
	// SnapshotInterval sets the interval at which state sync snapshots are taken.
	// 0 disables snapshots. Must be a multiple of PruningKeepEvery.
	SnapshotInterval uint64 `mapstructure:"snapshot-interval"`

	// SnapshotKeepRecent sets the number of recent state sync snapshots to keep.
	// 0 keeps all snapshots.
	SnapshotKeepRecent uint32 `mapstructure:"snapshot-keep-recent"`

	// SnapshotDirectory sets the parent directory for where state sync snapshots are persisted.
	// Default is empty which will then store under the app home directory.
	SnapshotDirectory string `mapstructure:"snapshot-directory"`
}

// GenesisConfig defines the genesis export, validation, and import configuration
type GenesisConfig struct {
	// StreamImport defines if the genesis.json is in stream form or not.
	StreamImport bool `mapstructure:"stream-import"`

	// GenesisStreamFile sets the genesis json file from which to stream from
	GenesisStreamFile string `mapstructure:"genesis-stream-file"`
}

// Config defines the server's top level configuration
type Config struct {
	BaseConfig `mapstructure:",squash"`

	// Telemetry defines the application telemetry configuration
	Telemetry   telemetry.Config         `mapstructure:"telemetry"`
	API         APIConfig                `mapstructure:"api"`
	GRPC        GRPCConfig               `mapstructure:"grpc"`
	Rosetta     RosettaConfig            `mapstructure:"rosetta"`
	GRPCWeb     GRPCWebConfig            `mapstructure:"grpc-web"`
	StateSync   StateSyncConfig          `mapstructure:"state-sync"`
	StateCommit config.StateCommitConfig `mapstructure:"state-commit"`
	StateStore  config.StateStoreConfig  `mapstructure:"state-store"`
	Genesis     GenesisConfig            `mapstructure:"genesis"`
}

// SetMinGasPrices sets the validator's minimum gas prices.
func (c *Config) SetMinGasPrices(gasPrices sdk.DecCoins) {
	c.MinGasPrices = gasPrices.String()
}

// GetMinGasPrices returns the validator's minimum gas prices based on the set
// configuration.
func (c *Config) GetMinGasPrices() sdk.DecCoins {
	if c.MinGasPrices == "" {
		return sdk.DecCoins{}
	}

	gasPricesStr := strings.Split(c.MinGasPrices, ";")
	gasPrices := make(sdk.DecCoins, len(gasPricesStr))

	for i, s := range gasPricesStr {
		gasPrice, err := sdk.ParseDecCoin(s)
		if err != nil {
			panic(fmt.Errorf("failed to parse minimum gas price coin (%s): %s", s, err))
		}

		gasPrices[i] = gasPrice
	}

	return gasPrices
}

// DefaultConfig returns server's default configuration.
func DefaultConfig() *Config {
	return &Config{
		BaseConfig: BaseConfig{
			MinGasPrices:       DefaultMinGasPrices,
			InterBlockCache:    true,
			Pruning:            storetypes.PruningOptionNothing,
			PruningKeepRecent:  "0",
			PruningKeepEvery:   "0",
			PruningInterval:    "0",
			MinRetainBlocks:    0,
			IndexEvents:        nil,
			CompactionInterval: 0,
			ConcurrencyWorkers: DefaultConcurrencyWorkers,
			OccEnabled:         DefaultOccEnabled,
		},
		Telemetry: telemetry.Config{
			Enabled:                 true,
			PrometheusRetentionTime: 7200,
			GlobalLabels:            nil,
		},
		API: APIConfig{
			Enable:             false,
			Swagger:            true,
			Address:            "tcp://0.0.0.0:1317",
			MaxOpenConnections: 1000,
			RPCReadTimeout:     10,
			RPCWriteTimeout:    0,
			RPCMaxBodyBytes:    1000000,
		},
		GRPC: GRPCConfig{
			Enable:                       true,
			Address:                      DefaultGRPCAddress,
			MaxRecvMsgSize:               DefaultGRPCMaxRecvMsgSize,
			MaxOpenConnections:           DefaultGRPCMaxOpenConnections,
			MaxConnectionIdle:            DefaultGRPCMaxConnectionIdle,
			MaxConnectionAge:             DefaultGRPCMaxConnectionAge,
			MaxConnectionAgeGrace:        DefaultGRPCMaxConnectionAgeGrace,
			KeepaliveTime:                DefaultGRPCKeepaliveTime,
			KeepaliveTimeout:             DefaultGRPCKeepaliveTimeout,
			KeepaliveMinTime:             DefaultGRPCKeepaliveMinTime,
			KeepalivePermitWithoutStream: DefaultGRPCKeepalivePermitWithoutStream,
		},
		Rosetta: RosettaConfig{
			Enable:     false,
			Address:    ":8080",
			Blockchain: "app",
			Network:    "network",
			Retries:    3,
			Offline:    false,
		},
		GRPCWeb: GRPCWebConfig{
			Enable:             true,
			Address:            DefaultGRPCWebAddress,
			MaxOpenConnections: DefaultGRPCWebMaxOpenConnections,
		},
		StateSync: StateSyncConfig{
			SnapshotInterval:   0,
			SnapshotKeepRecent: 2,
			SnapshotDirectory:  "",
		},
		StateCommit: config.DefaultStateCommitConfig(),
		StateStore:  config.DefaultStateStoreConfig(),
		Genesis: GenesisConfig{
			StreamImport:      false,
			GenesisStreamFile: "",
		},
	}
}

// clampNonNegativeDuration returns fallback when d is negative; zero and
// positive values (zero is gRPC's "infinity" for several keepalive fields) pass
// through unchanged.
func clampNonNegativeDuration(d, fallback time.Duration) time.Duration {
	if d < 0 {
		return fallback
	}
	return d
}

// GetConfig returns a fully parsed Config object.
func GetConfig(v *viper.Viper) (Config, error) {
	globalLabelsRaw, ok := v.Get("telemetry.global-labels").([]interface{})
	if !ok {
		return Config{}, fmt.Errorf("failed to parse global-labels config")
	}

	globalLabels := make([][]string, 0, len(globalLabelsRaw))
	for idx, glr := range globalLabelsRaw {
		labelsRaw, ok := glr.([]interface{})
		if !ok {
			return Config{}, fmt.Errorf("failed to parse global label number %d from config", idx)
		}
		if len(labelsRaw) == 2 {
			globalLabels = append(globalLabels, []string{labelsRaw[0].(string), labelsRaw[1].(string)})
		}
	}

	// Resolve sc-write-mode through ParseWriteMode so that misspellings fail
	// at parse-time with a clear error rather than later from
	// StateCommitConfig.Validate(). An empty value preserves the in-code
	// default, matching the behavior of app/seidb.go.
	scWriteMode := config.DefaultStateCommitConfig().WriteMode
	if wm := v.GetString("state-commit.sc-write-mode"); wm != "" {
		parsed, err := config.ParseSCWriteMode(wm)
		if err != nil {
			return Config{}, fmt.Errorf("invalid state-commit.sc-write-mode %q: %w", wm, err)
		}
		scWriteMode = parsed
	}
	// sc-write-mode-enable-auto (default true) forces the node into auto and
	// ignores the explicit sc-write-mode. An absent key keeps the default so
	// older configs (explicit memiavl_only, no auto key) still resolve to auto,
	// mirroring app/seidb.go. Set it to false to honor the explicit sc-write-mode
	// as a deliberate pin (see config.ApplyWriteModeAuto).
	scWriteModeEnableAuto := config.DefaultStateCommitConfig().WriteModeEnableAuto
	if v.IsSet("state-commit.sc-write-mode-enable-auto") {
		scWriteModeEnableAuto = v.GetBool("state-commit.sc-write-mode-enable-auto")
	}
	scWriteMode = config.ApplyWriteModeAuto(scWriteModeEnableAuto, scWriteMode)

	// FlatKV knobs are not rendered in the default app.toml template. GetConfig
	// is a faithful parse of app.toml/flags: it only reads the explicit
	// state-commit.flatkv.* keys (if an operator adds them by hand) on top of the
	// in-code defaults. The FlatKV-follows-memIAVL mirror (and snapshot cadence
	// normalization) is applied later by composite.alignFlatKVSnapshotWithMemIAVL
	// at store construction, so we deliberately do not mirror the sc-* keys here.
	flatKVConfig := config.DefaultStateCommitConfig().FlatKVConfig
	if v.IsSet("state-commit.flatkv.fsync") {
		flatKVConfig.Fsync = v.GetBool("state-commit.flatkv.fsync")
	}
	if v.IsSet("state-commit.flatkv.async-write-buffer") {
		flatKVConfig.AsyncWriteBuffer = v.GetInt("state-commit.flatkv.async-write-buffer")
	}
	if v.IsSet("state-commit.flatkv.snapshot-interval") {
		flatKVConfig.SnapshotInterval = v.GetUint32("state-commit.flatkv.snapshot-interval")
	}
	if v.IsSet("state-commit.flatkv.snapshot-keep-recent") {
		flatKVConfig.SnapshotKeepRecent = v.GetUint32("state-commit.flatkv.snapshot-keep-recent")
	}
	if v.IsSet("state-commit.flatkv.enable-read-write-metrics") {
		flatKVConfig.EnableReadWriteMetrics = v.GetBool("state-commit.flatkv.enable-read-write-metrics")
	}

	// Guard every memIAVL read with a presence check so that an absent
	// state-commit.sc-* key preserves the in-code default from
	// DefaultStateCommitConfig rather than reading back the zero value
	// (v.Get*(absent) == 0) and clobbering it. This matters for the keys whose
	// default is non-zero (async-commit-buffer 100, snapshot-interval 10000,
	// keep-recent 1, ...): a config that omits a key must not silently downgrade
	// the node (e.g. to synchronous commits or disabled snapshots). Mirrors the
	// guarded parse in app/seidb.go's parseSCConfigs. An explicit key (including
	// an explicit 0) is IsSet == true and is read verbatim.
	memIAVLConfig := config.DefaultStateCommitConfig().MemIAVLConfig
	if v.IsSet("state-commit.sc-async-commit-buffer") {
		memIAVLConfig.AsyncCommitBuffer = v.GetInt("state-commit.sc-async-commit-buffer")
	}
	if v.IsSet("state-commit.sc-keep-recent") {
		memIAVLConfig.SnapshotKeepRecent = v.GetUint32("state-commit.sc-keep-recent")
	}
	if v.IsSet("state-commit.sc-snapshot-interval") {
		memIAVLConfig.SnapshotInterval = v.GetUint32("state-commit.sc-snapshot-interval")
	}
	if v.IsSet("state-commit.sc-snapshot-min-time-interval") {
		memIAVLConfig.SnapshotMinTimeInterval = v.GetUint32("state-commit.sc-snapshot-min-time-interval")
	}
	if v.IsSet("state-commit.sc-snapshot-writer-limit") {
		memIAVLConfig.SnapshotWriterLimit = v.GetInt("state-commit.sc-snapshot-writer-limit")
	}
	if v.IsSet("state-commit.sc-snapshot-prefetch-threshold") {
		memIAVLConfig.SnapshotPrefetchThreshold = v.GetFloat64("state-commit.sc-snapshot-prefetch-threshold")
	}

	// Apply the in-code default when the key is absent so that nodes upgrading
	// with an older app.toml (which lacks this key) are still bounded rather
	// than running with unlimited connections.
	grpcWebMaxOpenConnections := uint(DefaultGRPCWebMaxOpenConnections)
	if v.IsSet("grpc-web.max-open-connections") {
		grpcWebMaxOpenConnections = v.GetUint("grpc-web.max-open-connections")
	}

	// Apply in-code defaults when keys are absent so that nodes upgrading with an
	// older app.toml (which lacks these keys) remain bounded rather than running
	// with unlimited connections / message sizes.
	grpcMaxRecvMsgSize := DefaultGRPCMaxRecvMsgSize
	if v.IsSet("grpc.max-recv-msg-size") {
		grpcMaxRecvMsgSize = v.GetInt("grpc.max-recv-msg-size")
	}
	grpcMaxOpenConnections := uint(DefaultGRPCMaxOpenConnections)
	if v.IsSet("grpc.max-open-connections") {
		grpcMaxOpenConnections = v.GetUint("grpc.max-open-connections")
	}
	// Clamp negative durations back to their in-code defaults. A negative
	// keepalive/connection-age value is a misconfiguration that gRPC would
	// otherwise accept verbatim, so fall back to the safe default instead.
	grpcMaxConnectionIdle := DefaultGRPCMaxConnectionIdle
	if v.IsSet("grpc.max-connection-idle") {
		grpcMaxConnectionIdle = clampNonNegativeDuration(v.GetDuration("grpc.max-connection-idle"), DefaultGRPCMaxConnectionIdle)
	}
	grpcKeepaliveTime := DefaultGRPCKeepaliveTime
	if v.IsSet("grpc.keepalive-time") {
		grpcKeepaliveTime = clampNonNegativeDuration(v.GetDuration("grpc.keepalive-time"), DefaultGRPCKeepaliveTime)
	}
	grpcKeepaliveTimeout := DefaultGRPCKeepaliveTimeout
	if v.IsSet("grpc.keepalive-timeout") {
		grpcKeepaliveTimeout = clampNonNegativeDuration(v.GetDuration("grpc.keepalive-timeout"), DefaultGRPCKeepaliveTimeout)
	}
	grpcKeepaliveMinTime := DefaultGRPCKeepaliveMinTime
	if v.IsSet("grpc.keepalive-min-time") {
		grpcKeepaliveMinTime = clampNonNegativeDuration(v.GetDuration("grpc.keepalive-min-time"), DefaultGRPCKeepaliveMinTime)
	}
	// MaxConnectionAge and MaxConnectionAgeGrace default to 0 (gRPC's "infinity"),
	// which is a valid value, so only a negative override needs clamping.
	grpcMaxConnectionAge := clampNonNegativeDuration(v.GetDuration("grpc.max-connection-age"), DefaultGRPCMaxConnectionAge)
	grpcMaxConnectionAgeGrace := clampNonNegativeDuration(v.GetDuration("grpc.max-connection-age-grace"), DefaultGRPCMaxConnectionAgeGrace)

	return Config{
		BaseConfig: BaseConfig{
			MinGasPrices:       v.GetString("minimum-gas-prices"),
			InterBlockCache:    v.GetBool("inter-block-cache"),
			Pruning:            v.GetString("pruning"),
			PruningKeepRecent:  v.GetString("pruning-keep-recent"),
			PruningInterval:    v.GetString("pruning-interval"),
			HaltHeight:         v.GetUint64("halt-height"),
			HaltTime:           v.GetUint64("halt-time"),
			IndexEvents:        v.GetStringSlice("index-events"),
			MinRetainBlocks:    v.GetUint64("min-retain-blocks"),
			CompactionInterval: v.GetUint64("compaction-interval"),
			ConcurrencyWorkers: v.GetInt("concurrency-workers"),
			OccEnabled:         v.GetBool("occ-enabled"),
		},
		Telemetry: telemetry.Config{
			ServiceName:             v.GetString("telemetry.service-name"),
			Enabled:                 v.GetBool("telemetry.enabled"),
			EnableHostname:          v.GetBool("telemetry.enable-hostname"),
			EnableHostnameLabel:     v.GetBool("telemetry.enable-hostname-label"),
			EnableServiceLabel:      v.GetBool("telemetry.enable-service-label"),
			PrometheusRetentionTime: v.GetInt64("telemetry.prometheus-retention-time"),
			GlobalLabels:            globalLabels,
		},
		API: APIConfig{
			Enable:             v.GetBool("api.enable"),
			Swagger:            v.GetBool("api.swagger"),
			Address:            v.GetString("api.address"),
			MaxOpenConnections: v.GetUint("api.max-open-connections"),
			RPCReadTimeout:     v.GetUint("api.rpc-read-timeout"),
			RPCWriteTimeout:    v.GetUint("api.rpc-write-timeout"),
			RPCMaxBodyBytes:    v.GetUint("api.rpc-max-body-bytes"),
			EnableUnsafeCORS:   v.GetBool("api.enabled-unsafe-cors"),
		},
		Rosetta: RosettaConfig{
			Enable:     v.GetBool("rosetta.enable"),
			Address:    v.GetString("rosetta.address"),
			Blockchain: v.GetString("rosetta.blockchain"),
			Network:    v.GetString("rosetta.network"),
			Retries:    v.GetInt("rosetta.retries"),
			Offline:    v.GetBool("rosetta.offline"),
		},
		GRPC: GRPCConfig{
			Enable:                       v.GetBool("grpc.enable"),
			Address:                      v.GetString("grpc.address"),
			MaxRecvMsgSize:               grpcMaxRecvMsgSize,
			MaxOpenConnections:           grpcMaxOpenConnections,
			MaxConnectionIdle:            grpcMaxConnectionIdle,
			MaxConnectionAge:             grpcMaxConnectionAge,
			MaxConnectionAgeGrace:        grpcMaxConnectionAgeGrace,
			KeepaliveTime:                grpcKeepaliveTime,
			KeepaliveTimeout:             grpcKeepaliveTimeout,
			KeepaliveMinTime:             grpcKeepaliveMinTime,
			KeepalivePermitWithoutStream: v.GetBool("grpc.keepalive-permit-without-stream"),
		},
		GRPCWeb: GRPCWebConfig{
			Enable:             v.GetBool("grpc-web.enable"),
			Address:            v.GetString("grpc-web.address"),
			EnableUnsafeCORS:   v.GetBool("grpc-web.enable-unsafe-cors"),
			MaxOpenConnections: grpcWebMaxOpenConnections,
		},
		StateSync: StateSyncConfig{
			SnapshotInterval:   v.GetUint64("state-sync.snapshot-interval"),
			SnapshotKeepRecent: v.GetUint32("state-sync.snapshot-keep-recent"),
			SnapshotDirectory:  v.GetString("state-sync.snapshot-directory"),
		},
		StateCommit: config.StateCommitConfig{
			Enable:              v.GetBool("state-commit.sc-enable"),
			Directory:           v.GetString("state-commit.sc-directory"),
			WriteMode:           scWriteMode,
			WriteModeEnableAuto: scWriteModeEnableAuto,
			MemIAVLConfig:       memIAVLConfig,
			FlatKVConfig:        flatKVConfig,
		},
		StateStore: config.StateStoreConfig{
			Enable:               v.GetBool("state-store.ss-enable"),
			DBDirectory:          v.GetString("state-store.ss-db-directory"),
			Backend:              v.GetString("state-store.ss-backend"),
			AsyncWriteBuffer:     v.GetInt("state-store.ss-async-write-buffer"),
			KeepRecent:           v.GetInt("state-store.ss-keep-recent"),
			PruneIntervalSeconds: v.GetInt("state-store.ss-prune-interval"),
			ImportNumWorkers:     v.GetInt("state-store.ss-import-num-workers"),
			EnableReadWriteMetrics: v.GetBool(
				"state-store.ss-enable-read-write-metrics",
			),
			EVMSplit:          v.GetBool("state-store.evm-ss-split"),
			EVMDBDirectory:    v.GetString("state-store.evm-ss-db-directory"),
			SeparateEVMSubDBs: v.GetBool("state-store.evm-ss-separate-dbs"),
		},
		Genesis: GenesisConfig{
			StreamImport:      v.GetBool("genesis.stream-import"),
			GenesisStreamFile: v.GetString("genesis.genesis-stream-file"),
		},
	}, nil
}

// ValidateBasic returns an error if min-gas-prices field is empty in BaseConfig. Otherwise, it returns nil.
func (c Config) ValidateBasic(tendermintConfig *tmcfg.Config) error {
	if c.MinGasPrices == "" {
		return sdkerrors.ErrAppConfig.Wrap("set min gas price in app.toml or flag or env variable")
	}
	if c.Pruning == storetypes.PruningOptionEverything && c.StateSync.SnapshotInterval > 0 {
		return sdkerrors.ErrAppConfig.Wrapf(
			"cannot enable state sync snapshots with '%s' pruning setting", storetypes.PruningOptionEverything,
		)
	}

	return nil
}
