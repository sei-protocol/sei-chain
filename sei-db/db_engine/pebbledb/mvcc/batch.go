package mvcc

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/batchrepr"
	"github.com/sei-protocol/sei-chain/sei-db/common/errors"
	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	pebbledbmetrics "github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type Batch struct {
	storage          *pebble.DB
	version          int64
	ops              []batchOp
	descending       bool
	operationMetrics *pebbledbmetrics.OperationMetrics
	dbName           string
}

type batchOp struct {
	storeKey  string
	key       []byte
	value     []byte
	version   int64
	tombstone int64
	delete    bool
}

var tombstonePayload = []byte(tombstoneVal)

// NewBatch creates a new Batch using the supplied MVCC encoding mode.
func NewBatch(storage *pebble.DB, version int64, descending bool, dbName string, operationMetrics ...*pebbledbmetrics.OperationMetrics) (*Batch, error) {
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
		dbName:           dbName,
	}, nil
}

func (b *Batch) Size() int {
	return len(b.ops)
}

func (b *Batch) Reset() {
	b.ops = b.ops[:0]
}

// grow ensures ops can hold n more entries without reallocating.
func (b *Batch) grow(n int) {
	b.ops = growOps(b.ops, n)
}

func (b *Batch) set(storeKey string, tombstone int64, key, value []byte) error {
	op := batchOp{
		storeKey:  storeKey,
		key:       utils.Clone(key),
		version:   b.version,
		tombstone: tombstone,
	}
	if tombstone == 0 {
		op.value = utils.Clone(value)
	} else {
		op.value = tombstonePayload
	}
	b.ops = append(b.ops, op)
	return nil
}

func (b *Batch) Set(storeKey string, key, value []byte) error {
	return b.set(storeKey, 0, key, value)
}

func (b *Batch) Delete(storeKey string, key []byte) error {
	return b.set(storeKey, b.version, key, nil)
}

func (b *Batch) Write() error {
	writeCount := int64(len(b.ops) + 1) // includes latest-version metadata.
	err := writeBatchOps(b.storage, b.ops, b.descending, b.dbName, func(batch *pebble.Batch) error {
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
	dbName           string
}

// NewRawBatch creates a new RawBatch using the supplied MVCC encoding mode.
func NewRawBatch(storage *pebble.DB, descending bool, dbName string, operationMetrics ...*pebbledbmetrics.OperationMetrics) (*RawBatch, error) {
	var metrics *pebbledbmetrics.OperationMetrics
	if len(operationMetrics) > 0 {
		metrics = operationMetrics[0]
	}

	return &RawBatch{
		storage:          storage,
		ops:              make([]batchOp, 0, 16),
		descending:       descending,
		operationMetrics: metrics,
		dbName:           dbName,
	}, nil
}

func (b *RawBatch) Size() int {
	return len(b.ops)
}

func (b *RawBatch) Reset() {
	b.ops = b.ops[:0]
}

func (b *RawBatch) set(storeKey string, tombstone int64, key, value []byte, version int64) error {
	op := batchOp{
		storeKey:  storeKey,
		key:       utils.Clone(key),
		version:   version,
		tombstone: tombstone,
	}
	if tombstone == 0 {
		op.value = utils.Clone(value)
	} else {
		op.value = tombstonePayload
	}
	b.ops = append(b.ops, op)
	return nil
}

func (b *RawBatch) Set(storeKey string, key, value []byte, version int64) error {
	return b.set(storeKey, 0, key, value, version)
}

func (b *RawBatch) Delete(storeKey string, key []byte, version int64) error {
	return b.set(storeKey, version, key, nil, version)
}

// HardDelete queues a physical delete of the encoded key at the batch's version.
func (b *Batch) HardDelete(storeKey string, key []byte) error {
	b.ops = append(b.ops, batchOp{
		storeKey: storeKey,
		key:      utils.Clone(key),
		version:  b.version,
		delete:   true,
	})
	return nil
}

func growOps(ops []batchOp, n int) []batchOp {
	if n <= 0 {
		return ops
	}
	want := len(ops) + n
	if cap(ops) >= want {
		return ops
	}
	grown := make([]batchOp, len(ops), want)
	copy(grown, ops)
	return grown
}

func (b *RawBatch) Write() error {
	writeCount := int64(len(b.ops))
	err := writeBatchOps(b.storage, b.ops, b.descending, b.dbName, nil)
	if err == nil && b.operationMetrics != nil {
		b.operationMetrics.AddWrite(writeCount)
	}
	return err
}

// writeBatchOps applies ops to a new pebble batch in sorted order, records
// otel metrics, and commits. The optional beforeCommit hook runs on the
// pebble batch right before commit (used by Batch.Write to stamp the
// latest-version metadata key).
func writeBatchOps(storage *pebble.DB, ops []batchOp, descending bool, dbName string, beforeCommit func(*pebble.Batch) error) (err error) {
	startTime := time.Now()
	batchSize := int64(len(ops))
	defer func() {
		ctx := context.Background()
		otelMetrics.batchWriteLatency.Record(
			ctx,
			time.Since(startTime).Seconds(),
			metric.WithAttributes(
				attribute.Bool("success", err == nil),
				attribute.String("db", dbName),
			),
		)
		otelMetrics.batchSize.Record(ctx, batchSize, metric.WithAttributes(attribute.String("db", dbName)))
	}()

	batch := storage.NewBatchWithSize(pebbleBatchBufSize(ops, beforeCommit != nil))
	defer func() {
		err = errors.Join(err, batch.Close())
	}()
	sortBatchOps(ops, descending)
	if err := encodeOpsDeferred(batch, ops, descending); err != nil {
		return err
	}
	if beforeCommit != nil {
		if err := beforeCommit(batch); err != nil {
			return err
		}
	}
	return batch.Commit(defaultWriteOpts)
}

func encodeOpsDeferred(batch *pebble.Batch, ops []batchOp, descending bool) error {
	for _, op := range ops {
		keyLen := mvccStoreKeyLen(op.storeKey, op.key, op.version)
		if op.delete {
			d := batch.DeleteDeferred(keyLen)
			encodeMVCCStoreKeyInto(d.Key, op.storeKey, op.key, op.version, descending)
			if err := d.Finish(); err != nil {
				return fmt.Errorf("failed to delete in PebbleDB batch: %w", err)
			}
			continue
		}
		valLen := mvccValueLen(op.value, op.tombstone)
		d := batch.SetDeferred(keyLen, valLen)
		encodeMVCCStoreKeyInto(d.Key, op.storeKey, op.key, op.version, descending)
		encodeMVCCValueInto(d.Value, op.value, op.tombstone, descending)
		if err := d.Finish(); err != nil {
			return fmt.Errorf("failed to write PebbleDB batch: %w", err)
		}
	}
	return nil
}

// batchOpOrder reports whether a sorts before (negative), with (zero), or
// after (positive) b: by store key, then user key, then version in the
// direction the DB was created with.
func batchOpOrder(a, b batchOp, descending bool) int {
	if a.storeKey != b.storeKey {
		if a.storeKey < b.storeKey {
			return -1
		}
		return 1
	}
	if c := bytes.Compare(a.key, b.key); c != 0 {
		return c
	}
	if a.version != b.version {
		if (a.version > b.version) == descending {
			return -1
		}
		return 1
	}
	return 0
}

// sortBatchOps puts ops into the order writeBatchOps writes them in.
//
// A caller that already produced that order — see SortChangesetPairs — pays
// only the linear IsSortedFunc scan, so presorting at the source is worth far
// more than the sort it replaces. The scan also means a caller whose order
// stops matching gets sorted correctly rather than silently written unsorted.
func sortBatchOps(ops []batchOp, descending bool) {
	cmp := func(a, b batchOp) int { return batchOpOrder(a, b, descending) }
	if slices.IsSortedFunc(ops, cmp) {
		return
	}
	slices.SortStableFunc(ops, cmp)
}

// SortChangesetPairs orders one changeset's pairs by key, the order
// ApplyChangesetSync's batch would otherwise be sorted into during its write.
// Pairs in a single changeset share a store key and a version, so key order
// alone settles them.
func SortChangesetPairs(pairs []*proto.KVPair) {
	slices.SortStableFunc(pairs, func(a, b *proto.KVPair) int {
		return bytes.Compare(a.Key, b.Key)
	})
}

// pebbleBatchBufSize returns a Pebble batch buffer capacity that fits ops
// without reallocating. Each record reserves binary.MaxVarintLen32 for every
// length prefix, matching Pebble's grow call, not the packed varint size.
// extraSet is the latest-version metadata record Batch.Write adds before commit.
func pebbleBatchBufSize(ops []batchOp, extraSet bool) int {
	n := batchrepr.HeaderLen
	for _, op := range ops {
		n += 1 + binary.MaxVarintLen32 + mvccStoreKeyLen(op.storeKey, op.key, op.version)
		if !op.delete {
			n += binary.MaxVarintLen32 + mvccValueLen(op.value, op.tombstone)
		}
	}
	if extraSet {
		n += 1 + binary.MaxVarintLen32 + len(latestVersionKey) + binary.MaxVarintLen32 + VersionSize
	}
	return n
}
