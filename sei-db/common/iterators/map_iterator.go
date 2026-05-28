package iterators

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	dbm "github.com/tendermint/tm-db"
)

var _ dbm.Iterator = (*mapIterator)(nil)

// Iterates over a map of key/value pairs.
type mapIterator struct {
	// kvPairs holds keys in iteration order, filtered to the domain.
	kvPairs []kvPair
	// currentIndex is the index of the entry returned by Key/Value.
	currentIndex int
	// start is the inclusive lower bound of the domain.
	start []byte
	// end is the exclusive upper bound of the domain.
	end []byte
}

type kvPair struct {
	key   []byte
	value []byte
}

// NewMapIterator returns an iterator over the union of maps in lexicographic order
// (or reverse lex order when ascending is false). start is inclusive; end is
// exclusive. nil start or end means unbounded on that side. Duplicate keys across
// maps are rejected.
func NewMapIterator(
	start []byte,
	end []byte,
	ascending bool,
	maps ...map[string][]byte,
) (dbm.Iterator, error) {
	pairs, err := buildMapPairs(start, end, ascending, maps...)
	if err != nil {
		return nil, err
	}
	return &mapIterator{
		kvPairs: pairs,
		start:   start,
		end:     end,
	}, nil
}

func buildMapPairs(start, end []byte, ascending bool, maps ...map[string][]byte) ([]kvPair, error) {
	if start != nil && end != nil && bytes.Compare(start, end) > 0 {
		return nil, nil
	}

	total := 0
	for _, data := range maps {
		total += len(data)
	}
	if total == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, total)
	pairs := make([]kvPair, 0, total)
	for _, data := range maps {
		if data == nil {
			continue
		}
		for k, v := range data {
			if _, dup := seen[k]; dup {
				return nil, fmt.Errorf("duplicate key %q", k)
			}
			seen[k] = struct{}{}

			key := []byte(k)
			if !keyInRange(key, start, end) {
				continue
			}
			pairs = append(pairs, kvPair{
				key:   utils.Clone(key),
				value: utils.Clone(v),
			})
		}
	}

	sort.Slice(pairs, func(i, j int) bool {
		cmp := bytes.Compare(pairs[i].key, pairs[j].key)
		if ascending {
			return cmp < 0
		}
		return cmp > 0
	})
	return pairs, nil
}

func keyInRange(key, start, end []byte) bool {
	if start != nil && bytes.Compare(key, start) < 0 {
		return false
	}
	if end != nil && bytes.Compare(key, end) >= 0 {
		return false
	}
	return true
}

func (m *mapIterator) Close() error {
	m.kvPairs = nil
	m.currentIndex = 0
	return nil
}

func (m *mapIterator) Domain() ([]byte, []byte) {
	return m.start, m.end
}

func (m *mapIterator) Error() error {
	return nil
}

func (m *mapIterator) Key() []byte {
	if !m.Valid() {
		return nil
	}
	return m.kvPairs[m.currentIndex].key
}

func (m *mapIterator) Next() {
	if !m.Valid() {
		return
	}
	m.currentIndex++
}

func (m *mapIterator) Valid() bool {
	return m.currentIndex >= 0 && m.currentIndex < len(m.kvPairs)
}

func (m *mapIterator) Value() []byte {
	if !m.Valid() {
		return nil
	}
	return m.kvPairs[m.currentIndex].value
}
