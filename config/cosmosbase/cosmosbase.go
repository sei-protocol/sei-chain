// Package cosmosbase registers the configuration sections whose keys belong to the Cosmos server.
//
// These five register here rather than beside the structs they describe, and the reason is an import edge.
// The mode rules their defaults answer through live in app/params, which imports the upstream server
// configuration, so that package cannot ask for them without a cycle. A vendored tree is not itself the
// obstacle: other sections do register inside one.
//
// A section belongs here only when its keys are upstream's and that edge is in the way. Everything else
// registers in the package that owns its struct, so the struct, the values and the keys stay together.
package cosmosbase

import (
	"github.com/sei-protocol/sei-chain/app/params"
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

// globalLabelsKey is the metric label set, which is the one key here no environment variable can supply.
const globalLabelsKey = TelemetrySectionName + ".global-labels"

// Registration puts the upstream server's configuration sections in the registry.
//
// Four of the five register the upstream struct directly, because their mapstructure tags already name the
// keys their reader resolves.
func init() {
	registry.RegisterRootKeys(BaseSectionName, &srvconfig.BaseConfig{}, baseDefaults)
	registry.RegisterSection(APISectionName, &srvconfig.APIConfig{}, apiDefaults)
	registry.RegisterSection(GRPCSectionName, &srvconfig.GRPCConfig{}, grpcDefaults)
	registry.RegisterSection(TelemetrySectionName, &telemetrySchema{}, telemetryDefaults)
	registry.RegisterSection(StateSyncSectionName, &srvconfig.StateSyncConfig{}, stateSyncDefaults)

	registry.RefuseFromEnvironment(TelemetrySectionName, globalLabelsKey,
		"the metric label set is a list of name and value rows, and its reader takes that exact shape "+
			"rather than casting what it finds, so no single environment string can supply it. Write it "+
			"in the configuration file instead")
}

// forMode is the server configuration a node of this kind is meant to run.
//
// The upstream defaults with the binary's own mode rules applied. Every section here answers through this,
// so a section states what a kind of node is meant to run rather than what the type holds before any mode
// is considered, and a rule added to those rules later moves these sections with nothing here changing.
//
// Three settings differ by mode today and each of them matters in a different direction. A node that
// serves queries needs the interfaces that serve them; a validator is meant to expose as little as it can;
// and how many blocks a node retains is a decision about its disk.
func forMode(mode registry.Mode) *srvconfig.Config {
	out := srvconfig.DefaultConfig()
	params.SetAppConfigByMode(out, params.NodeMode(mode))
	return out
}

// baseDefaults is what the node-wide settings resolve to for a node of this kind.
//
// One of these keys answers per mode: how many blocks a node retains, which is a hundred thousand for a
// full node and everything for the rest. The other two mode-varying keys in this package are the interface
// toggles, which belong to the sections that own them.
//
// Every one of these keys is read with a casting getter and no check that the key was present, so an
// absent key casts to a zero and clobbers the default beside it. Which keys those are, and what a node
// resolves for each instead, belongs in a measurement rather than in a count here.
//
// Several of these are not what a running node resolves today, and the causes differ: a bound command flag
// of the same name carries its own default below the file, and the command that assembles the server
// configuration overrides some of them before a node starts. The pruning strategy is the one worth naming,
// because the flag defaults it to the standard schedule while this declares it keeps everything.
//
// A caller resolving for a running node therefore has to supply that node's flag values, and only the ones
// an operator actually set. A flag nobody typed still reports a default, and this resolution ranks flags
// above the file, so passing defaults would put every one of them over an operator's own value.
func baseDefaults(mode registry.Mode) any { return forMode(mode).BaseConfig }

// apiDefaults is what the REST interface settings resolve to for a node that has written nothing.
//
// On for a full node and an archive node, off for a validator and a seed. Serving queries is what the
// first two are for, and the second two are meant to expose as little as they can.
func apiDefaults(mode registry.Mode) any { return forMode(mode).API }

// grpcDefaults is what the gRPC settings resolve to for a node that has written nothing.
//
// On for a full node and an archive node, off for a validator and a seed, which is the same rule the REST
// interface follows and for the same reason. The upstream default is on for every kind, so declaring that
// would state an open interface on the nodes meant to expose the least.
//
// Six of these eleven keys are read only when the key is present. Two more are durations read through a
// clamp that rescues a negative value and does nothing for an absent one, so those two are unguarded and
// their clobber leaves no trace. The durations are declared as durations and written into a file as text,
// which is the shape the reader parses back.
func grpcDefaults(mode registry.Mode) any { return forMode(mode).GRPC }

// stateSyncDefaults is what the snapshot settings resolve to for a node that has written nothing.
//
// All three keys are read with a casting getter and no presence check, and the retention is the one that
// inverts: it is declared as keeping two snapshots and an absent key casts to zero, which the file format
// documents as keeping every snapshot.
func stateSyncDefaults(mode registry.Mode) any { return forMode(mode).StateSync }

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
