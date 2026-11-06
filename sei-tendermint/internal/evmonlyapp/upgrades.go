package evmonlyapp

import (
	"github.com/sei-protocol/sei-chain/app/upgradenames"
	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles/upgrade"
)

// upgradeHandlers returns a handler for every upgrade name this build ships.
// None changes EVM-only state.
func upgradeHandlers() map[string]upgrade.Handler {
	handlers := map[string]upgrade.Handler{}
	for _, name := range upgradenames.Release() {
		handlers[name] = nil
	}
	return handlers
}
