package solstorage

import (
	"encoding/binary"
	"math/big"

	"github.com/ethereum/go-ethereum/common"

	"github.com/sei-protocol/sei-chain/giga/evmonly/precompiles"
)

// Store is one account's storage, read and written in Solidity's encoding.
// Packed fields are addressed by slot and byte offset, where offset counts from
// the low-order end of the word exactly as solc's storage layout reports it.
type Store struct {
	State   precompiles.State
	Address common.Address
}

func (s Store) Word(slot common.Hash) common.Hash {
	return s.State.GetState(s.Address, slot)
}

func (s Store) SetWord(slot, value common.Hash) {
	s.State.SetState(s.Address, slot, value)
}

func (s Store) Uint256(slot common.Hash) *big.Int {
	w := s.Word(slot)
	return new(big.Int).SetBytes(w[:])
}

// SetUint256 stores v, which must be non-negative and fit in 256 bits. A nil v
// stores zero.
func (s Store) SetUint256(slot common.Hash, v *big.Int) {
	if v == nil {
		s.SetWord(slot, common.Hash{})
		return
	}
	s.SetWord(slot, common.BigToHash(v))
}

func (s Store) Uint64(slot common.Hash, offset int) uint64 {
	w := s.Word(slot)
	return binary.BigEndian.Uint64(w[32-offset-8 : 32-offset])
}

func (s Store) SetUint64(slot common.Hash, offset int, v uint64) {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	s.setPacked(slot, offset, b[:])
}

func (s Store) Int64(slot common.Hash, offset int) int64 {
	return int64(s.Uint64(slot, offset)) //nolint:gosec // two's complement reinterpretation
}

func (s Store) SetInt64(slot common.Hash, offset int, v int64) {
	s.SetUint64(slot, offset, uint64(v)) //nolint:gosec // two's complement reinterpretation
}

func (s Store) Int32(slot common.Hash, offset int) int32 {
	w := s.Word(slot)
	return int32(binary.BigEndian.Uint32(w[32-offset-4 : 32-offset])) //nolint:gosec // two's complement reinterpretation
}

func (s Store) SetInt32(slot common.Hash, offset int, v int32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(v)) //nolint:gosec // two's complement reinterpretation
	s.setPacked(slot, offset, b[:])
}

func (s Store) Uint8(slot common.Hash, offset int) uint8 {
	w := s.Word(slot)
	return w[31-offset]
}

func (s Store) SetUint8(slot common.Hash, offset int, v uint8) {
	s.setPacked(slot, offset, []byte{v})
}

func (s Store) Bool(slot common.Hash, offset int) bool {
	w := s.Word(slot)
	return w[31-offset] != 0
}

func (s Store) SetBool(slot common.Hash, offset int, v bool) {
	var b [1]byte
	if v {
		b[0] = 1
	}
	s.setPacked(slot, offset, b[:])
}

// setPacked overwrites len(b) bytes at offset, leaving the rest of the word.
func (s Store) setPacked(slot common.Hash, offset int, b []byte) {
	w := s.Word(slot)
	copy(w[32-offset-len(b):32-offset], b)
	s.SetWord(slot, w)
}

// maxStringPrealloc caps the buffer String allocates up front from a stored
// length, so a corrupt length cannot force a large allocation.
const maxStringPrealloc = 64 << 10

// String reads a string or bytes value in Solidity's native encoding.
func (s Store) String(slot common.Hash) string {
	w := s.Word(slot)
	if w[31]&1 == 0 {
		return string(w[:w[31]/2])
	}
	n := new(big.Int).SetBytes(w[:])
	n.Rsh(n, 1)
	length := n.Uint64()
	out := make([]byte, 0, min(length, maxStringPrealloc))
	data := ArrayData(slot)
	for i := uint64(0); uint64(len(out)) < length && !s.State.Failed(); i++ {
		chunk := s.Word(Offset(data, i))
		out = append(out, chunk[:min(32, length-uint64(len(out)))]...)
	}
	return string(out)
}

// SetString writes v in Solidity's native encoding, zeroing any chunks of the
// previous value that v no longer covers.
func (s Store) SetString(slot common.Hash, v string) {
	oldChunks := s.StringChunks(slot)
	var head common.Hash
	newChunks := uint64(0)
	if len(v) <= 31 {
		copy(head[:], v)
		head[31] = byte(len(v) * 2) //nolint:gosec // len(v) <= 31
	} else {
		head = common.BigToHash(new(big.Int).SetUint64(uint64(len(v))*2 + 1))
		data := ArrayData(slot)
		for off := 0; off < len(v); off += 32 {
			var chunk common.Hash
			copy(chunk[:], v[off:])
			s.SetWord(Offset(data, newChunks), chunk)
			newChunks++
		}
	}
	data := ArrayData(slot)
	for i := newChunks; i < oldChunks; i++ {
		s.SetWord(Offset(data, i), common.Hash{})
	}
	s.SetWord(slot, head)
}

// StringChunks returns how many data chunks the string or bytes value at slot
// occupies, 0 for one held in the slot itself.
func (s Store) StringChunks(slot common.Hash) uint64 {
	w := s.Word(slot)
	if w[31]&1 == 0 {
		return 0
	}
	n := new(big.Int).SetBytes(w[:])
	n.Rsh(n, 1)
	return (n.Uint64() + 31) / 32
}
