package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"text/template"

	"github.com/spf13/viper"
)

// ManualConfigTemplate contains configuration sections that are frequently modified by users
const ManualConfigTemplate = `# This is a TOML config file.
# For more information, see https://github.com/toml-lang/toml

###############################################################################
###                           Base Configuration                            ###
###############################################################################

# The minimum gas prices a validator is willing to accept for processing a
# transaction. A transaction's fees must meet the minimum of any denomination
# specified in this config (e.g. 0.25token1;0.0001token2).
minimum-gas-prices = "{{ .BaseConfig.MinGasPrices }}"

# MinRetainBlocks defines the minimum block height offset from the current block
# for pruning Tendermint blocks. Set to 0 to disable pruning. This only affects
# Tendermint block pruning, not application state (see "pruning-*" configs).
min-retain-blocks = {{ .BaseConfig.MinRetainBlocks }}

# ConcurrencyWorkers defines how many workers to run for concurrent transaction execution.
# Default is dynamically set to 2x CPU cores, capped at 128, with a minimum of 10.
concurrency-workers = {{ .BaseConfig.ConcurrencyWorkers }}

# occ-enabled defines whether OCC is enabled or not for transaction execution
occ-enabled = {{ .BaseConfig.OccEnabled }}

# HaltHeight contains a non-zero block height at which a node will gracefully
# halt and shutdown that can be used to assist upgrades and testing.
#
# Note: Commitment of state will be attempted on the corresponding block.
halt-height = {{ .BaseConfig.HaltHeight }}

# FreezeHeight contains the first block height a full node must not execute.
# Query RPC remains available, while transaction and evidence submission,
# mempool gossip, and state sync are disabled from startup.
freeze-height = {{ .BaseConfig.FreezeHeight }}

# HaltTime contains a non-zero minimum block time (in Unix seconds) at which
# a node will gracefully halt and shutdown that can be used to assist upgrades
# and testing.
#
# Note: Commitment of state will be attempted on the corresponding block.
halt-time = {{ .BaseConfig.HaltTime }}

# InterBlockCache enables inter-block caching.
inter-block-cache = {{ .BaseConfig.InterBlockCache }}

# IndexEvents defines the set of events in the form {eventType}.{attributeKey},
# which informs Tendermint what to index. If empty, all events will be indexed.
#
# Example:
# ["message.sender", "message.recipient"]
index-events = {{ .BaseConfig.IndexEvents }}

# CompactionInterval sets (in seconds) the interval between forced levelDB
# compaction. A value of 0 means no forced levelDB.
# Default is 0.
compaction-interval = {{ .BaseConfig.CompactionInterval }}

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

# snapshot-directory sets the directory for where state sync snapshots are persisted.
# default is empty which will then store under the app home directory same as before.
snapshot-directory = "{{ .StateSync.SnapshotDirectory }}"

###############################################################################
###                         Query Configuration                             ###
###############################################################################

# Pagination limits on the ABCI query path protect the node from expensive store
# scans by untrusted callers (for example, public RPC clients). Operators tune these
# values for their deployment, allowlist trusted indexers on Tendermint RPC, or set
# disable-limits on nodes not exposed to untrusted callers.
[query]

# DisableLimits turns off pagination limits for all query origins. Use only on nodes
# that are not exposed to untrusted callers.
disable-limits = {{ .Query.DisableLimits }}

# TrustedCIDRs lists caller CIDRs that bypass pagination limits. Matching uses the
# direct TCP peer (Tendermint RPC RemoteAddr), not forwarded headers. It applies only
# to abci_query on the Tendermint RPC listener; REST/LCD on api.address (including
# grpc-gateway routes) relays queries in-process without caller origin, so
# trusted-cidrs has no effect there. Trusted indexers using REST should use
# Tendermint RPC, or set disable-limits on nodes not exposed to untrusted callers.
# Behind a load balancer or reverse proxy every external client shares the proxy
# address, so adding the proxy CIDR here bypasses limits for all traffic through
# that ingress, not just internal indexers.
trusted-cidrs = [{{ range $i, $cidr := .Query.TrustedCIDRs }}{{if $i}}, {{end}}"{{ $cidr }}"{{ end }}]

# MaxLimit is the maximum page size for untrusted query origins. 0 uses the default.
max-limit = {{ .Query.MaxLimit }}

# MaxOffset is the maximum offset for untrusted query origins. 0 uses the default.
max-offset = {{ .Query.MaxOffset }}

# MaxIterations is the maximum store entries a single untrusted query may scan. 0 uses
# the default. When the budget is exhausted after the offset is reached, the response is
# a partial page with next_key; total stays 0 even when the client set count_total, so
# next_key != nil with total == 0 means the total is unknown rather than zero results.
# When the budget is exhausted before the offset is reached, the query fails with
# InvalidArgument. Filtered queries (for example gov proposals with a status filter)
# can hit this on defaults, because entries the filter rejects still count against
# the budget but not toward the offset.
max-iterations = {{ .Query.MaxIterations }}
`

// AutoManagedConfigTemplate contains configuration sections that are auto-managed
const AutoManagedConfigTemplate = `
###############################################################################
###                   Telemetry Configuration (Auto-managed)                ###
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
{{if .Telemetry.GlobalLabels}}global-labels = [{{ range $k, $v := .Telemetry.GlobalLabels }}
  ["{{index $v 0 }}", "{{ index $v 1}}"],{{ end }}]{{else}}global-labels = []{{end}}

###############################################################################
###                       API Configuration (Auto-managed)                  ###
###############################################################################

[api]

# Enable defines if the API server should be enabled.
enable = {{ .API.Enable }}

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
###                         Genesis Configuration (Auto-managed)            ###
###############################################################################

# Genesis config allows configuring whether to stream from an genesis json file in streamed form
[genesis]

# stream-import specifies whether to the stream the import from the genesis json file. The genesis
# file must be in stream form and exported in a streaming fashion.
stream-import = {{ .Genesis.StreamImport }}

# genesis-stream-file specifies the path of the genesis json file to stream from.
genesis-stream-file = "{{ .Genesis.GenesisStreamFile }}"

`

// DefaultConfigTemplate combines manual and auto-managed templates for backward compatibility
const DefaultConfigTemplate = ManualConfigTemplate + AutoManagedConfigTemplate

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

	if err := os.WriteFile(filepath.Clean(configFilePath), buffer.Bytes(), 0600); err != nil {
		fmt.Printf("MustWriteFile failed: %v\n", err)
		os.Exit(1)
	}
}
