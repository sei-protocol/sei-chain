package littbuilder

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path"

	commonmetrics "github.com/sei-protocol/sei-chain/sei-db/common/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/dbcache"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable/keymap"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

// keymapBuilders contains builders for all supported keymap types.
var keymapBuilders = map[keymap.KeymapType]keymap.BuildKeymap{
	keymap.MemKeymapType:            keymap.NewMemKeymap,
	keymap.PebbleDBKeymapType:       keymap.NewPebbleDBKeymap,
	keymap.UnsafePebbleDBKeymapType: keymap.NewUnsafePebbleDBKeymap,
}

// cacheWeight is a function that calculates the weight of a cache entry.
func cacheWeight(key string, value []byte) uint64 {
	return uint64(len(key) + len(value)) //nolint:gosec // lengths non-negative
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
	logger *slog.Logger,
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
		logger.Warn("incomplete keymap initialization detected, deleting keymap directory",
			"directory", keymapDirectory)

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
		err := os.MkdirAll(keymapDirectory, 0750)
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
			err = os.MkdirAll(keymapDirectory, 0750)
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
		f, err := os.Create(keymapInitializedFile) //nolint:gosec // path within keymap directory
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
	runtimeConfig *litt.RuntimeConfig,
	name string,
	tableConfig litt.TableConfig,
	metrics *metrics.LittDBMetrics) (litt.ManagedTable, error) {

	var table litt.ManagedTable

	if err := tableConfig.Validate(); err != nil {
		return nil, fmt.Errorf("invalid table config: %w", err)
	}

	kmap, keymapDirectory, keymapTypeFile, requiresReload, err := buildKeymap(config, runtimeConfig.Logger, name)
	if err != nil {
		return nil, fmt.Errorf("error creating keymap: %w", err)
	}

	table, err = disktable.NewDiskTable(
		config,
		runtimeConfig,
		name,
		tableConfig,
		kmap,
		keymapDirectory,
		keymapTypeFile,
		config.Paths,
		requiresReload,
		metrics)

	if err != nil {
		return nil, fmt.Errorf("error creating table: %w", err)
	}

	writeCache := util.NewFIFOCache[string, []byte](
		tableConfig.WriteCacheSize, cacheWeight, metrics.GetWriteCacheMetrics())
	writeCache = util.NewThreadSafeCache(writeCache)

	readCache := util.NewFIFOCache[string, []byte](
		tableConfig.ReadCacheSize, cacheWeight, metrics.GetReadCacheMetrics())
	readCache = util.NewThreadSafeCache(readCache)

	cachedTable := dbcache.NewCachedTable(table, writeCache, readCache, metrics)

	return cachedTable, nil
}

// buildMetrics creates a new metrics object backed by the global OTel
// MeterProvider. It records into whatever MeterProvider is globally configured.
//
// When MetricsServeEndpoint is true, this also configures the global provider
// with a Prometheus exporter and starts an HTTP server on MetricsPort that
// serves /metrics; the returned shutdown function flushes that provider and is
// the responsibility of the caller to invoke during teardown. When
// MetricsServeEndpoint is false (the default), the embedding application is
// assumed to have already configured and served the global provider, so no
// exporter or server is created and the returned shutdown function is nil.
func buildMetrics(
	config *litt.Config,
	runtimeConfig *litt.RuntimeConfig,
) (*metrics.LittDBMetrics, func(context.Context) error) {
	if !config.MetricsEnabled {
		return nil, nil
	}

	if !config.MetricsServeEndpoint {
		return metrics.NewLittDBMetrics(), nil
	}

	reg, shutdown, err := commonmetrics.SetupOtelPrometheus()
	if err != nil {
		runtimeConfig.Logger.Error("failed to set up OTel Prometheus exporter", "error", err)
		return nil, nil
	}

	addr := fmt.Sprintf(":%d", config.MetricsPort)
	runtimeConfig.Logger.Info("Starting metrics server", "port", config.MetricsPort)
	commonmetrics.StartMetricsServer(runtimeConfig.CTX, reg, addr)

	return metrics.NewLittDBMetrics(), shutdown
}
