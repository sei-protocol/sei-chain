package mvcc

import (
	"context"
	"encoding/binary"
	"fmt"
	"sort"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/sei-protocol/sei-chain/sei-db/common/errors"
	pebbledbmetrics "github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Batch struct {
	storage          *pebble.DB
	version          int64
	ops              []batchOp
	descending       bool
	operationMetrics *pebbledbmetrics.OperationMetrics
}

type batchOp struct {
	key    []byte
	value  []byte
	delete bool
}

// NewBatch creates a new Batch using the supplied MVCC encoding mode.
func NewBatch(storage *pebble.DB, version int64, descending bool, operationMetrics ...*pebbledbmetrics.OperationMetrics) (*Batch, error) {
	if version < 0 {
		return nil, fmt.Errorf("version must be non-negative")
	}

	var metrics *pebbledbmetrics.OperationMetrics
	if len(operationMetrics) > 0 {
		metrics = operationMetrics[0]
	}

	return &Batch{
		storage:          storage,
		version:          version,
		ops:              make([]batchOp, 0, 16),
		descending:       descending,
		operationMetrics: metrics,
	}, nil
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

	b.ops = append(b.ops, batchOp{
		key:   append([]byte(nil), prefixedKey...),
		value: append([]byte(nil), prefixedVal...),
	})
	return nil
}

func (b *Batch) Set(storeKey string, key, value []byte) error {
	return b.set(storeKey, 0, key, value)
}

func (b *Batch) Delete(storeKey string, key []byte) error {
	return b.set(storeKey, b.version, key, []byte(tombstoneVal))
}

func (b *Batch) Write() error {
	writeCount := int64(len(b.ops) + 1) // includes latest-version metadata.
	err := writeBatchOps(b.storage, b.ops, func(batch *pebble.Batch) error {
		var versionBz [VersionSize]byte
		binary.LittleEndian.PutUint64(versionBz[:], uint64(b.version)) //nolint:gosec // block heights are non-negative and fit in int64
		if err := batch.Set([]byte(latestVersionKey), versionBz[:], nil); err != nil {
			return fmt.Errorf("failed to set latest version in batch: %w", err)
		}
		return nil
	})
	if err == nil && b.operationMetrics != nil {
		b.operationMetrics.AddWrite(writeCount)
	}
	return err
}

// For writing kv pairs in any order of version
type RawBatch struct {
	storage          *pebble.DB
	ops              []batchOp
	descending       bool
	operationMetrics *pebbledbmetrics.OperationMetrics
}

// NewRawBatch creates a new RawBatch using the supplied MVCC encoding mode.
func NewRawBatch(storage *pebble.DB, descending bool, operationMetrics ...*pebbledbmetrics.OperationMetrics) (*RawBatch, error) {
	var metrics *pebbledbmetrics.OperationMetrics
	if len(operationMetrics) > 0 {
		metrics = operationMetrics[0]
	}

	return &RawBatch{
		storage:          storage,
		ops:              make([]batchOp, 0, 16),
		descending:       descending,
		operationMetrics: metrics,
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

	b.ops = append(b.ops, batchOp{
		key:   append([]byte(nil), prefixedKey...),
		value: append([]byte(nil), prefixedVal...),
	})
	return nil
}

func (b *RawBatch) Set(storeKey string, key, value []byte, version int64) error {
	return b.set(storeKey, 0, key, value, version)
}

func (b *RawBatch) Delete(storeKey string, key []byte, version int64) error {
	return b.set(storeKey, version, key, []byte(tombstoneVal), version)
}

// HardDelete physically removes the key by encoding it with the batch's version
// and calling the underlying pebble.Batch.Delete.
func (b *Batch) HardDelete(storeKey string, key []byte) error {
	fullKey := MVCCEncode(prependStoreKey(storeKey, key), b.version, b.descending)
	b.ops = append(b.ops, batchOp{
		key:    append([]byte(nil), fullKey...),
		delete: true,
	})
	return nil
}

func (b *RawBatch) Write() error {
	writeCount := int64(len(b.ops))
	err := writeBatchOps(b.storage, b.ops, nil)
	if err == nil && b.operationMetrics != nil {
		b.operationMetrics.AddWrite(writeCount)
	}
	return err
}

// writeBatchOps applies ops to a new pebble batch in sorted order, records
// otel metrics, and commits. The optional beforeCommit hook runs on the
// pebble batch right before commit (used by Batch.Write to stamp the
// latest-version metadata key).
func writeBatchOps(storage *pebble.DB, ops []batchOp, beforeCommit func(*pebble.Batch) error) (err error) {
	startTime := time.Now()
	batchSize := int64(len(ops))
	defer func() {
		ctx := context.Background()
		otelMetrics.batchWriteLatency.Record(
			ctx,
			time.Since(startTime).Seconds(),
			metric.WithAttributes(attribute.Bool("success", err == nil)),
		)
		otelMetrics.batchSize.Record(ctx, batchSize)
	}()

	batch := storage.NewBatch()
	defer func() {
		err = errors.Join(err, batch.Close())
	}()
	sortBatchOps(ops)
	for _, op := range ops {
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
	if beforeCommit != nil {
		if err := beforeCommit(batch); err != nil {
			return err
		}
	}
	return batch.Commit(defaultWriteOpts)
}

func sortBatchOps(ops []batchOp) {
	sort.SliceStable(ops, func(i, j int) bool {
		return MVCCComparer.Compare(ops[i].key, ops[j].key) < 0
	})
}
