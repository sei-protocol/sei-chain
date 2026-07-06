package littbuilder

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

var _ litt.DB = &db{}

// db is an implementation of DB.
type db struct {
	// The serializable configuration for the database. Tables are built from this config.
	config *litt.Config

	// The non-serializable runtime dependencies for the database.
	runtimeConfig *litt.RuntimeConfig

	// A map of all tables in the database.
	tables map[string]litt.ManagedTable

	// Protects access to tables.
	lock sync.Mutex

	// True if the database has been stopped.
	stopped atomic.Bool

	// Metrics for the database.
	metrics *metrics.LittDBMetrics

	// Shuts down the OTel MeterProvider configured by buildMetrics. nil if metrics are disabled.
	metricsShutdown func(context.Context) error

	// A function that releases file locks.
	releaseLocks func()

	// Set to true when the database is closed.
	closed bool
}

// NewDB creates a new DB instance. After this method is called, the config object should not be modified.
// At most one RuntimeConfig may be provided. If none is provided, a default RuntimeConfig is used.
func NewDB(config *litt.Config, runtimeConfig ...*litt.RuntimeConfig) (litt.DB, error) {
	if len(runtimeConfig) > 1 {
		return nil, fmt.Errorf("at most one RuntimeConfig may be provided, got %d", len(runtimeConfig))
	}

	rc := litt.DefaultRuntimeConfig()
	if len(runtimeConfig) == 1 && runtimeConfig[0] != nil {
		rc = runtimeConfig[0]
	}
	if err := rc.Validate(); err != nil {
		return nil, fmt.Errorf("error validating runtime config: %w", err)
	}

	err := config.Validate()
	if err != nil {
		return nil, fmt.Errorf("error checking config: %w", err)
	}

	err = config.SanitizePaths()
	if err != nil {
		return nil, fmt.Errorf("error expanding tildes in config: %w", err)
	}

	if !config.Fsync {
		rc.Logger.Warn(
			"Fsync is disabled. Ok for unit tests that need to run fast, NOT OK FOR PRODUCTION USE.")
	}

	return NewDBUnsafe(config, rc)
}

// NewDBUnsafe creates a new DB instance without validating or sanitizing the provided config. This is intended
// for unit test use, and should not be considered a stable API. If runtimeConfig is nil, a default
// RuntimeConfig is used.
func NewDBUnsafe(
	config *litt.Config,
	runtimeConfig *litt.RuntimeConfig,
) (litt.DB, error) {
	if runtimeConfig == nil {
		runtimeConfig = litt.DefaultRuntimeConfig()
	}
	if err := runtimeConfig.Validate(); err != nil {
		return nil, fmt.Errorf("error validating runtime config: %w", err)
	}

	for _, rootPath := range config.Paths {
		err := util.EnsureDirectoryExists(rootPath, config.Fsync)
		if err != nil {
			return nil, fmt.Errorf("error ensuring directory %s exists: %w", rootPath, err)
		}
	}

	if config.PurgeLocks {
		runtimeConfig.Logger.Warn("Purging LittDB locks", "paths", config.Paths)
		err := disktable.Unlock(runtimeConfig.Logger, config.Paths)
		if err != nil {
			return nil, fmt.Errorf("error purging locks: %w", err)
		}
		runtimeConfig.Logger.Info("Locks purged successfully")
	} else {
		runtimeConfig.Logger.Info("Not purging locks, continuing with existing locks")
	}

	releaseLocks, err := util.LockDirectories(runtimeConfig.Logger, config.Paths, util.LockfileName, config.Fsync)
	if err != nil {
		return nil, fmt.Errorf("error acquiring locks on paths %v: %w", config.Paths, err)
	}

	var dbMetrics *metrics.LittDBMetrics
	var metricsShutdown func(context.Context) error
	if config.MetricsEnabled {
		dbMetrics, metricsShutdown = buildMetrics(config, runtimeConfig)
	}

	if config.SnapshotDirectory != "" {
		runtimeConfig.Logger.Info("LittDB rolling snapshots enabled",
			"directory", config.SnapshotDirectory)
	}

	database := &db{
		config:          config,
		runtimeConfig:   runtimeConfig,
		tables:          make(map[string]litt.ManagedTable),
		metrics:         dbMetrics,
		metricsShutdown: metricsShutdown,
		releaseLocks:    releaseLocks,
	}

	if config.MetricsEnabled {
		go database.gatherMetrics(config.MetricsUpdateInterval)
	}

	return database, nil
}

func (d *db) lockFreeSize() uint64 {
	size := uint64(0)
	for _, table := range d.tables {
		size += table.Size()
	}

	return size
}

// pruneDroppedTables removes any tables that have been dropped (see Table.Drop) from d.tables. The caller
// must hold d.lock. This is called by methods that should not operate on dropped tables, centralizing the
// bookkeeping rather than checking IsDropped at every iteration site.
func (d *db) pruneDroppedTables() {
	for name, table := range d.tables {
		if table.IsDropped() {
			delete(d.tables, name)
		}
	}
}

func (d *db) BuildTable(config litt.TableConfig) (litt.Table, error) {
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("error validating table config: %w", err)
	}

	d.lock.Lock()
	defer d.lock.Unlock()

	// Forget any dropped tables so a previously dropped name can be reused.
	d.pruneDroppedTables()

	if _, ok := d.tables[config.Name]; ok {
		return nil, fmt.Errorf("table '%s' is already open", config.Name)
	}

	table, err := buildTable(d.config, d.runtimeConfig, config.Name, config, d.metrics)
	if err != nil {
		return nil, fmt.Errorf("error creating table: %w", err)
	}
	d.runtimeConfig.Logger.Info("Table initialized",
		"table", config.Name,
		"keys", table.KeyCount(),
		"size", util.PrettyPrintBytes(table.Size()),
	)

	d.tables[config.Name] = table

	return table, nil
}

func (d *db) Close() error {
	d.lock.Lock()
	defer d.lock.Unlock()
	return d.closeUnsafe()
}

func (d *db) closeUnsafe() error {
	if d.closed {
		// closing more than once is a no-op
		return nil
	}

	d.pruneDroppedTables()

	d.runtimeConfig.Logger.Info("Stopping LittDB", "size", d.lockFreeSize())
	d.stopped.Store(true)

	for name, table := range d.tables {
		err := table.Close()
		if err != nil {
			return fmt.Errorf("error stopping table %s: %w", name, err)
		}
	}

	d.releaseLocks()

	d.closed = true

	return nil
}

func (d *db) Destroy() error {
	d.lock.Lock()
	defer d.lock.Unlock()

	err := d.closeUnsafe()
	if err != nil {
		return fmt.Errorf("error closing database: %w", err)
	}

	// closeUnsafe already pruned dropped tables; drop the rest.
	for name, table := range d.tables {
		err := table.Drop()
		if err != nil {
			return fmt.Errorf("error dropping table %s: %w", name, err)
		}
	}

	return nil
}

// gatherMetrics is a method that periodically collects metrics.
func (d *db) gatherMetrics(interval time.Duration) {
	if d.metricsShutdown != nil {
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := d.metricsShutdown(shutdownCtx)
			if err != nil {
				d.runtimeConfig.Logger.Error("error shutting down metrics provider", "error", err)
			}
		}()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for !d.stopped.Load() {
		select {
		case <-d.runtimeConfig.CTX.Done():
			return
		case <-ticker.C:
			d.lock.Lock()
			d.pruneDroppedTables()
			tablesCopy := make(map[string]litt.ManagedTable, len(d.tables))
			for name, table := range d.tables {
				tablesCopy[name] = table
			}
			d.lock.Unlock()

			d.metrics.CollectPeriodicMetrics(tablesCopy)
		}
	}
}
