package evmonlyapp

import (
	atypes "github.com/sei-protocol/sei-chain/sei-tendermint/autobahn/types"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/autobahn/epoch"
)

// EpochRegistryRowForTesting returns the module, key and value of the FlatKV row that holds
// registry. Intended for use in tests only.
func EpochRegistryRowForTesting(registry *atypes.EpochRegistry) (string, []byte, []byte) {
	return epochRegistryModule, []byte(epochRegistryKey), epoch.Encode(registry)
}
