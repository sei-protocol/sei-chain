package structures

import (
	"cmp"
	"iter"
)

// SortedMap is a map that keeps its entries in ascending key order. Get(), Remove(), and a Put() of a new key above or
// below every key present are O(1). A Put() of any other new key is O(n). Not thread safe.
type SortedMap[K cmp.Ordered, V any] struct {
	// Every entry, by key.
	index map[K]*sortedMapNode[K, V]

	// The entry with the lowest key, or nil if the map is empty.
	front *sortedMapNode[K, V]

	// The entry with the highest key, or nil if the map is empty.
	back *sortedMapNode[K, V]
}

// NewSortedMap returns an empty map.
func NewSortedMap[K cmp.Ordered, V any]() *SortedMap[K, V] {
	return &SortedMap[K, V]{index: make(map[K]*sortedMapNode[K, V])}
}

// Put sets the value at key.
func (m *SortedMap[K, V]) Put(key K, value V) {
	if node, ok := m.index[key]; ok {
		node.value = value
		return
	}
	node := &sortedMapNode[K, V]{key: key, value: value}
	m.index[key] = node

	// The new entry goes after the highest key below it, or at the front if there is none.
	var prev *sortedMapNode[K, V]
	if m.front != nil && key > m.front.key {
		prev = m.back
		for prev.key > key {
			prev = prev.prev
		}
	}
	m.insertAfter(node, prev)
}

// Get returns the value at key, and false if key is not present.
func (m *SortedMap[K, V]) Get(key K) (V, bool) {
	node, ok := m.index[key]
	if !ok {
		var zero V
		return zero, false
	}
	return node.value, true
}

// Remove removes key and returns its value, and false if key was not present.
func (m *SortedMap[K, V]) Remove(key K) (V, bool) {
	node, ok := m.index[key]
	if !ok {
		var zero V
		return zero, false
	}
	m.unlink(node)
	return node.value, true
}

// PeekFront returns the entry with the lowest key, and false if the map is empty.
func (m *SortedMap[K, V]) PeekFront() (key K, value V, ok bool) {
	if m.front == nil {
		return key, value, false
	}
	return m.front.key, m.front.value, true
}

// PopFront removes and returns the entry with the lowest key, and false if the map is empty.
func (m *SortedMap[K, V]) PopFront() (key K, value V, ok bool) {
	if m.front == nil {
		return key, value, false
	}
	node := m.front
	m.unlink(node)
	return node.key, node.value, true
}

// PeekBack returns the entry with the highest key, and false if the map is empty.
func (m *SortedMap[K, V]) PeekBack() (key K, value V, ok bool) {
	if m.back == nil {
		return key, value, false
	}
	return m.back.key, m.back.value, true
}

// PopBack removes and returns the entry with the highest key, and false if the map is empty.
func (m *SortedMap[K, V]) PopBack() (key K, value V, ok bool) {
	if m.back == nil {
		return key, value, false
	}
	node := m.back
	m.unlink(node)
	return node.key, node.value, true
}

// Next returns the entry after key, and false if key is not present or is the highest key.
func (m *SortedMap[K, V]) Next(key K) (next K, value V, ok bool) {
	node, found := m.index[key]
	if !found || node.next == nil {
		return next, value, false
	}
	return node.next.key, node.next.value, true
}

// Len returns the number of entries in the map.
func (m *SortedMap[K, V]) Len() int {
	return len(m.index)
}

// Forward returns an iterator that yields (key, value) pairs in ascending key order.
func (m *SortedMap[K, V]) Forward() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for node := m.front; node != nil; node = node.next {
			if !yield(node.key, node.value) {
				return
			}
		}
	}
}

// insertAfter links node into the list after prev, or at the front if prev is nil.
func (m *SortedMap[K, V]) insertAfter(node *sortedMapNode[K, V], prev *sortedMapNode[K, V]) {
	node.prev = prev
	if prev == nil {
		node.next = m.front
		m.front = node
	} else {
		node.next = prev.next
		prev.next = node
	}
	if node.next == nil {
		m.back = node
	} else {
		node.next.prev = node
	}
}

// unlink removes node from the map.
func (m *SortedMap[K, V]) unlink(node *sortedMapNode[K, V]) {
	delete(m.index, node.key)
	if node.prev == nil {
		m.front = node.next
	} else {
		node.prev.next = node.next
	}
	if node.next == nil {
		m.back = node.prev
	} else {
		node.next.prev = node.prev
	}
}

// sortedMapNode is one entry of a SortedMap.
type sortedMapNode[K cmp.Ordered, V any] struct {
	// The entry's key.
	key K

	// The entry's value.
	value V

	// The entry with the next lower key, or nil if this entry has the lowest.
	prev *sortedMapNode[K, V]

	// The entry with the next higher key, or nil if this entry has the highest.
	next *sortedMapNode[K, V]
}
