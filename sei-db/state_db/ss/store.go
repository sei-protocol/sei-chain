package ss

import (
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload"
)

// NewStateStore creates a CompositeStateStore which handles both Cosmos and EVM data.
// The backend (pebbledb or rocksdb) is resolved at compile time via build-tag-gated
// files in the backend package. When WriteMode/ReadMode are both cosmos_only (the default),
// the EVM stores are not opened and the composite store behaves identically to a plain cosmos state store.
func NewStateStore(homeDir string, ssConfig config.StateStoreConfig) (types.StateStore, error) {
	return NewStateStoreWithOffload(homeDir, ssConfig, nil)
}

// NewStateStoreWithOffload creates a state store with an optional history
// offload stream. When stream is nil, behavior is identical to NewStateStore.
func NewStateStoreWithOffload(
	homeDir string,
	ssConfig config.StateStoreConfig,
	stream offload.Stream,
) (types.StateStore, error) {
	return composite.NewCompositeStateStoreWithOffload(ssConfig, homeDir, stream)
}
