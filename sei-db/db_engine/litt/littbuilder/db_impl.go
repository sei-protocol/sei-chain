package littbuilder

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/disktable"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

var _ litt.DB = &db{}

// TableBuilderFunc is a function that creates a new table.
type TableBuilderFunc func(
	ctx context.Context,
	logger *slog.Logger,
	name string,
	metrics *metrics.LittDBMetrics) (litt.ManagedTable, error)

// db is an implementation of DB.
type db struct {
	ctx    context.Context
	logger *slog.Logger

	// A function that returns the current time.
	clock func() time.Time

	// The default time-to-live for new tables. Once created, the TTL for a table can be changed.
	ttl time.Duration

	// The period between garbage collection runs.
	gcPeriod time.Duration

	// A function that creates new tables.
	tableBuilder TableBuilderFunc

	// A map of all tables in the database.
	tables map[string]litt.ManagedTable

	// Protects access to tables and ttl.
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
func NewDB(config *litt.Config) (litt.DB, error) {
	config.Logger = buildLogger(config)

	err := config.SanityCheck()
	if err != nil {
		return nil, fmt.Errorf("error checking config: %w", err)
	}

	err = config.SanitizePaths()
	if err != nil {
		return nil, fmt.Errorf("error expanding tildes in config: %w", err)
	}

	if !config.Fsync {
		config.Logger.Warn(
			"Fsync is disabled. Ok for unit tests that need to run fast, NOT OK FOR PRODUCTION USE.")
	}

	tableBuilder := func(
		ctx context.Context,
		logger *slog.Logger,
		name string,
		metrics *metrics.LittDBMetrics) (litt.ManagedTable, error) {

		return buildTable(config, logger, name, metrics)
	}

	return NewDBUnsafe(config, tableBuilder)
}

// NewDBUnsafe creates a new DB instance with a custom table builder. This is intended for unit test use,
// and should not be considered a stable API.
func NewDBUnsafe(config *litt.Config, tableBuilder TableBuilderFunc) (litt.DB, error) {
	for _, rootPath := range config.Paths {
		err := util.EnsureDirectoryExists(rootPath, config.Fsync)
		if err != nil {
			return nil, fmt.Errorf("error ensuring directory %s exists: %w", rootPath, err)
		}
	}

	if config.PurgeLocks {
		config.Logger.Warn("Purging LittDB locks", "paths", config.Paths)
		err := disktable.Unlock(config.Logger, config.Paths)
		if err != nil {
			return nil, fmt.Errorf("error purging locks: %w", err)
		}
		config.Logger.Info("Locks purged successfully")
	} else {
		config.Logger.Info("Not purging locks, continuing with existing locks")
	}

	releaseLocks, err := util.LockDirectories(config.Logger, config.Paths, util.LockfileName, config.Fsync)
	if err != nil {
		return nil, fmt.Errorf("error acquiring locks on paths %v: %w", config.Paths, err)
	}

	if config.Logger == nil {
		config.Logger = buildLogger(config)
	}

	var dbMetrics *metrics.LittDBMetrics
	var metricsShutdown func(context.Context) error
	if config.MetricsEnabled {
		dbMetrics, metricsShutdown = buildMetrics(config, config.Logger)
	}

	if config.SnapshotDirectory != "" {
		config.Logger.Info("LittDB rolling snapshots enabled",
			"directory", config.SnapshotDirectory)
	}

	database := &db{
		ctx:             config.CTX,
		logger:          config.Logger,
		clock:           config.Clock,
		ttl:             config.TTL,
		gcPeriod:        config.GCPeriod,
		tableBuilder:    tableBuilder,
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

func (d *db) KeyCount() uint64 {
	d.lock.Lock()
	defer d.lock.Unlock()

	count := uint64(0)
	for _, table := range d.tables {
		count += table.KeyCount()
	}

	return count
}

func (d *db) Size() uint64 {
	d.lock.Lock()
	defer d.lock.Unlock()

	return d.lockFreeSize()
}

func (d *db) lockFreeSize() uint64 {
	size := uint64(0)
	for _, table := range d.tables {
		size += table.Size()
	}

	return size
}

func (d *db) GetTable(name string) (litt.Table, error) {
	d.lock.Lock()
	defer d.lock.Unlock()

	table, ok := d.tables[name]
	if !ok {
		if !litt.IsTableNameValid(name) {
			return nil, fmt.Errorf(
				"table name '%s' is invalid, must be at least one character long and "+
					"contain only letters, numbers, and underscores, and dashes", name)
		}

		var err error
		table, err = d.tableBuilder(d.ctx, d.logger, name, d.metrics)
		if err != nil {
			return nil, fmt.Errorf("error creating table: %w", err)
		}
		d.logger.Info("Table initialized",
			"table", name,
			"keys", table.KeyCount(),
			"size", util.PrettyPrintBytes(table.Size()),
		)

		d.tables[name] = table
	}

	return table, nil
}

func (d *db) DropTable(name string) error {
	d.lock.Lock()
	defer d.lock.Unlock()

	table, ok := d.tables[name]
	if !ok {
		// Table does not exist, nothing to do.
		d.logger.Info("table does not exist, cannot drop", "table", name)
		return nil
	}

	d.logger.Info("dropping table", "table", name)
	err := table.Destroy()
	if err != nil {
		return fmt.Errorf("error destroying table: %w", err)
	}
	delete(d.tables, name)

	return nil
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

	d.logger.Info("Stopping LittDB", "size", d.lockFreeSize())
	d.stopped.Store(true)

	for name, table := range d.tables {
		err := table.Close()
		if err != nil {
			return fmt.Errorf("error stopping table %s: %w", name, err)
		}
	}

	d.releaseLocks()

	return nil
}

func (d *db) Destroy() error {
	d.lock.Lock()
	defer d.lock.Unlock()

	err := d.closeUnsafe()
	if err != nil {
		return fmt.Errorf("error closing database: %w", err)
	}

	for name, table := range d.tables {
		err := table.Destroy()
		if err != nil {
			return fmt.Errorf("error destroying table %s: %w", name, err)
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
				d.logger.Error("error shutting down metrics provider", "error", err)
			}
		}()
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for !d.stopped.Load() {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.lock.Lock()
			tablesCopy := make(map[string]litt.ManagedTable, len(d.tables))
			for name, table := range d.tables {
				tablesCopy[name] = table
			}
			d.lock.Unlock()

			d.metrics.CollectPeriodicMetrics(tablesCopy)
		}
	}
}
