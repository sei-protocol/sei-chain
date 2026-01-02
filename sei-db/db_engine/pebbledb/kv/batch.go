package pebbledb

import (
	"encoding/binary"
	goerrors "errors"
	"fmt"

	"github.com/cockroachdb/pebble"
)

const (
	currentVersionKey = "s/_meta/version" // the key name of the version.
	VersionSize       = 8                 // the number of bytes needed to store the version.
)

// Batch is a set of modifications to apply to the store.
type Batch struct {
	db       *pebble.DB
	batch    *pebble.Batch
	writeOps *pebble.WriteOptions
}

// NewBatch creates new batch.
func NewBatch(db *pebble.DB) *Batch {
	batch := db.NewBatch()
	return &Batch{
		db:       db,
		batch:    batch,
		writeOps: pebble.NoSync,
	}
}

// Size returns number of operations in the batch.
func (b *Batch) Size() int {
	return b.batch.Len()
}

// Reset resets the batch.
func (b *Batch) Reset() {
	b.batch.Reset()
}

// Set sets key.
func (b *Batch) Set(key, value []byte) error {
	if err := b.batch.Set(key, value, nil); err != nil {
		return fmt.Errorf("failed to write PebbleDB batch: %w", err)
	}
	return nil
}

// SetCurrentVersion sets the version metadata.
func (b *Batch) SetCurrentVersion(version uint64) error {
	var versionBz [VersionSize]byte
	binary.LittleEndian.PutUint64(versionBz[:], version)
	if err := b.batch.Set([]byte(currentVersionKey), versionBz[:], nil); err != nil {
		return fmt.Errorf("failed to write current version to PebbleDB batch: %w", err)
	}
	return nil
}

// Delete deletes key from the store.
func (b *Batch) Delete(key []byte) error {
	return b.batch.Delete(key, nil)
}

// Commit commits changes.
func (b *Batch) Commit() (err error) {
	defer func() {
		err = goerrors.Join(err, b.batch.Close())
	}()

	return b.batch.Commit(b.writeOps)
}

// SetByStore sets key in the store.
func (b *Batch) SetByStore(storeKey string, key, value []byte) error {
	prefixedKey := prependStoreKey(storeKey, key)
	if err := b.batch.Set(prefixedKey, value, nil); err != nil {
		return fmt.Errorf("failed to write PebbleDB batch: %w", err)
	}
	return nil
}

// DeleteByStore deletes key from the store.
func (b *Batch) DeleteByStore(storeKey string, key []byte) error {
	prefixedKey := prependStoreKey(storeKey, key)
	return b.batch.Delete(prefixedKey, nil)
}

func getStorePrefix(storeKey string) []byte {
	return []byte(fmt.Sprintf("s/k:%s/", storeKey))
}

func prependStoreKey(storeKey string, key []byte) []byte {
	if storeKey == "" {
		return key
	}
	return append(getStorePrefix(storeKey), key...)
}
