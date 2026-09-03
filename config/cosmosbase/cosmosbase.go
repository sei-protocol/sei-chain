// Package cosmosbase registers the Cosmos server's configuration sections. They register here rather than
// beside their structs because their per-mode defaults come from app/params, which imports those structs.
package cosmosbase

import (
	"github.com/sei-protocol/sei-chain/app/params"
	"github.com/sei-protocol/sei-chain/config/registry"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
)

// The names these sections have in the configuration key space. BaseSectionName labels the root keys and
// is not part of any key.
const (
	BaseSectionName      = "base"
	APISectionName       = "api"
	GRPCSectionName      = "grpc"
	TelemetrySectionName = "telemetry"
	StateSyncSectionName = "state-sync"
	GRPCWebSectionName   = "grpc-web"
)

// globalLabelsKey is the metric label set, the one key here no environment variable can supply.
const globalLabelsKey = TelemetrySectionName + ".global-labels"

// Five sections register the upstream struct directly; telemetry registers telemetrySchema.
func init() {
	registry.RegisterRootKeys(BaseSectionName, &srvconfig.BaseConfig{}, baseDefaults)
	registry.RegisterSection(APISectionName, &srvconfig.APIConfig{}, apiDefaults)
	registry.RegisterSection(GRPCSectionName, &srvconfig.GRPCConfig{}, grpcDefaults)
	registry.RegisterSection(GRPCWebSectionName, &srvconfig.GRPCWebConfig{}, grpcWebDefaults)
	registry.RegisterSection(TelemetrySectionName, &telemetrySchema{}, telemetryDefaults)
	registry.RegisterSection(StateSyncSectionName, &srvconfig.StateSyncConfig{}, stateSyncDefaults)
}

// forMode is the server configuration seid init writes for a node of this kind: the upstream defaults
// with the mode rules applied. It is not what seid start writes when no file exists, nor what a node with
// an empty file resolves; agreement_test.go measures the latter difference.
func forMode(mode registry.Mode) *srvconfig.Config {
	out := srvconfig.DefaultConfig()
	params.SetAppConfigByMode(out, params.NodeMode(mode))
	return out
}

// baseDefaults is the root-level settings' default for a mode. Only block retention varies by mode.
func baseDefaults(mode registry.Mode) any { return forMode(mode).BaseConfig }

// apiDefaults is the REST interface settings' default for a mode: on for full and archive nodes, off
// otherwise.
func apiDefaults(mode registry.Mode) any { return forMode(mode).API }

// grpcDefaults is the gRPC settings' default for a mode: on for full and archive nodes, off otherwise.
func grpcDefaults(mode registry.Mode) any { return forMode(mode).GRPC }

// grpcWebDefaults is the gRPC-web settings' default for a mode: on for full and archive nodes, off
// otherwise.
func grpcWebDefaults(mode registry.Mode) any { return forMode(mode).GRPCWeb }

// stateSyncDefaults is the snapshot settings' default for a mode. An absent snapshot-keep-recent reads
// as zero, which keeps every snapshot, where the default keeps two.
func stateSyncDefaults(mode registry.Mode) any { return forMode(mode).StateSync }

// telemetrySchema is the upstream telemetry struct with GlobalLabels as []any, the shape its reader
// asserts. The upstream [][]string type would resolve a default the reader rejects, stopping every node.
type telemetrySchema struct {
	ServiceName             string `mapstructure:"service-name"`
	Enabled                 bool   `mapstructure:"enabled"`
	EnableHostname          bool   `mapstructure:"enable-hostname"`
	EnableHostnameLabel     bool   `mapstructure:"enable-hostname-label"`
	EnableServiceLabel      bool   `mapstructure:"enable-service-label"`
	PrometheusRetentionTime int64  `mapstructure:"prometheus-retention-time"`
	GlobalLabels            []any  `mapstructure:"global-labels"`
}

// telemetryDefaults is the upstream telemetry default for a mode. GlobalLabels is empty upstream, which a
// test holds, so it needs no conversion.
func telemetryDefaults(mode registry.Mode) any {
	live := forMode(mode).Telemetry
	return telemetrySchema{
		ServiceName:             live.ServiceName,
		Enabled:                 live.Enabled,
		EnableHostname:          live.EnableHostname,
		EnableHostnameLabel:     live.EnableHostnameLabel,
		EnableServiceLabel:      live.EnableServiceLabel,
		PrometheusRetentionTime: live.PrometheusRetentionTime,
		GlobalLabels:            []any{},
	}
}
