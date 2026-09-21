package pebbledb

import (
	"errors"
	"slices"

	"github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
)

// pebbleBatch collects writes and hands them to the write pipeline at Commit. It does no ordering and
// holds no pebble state of its own: the pipeline orders the entries and applies them.
type pebbleBatch struct {
	// The pipeline that orders and applies this batch.
	pipeline *writePipeline

	// The writes collected so far, in the order they were collected.
	entries []batchEntry

	// The size the collected writes will occupy encoded, as Len reports it.
	size int

	// Whether the batch has been committed, after which it accepts no further writes.
	committed bool
}

var _ types.Batch = (*pebbleBatch)(nil)

// batchEntry is one collected write. The key is held as a string so that a caller passing a map key
// hands it over without converting it to bytes.
type batchEntry struct {
	// The key to write.
	key string

	// The value to write. Ignored when deleted is true.
	value []byte

	// Whether the write removes the key rather than setting it.
	deleted bool
}

// batchHeaderSize is the size of the header pebble writes in front of a batch's records.
const batchHeaderSize = 12

// errCommitted is reported by a batch that has already been committed. Committing hands its writes
// to the pipeline, so one arriving afterwards would silently open a second batch rather than
// joining the one the caller believes it is filling.
var errCommitted = errors.New("batch has already been committed")

// NewBatch returns a batch that collects writes for this database's write pipeline.
func (p *pebbleDB) NewBatch() types.Batch {
	return &pebbleBatch{pipeline: p.pipeline, size: batchHeaderSize}
}

func (pb *pebbleBatch) Set(key, value []byte) error {
	if pb.committed {
		return errCommitted
	}
	pb.appendEntry(batchEntry{key: string(key), value: value})
	return nil
}

func (pb *pebbleBatch) Delete(key []byte) error {
	if pb.committed {
		return errCommitted
	}
	pb.appendEntry(batchEntry{key: string(key), deleted: true})
	return nil
}

func (pb *pebbleBatch) SetAll(writes map[string][]byte) error {
	if pb.committed {
		return errCommitted
	}
	pb.entries = slices.Grow(pb.entries, len(writes))
	for key, value := range writes {
		pb.appendEntry(batchEntry{key: key, value: value, deleted: value == nil})
	}
	return nil
}

// Commit hands the collected writes to the pipeline, which orders and applies them. The batch is
// spent afterwards.
func (pb *pebbleBatch) Commit(opts types.WriteOptions) (types.CommitHandle, error) {
	if pb.committed {
		return nil, errCommitted
	}

	handle := &commitHandle{
		entries: pb.entries,
		opts:    opts,
		sorted:  make(chan struct{}),
		done:    make(chan struct{}),
	}
	pb.committed = true
	// The slice belongs to the pipeline now, which sorts it in place.
	pb.entries = nil

	// A refused submit hands back no handle: the batch will not be applied, so there is nothing for
	// the caller to wait on.
	if err := pb.pipeline.Submit(handle); err != nil {
		return nil, err
	}

	return handle, nil
}

// Len returns the size the batch's writes will occupy encoded, mirroring pebble's wire format: a
// fixed header, then per write a kind byte, uvarint lengths, and the payload.
func (pb *pebbleBatch) Len() int {
	return pb.size
}

// appendEntry collects one write and charges its encoded size to the batch.
func (pb *pebbleBatch) appendEntry(entry batchEntry) {
	pb.size += 1 + uvarintLen(len(entry.key)) + len(entry.key)
	if !entry.deleted {
		pb.size += uvarintLen(len(entry.value)) + len(entry.value)
	}
	pb.entries = append(pb.entries, entry)
}

// uvarintLen returns the number of bytes x occupies encoded as a uvarint.
func uvarintLen(x int) int {
	n := 1
	for x >= 0x80 {
		x >>= 7
		n++
	}
	return n
}
