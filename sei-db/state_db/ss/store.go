package ss

import (
	"fmt"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/config"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/composite"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/ss/offload/historical"
)

// NewStateStore creates a CompositeStateStore which handles both Cosmos and EVM data.
// The backend (pebbledb or rocksdb) is resolved at compile time via build-tag-gated
// files in the backend package. When WriteMode/ReadMode are both cosmos_only (the default),
// the EVM stores are not opened and the composite store behaves identically to a plain cosmos state store.
func NewStateStore(homeDir string, ssConfig config.StateStoreConfig) (types.StateStore, error) {
	primary, err := composite.NewCompositeStateStore(ssConfig, homeDir)
	if err != nil {
		return nil, err
	}
	scyllaCfg := historical.ScyllaConfig{
		Hosts:       splitCSV(ssConfig.HistoricalOffloadScyllaHosts),
		Keyspace:    ssConfig.HistoricalOffloadScyllaKeyspace,
		Username:    ssConfig.HistoricalOffloadScyllaUsername,
		Password:    ssConfig.HistoricalOffloadScyllaPassword,
		Datacenter:  ssConfig.HistoricalOffloadScyllaDatacenter,
		Consistency: ssConfig.HistoricalOffloadScyllaConsistency,
		Timeout:     time.Duration(ssConfig.HistoricalOffloadScyllaTimeoutMS) * time.Millisecond,
	}
	bigtableCfg := historical.BigtableConfig{
		ProjectID:  ssConfig.HistoricalOffloadBigtableProjectID,
		InstanceID: ssConfig.HistoricalOffloadBigtableInstance,
		Table:      ssConfig.HistoricalOffloadBigtableTable,
		Family:     ssConfig.HistoricalOffloadBigtableFamily,
		AppProfile: ssConfig.HistoricalOffloadBigtableAppProfile,
		Shards:     ssConfig.HistoricalOffloadBigtableShards,
	}
	if scyllaCfg.Configured() && bigtableCfg.Configured() {
		_ = primary.Close()
		return nil, fmt.Errorf("only one historical offload fallback can be configured")
	}
	if !scyllaCfg.Configured() && !bigtableCfg.Configured() {
		return primary, nil
	}
	if bigtableCfg.Configured() {
		reader, err := historical.NewBigtableReader(bigtableCfg)
		if err != nil {
			_ = primary.Close()
			return nil, fmt.Errorf("open bigtable historical offload reader: %w", err)
		}
		return historical.NewFallbackStateStore(primary, reader, ssConfig.HistoricalOffloadEarliestVersion), nil
	}
	reader, err := historical.NewScyllaReader(scyllaCfg)
	if err != nil {
		_ = primary.Close()
		return nil, fmt.Errorf("open scylla/cassandra historical offload reader: %w", err)
	}
	return historical.NewFallbackStateStore(primary, reader, ssConfig.HistoricalOffloadEarliestVersion), nil
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
