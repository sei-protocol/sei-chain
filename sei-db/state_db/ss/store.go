package ss

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/bigtable"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload/historical"
)

// NewStateStore creates a CompositeStateStore which handles both Cosmos and EVM data.
// The backend (pebbledb or rocksdb) is resolved at compile time via build-tag-gated
// files in the backend package. When WriteMode/ReadMode are both cosmos_only (the default),
// the EVM stores are not opened and the composite store behaves identically to a plain cosmos state store.
// When the Bigtable historical offload is configured, the store is wrapped so
// pruned point reads fall back to Bigtable.
func NewStateStore(homeDir string, ssConfig config.StateStoreConfig) (types.StateStore, error) {
	primary, err := composite.NewCompositeStateStore(ssConfig, homeDir)
	if err != nil {
		return nil, err
	}
	bigtableCfg := bigtable.Config{
		ProjectID:  ssConfig.HistoricalOffloadBigtableProjectID,
		InstanceID: ssConfig.HistoricalOffloadBigtableInstance,
		Table:      ssConfig.HistoricalOffloadBigtableTable,
		Family:     ssConfig.HistoricalOffloadBigtableFamily,
		AppProfile: ssConfig.HistoricalOffloadBigtableAppProfile,
		Shards:     ssConfig.HistoricalOffloadBigtableShards,
	}
	if !bigtableCfg.Configured() {
		return primary, nil
	}
	reader, err := historical.NewBigtableReader(bigtableCfg)
	if err != nil {
		_ = primary.Close()
		return nil, fmt.Errorf("open bigtable historical offload reader: %w", err)
	}
	return historical.NewFallbackStateStore(primary, reader, ssConfig.HistoricalOffloadEarliestVersion), nil
}
