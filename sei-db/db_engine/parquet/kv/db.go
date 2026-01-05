package parquet

import (
	"errors"
	"os"
	"sync"

	"github.com/parquet-go/parquet-go"
)

// ErrReadNotSupported is returned when attempting to read from the parquet store.
// Use DuckDB to query parquet files instead.
var ErrReadNotSupported = errors.New("parquet store is write-only; use DuckDB to read")

// Record represents a single key-value entry in the parquet file.
// This schema is optimized for DuckDB queries.
type Record struct {
	StoreKey string `parquet:"store_key,snappy"`
	Key      []byte `parquet:"key,snappy"`
	Value    []byte `parquet:"value,snappy"`
	Version  int64  `parquet:"version,snappy"`
}

// Database represents a parquet-based append-only KV store.
type Database struct {
	path   string
	file   *os.File
	writer *parquet.GenericWriter[Record]
	mu     sync.Mutex
}

// OpenDB opens or creates a parquet file for append-only writes.
func OpenDB(dbPath string) (*Database, error) {
	file, err := os.OpenFile(dbPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}

	writer := parquet.NewGenericWriter[Record](file)

	return &Database{
		path:   dbPath,
		file:   file,
		writer: writer,
	}, nil
}

// Get is not supported for parquet store. Use DuckDB to query the parquet file.
func (db *Database) Get(key []byte) ([]byte, error) {
	return nil, ErrReadNotSupported
}

// Has is not supported for parquet store. Use DuckDB to query the parquet file.
func (db *Database) Has(key []byte) (bool, error) {
	return false, ErrReadNotSupported
}

// Set writes a single key-value pair to the parquet file.
// For better performance, use Batch for multiple writes.
func (db *Database) Set(key []byte, value []byte) error {
	db.mu.Lock()
	defer db.mu.Unlock()

	record := Record{
		Key:   key,
		Value: value,
	}

	_, err := db.writer.Write([]Record{record})
	return err
}

// WriteRecords writes multiple records to the parquet file.
// This is the primary method for batch writes.
func (db *Database) WriteRecords(records []Record) error {
	if len(records) == 0 {
		return nil
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	_, err := db.writer.Write(records)
	return err
}

// Flush flushes any buffered data to disk.
func (db *Database) Flush() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	return db.writer.Flush()
}

// Close flushes and closes the parquet writer and file.
func (db *Database) Close() error {
	db.mu.Lock()
	defer db.mu.Unlock()

	if err := db.writer.Close(); err != nil {
		_ = db.file.Close()
		return err
	}
	return db.file.Close()
}
