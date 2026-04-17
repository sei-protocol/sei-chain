package mvcc

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Batch struct {
	storage    *pebble.DB
	version    int64
	ops        []batchOp
	descending bool
}

type batchOp struct {
	key    []byte
	value  []byte
	delete bool
}

// NewBatch creates a new descending-mode Batch. Callers that need ascending-mode
// encoding for legacy DBs should use NewBatchWithMode.
func NewBatch(storage *pebble.DB, version int64) (*Batch, error) {
	return NewBatchWithMode(storage, version, true)
}

// NewBatchWithMode creates a new Batch using the supplied MVCC encoding mode.
func NewBatchWithMode(storage *pebble.DB, version int64, descending bool) (*Batch, error) {
	if version < 0 {
		return nil, fmt.Errorf("version must be non-negative")
	}
	b := &Batch{
		storage:    storage,
		version:    version,
		ops:        make([]batchOp, 0, 16),
		descending: descending,
	}
	return b, nil
}

func (b *Batch) Size() int {
	return len(b.ops)
}

func (b *Batch) Reset() {
	b.ops = b.ops[:0]
}

func (b *Batch) set(storeKey string, tombstone int64, key, value []byte) error {
	prefixedKey := MVCCEncode(prependStoreKey(storeKey, key), b.version, b.descending)
	prefixedVal := MVCCEncode(value, tombstone, b.descending)

	b.appendSet(prefixedKey, prefixedVal)
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
	batchSize := int64(len(b.ops))

	defer func() {
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

	batch := b.storage.NewBatch()
	defer func() {
		err = errors.Join(err, batch.Close())
	}()
	sortBatchOps(b.ops)
	for _, op := range b.ops {
		if op.delete {
			if e := batch.Delete(op.key, nil); e != nil {
				return fmt.Errorf("failed to delete in PebbleDB batch: %w", e)
			}
			continue
		}
		if e := batch.Set(op.key, op.value, nil); e != nil {
			return fmt.Errorf("failed to write PebbleDB batch: %w", e)
		}
	}
	var versionBz [VersionSize]byte
	binary.LittleEndian.PutUint64(versionBz[:], uint64(b.version)) //nolint:gosec // block heights are non-negative and fit in int64
	if err := batch.Set([]byte(latestVersionKey), versionBz[:], nil); err != nil {
		return fmt.Errorf("failed to set latest version in batch: %w", err)
	}
	return batch.Commit(defaultWriteOpts)
}

// For writing kv pairs in any order of version
type RawBatch struct {
	storage    *pebble.DB
	ops        []batchOp
	descending bool
}

// NewRawBatch creates a new descending-mode RawBatch.
func NewRawBatch(storage *pebble.DB) (*RawBatch, error) {
	return NewRawBatchWithMode(storage, true)
}

// NewRawBatchWithMode creates a new RawBatch using the supplied MVCC encoding
// mode.
func NewRawBatchWithMode(storage *pebble.DB, descending bool) (*RawBatch, error) {
	return &RawBatch{
		storage:    storage,
		ops:        make([]batchOp, 0, 16),
		descending: descending,
	}, nil
}

func (b *RawBatch) Size() int {
	return len(b.ops)
}

func (b *RawBatch) Reset() {
	b.ops = b.ops[:0]
}

func (b *RawBatch) set(storeKey string, tombstone int64, key, value []byte, version int64) error {
	prefixedKey := MVCCEncode(prependStoreKey(storeKey, key), version, b.descending)
	prefixedVal := MVCCEncode(value, tombstone, b.descending)

	b.appendSet(prefixedKey, prefixedVal)
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
	fullKey := MVCCEncode(prependStoreKey(storeKey, key), b.version, b.descending)
	b.appendDelete(fullKey)
	return nil
}

func (b *RawBatch) Write() (err error) {
	startTime := time.Now()
	batchSize := int64(len(b.ops))
	defer func() {
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

	batch := b.storage.NewBatch()
	defer func() {
		err = errors.Join(err, batch.Close())
	}()
	sortBatchOps(b.ops)
	for _, op := range b.ops {
		if op.delete {
			if e := batch.Delete(op.key, nil); e != nil {
				return fmt.Errorf("failed to delete in PebbleDB batch: %w", e)
			}
			continue
		}
		if e := batch.Set(op.key, op.value, nil); e != nil {
			return fmt.Errorf("failed to write PebbleDB batch: %w", e)
		}
	}
	return batch.Commit(defaultWriteOpts)
}

func (b *Batch) appendSet(key, value []byte) {
	b.ops = append(b.ops, batchOp{
		key:   append([]byte(nil), key...),
		value: append([]byte(nil), value...),
	})
}

func (b *Batch) appendDelete(key []byte) {
	b.ops = append(b.ops, batchOp{
		key:    append([]byte(nil), key...),
		delete: true,
	})
}

func (b *RawBatch) appendSet(key, value []byte) {
	b.ops = append(b.ops, batchOp{
		key:   append([]byte(nil), key...),
		value: append([]byte(nil), value...),
	})
}

func sortBatchOps(ops []batchOp) {
	sort.SliceStable(ops, func(i, j int) bool {
		return MVCCComparer.Compare(ops[i].key, ops[j].key) < 0
	})
}
