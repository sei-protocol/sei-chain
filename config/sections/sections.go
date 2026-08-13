// Package sections states which configuration sections a seid binary declares.
//
// A section reaches the registry by its owning package's initialisation, which happens only if
// something imports that package. That makes the set of sections a consequence of the import graph:
// a section whose owner nothing imports is absent from every diagnostic, and absent silently, since
// an undeclared key is left to the machinery that already answers it. Two of the sections were in
// fact reaching the registry only because a command package imported their owner for an unrelated
// reason, and the verb packages saw a fraction of the set.
//
// Importing this package is what makes the set the same everywhere. The list below is what makes it
// stated, so a section that stops registering is a failure rather than a shorter report.
package sections

import (
	"sort"

	"github.com/sei-protocol/sei-chain/config/registry"

	_ "github.com/sei-protocol/sei-chain/admin"
	_ "github.com/sei-protocol/sei-chain/app"
	_ "github.com/sei-protocol/sei-chain/evmrpc/config"
	_ "github.com/sei-protocol/sei-chain/giga/executor/config"
	_ "github.com/sei-protocol/sei-chain/sei-db/config"
	_ "github.com/sei-protocol/sei-chain/sei-wasmd/x/wasm"
	_ "github.com/sei-protocol/sei-chain/x/evm/blocktest"
	_ "github.com/sei-protocol/sei-chain/x/evm/querier"
	_ "github.com/sei-protocol/sei-chain/x/evm/replay"
)

// Names are the configuration sections a seid binary declares, sorted.
//
// Written out rather than read back from the registry. Reading it back would answer with whatever
// the import graph produced, which is the thing this list exists to check.
var Names = []string{
	"admin_server",
	"eth_blocktest",
	"eth_replay",
	"evm",
	"evm_query",
	"genesis",
	"giga_executor",
	"light_invariance",
	"receipt-store",
	"wasm",
}

// Missing returns the declared sections the registry does not hold, sorted.
//
// Empty is the only correct answer. A name here means that section's keys resolve through the
// machinery that answered them before the registry existed, and nothing else reports that.
func Missing() []string {
	held := map[string]bool{}
	for _, section := range registry.Sections() {
		held[section.Name] = true
	}
	var missing []string
	for _, name := range Names {
		if !held[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	return missing
}

// Unexpected returns the registered sections this package does not name, sorted.
//
// A name here is a section that entered the key space without being written down. It is not
// necessarily wrong, but nothing has stated that it should be there, so nothing would notice it
// leaving again.
func Unexpected() []string {
	declared := map[string]bool{}
	for _, name := range Names {
		declared[name] = true
	}
	var unexpected []string
	for _, section := range registry.Sections() {
		if !declared[section.Name] {
			unexpected = append(unexpected, section.Name)
		}
	}
	sort.Strings(unexpected)
	return unexpected
}
