package ss

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload/historical"
)

// NewStateStore creates a CompositeStateStore which handles both Cosmos and EVM data.
// The backend (pebbledb or rocksdb) is resolved at compile time via build-tag-gated
// files in the backend package. When WriteMode/ReadMode are both cosmos_only (the default),
// the EVM stores are not opened and the composite store behaves identically to a plain cosmos state store.
//
// If ssConfig.HistoricalOffloadDSN is set, the composite store is wrapped with
// a historical.FallbackStateStore so reads of pruned versions are served from
// the offload-pipeline CockroachDB cluster.
func NewStateStore(homeDir string, ssConfig config.StateStoreConfig) (types.StateStore, error) {
	cs, err := composite.NewCompositeStateStore(ssConfig, homeDir)
	if err != nil {
		return nil, err
	}
	if ssConfig.HistoricalOffloadDSN == "" {
		return cs, nil
	}
	reader, err := historical.NewCockroachReader(historical.CockroachConfig{
		DSN: ssConfig.HistoricalOffloadDSN,
	})
	if err != nil {
		_ = cs.Close()
		return nil, fmt.Errorf("open historical offload reader: %w", err)
	}
	return historical.NewFallbackStateStore(cs, reader), nil
}
