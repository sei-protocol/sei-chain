// Package upgradespec defines upgrade metadata shared by handlers and
// cross-version tests.
package upgradespec

import (
	"slices"

	storekeys "github.com/sei-protocol/sei-chain/sei-db/common/keys"
)

var v67RetiredModules = []string{
	storekeys.CapabilityStoreKey,
	storekeys.FeegrantStoreKey,
	storekeys.IBCStoreKey,
	storekeys.IBCTransferStoreKey,
}

// V67RetiredModules returns the modules v6.7 removes while leaving their
// stores mounted. Each module's store key is its module name.
func V67RetiredModules() []string {
	return slices.Clone(v67RetiredModules)
}
