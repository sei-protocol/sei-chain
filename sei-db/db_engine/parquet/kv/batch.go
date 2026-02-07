package parquet

const (
	currentVersionKey = "s/_meta/version" // the key name of the version.
	VersionSize       = 8                 // the number of bytes needed to store the version.
)

// Batch accumulates records for batch writing to the parquet store.
type Batch struct {
	db      *Database
	records []Record
	version int64
}

// NewBatch creates a new batch for the given database.
func NewBatch(db *Database) *Batch {
	return &Batch{
		db:      db,
		records: make([]Record, 0),
	}
}

// Size returns the number of records in the batch.
func (b *Batch) Size() int {
	return len(b.records)
}

// Reset clears all records from the batch.
func (b *Batch) Reset() {
	b.records = b.records[:0]
}

// Set adds a key-value pair to the batch with an empty store key.
func (b *Batch) Set(key, value []byte) error {
	b.records = append(b.records, Record{
		Key:     key,
		Value:   value,
		Version: b.version,
	})
	return nil
}

// SetByStore adds a key-value pair to the batch with the given store key.
func (b *Batch) SetByStore(storeKey string, key, value []byte) error {
	b.records = append(b.records, Record{
		StoreKey: storeKey,
		Key:      key,
		Value:    value,
		Version:  b.version,
	})
	return nil
}

// SetCurrentVersion sets the version for subsequent records in this batch.
func (b *Batch) SetCurrentVersion(version uint64) error {
	b.version = int64(version)
	return nil
}

// Delete is a no-op for append-only parquet store.
// Records are never deleted; use version filtering in DuckDB queries instead.
func (b *Batch) Delete(key []byte) error {
	return nil
}

// DeleteByStore is a no-op for append-only parquet store.
// Records are never deleted; use version filtering in DuckDB queries instead.
func (b *Batch) DeleteByStore(storeKey string, key []byte) error {
	return nil
}

// Commit writes all accumulated records to the parquet file.
func (b *Batch) Commit() error {
	if len(b.records) == 0 {
		return nil
	}

	err := b.db.WriteRecords(b.records)
	if err != nil {
		return err
	}

	// Clear the batch after successful commit
	b.Reset()
	return nil
}
