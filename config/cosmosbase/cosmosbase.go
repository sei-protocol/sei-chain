// Package cosmosbase declares the configuration sections the upstream Cosmos server owns.
//
// These sections have no owning package inside this repository. Their structs and their key constants
// live in sei-cosmos, which this repository does not change, and the code that resolves them is spread
// between the command that builds the application and the upstream config reader. So they are declared
// here, on that code's behalf, and held against the key constants it resolves.
//
// A section belongs here only when the keys are upstream's. Everything a sei package owns registers from
// that package, so its struct, its defaults and its keys stay in one place.
package cosmosbase

import (
	"github.com/sei-protocol/sei-chain/config/registry"
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"
)

// The names these sections are looked up and reported under.
//
// BaseSectionName names a section whose keys carry no prefix at all: they sit at the root of app.toml and
// are read as "pruning", "occ-enabled" and so on. The name is for lookups and reports and is not part of
// any key, because giving those settings a section would rename every one of them.
const (
	StateSyncSectionName = "state-sync"
	BaseSectionName      = "base"
	APISectionName       = "api"
	GRPCSectionName      = "grpc"
	TelemetrySectionName = "telemetry"
)

// GlobalLabelsKey is the metric label set, and the one key no environment variable can supply.
const GlobalLabelsKey = "telemetry.global-labels"

// Registration puts the upstream sections in the configuration registry.
//
// srvconfig.StateSyncConfig is registered directly rather than through a schema written here, because
// its mapstructure tags already name the keys the readers resolve. That is worth stating: the two
// SeiDB sections needed a schema precisely because their tags name something else.
func init() {
	registry.RegisterSection(StateSyncSectionName, &srvconfig.StateSyncConfig{}, stateSyncBaseline)
	registry.RegisterRootKeys(BaseSectionName, &srvconfig.BaseConfig{}, baseBaseline)
	registry.RegisterSection(APISectionName, &srvconfig.APIConfig{}, apiBaseline)
	registry.RegisterSection(GRPCSectionName, &srvconfig.GRPCConfig{}, grpcBaseline)
	registry.RegisterSection(TelemetrySectionName, &telemetrySchema{}, telemetryBaseline)

	// The label set is a list of name/value pairs and the reader takes that exact type rather than casting
	// what it finds. An environment variable carries one string, so there is no value of it the reader can
	// use, and resolving one would install a value that stops the node.
	// The upstream server configuration reader assigns these straight from a lookup, so a key nothing
	// supplies resolves to the zero rather than to the default beside it. Nine of them are also bound
	// start flags, whose registration default reaches the reader first, so a migration consults the flag
	// before it consults this. The declaration describes the reader; what a node actually resolves is
	// decided by which channel answers.
	registry.DeclareZeroWhenAbsent(BaseSectionName,
		"concurrency-workers", "inter-block-cache", "minimum-gas-prices", "occ-enabled",
		"pruning", "pruning-interval", "pruning-keep-recent",
	)
	registry.DeclareZeroWhenAbsent(APISectionName,
		"api.address", "api.max-open-connections", "api.rpc-max-body-bytes",
		"api.rpc-read-timeout", "api.swagger",
	)
	registry.DeclareZeroWhenAbsent(GRPCSectionName, "grpc.address", "grpc.enable")
	registry.DeclareZeroWhenAbsent(StateSyncSectionName, "state-sync.snapshot-keep-recent")
	registry.DeclareZeroWhenAbsent(TelemetrySectionName,
		"telemetry.enabled", "telemetry.prometheus-retention-time",
	)

	registry.RefuseFromEnvironment(GlobalLabelsKey,
		"the metric label set is a list of name/value pairs and its reader takes that exact type rather "+
			"than casting, so no single environment string can supply it. Write it in sei.toml instead")
}

// telemetrySchema declares the keys the metric settings reader resolves.
//
// A schema rather than telemetry.Config, and this is the one upstream section that needs one. Its type
// declares the label set as a list of string pairs, and the reader takes a list of untyped rows instead:
// it asserts that exact type rather than casting what it finds, and refuses the struct's own type,
// including the empty value of it. Registering the type directly would resolve a baseline the reader
// refuses, and that refusal stops the node. Every node, not only one that wrote the key.
//
// Every other field matches, so the difference is one field's type and not the section's shape.
type telemetrySchema struct {
	ServiceName             string `mapstructure:"service-name"`
	Enabled                 bool   `mapstructure:"enabled"`
	EnableHostname          bool   `mapstructure:"enable-hostname"`
	EnableHostnameLabel     bool   `mapstructure:"enable-hostname-label"`
	EnableServiceLabel      bool   `mapstructure:"enable-service-label"`
	PrometheusRetentionTime int64  `mapstructure:"prometheus-retention-time"`
	GlobalLabels            []any  `mapstructure:"global-labels"`
}

// telemetryBaseline is what this section resolves to for a node that has written nothing.
//
// Read out of the upstream defaults, with the label set converted to the rows its reader accepts. The same
// values for every mode: what a node labels its metrics with is an operator's decision about their
// monitoring, and no node mode implies one.
func telemetryBaseline(registry.Mode) any {
	live := srvconfig.DefaultConfig().Telemetry
	labels := make([]any, 0, len(live.GlobalLabels))
	for _, pair := range live.GlobalLabels {
		row := make([]any, 0, len(pair))
		for _, item := range pair {
			row = append(row, item)
		}
		labels = append(labels, row)
	}
	return telemetrySchema{
		ServiceName:             live.ServiceName,
		Enabled:                 live.Enabled,
		EnableHostname:          live.EnableHostname,
		EnableHostnameLabel:     live.EnableHostnameLabel,
		EnableServiceLabel:      live.EnableServiceLabel,
		PrometheusRetentionTime: live.PrometheusRetentionTime,
		GlobalLabels:            labels,
	}
}

// apiBaseline and grpcBaseline are what these sections resolve to for a node that has written nothing.
//
// Both register their upstream type directly, because its mapstructure tags already name the keys the
// reader resolves: eight tags and eight reads for the one, eleven and eleven for the other.
//
// Read out of the upstream defaults rather than written again here. The same values for every mode: which
// interfaces a node serves is an operator's decision, and a seed node offers them no differently from a
// validator.
func apiBaseline(registry.Mode) any { return srvconfig.DefaultConfig().API }

func grpcBaseline(registry.Mode) any { return srvconfig.DefaultConfig().GRPC }

// baseBaseline is what the node-wide settings resolve to for a node that has written nothing.
//
// The upstream defaults, which is what seid init writes into app.toml. Every one of these keys is read
// with a casting getter, and an absent key casts to zero, so a node whose app.toml predates one of them
// runs the zero rather than the default beside it. testdata/base.absent.golden records which keys that is;
// five of the thirteen have a non-zero default, and the pruning strategy is the one that matters most,
// since an empty strategy is not a strategy.
//
// The same values for every mode. How much history a node keeps and how many workers it runs are
// decisions about disk and CPU that an operator writes down.
func baseBaseline(registry.Mode) any {
	return srvconfig.DefaultConfig().BaseConfig
}

// stateSyncBaseline is what this section resolves to for a node that has written nothing.
//
// Read out of the upstream default configuration rather than written again here, so a changed upstream
// default moves with it. The same values for every mode: whether a node offers state-sync snapshots to
// its peers is an operator's decision about disk and upload, and a seed node serves them no differently
// from a full one.
func stateSyncBaseline(registry.Mode) any {
	return srvconfig.DefaultConfig().StateSync
}
