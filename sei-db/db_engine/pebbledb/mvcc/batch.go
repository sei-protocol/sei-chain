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
	storage *pebble.DB
	version int64
	ops     []batchOp
}

type batchOp struct {
	key    []byte
	value  []byte
	delete bool
	order  int
}

func NewBatch(storage *pebble.DB, version int64) (*Batch, error) {
	if version < 0 {
		return nil, fmt.Errorf("version must be non-negative")
	}
	b := &Batch{
		storage: storage,
		version: version,
		ops:     make([]batchOp, 0, 16),
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
	prefixedKey := MVCCEncode(prependStoreKey(storeKey, key), b.version)
	prefixedVal := MVCCEncode(value, tombstone)

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
	if err := batch.Commit(defaultWriteOpts); err != nil {
		return err
	}
	var versionBz [VersionSize]byte
	binary.LittleEndian.PutUint64(versionBz[:], uint64(b.version))
	if err := b.storage.Set([]byte(latestVersionKey), versionBz[:], defaultWriteOpts); err != nil {
		return fmt.Errorf("failed to update latest version after batch commit: %w", err)
	}
	return nil
}

// For writing kv pairs in any order of version
type RawBatch struct {
	storage *pebble.DB
	ops     []batchOp
}

func NewRawBatch(storage *pebble.DB) (*RawBatch, error) {
	return &RawBatch{
		storage: storage,
		ops:     make([]batchOp, 0, 16),
	}, nil
}

func (b *RawBatch) Size() int {
	return len(b.ops)
}

func (b *RawBatch) Reset() {
	b.ops = b.ops[:0]
}

func (b *RawBatch) set(storeKey string, tombstone int64, key, value []byte, version int64) error {
	prefixedKey := MVCCEncode(prependStoreKey(storeKey, key), version)
	prefixedVal := MVCCEncode(value, tombstone)

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
	fullKey := MVCCEncode(prependStoreKey(storeKey, key), b.version)
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
		order: len(b.ops),
	})
}

func (b *Batch) appendDelete(key []byte) {
	b.ops = append(b.ops, batchOp{
		key:    append([]byte(nil), key...),
		delete: true,
		order:  len(b.ops),
	})
}

func (b *RawBatch) appendSet(key, value []byte) {
	b.ops = append(b.ops, batchOp{
		key:   append([]byte(nil), key...),
		value: append([]byte(nil), value...),
		order: len(b.ops),
	})
}

func sortBatchOps(ops []batchOp) {
	sort.SliceStable(ops, func(i, j int) bool {
		return MVCCComparer.Compare(ops[i].key, ops[j].key) < 0
	})
}
