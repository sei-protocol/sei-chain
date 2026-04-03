package littbuilder

import (
	"fmt"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/Layr-Labs/eigenda/common"
	"github.com/Layr-Labs/eigenda/common/cache"
	"github.com/Layr-Labs/eigenda/litt"
	tablecache "github.com/Layr-Labs/eigenda/litt/cache"
	"github.com/Layr-Labs/eigenda/litt/disktable"
	"github.com/Layr-Labs/eigenda/litt/disktable/keymap"
	"github.com/Layr-Labs/eigenda/litt/metrics"
	"github.com/Layr-Labs/eigenda/litt/util"
	"github.com/Layr-Labs/eigensdk-go/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// keymapBuilders contains builders for all supported keymap types.
var keymapBuilders = map[keymap.KeymapType]keymap.BuildKeymap{
	keymap.MemKeymapType:           keymap.NewMemKeymap,
	keymap.LevelDBKeymapType:       keymap.NewLevelDBKeymap,
	keymap.UnsafeLevelDBKeymapType: keymap.NewUnsafeLevelDBKeymap,
}

// cacheWeight is a function that calculates the weight of a cache entry.
func cacheWeight(key string, value []byte) uint64 {
	return uint64(len(key) + len(value))
}

// Look for a table's keymap directory in the provided segment paths.
func FindKeymapLocation(
	rootPaths []string,
	tableName string,
) (keymapDirectory string, keymapInitialized bool, keymapTypeFile *keymap.KeymapTypeFile, error error) {

	if len(rootPaths) == 0 {
		return "", false, nil,
			fmt.Errorf("no segment paths provided for keymap search")
	}

	potentialKeymapDirectories := make([]string, len(rootPaths))
	for i, rootPath := range rootPaths {
		potentialKeymapDirectories[i] = path.Join(rootPath, tableName, keymap.KeymapDirectoryName)
	}

	for _, directory := range potentialKeymapDirectories {
		exists, err := util.Exists(directory)
		if err != nil {
			return "", false, nil,
				fmt.Errorf("error checking for keymap type file: %w", err)
		}
		if exists {
			if keymapDirectory != "" {
				return "", false, nil,
					fmt.Errorf("multiple keymap directories found: %s and %s", keymapDirectory, directory)
			}

			keymapDirectory = directory
			keymapTypeFile, err = keymap.LoadKeymapTypeFile(directory)
			if err != nil {
				return "", false, nil,
					fmt.Errorf("error loading keymap type file: %w", err)
			}

			initializedExists, err := util.Exists(path.Join(keymapDirectory, keymap.KeymapInitializedFileName))
			if err != nil {
				return "", false, nil,
					fmt.Errorf("error checking for keymap initialized file: %w", err)
			}
			if initializedExists {
				keymapInitialized = true
			}
		}
	}

	return keymapDirectory, keymapInitialized, keymapTypeFile, nil
}

// buildKeymap creates a new keymap based on the configuration.
func buildKeymap(
	config *litt.Config,
	logger logging.Logger,
	tableName string,
) (kmap keymap.Keymap, keymapPath string, keymapTypeFile *keymap.KeymapTypeFile, requiresReload bool, err error) {

	builderForConfiguredType, ok := keymapBuilders[config.KeymapType]
	if !ok {
		return nil, "", nil, false,
			fmt.Errorf("unsupported keymap type: %v", config.KeymapType)
	}

	keymapDirectory, keymapInitialized, keymapTypeFile, err := FindKeymapLocation(config.Paths, tableName)
	if err != nil {
		return nil, "", nil, false,
			fmt.Errorf("error finding keymap location: %w", err)
	}

	if keymapTypeFile != nil && !keymapInitialized {
		// The keymap has not been fully initialized. This is likely due to a crash during the keymap reloading process.
		logger.Warnf("incomplete keymap initialization detected. Deleting keymap directory: %s",
			keymapDirectory)

		err := os.RemoveAll(keymapDirectory)
		if err != nil {
			return nil, "", nil, false,
				fmt.Errorf("error deleting keymap directory: %w", err)
		}

		keymapTypeFile = nil
		keymapDirectory = ""
	}

	newKeymap := false
	if keymapTypeFile == nil {
		// No previous keymap exists. Either we are starting fresh or the keymap was deleted.
		newKeymap = true

		// by convention, always select the first path as the keymap directory
		keymapDirectory = path.Join(config.Paths[0], tableName, keymap.KeymapDirectoryName)
		keymapTypeFile = keymap.NewKeymapTypeFile(keymapDirectory, config.KeymapType)

		// create the keymap directory
		err := os.MkdirAll(keymapDirectory, 0755)
		if err != nil {
			return nil, "", nil, false,
				fmt.Errorf("error creating keymap directory: %w", err)
		}

		// write the keymap type file
		err = keymapTypeFile.Write()
		if err != nil {
			return nil, "", nil, false,
				fmt.Errorf("error writing keymap type file: %w", err)
		}

	} else {
		// A previous keymap exists. Check if the keymap type has changed.
		if config.KeymapType != keymapTypeFile.Type() {
			// The previously used keymap type is different from the one in the configuration.

			keymapTypeFile = nil

			// delete the old keymap
			err = os.RemoveAll(keymapDirectory)
			if err != nil {
				return nil, "", nil, false,
					fmt.Errorf("error deleting keymap files: %w", err)
			}

			// write the new keymap type file
			err = os.MkdirAll(keymapDirectory, 0755)
			if err != nil {
				return nil, "", nil, false,
					fmt.Errorf("error creating keymap directory: %w", err)
			}
			keymapTypeFile = keymap.NewKeymapTypeFile(keymapDirectory, config.KeymapType)
			err = keymapTypeFile.Write()
			if err != nil {
				return nil, "", nil, false,
					fmt.Errorf("error writing keymap type file: %w", err)
			}
		}
	}

	keymapDataDirectory := path.Join(keymapDirectory, keymap.KeymapDataDirectoryName)
	kmap, requiresReload, err = builderForConfiguredType(logger, keymapDataDirectory, config.DoubleWriteProtection)
	if err != nil {
		return nil, "", nil, false,
			fmt.Errorf("error building keymap: %w", err)
	}

	if !requiresReload {
		// If the keymap does not need to be reloaded, then it is already fully initialized.
		keymapInitializedFile := path.Join(keymapDirectory, keymap.KeymapInitializedFileName)
		f, err := os.Create(keymapInitializedFile)
		if err != nil {
			return nil, "", nil, false,
				fmt.Errorf("failed to create keymap initialized file: %v", err)
		}
		err = f.Close()
		if err != nil {
			return nil, "", nil, false,
				fmt.Errorf("failed to close keymap initialized file: %v", err)
		}
	}

	return kmap, keymapDirectory, keymapTypeFile, requiresReload || newKeymap, nil
}

// buildTable creates a new table based on the configuration.
func buildTable(
	config *litt.Config,
	logger logging.Logger,
	name string,
	metrics *metrics.LittDBMetrics) (litt.ManagedTable, error) {

	var table litt.ManagedTable

	if config.ShardingFactor < 1 {
		return nil, fmt.Errorf("sharding factor must be at least 1")
	}

	kmap, keymapDirectory, keymapTypeFile, requiresReload, err := buildKeymap(config, logger, name)
	if err != nil {
		return nil, fmt.Errorf("error creating keymap: %w", err)
	}

	table, err = disktable.NewDiskTable(
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

	cachedTable := tablecache.NewCachedTable(table, writeCache, readCache, metrics)

	return cachedTable, nil
}

// buildLogger creates a new logger based on the configuration.
func buildLogger(config *litt.Config) (logging.Logger, error) {
	if config.Logger != nil {
		return config.Logger, nil
	}

	return common.NewLogger(config.LoggerConfig)
}

// buildMetrics creates a new metrics object based on the configuration. If the returned server is not nil,
// then it is the responsibility of the caller to eventually call server.Shutdown().
func buildMetrics(config *litt.Config, logger logging.Logger) (*metrics.LittDBMetrics, *http.Server) {
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

			logger.Infof("Starting metrics server at port %d", config.MetricsPort)
			addr := fmt.Sprintf(":%d", config.MetricsPort)
			mux := http.NewServeMux()
			mux.Handle("/metrics", promhttp.HandlerFor(
				registry,
				promhttp.HandlerOpts{},
			))
			server = &http.Server{
				Addr:    addr,
				Handler: mux,
			}

			go func() {
				err := server.ListenAndServe()
				if err != nil && !strings.Contains(err.Error(), "http: Server closed") {
					logger.Errorf("metrics server error: %v", err)
				}
			}()
		} else {
			registry = config.MetricsRegistry
		}
	}

	return metrics.NewLittDBMetrics(registry, config.MetricsNamespace), server
}
