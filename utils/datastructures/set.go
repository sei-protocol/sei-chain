package datastructures

import "sync"

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

func (s *SyncSet[T]) Remove(val T) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.dict, val)
}

func (s *SyncSet[T]) RemoveAll(vals []T) {
	for _, val := range vals {
		s.Remove(val)
	}
}

func (s *SyncSet[T]) Contains(val T) bool {
	_, ok := s.dict[val]
	return ok
}

func (s *SyncSet[T]) ToSlice() []T {
	res := []T{}
	for s := range s.dict {
		res = append(res, s)
	}
	return res
}

func (s *SyncSet[T]) Size() int {
	return len(s.dict)
}
