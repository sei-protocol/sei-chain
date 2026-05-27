package flatkv

import (
	dbm "github.com/tendermint/tm-db"

	seidbtypes "github.com/sei-protocol/sei-chain/sei-db/db_engine/types"
	"github.com/sei-protocol/sei-chain/sei-db/state_db/sc/flatkv/ktype"
)

var _ dbm.Iterator = (*rawIterator)(nil)

// Iteratates over the raw key-value pairs in a set of pebbleDB databases. Intended for use by the import/export
// workflow.
type rawIterator struct {
	dbs []seidbtypes.KeyValueDB
	// index into dbs for the current DB
	dbIdx int
	iter  seidbtypes.DBIterator
	err   error
}

// newRawIterator returns an iterator positioned on the first non-meta key across
// dbs (account → code → storage → legacy), or invalid if empty.
func newRawIterator(dbs []seidbtypes.KeyValueDB) *rawIterator {
	s := &rawIterator{dbs: dbs}
	if !s.openCurrent() {
		return s
	}
	skipMeta(s.iter)
	if !s.iter.Valid() {
		s.advanceDB()
	}
	return s
}

// openCurrent opens an iterator on dbs[dbIdx]. Returns false if no more DBs.
func (s *rawIterator) openCurrent() bool {
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
func (s *rawIterator) advanceDB() bool {
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
		skipMeta(s.iter)
		if s.iter.Valid() {
			return true
		}
	}
}

func skipMeta(it seidbtypes.DBIterator) {
	for it.Valid() && ktype.IsMetaKey(it.Key()) {
		it.Next()
	}
}

func (s *rawIterator) Domain() ([]byte, []byte) { return nil, nil }

func (s *rawIterator) Valid() bool {
	return s.iter != nil && s.iter.Valid()
}

func (s *rawIterator) Error() error {
	if s.err != nil {
		return s.err
	}
	if s.iter != nil {
		return s.iter.Error()
	}
	return nil
}

func (s *rawIterator) Close() error {
	if s.iter != nil {
		_ = s.iter.Close()
		s.iter = nil
	}
	return nil
}

func (s *rawIterator) Next() {
	if !s.Valid() {
		return
	}
	s.iter.Next()
	skipMeta(s.iter)
	if s.iter.Valid() {
		return
	}
	s.advanceDB()
}

func (s *rawIterator) Key() []byte {
	if !s.Valid() {
		return nil
	}
	return s.iter.Key()
}

func (s *rawIterator) Value() []byte {
	if !s.Valid() {
		return nil
	}
	return s.iter.Value()
}
