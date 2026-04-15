package flatkv

import (
	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
)

// RawGlobalIterator returns an iterator that walks each data DB sequentially
// in fixed order (account → code → storage → legacy). Within each DB the
// keys are returned in PebbleDB's natural order. Per-DB _meta/* keys are
// skipped. Pending writes are not visible. metadataDB is not included.
func (s *CommitStore) RawGlobalIterator() Iterator {
	return &sequentialIterator{dbs: s.dataDBs()}
}

// sequentialIterator iterates through a slice of DBs one at a time.
// It fully drains the current DB before moving to the next.
type sequentialIterator struct {
	dbs   []seidbtypes.KeyValueDB
	dbIdx int // index into dbs for the current DB
	iter  seidbtypes.KeyValueDBIterator
	err   error
}

// openCurrent opens an iterator on dbs[dbIdx]. Returns false if no more DBs.
func (s *sequentialIterator) openCurrent() bool {
	if s.dbIdx >= len(s.dbs) {
		return false
	}
	it, err := s.dbs[s.dbIdx].NewIter(nil)
	if err != nil {
		s.err = err
		return false
	}
	s.iter = it
	return true
}

// advanceDB closes the current iterator and moves to the next DB,
// positioning at the first non-meta key. Returns true if positioned.
// If the current iterator has an error, it is captured and iteration stops.
func (s *sequentialIterator) advanceDB() bool {
	for {
		if s.iter != nil {
			if err := s.iter.Error(); err != nil {
				s.err = err
				_ = s.iter.Close()
				s.iter = nil
				return false
			}
			_ = s.iter.Close()
			s.iter = nil
		}
		s.dbIdx++
		if !s.openCurrent() {
			return false
		}
		s.iter.First()
		skipMeta(s.iter)
		if s.iter.Valid() {
			return true
		}
	}
}

func skipMeta(it seidbtypes.KeyValueDBIterator) {
	for it.Valid() && ktype.IsMetaKey(it.Key()) {
		it.Next()
	}
}

func (s *sequentialIterator) Domain() ([]byte, []byte) { return nil, nil }

func (s *sequentialIterator) Valid() bool {
	return s.iter != nil && s.iter.Valid()
}

func (s *sequentialIterator) Error() error {
	if s.err != nil {
		return s.err
	}
	if s.iter != nil {
		return s.iter.Error()
	}
	return nil
}

func (s *sequentialIterator) Close() error {
	if s.iter != nil {
		_ = s.iter.Close()
		s.iter = nil
	}
	return nil
}

func (s *sequentialIterator) First() bool {
	if s.iter != nil {
		_ = s.iter.Close()
		s.iter = nil
	}
	s.dbIdx = 0
	if !s.openCurrent() {
		return false
	}
	s.iter.First()
	skipMeta(s.iter)
	if s.iter.Valid() {
		return true
	}
	return s.advanceDB()
}

func (s *sequentialIterator) Next() bool {
	if !s.Valid() {
		return false
	}
	s.iter.Next()
	skipMeta(s.iter)
	if s.iter.Valid() {
		return true
	}
	return s.advanceDB()
}

func (s *sequentialIterator) Key() []byte {
	if !s.Valid() {
		return nil
	}
	return s.iter.Key()
}

func (s *sequentialIterator) Value() []byte {
	if !s.Valid() {
		return nil
	}
	return s.iter.Value()
}

// Unsupported positioning methods — not needed for forward-only scanning.

func (s *sequentialIterator) Last() bool         { return false }
func (s *sequentialIterator) SeekGE([]byte) bool { return false }
func (s *sequentialIterator) SeekLT([]byte) bool { return false }
func (s *sequentialIterator) Prev() bool         { return false }
