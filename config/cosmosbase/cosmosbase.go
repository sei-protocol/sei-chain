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
}

// forMode is the server configuration the seid init command writes for a node of this kind.
//
// The upstream defaults with the binary's own mode rules applied, which is the pipeline that command
// builds and renders through the template. So a declared value here is what that file would have held, and
// a caller writing a configuration file writes what that command would have written.
//
// Named by the command, because this binary generates a file two ways and they do not agree. A node
// starting without one gets a file from a second pipeline that applies no mode rules at all and carries
// overrides of its own, so it writes the standard pruning strategy where this writes keeping everything,
// a metric retention of sixty where this writes seven thousand two hundred, the REST interface on for a
// validator where this writes it off, and a pruning interval drawn at random each time it runs. This
// follows the command an operator runs to provision a node, not the file a node writes for itself.
//
// That is what a declared value states, and it is deliberately not what a node with nothing written
// resolves. Those differ for a good number of these keys, because most are read with no check that the key
// was present and several are bound to a command flag carrying its own default below the file. The set is
// measured rather than counted, in the agreement test beside this one.
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
// A caller resolving for a running node has to supply that node's flag values, and only the ones an
// operator actually set. A flag nobody typed still reports a default, and this resolution ranks flags above
// the file, so passing defaults would put every one of them over an operator's own value.
func baseDefaults(mode registry.Mode) any { return forMode(mode).BaseConfig }

// apiDefaults is what the REST interface settings resolve to for a node of this kind.
//
// On for a full node and an archive node, off for a validator and a seed. Serving queries is what the
// first two are for, and the second two are meant to expose as little as they can.
func apiDefaults(mode registry.Mode) any { return forMode(mode).API }

// grpcDefaults is what the gRPC settings resolve to for a node of this kind.
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

// stateSyncDefaults is what the snapshot settings resolve to for a node of this kind.
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

// telemetryDefaults is what the metric settings resolve to for a node of this kind.
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
