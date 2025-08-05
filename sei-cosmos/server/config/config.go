package config

import (
	"fmt"
	"strings"

	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/sei-protocol/sei-db/config"
	"github.com/spf13/viper"
	tmcfg "github.com/tendermint/tendermint/config"
)

const (
	defaultMinGasPrices = ""

	// DefaultGRPCAddress defines the default address to bind the gRPC server to.
	DefaultGRPCAddress = "0.0.0.0:9090"

	// DefaultGRPCWebAddress defines the default address to bind the gRPC-web server to.
	DefaultGRPCWebAddress = "0.0.0.0:9091"

	// DefaultConcurrencyWorkers defines the default workers to use for concurrent transactions
	DefaultConcurrencyWorkers = 20

	// DefaultOccEanbled defines whether to use OCC for tx processing
	DefaultOccEnabled = false
)

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

	// IavlCacheSize set the size of the iavl tree cache.
	IAVLCacheSize uint64 `mapstructure:"iavl-cache-size"`

	// IAVLDisableFastNode enables or disables the fast sync node.
	IAVLDisableFastNode bool `mapstructure:"iavl-disable-fastnode"`

	// CompactionInterval sets (in seconds) the interval between forced levelDB
	// compaction. A value of 0 means no forced levelDB
	CompactionInterval uint64 `mapstructure:"compaction-interval"`

	// deprecated
	NoVersioning bool `mapstructure:"no-versioning"`

	SeparateOrphanStorage        bool   `mapstructure:"separate-orphan-storage"`
	SeparateOrphanVersionsToKeep int64  `mapstructure:"separate-orphan-versions-to-keep"`
	NumOrphanPerFile             int    `mapstructure:"num-orphan-per-file"`
	OrphanDirectory              string `mapstructure:"orphan-dir"`

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
}

// GRPCWebConfig defines configuration for the gRPC-web server.
type GRPCWebConfig struct {
	// Enable defines if the gRPC-web should be enabled.
	Enable bool `mapstructure:"enable"`

	// Address defines the gRPC-web server to listen on
	Address string `mapstructure:"address"`

	// EnableUnsafeCORS defines if CORS should be enabled (unsafe - use it at your own risk)
	EnableUnsafeCORS bool `mapstructure:"enable-unsafe-cors"`
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
	// Default is emtpy which will then store under the app home directory.
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
	Genesis     GenesisConfig            `mapstructure:genesis`
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
			MinGasPrices:        defaultMinGasPrices,
			InterBlockCache:     true,
			Pruning:             storetypes.PruningOptionDefault,
			PruningKeepRecent:   "0",
			PruningKeepEvery:    "0",
			PruningInterval:     "0",
			MinRetainBlocks:     0,
			IndexEvents:         make([]string, 0),
			IAVLCacheSize:       781250, // 50 MB
			IAVLDisableFastNode: true,
			CompactionInterval:  0,
			NoVersioning:        false,
			ConcurrencyWorkers:  DefaultConcurrencyWorkers,
			OccEnabled:          DefaultOccEnabled,
		},
		Telemetry: telemetry.Config{
			Enabled:      false,
			GlobalLabels: [][]string{},
		},
		API: APIConfig{
			Enable:             false,
			Swagger:            true,
			Address:            "tcp://0.0.0.0:1317",
			MaxOpenConnections: 1000,
			RPCReadTimeout:     10,
			RPCMaxBodyBytes:    1000000,
		},
		GRPC: GRPCConfig{
			Enable:  true,
			Address: DefaultGRPCAddress,
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
			Enable:  true,
			Address: DefaultGRPCWebAddress,
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

	return Config{
		BaseConfig: BaseConfig{
			MinGasPrices:                 v.GetString("minimum-gas-prices"),
			InterBlockCache:              v.GetBool("inter-block-cache"),
			Pruning:                      v.GetString("pruning"),
			PruningKeepRecent:            v.GetString("pruning-keep-recent"),
			PruningInterval:              v.GetString("pruning-interval"),
			HaltHeight:                   v.GetUint64("halt-height"),
			HaltTime:                     v.GetUint64("halt-time"),
			IndexEvents:                  v.GetStringSlice("index-events"),
			MinRetainBlocks:              v.GetUint64("min-retain-blocks"),
			IAVLCacheSize:                v.GetUint64("iavl-cache-size"),
			IAVLDisableFastNode:          v.GetBool("iavl-disable-fastnode"),
			CompactionInterval:           v.GetUint64("compaction-interval"),
			NoVersioning:                 v.GetBool("no-versioning"),
			SeparateOrphanStorage:        v.GetBool("separate-orphan-storage"),
			SeparateOrphanVersionsToKeep: v.GetInt64("separate-orphan-versions-to-keep"),
			NumOrphanPerFile:             v.GetInt("num-orphan-per-file"),
			OrphanDirectory:              v.GetString("orphan-dir"),
			ConcurrencyWorkers:           v.GetInt("concurrency-workers"),
			OccEnabled:                   v.GetBool("occ-enabled"),
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
			Enable:  v.GetBool("grpc.enable"),
			Address: v.GetString("grpc.address"),
		},
		GRPCWeb: GRPCWebConfig{
			Enable:           v.GetBool("grpc-web.enable"),
			Address:          v.GetString("grpc-web.address"),
			EnableUnsafeCORS: v.GetBool("grpc-web.enable-unsafe-cors"),
		},
		StateSync: StateSyncConfig{
			SnapshotInterval:   v.GetUint64("state-sync.snapshot-interval"),
			SnapshotKeepRecent: v.GetUint32("state-sync.snapshot-keep-recent"),
			SnapshotDirectory:  v.GetString("state-sync.snapshot-directory"),
		},
		StateCommit: config.StateCommitConfig{
			Enable:                           v.GetBool("state-commit.enable"),
			Directory:                        v.GetString("state-commit.directory"),
			ZeroCopy:                         v.GetBool("state-commit.zero-copy"),
			AsyncCommitBuffer:                v.GetInt("state-commit.async-commit-buffer"),
			SnapshotKeepRecent:               v.GetUint32("state-commit.snapshot-keep-recent"),
			SnapshotInterval:                 v.GetUint32("state-commit.snapshot-interval"),
			SnapshotWriterLimit:              v.GetInt("state-commit.snapshot-writer-limit"),
			CacheSize:                        v.GetInt("state-commit.cache-size"),
			OnlyAllowExportOnSnapshotVersion: v.GetBool("state-commit.only-allow-export-on-snapshot-version"),
		},
		StateStore: config.StateStoreConfig{
			Enable:               v.GetBool("state-store.enable"),
			DBDirectory:          v.GetString("state-store.db-directory"),
			Backend:              v.GetString("state-store.backend"),
			AsyncWriteBuffer:     v.GetInt("state-store.async-write-buffer"),
			KeepRecent:           v.GetInt("state-store.keep-recent"),
			PruneIntervalSeconds: v.GetInt("state-store.prune-interval-seconds"),
			ImportNumWorkers:     v.GetInt("state-store.import-num-workers"),
		},
		Genesis: GenesisConfig{
			StreamImport:      v.GetBool("genesis.stream-import"),
			GenesisStreamFile: v.GetString("genesis.genesis-stream-file"),
		},
	}, nil
}

// ValidateBasic returns an error if min-gas-prices field is empty in BaseConfig. Otherwise, it returns nil.
func (c Config) ValidateBasic(tendermintConfig *tmcfg.Config) error {
	if c.BaseConfig.MinGasPrices == "" {
		return sdkerrors.ErrAppConfig.Wrap("set min gas price in app.toml or flag or env variable")
	}
	if c.Pruning == storetypes.PruningOptionEverything && c.StateSync.SnapshotInterval > 0 {
		return sdkerrors.ErrAppConfig.Wrapf(
			"cannot enable state sync snapshots with '%s' pruning setting", storetypes.PruningOptionEverything,
		)
	}

	return nil
}
