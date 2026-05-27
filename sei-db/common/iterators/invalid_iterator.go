package iterators

import dbm "github.com/tendermint/tm-db"

var _ dbm.Iterator = (*invalidIterator)(nil)

// invalidIterator is always invalid and reports a fixed construction error.
type invalidIterator struct {
	err error
}

// NewInvalidIterator returns an iterator that is never valid and reports err
// from Error(). Used when iterator construction fails but the API cannot
// return an error (e.g. RawGlobalIterator).
func NewInvalidIterator(err error) dbm.Iterator {
	return &invalidIterator{err: err}
}

func (m *invalidIterator) Close() error { return nil }

func (m *invalidIterator) Domain() ([]byte, []byte) { return nil, nil }

func (m *invalidIterator) Error() error { return m.err }

func (m *invalidIterator) Key() []byte { return nil }

func (m *invalidIterator) Next() {}

func (m *invalidIterator) Valid() bool { return false }

func (m *invalidIterator) Value() []byte { return nil }
