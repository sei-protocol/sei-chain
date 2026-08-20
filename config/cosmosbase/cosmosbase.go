// Package cosmosbase registers the configuration sections whose keys belong to the Cosmos server.
//
// These sections have no owning package inside this repository. Their structs and their readers live in
// sei-cosmos, which this repository vendors rather than authors, so there is nowhere upstream to put a
// registration that this repository's registry would see. A section belongs here only when its keys are
// upstream's; a section this repository owns registers in the package that owns its struct.
package cosmosbase

import (
	"github.com/sei-protocol/sei-chain/config/registry"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
)

// The names these sections have in the configuration key space.
//
// BaseSectionName names a section whose keys carry no prefix at all. The name is for lookups and reports
// and is not part of any key, because giving those settings a section would rename every one of them.
const (
	BaseSectionName      = "base"
	APISectionName       = "api"
	GRPCSectionName      = "grpc"
	TelemetrySectionName = "telemetry"
	StateSyncSectionName = "state-sync"
)

// GlobalLabelsKey is the metric label set, which is the one key here no environment variable can supply.
const GlobalLabelsKey = TelemetrySectionName + ".global-labels"

// Registration puts the upstream server's configuration sections in the registry.
//
// Four of the five register the upstream struct directly, because their mapstructure tags already name the
// keys their reader resolves. That is worth stating rather than assuming: the two SeiDB sections needed a
// schema precisely because their tags name something else.
func init() {
	registry.RegisterRootKeys(BaseSectionName, &srvconfig.BaseConfig{}, baseDefaults)
	registry.RegisterSection(APISectionName, &srvconfig.APIConfig{}, apiDefaults)
	registry.RegisterSection(GRPCSectionName, &srvconfig.GRPCConfig{}, grpcDefaults)
	registry.RegisterSection(TelemetrySectionName, &telemetrySchema{}, telemetryDefaults)
	registry.RegisterSection(StateSyncSectionName, &srvconfig.StateSyncConfig{}, stateSyncDefaults)

	registry.RefuseFromEnvironment(GlobalLabelsKey,
		"the metric label set is a list of name and value rows, and its reader takes that exact shape "+
			"rather than casting what it finds, so no single environment string can supply it. Write it "+
			"in the configuration file instead")
}

// baseDefaults is what the node-wide settings resolve to for a node that has written nothing.
//
// The upstream defaults, unchanged by mode. Every one of these keys is read with a casting getter and no
// check that the key was present, so an absent key casts to a zero and clobbers the default beside it.
// Five of the fourteen have a non-zero default, and the pruning strategy is the one that matters, because
// an empty strategy is not a strategy.
//
// Three keys elsewhere in this package vary by node mode, and none of them varies here. seid init writes
// the interface toggles and the block retention per mode, so a node it provisioned carries those as
// written values, and a written value is what resolves. These are what a node with nothing written runs.
//
// One value here is not what a running node uses today, and it is worth knowing which. The pruning
// strategy is declared as keeping everything, while the command line registers a flag of the same name
// defaulting to the standard strategy, and a bound flag is a source of its own below the file. So a node
// started with no pruning key written prunes on the standard schedule and this states that it would keep
// everything. Whoever resolves for a running node has to supply the flag values to get the answer that
// node uses.
func baseDefaults(registry.Mode) any { return srvconfig.DefaultConfig().BaseConfig }

// apiDefaults is what the REST interface settings resolve to for a node that has written nothing.
//
// The interface is off, for every mode. seid init turns it on for a full node and an archive node, so
// those carry it written, and a node whose file lacks the key does not serve REST whatever kind it is.
func apiDefaults(registry.Mode) any { return srvconfig.DefaultConfig().API }

// grpcDefaults is what the gRPC settings resolve to for a node that has written nothing.
//
// The interface is on, which is the upstream default, and seid init writes it off for a validator and a
// seed. Six of these eleven keys are read only when the key is present, so for those the declared default
// is also what an absent key resolves to today.
//
// The six durations are declared as durations and written into a file as text, which is the shape the
// reader parses back.
func grpcDefaults(registry.Mode) any { return srvconfig.DefaultConfig().GRPC }

// stateSyncDefaults is what the snapshot settings resolve to for a node that has written nothing.
//
// All three keys are read with a casting getter and no presence check, and the retention is the one that
// inverts: it is declared as keeping two snapshots and an absent key casts to zero, which the file format
// documents as keeping every snapshot.
func stateSyncDefaults(registry.Mode) any { return srvconfig.DefaultConfig().StateSync }

// telemetrySchema declares the keys the metric settings reader resolves.
//
// A schema rather than the upstream type, and the only one of these five that needs one. The difference is
// a single field's type. The upstream struct declares the label set as a list of string pairs, and the
// reader takes a list of untyped rows: it asserts that exact shape rather than casting what it finds, and
// the struct's own type does not satisfy it, including that type's empty value. Registering the upstream
// type would resolve a default the reader refuses, and it refuses by returning an error that is the first
// statement of the whole server configuration, so the node stops. Every node, not only one that wrote the
// key.
//
// Every other field matches the upstream type, so this is one field's shape and not the section's.
type telemetrySchema struct {
	ServiceName             string `mapstructure:"service-name"`
	Enabled                 bool   `mapstructure:"enabled"`
	EnableHostname          bool   `mapstructure:"enable-hostname"`
	EnableHostnameLabel     bool   `mapstructure:"enable-hostname-label"`
	EnableServiceLabel      bool   `mapstructure:"enable-service-label"`
	PrometheusRetentionTime int64  `mapstructure:"prometheus-retention-time"`
	GlobalLabels            []any  `mapstructure:"global-labels"`
}

// telemetryDefaults is what the metric settings resolve to for a node that has written nothing.
//
// Read out of the upstream defaults rather than written again here, so a changed default moves both at
// once and this states only which key carries which setting.
//
// The label set is empty, which is what the upstream default holds, so there is nothing to convert into
// the untyped rows the reader takes. A test holds that emptiness, because a default that gained rows would
// need converting and would otherwise reach the reader as the shape it refuses.
func telemetryDefaults(registry.Mode) any {
	live := srvconfig.DefaultConfig().Telemetry
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
