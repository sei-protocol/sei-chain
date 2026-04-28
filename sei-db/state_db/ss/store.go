package ss

import (
	"fmt"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload/historical"
)

// NewStateStore opens the composite SS and, if HistoricalOffloadDSN is set,
// wraps it with a Cockroach-backed fallback for reads of pruned versions.
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
