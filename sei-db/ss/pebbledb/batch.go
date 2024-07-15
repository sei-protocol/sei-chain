package pebbledb

import (
	"encoding/binary"
	"fmt"

	"github.com/cockroachdb/pebble"
	"github.com/sei-protocol/sei-db/common/errors"
)

type Batch struct {
	storage *pebble.DB
	batch   *pebble.Batch
	version int64
}

func NewBatch(storage *pebble.DB, version int64) (*Batch, error) {
	var versionBz [VersionSize]byte
	binary.LittleEndian.PutUint64(versionBz[:], uint64(version))

	batch := storage.NewBatch()

	if err := batch.Set([]byte(latestVersionKey), versionBz[:], nil); err != nil {
		return nil, fmt.Errorf("failed to write PebbleDB batch: %w", err)
	}

	return &Batch{
		storage: storage,
		batch:   batch,
		version: version,
	}, nil
}

func (b *Batch) Size() int {
	return b.batch.Len()
}

func (b *Batch) Reset() {
	b.batch.Reset()
}

func (b *Batch) set(storeKey string, tombstone int64, key, value []byte) error {
	prefixedKey := MVCCEncode(prependStoreKey(storeKey, key), b.version)
	prefixedVal := MVCCEncode(value, tombstone)

	if err := b.batch.Set(prefixedKey, prefixedVal, nil); err != nil {
		return fmt.Errorf("failed to write PebbleDB batch: %w", err)
	}

	return nil
}

func (b *Batch) Set(storeKey string, key, value []byte) error {
	return b.set(storeKey, 0, key, value)
}

func (b *Batch) Delete(storeKey string, key []byte) error {
	return b.set(storeKey, b.version, key, []byte(tombstoneVal))
}

func (b *Batch) Write() (err error) {
	defer func() {
		err = errors.Join(err, b.batch.Close())
	}()

	return b.batch.Commit(defaultWriteOpts)
}

// For writing kv pairs in any order of version
type RawBatch struct {
	storage *pebble.DB
	batch   *pebble.Batch
}

func NewRawBatch(storage *pebble.DB) (*RawBatch, error) {
	batch := storage.NewBatch()

	return &RawBatch{
		storage: storage,
		batch:   batch,
	}, nil
}

func (b *RawBatch) Size() int {
	return b.batch.Len()
}

func (b *RawBatch) Reset() {
	b.batch.Reset()
}

func (b *RawBatch) set(storeKey string, tombstone int64, key, value []byte, version int64) error {
	prefixedKey := MVCCEncode(prependStoreKey(storeKey, key), version)
	prefixedVal := MVCCEncode(value, tombstone)

	if err := b.batch.Set(prefixedKey, prefixedVal, nil); err != nil {
		return fmt.Errorf("failed to write PebbleDB batch: %w", err)
	}

	return nil
}

func (b *RawBatch) Set(storeKey string, key, value []byte, version int64) error {
	return b.set(storeKey, 0, key, value, version)
}

func (b *RawBatch) Delete(storeKey string, key []byte, version int64) error {
	return b.set(storeKey, version, key, []byte(tombstoneVal), version)
}

func (b *RawBatch) Write() (err error) {
	defer func() {
		err = errors.Join(err, b.batch.Close())
	}()

	return b.batch.Commit(defaultWriteOpts)
}
