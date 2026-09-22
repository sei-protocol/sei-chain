package mvcc

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"slices"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/sei-protocol/sei-chain/sei-db/common/errors"
	pebbledbmetrics "github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var tombstonePayload = []byte(tombstoneVal)

// Directly uses pebble.Batch as the underlying batch
// implementation to avoid the overhead of allocating an intermediate Batch struct.
type Batch struct {
	pb               *pebble.Batch
	version          int64
	descending       bool
	operationMetrics *pebbledbmetrics.OperationMetrics
	dbName           string
}

// NewBatch creates a new Batch using the supplied MVCC encoding mode.
func NewBatch(
	storage *pebble.DB,
	version int64,
	descending bool,
	dbName string,
	operationMetrics ...*pebbledbmetrics.OperationMetrics,
) (*Batch, error) {
	if version < 0 {
		return nil, fmt.Errorf("version must be non-negative")
	}

	var metrics *pebbledbmetrics.OperationMetrics
	if len(operationMetrics) > 0 {
		metrics = operationMetrics[0]
	}

	return &Batch{
		pb:               storage.NewBatch(),
		version:          version,
		descending:       descending,
		operationMetrics: metrics,
		dbName:           dbName,
	}, nil
}

func (b *Batch) Size() int {
	return int(b.pb.Count())
}

func (b *Batch) Reset() {
	b.pb.Reset()
}

func (b *Batch) set(storeKey string, tombstone int64, key, value []byte) error {
	val := value
	if tombstone != 0 {
		val = tombstonePayload
	}
	keyLen := mvccEncodedLen(storeKey, key, b.version)
	valLen := mvccEncodedLen("", val, tombstone)
	d := b.pb.SetDeferred(keyLen, valLen)
	encodeMVCCInto(d.Key, storeKey, key, b.version, b.descending)
	encodeMVCCInto(d.Value, "", val, tombstone, b.descending)
	if err := d.Finish(); err != nil {
		return fmt.Errorf("failed to write PebbleDB batch: %w", err)
	}
	return nil
}

func (b *Batch) Set(storeKey string, key, value []byte) error {
	return b.set(storeKey, 0, key, value)
}

func (b *Batch) Delete(storeKey string, key []byte) error {
	return b.set(storeKey, b.version, key, nil)
}

func (b *Batch) HardDelete(storeKey string, key []byte) error {
	keyLen := mvccEncodedLen(storeKey, key, b.version)
	d := b.pb.DeleteDeferred(keyLen)
	encodeMVCCInto(d.Key, storeKey, key, b.version, b.descending)
	if err := d.Finish(); err != nil {
		return fmt.Errorf("failed to delete in PebbleDB batch: %w", err)
	}
	return nil
}

func (b *Batch) Write() (err error) {
	startTime := time.Now()
	opCount := int64(b.pb.Count())
	defer recordBatchMetrics(startTime, opCount, &err, b.dbName)
	defer func() { err = errors.Join(err, b.pb.Close()) }()

	var versionBz [VersionSize]byte
	binary.LittleEndian.PutUint64(
		versionBz[:],
		uint64(b.version), //nolint:gosec // block heights are non-negative and fit in int64
	)
	if err = b.pb.Set([]byte(latestVersionKey), versionBz[:], nil); err != nil {
		return fmt.Errorf("failed to set latest version in batch: %w", err)
	}
	if err = b.pb.Commit(defaultWriteOpts); err != nil {
		return err
	}
	if b.operationMetrics != nil {
		b.operationMetrics.AddWrite(opCount + 1)
	}
	return nil
}

// recordBatchMetrics records the otel instruments for a Batch write. err is
// read at defer time, after any Close error a caller joins into it, so
// "success" reflects the write's final outcome.
func recordBatchMetrics(startTime time.Time, opCount int64, err *error, dbName string) {
	ctx := context.Background()
	otelMetrics.batchWriteLatency.Record(
		ctx,
		time.Since(startTime).Seconds(),
		metric.WithAttributes(
			attribute.Bool("success", *err == nil),
			attribute.String("db", dbName),
		),
	)
	otelMetrics.batchSize.Record(ctx, opCount, metric.WithAttributes(attribute.String("db", dbName)))
}

// SortChangesetPairs orders one changeset's pairs by key, the order PebbleDB
// inserts fastest into its memtable. Pairs in a single changeset share a
// store key and a version, so key order alone settles them.
func SortChangesetPairs(pairs []*proto.KVPair) {
	slices.SortStableFunc(pairs, func(a, b *proto.KVPair) int {
		return bytes.Compare(a.Key, b.Key)
	})
}
