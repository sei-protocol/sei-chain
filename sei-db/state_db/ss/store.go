package ss

import (
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
)

// NewStateStore returns the StateStore interface, so the live commit path resolves BlockCommitter by
// type assertion. Pinning it here means a store that loses the method fails the build rather than
// leaving the commit path with nowhere to hand a block.
var _ types.BlockCommitter = (*composite.CompositeStateStore)(nil)

// NewStateStore creates a CompositeStateStore which handles both Cosmos and EVM data.
// The backend (pebbledb or rocksdb) is resolved at compile time via build-tag-gated
// files in the backend package. When WriteMode/ReadMode are both cosmos_only (the default),
// the EVM stores are not opened and the composite store behaves identically to a plain cosmos state store.
func NewStateStore(homeDir string, ssConfig config.StateStoreConfig) (types.StateStore, error) {
	return composite.NewCompositeStateStore(ssConfig, homeDir)
}
