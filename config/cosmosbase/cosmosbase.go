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
	srvconfig "github.com/sei-protocol/sei-chain/sei-cosmos/server/config"

	"github.com/sei-protocol/sei-chain/config/registry"
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
)

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
