package snapshot

import (
	"bytes"
	"sort"

	"github.com/sei-protocol/sei-chain/sei-db/proto"
)

// modelEngine is a deliberately naive, obviously-correct reference implementation of the
// SnapshotEngine's observable read semantics, used as an oracle for differential testing. It makes a
// full deep copy of the keyspace on every Commit(), so there is no clever versioning to get wrong.
//
// Conventions (matching the real engine):
//   - versions start at 1; Commit() seals the current version, then advances.
//   - a nil value passed to Set is a delete; tombstones remove the key from the materialized state
//     and are recorded as a nil value in that version's diff.
//   - reads return (value, found); a found key always has a non-nil (possibly empty) value.
type modelEngine struct {
	// current is the materialized state of the live (mutable) version: key -> value. Deleted keys are
	// absent. Seeded from the backing DB's initial contents.
	current map[string][]byte
	// pending is the set of mutations applied to the live version since the last Commit(). A nil
	// value records a delete.
	pending map[string][]byte
	// versions holds sealed snapshots by version number.
	versions       map[uint64]*modelVersion
	currentVersion uint64
}

type modelVersion struct {
	// full is a complete deep copy of the materialized state as of this version.
	full map[string][]byte
	// diff is the set of mutations applied in this version only (nil value == delete).
	diff map[string][]byte
}

func newModelEngine(seed map[string][]byte) *modelEngine {
	return &modelEngine{
		current:        cloneMap(seed),
		pending:        make(map[string][]byte),
		versions:       make(map[uint64]*modelVersion),
		currentVersion: 1,
	}
}

func (m *modelEngine) Set(key, value []byte) {
	if value == nil {
		m.Delete(key)
		return
	}
	k := string(key)
	m.current[k] = cloneBytes(value)
	m.pending[k] = cloneBytes(value)
}

func (m *modelEngine) Delete(key []byte) {
	k := string(key)
	delete(m.current, k)
	m.pending[k] = nil
}

func (m *modelEngine) BatchSet(muts []*proto.KVPair) {
	for i := range muts {
		if muts[i].Delete {
			m.Delete(muts[i].Key)
		} else {
			m.Set(muts[i].Key, muts[i].Value)
		}
	}
}

// Snapshot seals the current version and advances, returning the sealed version number.
func (m *modelEngine) Commit() uint64 {
	v := m.currentVersion
	m.versions[v] = &modelVersion{
		full: cloneMap(m.current),
		diff: m.pending,
	}
	m.pending = make(map[string][]byte)
	m.currentVersion++
	return v
}

// GetLive returns the value for key in the live (mutable) version.
func (m *modelEngine) GetLive(key []byte) ([]byte, bool) {
	v, ok := m.current[string(key)]
	return v, ok
}

// GetAt returns the value for key as of the given sealed version.
func (m *modelEngine) GetAt(version uint64, key []byte) ([]byte, bool) {
	mv := m.versions[version]
	v, ok := mv.full[string(key)]
	return v, ok
}

// DiffAt returns the mutations applied in the given sealed version (nil value == delete).
func (m *modelEngine) DiffAt(version uint64) map[string][]byte {
	return m.versions[version].diff
}

// IterateAt returns the ascending, tombstone-free key/value pairs of the given sealed version.
func (m *modelEngine) IterateAt(version uint64) []kvPair {
	return sortedEntries(m.versions[version].full)
}

func sortedEntries(store map[string][]byte) []kvPair {
	out := make([]kvPair, 0, len(store))
	for k, v := range store {
		if v == nil {
			continue
		}
		out = append(out, kvPair{key: []byte(k), value: v})
	}
	sort.Slice(out, func(i, j int) bool { return bytes.Compare(out[i].key, out[j].key) < 0 })
	return out
}

func cloneMap(src map[string][]byte) map[string][]byte {
	dst := make(map[string][]byte, len(src))
	for k, v := range src {
		dst[k] = cloneBytes(v)
	}
	return dst
}

// cloneBytes deep-copies b, preserving the nil vs non-nil-empty distinction (nil stays nil; a
// zero-length non-nil slice stays zero-length non-nil).
func cloneBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	return append([]byte{}, b...)
}
