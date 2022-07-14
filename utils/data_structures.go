package utils

type StringSet struct {
	dict map[string]bool
}

func NewStringSet(initial []string) StringSet {
	res := StringSet{dict: map[string]bool{}}
	for _, s := range initial {
		res.dict[s] = true
	}
	return res
}

func (s *StringSet) Add(val string) {
	s.dict[val] = true
}

func (s *StringSet) Remove(val string) {
	delete(s.dict, val)
}

func (s *StringSet) RemoveAll(vals []string) {
	for _, val := range vals {
		s.Remove(val)
	}
}

func (s *StringSet) Contains(val string) bool {
	_, ok := s.dict[val]
	return ok
}

func (s *StringSet) ToSlice() []string {
	res := []string{}
	for s := range s.dict {
		res = append(res, s)
	}
	return res
}

func (s *StringSet) Size() int {
	return len(s.dict)
}

type UInt64Set struct {
	dict map[uint64]bool
}

func NewUInt64Set(initial []uint64) UInt64Set {
	res := UInt64Set{dict: map[uint64]bool{}}
	for _, s := range initial {
		res.dict[s] = true
	}
	return res
}

func (s *UInt64Set) Add(val uint64) {
	s.dict[val] = true
}

func (s *UInt64Set) Remove(val uint64) {
	delete(s.dict, val)
}

func (s *UInt64Set) Contains(val uint64) bool {
	_, ok := s.dict[val]
	return ok
}

func (s *UInt64Set) ToSlice() []uint64 {
	res := []uint64{}
	for s := range s.dict {
		res = append(res, s)
	}
	return res
}
