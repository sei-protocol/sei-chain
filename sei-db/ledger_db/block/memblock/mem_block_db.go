package memblock

import (
	"sync"

	blocktypes "github.com/sei-protocol/sei-chain/sei-db/ledger_db/block/types"
)

var _ blocktypes.BlockDB = (*blockDB)(nil)

// blockDB is an in-memory blocktypes.BlockDB. Records are kept in a map for
// lookup and in an append-only log for write-order scans. Raising the watermark
// reclaims eagerly, so memory tracks what the store still serves.
type blockDB struct {
	mu sync.RWMutex
	// log holds one entry per primary record, in the order it was written.
	log []blocktypes.Record
	// values maps primary and alias keys to their stored value bytes.
	values map[string][]byte
	// watermark is the reclamation floor.
	watermark blocktypes.BlockNumber
}

// NewBlockDB returns an in-memory blocktypes.BlockDB.
func NewBlockDB() blocktypes.BlockDB {
	return &blockDB{values: make(map[string][]byte)}
}

func (s *blockDB) PutQC(qc blocktypes.QCRecord) error {
	record, err := qc.Encode()
	if err != nil {
		return err
	}
	return s.putRecord(record)
}

func (s *blockDB) PutAppQC(qc blocktypes.AppQCRecord) error {
	record, err := qc.Encode()
	if err != nil {
		return err
	}
	return s.putRecord(record)
}

func (s *blockDB) PutAppProposal(proposal blocktypes.AppProposalRecord) error {
	record, err := proposal.Encode()
	if err != nil {
		return err
	}
	return s.putRecord(record)
}

// putRecord inserts an encoded record and its aliases.
func (s *blockDB) putRecord(record blocktypes.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[string(record.Key)] = record.Value
	for _, alias := range record.Aliases {
		s.values[string(alias.Key)] = record.Value[alias.Offset : alias.Offset+alias.Length]
	}
	s.log = append(s.log, record)
	return nil
}

func (s *blockDB) GetRecord(kind blocktypes.RecordKind, n blocktypes.BlockNumber) ([]byte, bool, error) {
	key, err := kind.Key(n)
	if err != nil {
		return nil, false, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.values[string(key)]
	return value, ok, nil
}

func (s *blockDB) GetBlockByHash(hash []byte) ([]byte, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	value, ok := s.values[string(blocktypes.BlockHashKey(hash))]
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
func (s *blockDB) GetPruneWatermark() blocktypes.BlockNumber {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.watermark
}

// SetPruneWatermark raises the reclamation floor and eagerly reclaims eligible records.
func (s *blockDB) SetPruneWatermark(n blocktypes.BlockNumber) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= s.watermark {
		return
	}
	s.watermark = n
	s.reclaim()
}

// reclaim drops records whose keys and aliases are all below the watermark.
// mu must be held.
func (s *blockDB) reclaim() {
	canReclaim := func(key blocktypes.Key) bool {
		eligible, err := key.Reclaimable(s.watermark)
		if err != nil {
			panic(err)
		} // Keys are produced by record Encode methods.
		return eligible
	}
	// Existing iterators keep their snapshot of the old backing array.
	kept := make([]blocktypes.Record, 0, len(s.log))
	for _, record := range s.log {
		eligible := canReclaim(record.Key)
		if eligible {
			for _, alias := range record.Aliases {
				if !canReclaim(alias.Key) {
					eligible = false
					break
				}
			}
		}
		if !eligible {
			kept = append(kept, record)
			continue
		}
		delete(s.values, string(record.Key))
		for _, alias := range record.Aliases {
			delete(s.values, string(alias.Key))
		}
	}
	s.log = kept
}

// Flush is a no-op: an in-memory store has no disk to make durable.
func (s *blockDB) Flush() error { return nil }

// Close is a no-op: an in-memory store holds no resources to release.
func (s *blockDB) Close() error { return nil }

// recordIterator walks a snapshot of the write-order log.
type recordIterator struct {
	records     []blocktypes.Record
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

func (r *recordIterator) current() blocktypes.Record {
	if r.newestFirst {
		return r.records[len(r.records)-1-r.pos]
	}
	return r.records[r.pos]
}

func (r *recordIterator) Kind() blocktypes.RecordKind {
	kind, _ := r.current().Key.RecordKind()
	return kind
}

func (r *recordIterator) Number() blocktypes.BlockNumber { return r.current().Key.Number() }

func (r *recordIterator) Value() ([]byte, error) { return r.current().Value, nil }

// Close is a no-op: the iterator holds no resources.
func (r *recordIterator) Close() error { return nil }
