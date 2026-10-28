package solstorage

import (
	"encoding/binary"

	"github.com/ethereum/go-ethereum/common"
)

// Set is an OpenZeppelin EnumerableSet (AddressSet, UintSet or Bytes32Set)
// whose two-slot struct begins at Base: the _values array length at Base and
// the 1-based _positions mapping at Base+1.
type Set struct {
	Store Store
	Base  common.Hash
}

func (s Set) Len() uint64 {
	return s.Store.Uint64(s.Base, 0)
}

// At returns the member at index i, which must be below Len.
func (s Set) At(i uint64) common.Hash {
	return s.Store.Word(Offset(ArrayData(s.Base), i))
}

func (s Set) Contains(v common.Hash) bool {
	return s.position(v) != 0
}

// Add inserts v, reporting whether it was absent.
func (s Set) Add(v common.Hash) bool {
	if s.Contains(v) {
		return false
	}
	n := s.Len()
	s.Store.SetWord(Offset(ArrayData(s.Base), n), v)
	s.Store.SetWord(s.Base, Slot(n+1))
	s.setPosition(v, n+1)
	return true
}

// Remove deletes v by swap-and-pop, reporting whether it was present.
func (s Set) Remove(v common.Hash) bool {
	pos := s.position(v)
	if pos == 0 || pos > s.Len() {
		return false
	}
	last := s.Len() - 1
	data := ArrayData(s.Base)
	if idx := pos - 1; idx != last {
		moved := s.Store.Word(Offset(data, last))
		s.Store.SetWord(Offset(data, idx), moved)
		s.setPosition(moved, pos)
	}
	s.Store.SetWord(Offset(data, last), common.Hash{})
	s.Store.SetWord(s.Base, Slot(last))
	s.setPosition(v, 0)
	return true
}

func (s Set) positionSlot(v common.Hash) common.Hash {
	return MapWord(Offset(s.Base, 1), v)
}

func (s Set) position(v common.Hash) uint64 {
	return s.Store.Uint64(s.positionSlot(v), 0)
}

func (s Set) setPosition(v common.Hash, pos uint64) {
	s.Store.SetWord(s.positionSlot(v), Slot(pos))
}

// AddressKey is the set member encoding of an address.
func AddressKey(a common.Address) common.Hash {
	return common.BytesToHash(a[:])
}

// MemberAddress decodes a set member written by AddressKey.
func MemberAddress(v common.Hash) common.Address {
	return common.BytesToAddress(v[12:])
}

// MemberUint64 decodes a set member written by Slot.
func MemberUint64(v common.Hash) uint64 {
	return binary.BigEndian.Uint64(v[24:])
}
