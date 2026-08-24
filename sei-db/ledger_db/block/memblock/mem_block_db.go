package memblock

import (
	"sync"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

var _ blocktypes.BlockDB = (*blockDB)(nil)

// recordKey addresses a stored value by the kind and number it is filed under.
type recordKey struct {
	kind   blocktypes.RecordKind
	number uint64
}

// record is one primary entry in the write-order log. next is the exclusive end
// of the range the value is addressable over, and hash is the header-hash alias
// of a block record, empty for every other kind; reclamation needs both to find
// the entries a dropped record leaves behind.
type record struct {
	kind   blocktypes.RecordKind
	number uint64
	next   uint64
	hash   string
	value  []byte
}

// blockDB is an in-memory blocktypes.BlockDB. Records are kept in maps for
// lookup and in an append-only log for write-order scans. Raising the watermark
// reclaims eagerly, so memory tracks what the store still serves.
type blockDB struct {
	mu sync.RWMutex

	// log holds one entry per primary record, in the order it was written.
	log []record
	// values maps every number a record is addressable at — its primary and
	// each covered-range alias — to the single shared value.
	values map[recordKey][]byte
	// blocksByHash aliases block records by header hash.
	blocksByHash map[string][]byte
	// watermark is the reclamation floor.
	watermark uint64
}

// NewBlockDB returns an in-memory blocktypes.BlockDB.
func NewBlockDB() blocktypes.BlockDB {
	return &blockDB{
		values:       make(map[recordKey][]byte),
		blocksByHash: make(map[string][]byte),
	}
}

func (s *blockDB) PutRecord(kind blocktypes.RecordKind, first, next uint64, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for n := first; n < next; n++ {
		s.values[recordKey{kind: kind, number: n}] = value
	}
	s.log = append(s.log, record{kind: kind, number: first, next: next, value: value})
	return nil
}

func (s *blockDB) PutBlock(n uint64, hash []byte, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[recordKey{kind: blocktypes.KindBlock, number: n}] = value
	s.blocksByHash[string(hash)] = value
	s.log = append(s.log, record{
		kind: blocktypes.KindBlock, number: n, next: n + 1, hash: string(hash), value: value,
	})
	return nil
}

func (s *blockDB) GetRecord(kind blocktypes.RecordKind, n uint64) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.values[recordKey{kind: kind, number: n}]
	return value, ok, nil
}

func (s *blockDB) GetBlockByHash(hash []byte) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.blocksByHash[string(hash)]
	return value, ok, nil
}

func (s *blockDB) Scan(newestFirst bool) (blocktypes.RecordIterator, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	// The log is append-only and its entries are immutable, so a reslice is a
	// snapshot of the records present now.
	return &recordIterator{records: s.log[:len(s.log):len(s.log)], newestFirst: newestFirst, pos: -1}, nil
}

// GetPruneWatermark returns the reclamation floor.
func (s *blockDB) GetPruneWatermark() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.watermark
}

// SetPruneWatermark raises the reclamation floor to n and immediately drops
// every record that falls wholly below it.
func (s *blockDB) SetPruneWatermark(n uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= s.watermark {
		return
	}
	s.watermark = n
	s.reclaim()
}

// reclaim drops the records whose coverage ends at or below the watermark. A
// record straddling it is kept whole, since its value is shared by every number
// it covers. mu must be held.
func (s *blockDB) reclaim() {
	// A fresh slice rather than a filter in place: iterators handed out by Scan
	// hold a reslice of the current backing array as their snapshot.
	kept := make([]record, 0, len(s.log))
	for _, rec := range s.log {
		if rec.next > s.watermark {
			kept = append(kept, rec)
			continue
		}
		for n := rec.number; n < rec.next; n++ {
			delete(s.values, recordKey{kind: rec.kind, number: n})
		}
		if rec.hash != "" {
			delete(s.blocksByHash, rec.hash)
		}
	}
	s.log = kept
}

// Flush is a no-op: an in-memory store has no disk to make durable.
func (s *blockDB) Flush() error {
	return nil
}

// Close is a no-op: an in-memory store holds no resources to release.
func (s *blockDB) Close() error {
	return nil
}

// recordIterator walks a snapshot of the write-order log.
type recordIterator struct {
	records     []record
	newestFirst bool
	// pos counts advances, starting at -1 before the first Next.
	pos int
}

func (r *recordIterator) Next() (bool, error) {
	if r.pos+1 >= len(r.records) {
		r.pos = len(r.records)
		return false, nil
	}
	r.pos++
	return true, nil
}

func (r *recordIterator) current() record {
	if r.newestFirst {
		return r.records[len(r.records)-1-r.pos]
	}
	return r.records[r.pos]
}

func (r *recordIterator) Kind() blocktypes.RecordKind {
	return r.current().kind
}

func (r *recordIterator) Number() uint64 {
	return r.current().number
}

func (r *recordIterator) Value() ([]byte, error) {
	return r.current().value, nil
}

// Close is a no-op: the iterator holds no resources.
func (r *recordIterator) Close() error {
	return nil
}
