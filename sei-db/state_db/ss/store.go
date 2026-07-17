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
	scyllaConfigured := scyllaHistoricalOffloadConfigured(ssConfig)
	bigtableConfigured := bigtableHistoricalOffloadConfigured(ssConfig)
	if scyllaConfigured && bigtableConfigured {
		_ = primary.Close()
		return nil, fmt.Errorf("only one historical offload fallback can be configured")
	}
	if !scyllaConfigured && !bigtableConfigured {
		return primary, nil
	}
	fallbackOpts := historical.FallbackOptions{
		EarliestVersion: ssConfig.HistoricalOffloadEarliestVersion,
	}
	if bigtableConfigured {
		reader, err := historical.NewBigtableReader(historical.BigtableConfig{
			ProjectID:  ssConfig.HistoricalOffloadBigtableProjectID,
			InstanceID: ssConfig.HistoricalOffloadBigtableInstance,
			Table:      ssConfig.HistoricalOffloadBigtableTable,
			Family:     ssConfig.HistoricalOffloadBigtableFamily,
			AppProfile: ssConfig.HistoricalOffloadBigtableAppProfile,
			Shards:     ssConfig.HistoricalOffloadBigtableShards,
		})
		if err != nil {
			_ = primary.Close()
			return nil, fmt.Errorf("open bigtable historical offload reader: %w", err)
		}
		return historical.NewFallbackStateStore(primary, reader, fallbackOpts), nil
	}
	reader, err := historical.NewScyllaReader(historical.ScyllaConfig{
		Hosts:       splitCSV(ssConfig.HistoricalOffloadScyllaHosts),
		Keyspace:    ssConfig.HistoricalOffloadScyllaKeyspace,
		Username:    ssConfig.HistoricalOffloadScyllaUsername,
		Password:    ssConfig.HistoricalOffloadScyllaPassword,
		Datacenter:  ssConfig.HistoricalOffloadScyllaDatacenter,
		Consistency: ssConfig.HistoricalOffloadScyllaConsistency,
		Timeout:     time.Duration(ssConfig.HistoricalOffloadScyllaTimeoutMS) * time.Millisecond,
	})
	if err != nil {
		_ = primary.Close()
		return nil, fmt.Errorf("open scylla/cassandra historical offload reader: %w", err)
	}
	return historical.NewFallbackStateStore(primary, reader, fallbackOpts), nil
}

func scyllaHistoricalOffloadConfigured(cfg config.StateStoreConfig) bool {
	return strings.TrimSpace(cfg.HistoricalOffloadScyllaHosts) != "" ||
		strings.TrimSpace(cfg.HistoricalOffloadScyllaKeyspace) != ""
}

func bigtableHistoricalOffloadConfigured(cfg config.StateStoreConfig) bool {
	return strings.TrimSpace(cfg.HistoricalOffloadBigtableProjectID) != "" ||
		strings.TrimSpace(cfg.HistoricalOffloadBigtableInstance) != "" ||
		strings.TrimSpace(cfg.HistoricalOffloadBigtableTable) != ""
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
