package mvcc

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/sei-protocol/sei-chain/sei-db/common/errors"
	pebbledbmetrics "github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Batch struct {
	storage          *pebble.DB
	batch            *pebble.Batch
	version          int64
	operationMetrics *pebbledbmetrics.OperationMetrics
}

func NewBatch(storage *pebble.DB, version int64, operationMetrics ...*pebbledbmetrics.OperationMetrics) (*Batch, error) {
	if version < 0 {
		return nil, fmt.Errorf("version must be non-negative")
	}
	var metrics *pebbledbmetrics.OperationMetrics
	if len(operationMetrics) > 0 {
		metrics = operationMetrics[0]
	}
	var versionBz [VersionSize]byte
	binary.LittleEndian.PutUint64(versionBz[:], uint64(version))

	batch := storage.NewBatch()

	if err := batch.Set([]byte(latestVersionKey), versionBz[:], nil); err != nil {
		return nil, fmt.Errorf("failed to write PebbleDB batch: %w", err)
	}

	return &Batch{
		storage:          storage,
		batch:            batch,
		version:          version,
		operationMetrics: metrics,
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
	writeCount := int64(b.batch.Count())

	defer func() {
		err = errors.Join(err, b.batch.Close())
		ctx := context.Background()
		otelMetrics.batchWriteLatency.Record(
			ctx,
			time.Since(startTime).Seconds(),
			metric.WithAttributes(attribute.Bool("success", err == nil)),
		)
		otelMetrics.batchSize.Record(
			ctx,
			batchSize,
		)
	}()

	if err := b.batch.Commit(defaultWriteOpts); err != nil {
		return err
	}
	b.operationMetrics.AddWrite(writeCount)
	return nil
}

// For writing kv pairs in any order of version
type RawBatch struct {
	storage          *pebble.DB
	batch            *pebble.Batch
	operationMetrics *pebbledbmetrics.OperationMetrics
}

func NewRawBatch(storage *pebble.DB, operationMetrics ...*pebbledbmetrics.OperationMetrics) (*RawBatch, error) {
	var metrics *pebbledbmetrics.OperationMetrics
	if len(operationMetrics) > 0 {
		metrics = operationMetrics[0]
	}
	batch := storage.NewBatch()

	return &RawBatch{
		storage:          storage,
		batch:            batch,
		operationMetrics: metrics,
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
	writeCount := int64(b.batch.Count())
	defer func() {
		err = errors.Join(err, b.batch.Close())
		ctx := context.Background()
		otelMetrics.batchWriteLatency.Record(
			ctx,
			time.Since(startTime).Seconds(),
			metric.WithAttributes(attribute.Bool("success", err == nil)),
		)
		otelMetrics.batchSize.Record(
			ctx,
			batchSize,
		)
	}()

	if err := b.batch.Commit(defaultWriteOpts); err != nil {
		return err
	}
	b.operationMetrics.AddWrite(writeCount)
	return nil
}
