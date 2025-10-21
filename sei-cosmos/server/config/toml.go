package config

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"text/template"

	"github.com/sei-protocol/sei-db/config"
	"github.com/spf13/viper"
)

const DefaultConfigTemplate = `# This is a TOML config file.
# For more information, see https://github.com/toml-lang/toml

###############################################################################
###                   User-Configurable Settings                            ###
###############################################################################

# The minimum gas prices a validator is willing to accept for processing a
# transaction. A transaction's fees must meet the minimum of any denomination
# specified in this config (e.g. 0.25token1;0.0001token2).
minimum-gas-prices = "{{ .BaseConfig.MinGasPrices }}"

# Pruning Strategies:
# - default: Keep recent 362880 blocks, prune every 10 blocks
# - nothing: Keep all historic states (archiving node)
# - everything: Keep only recent 2 blocks, prune at every block
# - custom: Manually specify via 'pruning-keep-recent' and 'pruning-interval'
# NOTE: Pruning strategy is completely ignored when SeiDB is enabled (default)
pruning = "{{ .BaseConfig.Pruning }}"

# Applied only if pruning strategy is 'custom' and SeiDB is disabled
pruning-keep-recent = "{{ .BaseConfig.PruningKeepRecent }}"
pruning-keep-every = "{{ .BaseConfig.PruningKeepEvery }}"
pruning-interval = "{{ .BaseConfig.PruningInterval }}"

# MinRetainBlocks defines the minimum block height offset from the current
# block being committed, such that all blocks past this offset are pruned
# from Tendermint. It is used as part of the process of determining the
# ResponseCommit.RetainHeight value during ABCI Commit. A value of 0 indicates
# that no blocks should be pruned.
#
# This configuration value is only responsible for pruning Tendermint blocks.
# It has no bearing on application state pruning which is determined by the
# "pruning-*" configurations.
#
# Note: Tendermint block pruning is dependant on this parameter in conunction
# with the unbonding (safety threshold) period, state pruning and state sync
# snapshot parameters to determine the correct minimum value of
# ResponseCommit.RetainHeight.
min-retain-blocks = {{ .BaseConfig.MinRetainBlocks }}

###############################################################################
###                           gRPC Configuration                            ###
###############################################################################

[grpc]

# Enable defines if the gRPC server should be enabled.
# Recommended: False for validators, True for full nodes
enable = {{ .GRPC.Enable }}

# Address defines the gRPC server address to bind to.
address = "{{ .GRPC.Address }}"

###############################################################################
###                        State Sync Configuration                         ###
###############################################################################

# State sync snapshots allow other nodes to rapidly join the network without replaying historical
# blocks, instead downloading and applying a snapshot of the application state at a given height.
[state-sync]

# snapshot-interval specifies the block interval at which local state sync snapshots are
# taken (0 to disable). Must be a multiple of pruning-keep-every.
snapshot-interval = {{ .StateSync.SnapshotInterval }}

# snapshot-keep-recent specifies the number of recent snapshots to keep and serve (0 to keep all).
snapshot-keep-recent = {{ .StateSync.SnapshotKeepRecent }}

###############################################################################
###                             SeiDB Configuration                         ###
###############################################################################

[state-commit]
# Enable defines if the state-commit (memiavl) should be enabled to override existing IAVL db backend.
sc-enable = {{ .StateCommit.Enable }}

# SnapshotInterval defines the block interval at which SeiDB takes state-commit snapshots.
# When a node restarts, it needs to "replay changelog entries" from the last snapshot to the current height.
# Default: 10000 blocks (recommended for most nodes)
sc-snapshot-interval = {{ .StateCommit.SnapshotInterval }}

[state-store]

# Enable defines whether the state-store should be enabled for storing historical data.
# Supporting historical queries or exporting state snapshot requires setting this to true
# This config only take effect when SeiDB is enabled (sc-enable = true)
ss-enable = {{ .StateStore.Enable }}

# DBBackend defines the backend database used for state-store.
# Supported backends: pebbledb, rocksdb
# defaults to pebbledb (recommended)
ss-backend = "{{ .StateStore.Backend }}"

# KeepRecent defines the number of versions to keep in state store
# Setting it to 0 means keep everything
# Default to keep the last 100,000 blocks
ss-keep-recent = {{ .StateStore.KeepRecent }}

###############################################################################
###                  Default Configuration (Auto-managed)                   ###
###############################################################################
# The following sections use default values and typically do NOT need to be
# modified by node operators. These are auto-managed or use sensible defaults.

###############################################################################
###                         Base Defaults (Auto-managed)                    ###
###############################################################################

# InterBlockCache enables inter-block caching.
inter-block-cache = {{ .BaseConfig.InterBlockCache }}

# IndexEvents defines the set of events in the form {eventType}.{attributeKey},
# which informs Tendermint what to index. If empty, all events will be indexed.
#
# Example:
# ["message.sender", "message.recipient"]
index-events = {{ .BaseConfig.IndexEvents }}

# IavlCacheSize set the size of the iavl tree cache.
# Default cache size is 50mb.
iavl-cache-size = {{ .BaseConfig.IAVLCacheSize }}

# IAVLDisableFastNode enables or disables the fast node feature of IAVL.
# Default is true.
iavl-disable-fastnode = {{ .BaseConfig.IAVLDisableFastNode }}

# CompactionInterval sets (in seconds) the interval between forced levelDB
# compaction. A value of 0 means no forced levelDB.
# Default is 0.
compaction-interval = {{ .BaseConfig.CompactionInterval }}

###############################################################################
###                    Telemetry Configuration (Auto-managed)                ###
###############################################################################

[telemetry]

# Prefixed with keys to separate services.
service-name = "{{ .Telemetry.ServiceName }}"

# Enabled enables the application telemetry functionality. When enabled,
# an in-memory sink is also enabled by default. Operators may also enabled
# other sinks such as Prometheus.
enabled = {{ .Telemetry.Enabled }}

# Enable prefixing gauge values with hostname.
enable-hostname = {{ .Telemetry.EnableHostname }}

# Enable adding hostname to labels.
enable-hostname-label = {{ .Telemetry.EnableHostnameLabel }}

# Enable adding service to labels.
enable-service-label = {{ .Telemetry.EnableServiceLabel }}

# PrometheusRetentionTime, when positive, enables a Prometheus metrics sink.
prometheus-retention-time = {{ .Telemetry.PrometheusRetentionTime }}

# When both 'api.enable' and 'telemetry.enabled' are true, this node will expose
# application metrics (custom Cosmos SDK metrics) on the API server endpoint along with the
# Tendermint metrics (port 26660) which are always enabled.

# GlobalLabels defines a global set of name/value label tuples applied to all
# metrics emitted using the wrapper functions defined in telemetry package.
#
# Example:
# [["chain_id", "cosmoshub-1"]]
global-labels = [{{ range $k, $v := .Telemetry.GlobalLabels }}
  ["{{index $v 0 }}", "{{ index $v 1}}"],{{ end }}
]

###############################################################################
###                      API Configuration (Auto-managed)                    ###
###############################################################################

[api]

# Enable defines if the API server should be enabled.
enable = {{ .API.Enable }}

# Swagger defines if swagger documentation should automatically be registered.
swagger = {{ .API.Swagger }}

# Address defines the API server to listen on.
address = "{{ .API.Address }}"

# MaxOpenConnections defines the number of maximum open connections.
max-open-connections = {{ .API.MaxOpenConnections }}

# RPCReadTimeout defines the Tendermint RPC read timeout (in seconds).
rpc-read-timeout = {{ .API.RPCReadTimeout }}

# RPCWriteTimeout defines the Tendermint RPC write timeout (in seconds).
rpc-write-timeout = {{ .API.RPCWriteTimeout }}

# RPCMaxBodyBytes defines the Tendermint maximum response body (in bytes).
rpc-max-body-bytes = {{ .API.RPCMaxBodyBytes }}

# EnableUnsafeCORS defines if CORS should be enabled (unsafe - use it at your own risk).
enabled-unsafe-cors = {{ .API.EnableUnsafeCORS }}

###############################################################################
###                    Rosetta Configuration (Auto-managed)                  ###
###############################################################################

[rosetta]

# Enable defines if the Rosetta API server should be enabled.
enable = {{ .Rosetta.Enable }}

# Address defines the Rosetta API server to listen on.
address = "{{ .Rosetta.Address }}"

# Network defines the name of the blockchain that will be returned by Rosetta.
blockchain = "{{ .Rosetta.Blockchain }}"

# Network defines the name of the network that will be returned by Rosetta.
network = "{{ .Rosetta.Network }}"

# Retries defines the number of retries when connecting to the node before failing.
retries = {{ .Rosetta.Retries }}

# Offline defines if Rosetta server should run in offline mode.
offline = {{ .Rosetta.Offline }}

###############################################################################
###                   gRPC Web Configuration (Auto-managed)                  ###
###############################################################################

[grpc-web]

# GRPCWebEnable defines if the gRPC-web should be enabled.
# NOTE: gRPC must also be enabled, otherwise, this configuration is a no-op.
enable = {{ .GRPCWeb.Enable }}

# Address defines the gRPC-web server address to bind to.
address = "{{ .GRPCWeb.Address }}"

# EnableUnsafeCORS defines if CORS should be enabled (unsafe - use it at your own risk).
enable-unsafe-cors = {{ .GRPCWeb.EnableUnsafeCORS }}

# snapshot-directory sets the directory for where state sync snapshots are persisted.
# default is emtpy which will then store under the app home directory same as before.
snapshot-directory = "{{ .StateSync.SnapshotDirectory }}"

###############################################################################
###                        Genesis Configuration (Auto-managed)              ###
###############################################################################

[genesis]

# stream-import specifies whether to the stream the import from the genesis json file. The genesis
# file must be in stream form and exported in a streaming fashion.
stream-import = {{ .Genesis.StreamImport }}

# genesis-stream-file specifies the path of the genesis json file to stream from.
genesis-stream-file = "{{ .Genesis.GenesisStreamFile }}"

#######################################################
###         Halt & Shutdown (Auto-managed)           ###
#######################################################
# AUTO-MANAGED: These fields may be automatically set by upgrade handlers.
# Most node operators should NOT manually configure these settings.

# HaltHeight contains a non-zero block height at which a node will gracefully
# halt and shutdown that can be used to assist upgrades and testing.
#
# Note: Commitment of state will be attempted on the corresponding block.
halt-height = {{ .BaseConfig.HaltHeight }}

# HaltTime contains a non-zero minimum block time (in Unix seconds) at which
# a node will gracefully halt and shutdown that can be used to assist upgrades
# and testing.
#
# Note: Commitment of state will be attempted on the corresponding block.
halt-time = {{ .BaseConfig.HaltTime }}

#######################################################
###         Legacy IAVL Settings (Auto-managed)      ###
#######################################################
# AUTO-MANAGED: These settings are deprecated and retained for backward compatibility.
# Most node operators should NOT manually configure these settings.

# deprecated
no-versioning = {{ .BaseConfig.NoVersioning }}

# Whether to store orphan data (to-be-deleted data pointers) outside the main
# application LevelDB
separate-orphan-storage = {{ .BaseConfig.SeparateOrphanStorage }}

# if separate-orphan-storage is true, how many versions of orphan data to keep
separate-orphan-versions-to-keep = {{ .BaseConfig.SeparateOrphanVersionsToKeep }}

# if separate-orphan-storage is true, how many orphans to store in each file
num-orphan-per-file = {{ .BaseConfig.NumOrphanPerFile }}

# if separate-orphan-storage is true, where to store orphan data
orphan-dir = "{{ .BaseConfig.OrphanDirectory }}"

#######################################################
###         Concurrency & OCC (Auto-managed)         ###
#######################################################
# AUTO-MANAGED: These fields use dynamically calculated defaults.
# Most node operators should NOT manually configure these settings.

# concurrency-workers defines how many workers to run for concurrent transaction execution
# Default is dynamically set to 2x CPU cores, capped at 128, with a minimum of 10
concurrency-workers = {{ .BaseConfig.ConcurrencyWorkers }}

# occ-enabled defines whether OCC is enabled or not for transaction execution
occ-enabled = {{ .BaseConfig.OccEnabled }}
` + config.DefaultConfigTemplate

var configTemplate *template.Template

func init() {
	var err error

	tmpl := template.New("appConfigFileTemplate")

	if configTemplate, err = tmpl.Parse(DefaultConfigTemplate); err != nil {
		panic(err)
	}
}

// ParseConfig retrieves the default environment configuration for the
// application.
func ParseConfig(v *viper.Viper) (*Config, error) {
	conf := DefaultConfig()
	err := v.Unmarshal(conf)

	return conf, err
}

// SetConfigTemplate sets the custom app config template for
// the application
func SetConfigTemplate(customTemplate string) {
	var err error

	tmpl := template.New("appConfigFileTemplate")

	if configTemplate, err = tmpl.Parse(customTemplate); err != nil {
		panic(err)
	}
}

// WriteConfigFile renders config using the template and writes it to
// configFilePath.
func WriteConfigFile(configFilePath string, config interface{}) {
	var buffer bytes.Buffer

	if err := configTemplate.Execute(&buffer, config); err != nil {
		panic(err)
	}

	if err := ioutil.WriteFile(configFilePath, buffer.Bytes(), 0644); err != nil {
		fmt.Printf("MustWriteFile failed: %v\n", err)
		os.Exit(1)
	}
}
