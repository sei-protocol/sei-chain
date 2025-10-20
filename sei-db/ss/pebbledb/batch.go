package pebbledb

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/sei-protocol/sei-db/common/errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Batch struct {
	storage *pebble.DB
	batch   *pebble.Batch
	version int64
}

func NewBatch(storage *pebble.DB, version int64) (*Batch, error) {
	if version < 0 {
		return nil, fmt.Errorf("version must be non-negative")
	}
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
	startTime := time.Now()
	batchSize := int64(b.batch.Len())

	defer func() {
		ctx := context.Background()
		Metrics.BatchWriteLatency.Record(
			ctx,
			time.Since(startTime).Seconds(),
			metric.WithAttributes(attribute.Bool("success", err == nil)),
		)
		Metrics.BatchSize.Record(
			ctx,
			batchSize,
		)
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

// HardDelete physically removes the key by encoding it with the batch’s version
// and calling the underlying pebble.Batch.Delete.
func (b *Batch) HardDelete(storeKey string, key []byte) error {
	fullKey := MVCCEncode(prependStoreKey(storeKey, key), b.version)
	if err := b.batch.Delete(fullKey, nil); err != nil {
		return fmt.Errorf("failed to hard delete key: %w", err)
	}
	return nil
}

func (b *RawBatch) Write() (err error) {
	startTime := time.Now()
	batchSize := int64(b.batch.Len())
	defer func() {
		ctx := context.Background()
		Metrics.BatchWriteLatency.Record(
			ctx,
			time.Since(startTime).Seconds(),
			metric.WithAttributes(attribute.Bool("success", err == nil)),
		)
		Metrics.BatchSize.Record(
			ctx,
			batchSize,
		)
		err = errors.Join(err, b.batch.Close())
	}()

	return b.batch.Commit(defaultWriteOpts)
}
