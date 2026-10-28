package solstorage

import (
	"bytes"

	"github.com/ethereum/go-ethereum/common"
)

// Heap is a min-heap of distinct words, ordered as big-endian integers, in a
// two-slot struct at Base with EnumerableSet's layout: the heap array at Base
// and each word's 1-based array position in a mapping at Base+1.
type Heap struct {
	set Set
}

// NewHeap returns the heap whose struct begins at base.
func NewHeap(store Store, base common.Hash) Heap {
	return Heap{Set{Store: store, Base: base}}
}

func (h Heap) Len() uint64 {
	return h.set.Len()
}

// At returns the word at array index i, which must be below Len. Index 0 is
// the minimum.
func (h Heap) At(i uint64) common.Hash {
	return h.set.At(i)
}

func (h Heap) Contains(v common.Hash) bool {
	return h.set.Contains(v)
}

// Push inserts v, reporting whether it was absent.
func (h Heap) Push(v common.Hash) bool {
	if !h.set.Add(v) {
		return false
	}
	h.up(h.Len() - 1)
	return true
}

// Remove deletes v, reporting whether it was present.
func (h Heap) Remove(v common.Hash) bool {
	pos := h.set.position(v)
	if pos == 0 || pos > h.Len() {
		return false
	}
	i, last := pos-1, h.Len()-1
	if i != last {
		h.swap(i, last)
	}
	h.set.Store.SetWord(h.slot(last), common.Hash{})
	h.set.Store.SetWord(h.set.Base, Slot(last))
	h.set.setPosition(v, 0)
	if i != last {
		h.down(i)
		h.up(i)
	}
	return true
}

func (h Heap) up(i uint64) {
	for i > 0 {
		parent := (i - 1) / 2
		if !h.less(i, parent) {
			return
		}
		h.swap(i, parent)
		i = parent
	}
}

func (h Heap) down(i uint64) {
	n := h.Len()
	for {
		least := i
		for _, child := range []uint64{2*i + 1, 2*i + 2} {
			if child < n && h.less(child, least) {
				least = child
			}
		}
		if least == i {
			return
		}
		h.swap(i, least)
		i = least
	}
}

func (h Heap) less(i, j uint64) bool {
	a, b := h.At(i), h.At(j)
	return bytes.Compare(a[:], b[:]) < 0
}

func (h Heap) swap(i, j uint64) {
	a, b := h.At(i), h.At(j)
	h.set.Store.SetWord(h.slot(i), b)
	h.set.Store.SetWord(h.slot(j), a)
	h.set.setPosition(b, i+1)
	h.set.setPosition(a, j+1)
}

func (h Heap) slot(i uint64) common.Hash {
	return Offset(ArrayData(h.set.Base), i)
}
