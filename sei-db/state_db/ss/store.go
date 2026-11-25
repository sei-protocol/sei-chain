package ss

import (
	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
)

// NewStateStore opens the composite state store in homeDir, which serves both Cosmos and EVM data. The
// backend (pebbledb or rocksdb) is resolved at compile time via build-tag-gated files in the backend
// package. When WriteMode/ReadMode are both cosmos_only (the default), the EVM stores are not opened and
// the store behaves identically to a plain cosmos state store.
func NewStateStore(homeDir string, ssConfig config.StateStoreConfig) (*composite.CompositeStateStore, error) {
	return composite.NewCompositeStateStore(ssConfig, homeDir)
}
