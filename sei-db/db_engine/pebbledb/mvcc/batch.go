package mvcc

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/cockroachdb/pebble/v2"
	"github.com/cockroachdb/pebble/v2/batchrepr"
	pebbledbmetrics "github.com/sei-protocol/sei-chain/sei-db/db_engine/pebbledb"
	"github.com/sei-protocol/sei-chain/sei-db/proto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var tombstonePayload = []byte(tombstoneVal)

// errBatchClosed is returned by a Batch used after Write or Close.
var errBatchClosed = errors.New("pebbledb: batch already written or closed")

// Batch accumulates MVCC-encoded writes at a single version in a pebble.Batch.
// It is single-use: Write commits and releases it, and Close releases it
// without committing.
type Batch struct {
	pb               *pebble.Batch
	version          int64
	descending       bool
	operationMetrics *pebbledbmetrics.OperationMetrics
	dbName           string
}

// NewBatch creates a new Batch using the supplied MVCC encoding mode. bufSize is
// the byte capacity to reserve for the encoded batch, as changesetBatchSize
// computes it; 0 lets Pebble grow the buffer on demand.
func NewBatch(
	storage *pebble.DB,
	version int64,
	bufSize int,
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
		pb:               storage.NewBatchWithSize(bufSize),
		version:          version,
		descending:       descending,
		operationMetrics: metrics,
		dbName:           dbName,
	}, nil
}

// Size returns the number of queued writes, or 0 once the batch is released.
func (b *Batch) Size() int {
	if b.pb == nil {
		return 0
	}
	return int(b.pb.Count())
}

func (b *Batch) Reset() {
	if b.pb != nil {
		b.pb.Reset()
	}
}

// Close releases the underlying pebble.Batch without committing it. It is a
// no-op on a batch that was already written or closed.
func (b *Batch) Close() error {
	if b.pb == nil {
		return nil
	}
	err := b.pb.Close()
	b.pb = nil
	return err
}

func (b *Batch) set(storeKey string, tombstone int64, key, value []byte) error {
	if b.pb == nil {
		return errBatchClosed
	}
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

// HardDelete queues a physical delete of the encoded key at the batch's version.
func (b *Batch) HardDelete(storeKey string, key []byte) error {
	if b.pb == nil {
		return errBatchClosed
	}
	keyLen := mvccEncodedLen(storeKey, key, b.version)
	d := b.pb.DeleteDeferred(keyLen)
	encodeMVCCInto(d.Key, storeKey, key, b.version, b.descending)
	if err := d.Finish(); err != nil {
		return fmt.Errorf("failed to delete in PebbleDB batch: %w", err)
	}
	return nil
}

// Write stamps the latest-version marker, commits, and releases the batch.
func (b *Batch) Write() (err error) {
	if b.pb == nil {
		return errBatchClosed
	}
	startTime := time.Now()
	opCount := int64(b.pb.Count())
	defer recordBatchMetrics(startTime, opCount, &err, b.dbName)
	defer func() { err = errors.Join(err, b.Close()) }()

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

// changesetBatchSize returns a pebble batch capacity that holds changesets
// written at version, plus the latest-version record Write adds, without the
// buffer having to grow.
func changesetBatchSize(changesets []*proto.NamedChangeSet, version int64) int {
	n := batchrepr.HeaderLen + batchRecordSize(len(latestVersionKey), VersionSize)
	for _, cs := range changesets {
		for _, pair := range cs.Changeset.Pairs {
			keyLen := mvccEncodedLen(cs.Name, pair.Key, version)
			if pair.Value == nil {
				n += batchRecordSize(keyLen, mvccEncodedLen("", tombstonePayload, version))
			} else {
				n += batchRecordSize(keyLen, mvccEncodedLen("", pair.Value, 0))
			}
		}
	}
	return n
}

// batchRecordSize returns the room Pebble reserves for one key/value record: a
// kind byte and both length prefixes at their widest varint encoding.
func batchRecordSize(keyLen, valueLen int) int {
	return 1 + 2*binary.MaxVarintLen32 + keyLen + valueLen
}
