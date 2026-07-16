package iterators

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/sei-protocol/sei-chain/sei-db/common/utils"
	dbm "github.com/tendermint/tm-db"
)

var _ dbm.Iterator = (*mapIterator[any])(nil)

// Iterates over a map of key/value pairs.
type mapIterator[T any] struct {
	kvPairs      []kvPair
	currentIndex int
	start        []byte
	end          []byte
}

type kvPair struct {
	key   []byte
	value []byte
}

// BytesSerializer is a pass-through serializer for map[string][]byte.
func BytesSerializer(v []byte) ([]byte, error) {
	return v, nil
}

// NewMapIterator returns an iterator over the union of maps in lexicographic order
// (or reverse lex order when ascending is false). start is inclusive; end is
// exclusive. nil start or end means unbounded on that side. Duplicate keys across
// maps are rejected. Values are serialized with serializer before iteration.
func NewMapIterator[T any](
	start []byte,
	end []byte,
	ascending bool,
	serializer func(T) ([]byte, error),
	maps ...map[string]T,
) (dbm.Iterator, error) {
	if serializer == nil {
		return nil, fmt.Errorf("nil serializer")
	}
	pairs, err := buildMapPairs(start, end, ascending, serializer, maps...)
	if err != nil {
		return nil, err
	}
	return &mapIterator[T]{
		kvPairs: pairs,
		start:   start,
		end:     end,
	}, nil
}

func buildMapPairs[T any](
	start, end []byte,
	ascending bool,
	serializer func(T) ([]byte, error),
	maps ...map[string]T,
) ([]kvPair, error) {
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

			serialized, err := serializer(v)
			if err != nil {
				return nil, fmt.Errorf("serialize key %q: %w", k, err)
			}
			pairs = append(pairs, kvPair{
				key:   utils.Clone(key),
				value: utils.Clone(serialized),
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

func (m *mapIterator[T]) Close() error {
	m.kvPairs = nil
	m.currentIndex = 0
	return nil
}

func (m *mapIterator[T]) Domain() ([]byte, []byte) {
	return m.start, m.end
}

func (m *mapIterator[T]) Error() error {
	return nil
}

func (m *mapIterator[T]) Key() []byte {
	if !m.Valid() {
		return nil
	}
	return m.kvPairs[m.currentIndex].key
}

func (m *mapIterator[T]) Next() {
	if !m.Valid() {
		return
	}
	m.currentIndex++
}

func (m *mapIterator[T]) Valid() bool {
	return m.currentIndex >= 0 && m.currentIndex < len(m.kvPairs)
}

func (m *mapIterator[T]) Value() []byte {
	if !m.Valid() {
		return nil
	}
	return m.kvPairs[m.currentIndex].value
}
