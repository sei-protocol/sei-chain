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

// StateSyncSectionName is this section's name in the configuration key space.
const StateSyncSectionName = "state-sync"

// Registration puts the upstream sections in the configuration registry.
//
// srvconfig.StateSyncConfig is registered directly rather than through a schema written here, because
// its mapstructure tags already name the keys the readers resolve. That is worth stating: the two
// SeiDB sections needed a schema precisely because their tags name something else.
func init() {
	registry.RegisterSection(StateSyncSectionName, &srvconfig.StateSyncConfig{}, stateSyncBaseline)
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
