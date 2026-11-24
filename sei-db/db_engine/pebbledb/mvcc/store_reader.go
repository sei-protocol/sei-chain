package mvcc

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/cockroachdb/pebble/v2"
)

// StoreReader walks the keys of one store in a DB opened read-only, in key
// order, yielding for each key the value a read at the DB's latest version
// returns.
type StoreReader struct {
	db     *offlineDB
	itr    *pebble.Iterator
	prefix []byte
	// next is the entry under the iterator, already decoded, and hasNext is
	// false once the iterator is exhausted.
	next    storeEntry
	hasNext bool

	key     []byte
	version int64
	value   []byte
}

// OpenStoreReader opens the DB at dir read-only to walk the keys of storeKey.
// It fails if the DB's changelog holds a version the DB has not applied.
func OpenStoreReader(dir, storeKey string) (*StoreReader, error) {
	m, err := openReplayedDB(dir, true)
	if err != nil {
		return nil, err
	}
	prefix := storePrefix(storeKey)
	itr, err := m.db.NewIter(&pebble.IterOptions{
		LowerBound: MVCCEncode(prefix, 0, m.info.Descending),
		UpperBound: MVCCEncode(prefixEnd(prefix), 0, m.info.Descending),
	})
	if err != nil {
		_ = m.close()
		return nil, fmt.Errorf("failed to create PebbleDB iterator: %w", err)
	}
	r := &StoreReader{db: m, itr: itr, prefix: prefix}
	if err := r.advance(itr.First()); err != nil {
		_ = r.Close()
		return nil, err
	}
	return r, nil
}

// Info returns the DB's encoding, version markers and store keys.
func (r *StoreReader) Info() DBInfo {
	return r.db.info
}

// Next moves to the next key a read at the latest version finds a value for,
// and reports false once every key has been read.
func (r *StoreReader) Next() (bool, error) {
	for r.hasNext {
		userKey := r.next.userKey
		newest, found := storeEntry{}, false
		for r.hasNext && bytes.Equal(r.next.userKey, userKey) {
			if r.next.version <= r.db.info.Latest && (!found || r.next.version > newest.version) {
				newest, found = r.next, true
			}
			if err := r.advance(r.itr.Next()); err != nil {
				return false, err
			}
		}
		if found && newest.live {
			r.key, r.version, r.value = bytes.TrimPrefix(userKey, r.prefix), newest.version, newest.value
			return true, nil
		}
	}
	return false, r.itr.Error()
}

// advance decodes the entry the iterator moved to, valid being what the move
// reported.
func (r *StoreReader) advance(valid bool) error {
	r.hasNext = valid
	if !valid {
		return nil
	}
	entry, err := r.db.decodeEntry(r.itr)
	if err != nil {
		return err
	}
	r.next = entry
	return nil
}

// Key returns the current key, relative to the store.
func (r *StoreReader) Key() []byte {
	return r.key
}

// Version returns the version of the current key's value.
func (r *StoreReader) Version() int64 {
	return r.version
}

// Value returns the current key's value.
func (r *StoreReader) Value() []byte {
	return r.value
}

// Close releases the iterator and closes the DB.
func (r *StoreReader) Close() error {
	return errors.Join(r.itr.Close(), r.db.close())
}
