package litt

import (
	"fmt"
	"net/http"
	"strings"

	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/common"
	cache "github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/common/datacache"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/keymap"
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

	return common.NewLogger(config.LoggerConfig)
}

// buildMetrics creates a new metrics object based on the configuration. If the returned server is not nil,
// then it is the responsibility of the caller to eventually call server.Shutdown().
func buildMetrics(config *Config, logger *slog.Logger) (*littDBMetrics, *http.Server) {
	if !config.MetricsEnabled {
		return nil, nil
	}

	var registry *prometheus.Registry
	var server *http.Server

	if config.MetricsEnabled {
		if config.MetricsRegistry == nil {
			registry = prometheus.NewRegistry()
			registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
			registry.MustRegister(collectors.NewGoCollector())

			logger.Info(fmt.Sprintf("Starting metrics server at port %d", config.MetricsPort))
			addr := fmt.Sprintf(":%d", config.MetricsPort)
			mux := http.NewServeMux()
			mux.Handle("/metrics", promhttp.HandlerFor(
				registry,
				promhttp.HandlerOpts{},
			))
			server = &http.Server{ //nolint:gosec
				Addr:    addr,
				Handler: mux,
			}

			go func() {
				err := server.ListenAndServe()
				if err != nil && !strings.Contains(err.Error(), "http: Server closed") {
					logger.Error(fmt.Sprintf("metrics server error: %v", err))
				}
			}()
		} else {
			registry = config.MetricsRegistry
		}
	}

	return newLittDBMetrics(registry, config.MetricsNamespace), server
}
