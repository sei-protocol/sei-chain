package litt

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"log/slog"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/metrics"
	"github.com/sei-protocol/sei-chain/sei-db/db_engine/litt/util"
)

var _ DB = &db{}

// db is an implementation of DB.
type db struct {
	ctx    context.Context
	config *Config
	logger *slog.Logger

	// A map of all tables in the database.
	tables map[string]ManagedTable

	// Protects access to tables.
	lock sync.Mutex

	// True if the database has been stopped.
	stopped atomic.Bool

	// Metrics for the database.
	metrics *metrics.LittDBMetrics

	// A function that releases file locks.
	releaseLocks func()

	// Set to true when the database is closed.
	closed bool
}

// NewDB creates a new DB instance. After this method is called, the config object should not be modified.
func NewDB(config *Config) (DB, error) {
	if config.Logger == nil {
		var err error
		config.Logger, err = buildLogger(config)
		if err != nil {
			return nil, fmt.Errorf("error building logger: %w", err)
		}
	}

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

	for _, rootPath := range config.Paths {
		err := util.EnsureDirectoryExists(rootPath, config.Fsync)
		if err != nil {
			return nil, fmt.Errorf("error ensuring directory %s exists: %w", rootPath, err)
		}
	}

	if config.PurgeLocks {
		config.Logger.Warn(fmt.Sprintf("Purging LittDB locks from paths %v", config.Paths))
		err := Unlock(config.Logger, config.Paths)
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

	dbMetrics := metrics.NewLittDBMetrics()

	if config.SnapshotDirectory != "" {
		config.Logger.Info(fmt.Sprintf("LittDB rolling snapshots enabled, snapshot data will be stored in %s",
			config.SnapshotDirectory))
	}

	database := &db{
		ctx:          config.CTX,
		config:       config,
		logger:       config.Logger,
		tables:       make(map[string]ManagedTable),
		metrics:      dbMetrics,
		releaseLocks: releaseLocks,
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

func (d *db) GetTable(name string) (Table, error) {
	d.lock.Lock()
	defer d.lock.Unlock()

	table, ok := d.tables[name]
	if !ok {
		if !IsTableNameValid(name) {
			return nil, fmt.Errorf(
				"table name '%s' is invalid, must be at least one character long and "+
					"contain only letters, numbers, and underscores, and dashes", name)
		}

		var err error
		table, err = buildTable(d.config, d.logger, name, d.metrics)
		if err != nil {
			return nil, fmt.Errorf("error creating table: %w", err)
		}
		d.logger.Info(fmt.Sprintf(
			"Table '%s' initialized, table contains %d key-value pairs and has a size of %s.",
			name, table.KeyCount(), util.PrettyPrintBytes(table.Size())))

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
		d.logger.Info(fmt.Sprintf("table %s does not exist, cannot drop", name))
		return nil
	}

	d.logger.Info(fmt.Sprintf("dropping table %s", name))
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

	d.logger.Info(fmt.Sprintf("Stopping LittDB, estimated data size: %d", d.lockFreeSize()))
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

// gatherMetrics periodically snapshots table-level gauge metrics (size, key count).
func (d *db) gatherMetrics(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for !d.stopped.Load() {
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			d.lock.Lock()
			tables := make([]metrics.TableInfo, 0, len(d.tables))
			for _, table := range d.tables {
				tables = append(tables, table)
			}
			d.lock.Unlock()

			d.metrics.CollectPeriodicMetrics(tables)
		}
	}
}
