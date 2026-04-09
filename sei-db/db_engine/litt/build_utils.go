package litt

import (
	"fmt"

	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
	cache "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util/datacache"
)

// cacheWeight is a function that calculates the weight of a cache entry.
func cacheWeight(key string, value []byte) uint64 {
	return uint64(len(key) + len(value)) //nolint:gosec
}

// buildTable creates a new table based on the configuration.
func buildTable(
	config *Config,
	logger *slog.Logger,
	name string,
	metrics *littDBMetrics) (ManagedTable, error) {

	var table ManagedTable

	if config.ShardingFactor < 1 {
		return nil, fmt.Errorf("sharding factor must be at least 1")
	}

	kmap, keymapDirectory, keymapTypeFile, requiresReload, err := keymap.OpenOrCreate(
		logger, config.KeymapType, config.Paths, name, config.DoubleWriteProtection)
	if err != nil {
		return nil, fmt.Errorf("error creating keymap: %w", err)
	}

	table, err = newDiskTable(
		config,
		name,
		kmap,
		keymapDirectory,
		keymapTypeFile,
		config.Paths,
		requiresReload,
		metrics)

	if err != nil {
		return nil, fmt.Errorf("error creating table: %w", err)
	}

	writeCache := cache.NewFIFOCache[string, []byte](config.WriteCacheSize, cacheWeight, metrics.GetWriteCacheMetrics())
	writeCache = cache.NewThreadSafeCache(writeCache)

	readCache := cache.NewFIFOCache[string, []byte](config.ReadCacheSize, cacheWeight, metrics.GetReadCacheMetrics())
	readCache = cache.NewThreadSafeCache(readCache)

	cachedTable := newCachedTable(table, writeCache, readCache, metrics)

	return cachedTable, nil
}

// buildLogger creates a new logger based on the configuration.
func buildLogger(config *Config) (*slog.Logger, error) {
	if config.Logger != nil {
		return config.Logger, nil
	}

	return util.NewLogger(config.LoggerConfig)
}
