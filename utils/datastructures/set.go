package datastructures

import (
	"sort"
	"sync"
)

// A set-like data structure that is guaranteed to be data race free during write
// operations. It can return internal data as a slice with a comparator provided,
// so that the resulting slice has a deterministic ordering.
type SyncSet[T comparable] struct {
	dict map[T]bool
	mu   *sync.Mutex
}

func NewSyncSet[T comparable](initial []T) SyncSet[T] {
	res := SyncSet[T]{
		dict: map[T]bool{},
		mu:   &sync.Mutex{},
	}
	for _, s := range initial {
		res.dict[s] = true
	}
	return res
}

func (s *SyncSet[T]) Add(val T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dict[val] = true
}

func (s *SyncSet[T]) AddAll(vals []T) {
	for _, val := range vals {
		s.Add(val)
	}
}

func (s *SyncSet[T]) Remove(val T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.dict, val)
}

func (s *SyncSet[T]) RemoveAll(vals []T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, val := range vals {
		delete(s.dict, val)
	}
}

func (s *SyncSet[T]) Contains(val T) bool {
	_, ok := s.dict[val]
	return ok
}

func (s *SyncSet[T]) ToOrderedSlice(comparator func(T, T) bool) []T {
	res := []T{}
	for s := range s.dict {
		res = append(res, s)
	}
	sort.SliceStable(res, func(i, j int) bool {
		return comparator(res[i], res[j])
	})

	return res
}

func (s *SyncSet[T]) Size() int {
	return len(s.dict)
}

func StringComparator(s1 string, s2 string) bool {
	return s1 < s2
}
