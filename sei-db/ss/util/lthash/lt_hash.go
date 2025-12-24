package lthash

import (
	"encoding/binary"
	"fmt"
	"sync"
	"unsafe"

	"github.com/zeebo/blake3"
)

var xofBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, LtHashBytes)
		return &buf
	},
}

var blake3HasherPool = sync.Pool{
	New: func() interface{} {
		return blake3.New()
	},
}

const (
	// LtHashSize is the number of uint16 limbs.
	LtHashSize = 1024
	// LtHashBytes is the size in bytes.
	LtHashBytes = LtHashSize * 2
)

// LtHash uses unsafe byte copies and requires little-endian.
var ltHashIsLittleEndian = func() bool {
	var x uint16 = 1
	b := (*[2]byte)(unsafe.Pointer(&x))
	return b[0] == 1
}()

func ensureLtHashLittleEndian() {
	if !ltHashIsLittleEndian {
		panic("LtHash requires little-endian architecture")
	}
}

// LtHash represents a 1024-element uint16 vector for lattice hashing
type LtHash struct {
	limbs [LtHashSize]uint16
}

// NewEmptyLtHash creates a new zero-initialized LtHash (identity element)
func NewEmptyLtHash() *LtHash {
	return &LtHash{}
}

// Identity sets the LtHash to the identity element (all zeros)
func (l *LtHash) Identity() {
	for i := range l.limbs {
		l.limbs[i] = 0
	}
}

// IsIdentity checks if the LtHash is the identity element (all zeros)
func (l *LtHash) IsIdentity() bool {
	for i := range l.limbs {
		if l.limbs[i] != 0 {
			return false
		}
	}
	return true
}

// MixIn adds another LtHash to this one (element-wise addition mod 2^16)
func (l *LtHash) MixIn(other interface{}) {
	switch v := other.(type) {
	case *LtHash:
		for i := 0; i < LtHashSize; i += 8 {
			l.limbs[i] += v.limbs[i]
			l.limbs[i+1] += v.limbs[i+1]
			l.limbs[i+2] += v.limbs[i+2]
			l.limbs[i+3] += v.limbs[i+3]
			l.limbs[i+4] += v.limbs[i+4]
			l.limbs[i+5] += v.limbs[i+5]
			l.limbs[i+6] += v.limbs[i+6]
			l.limbs[i+7] += v.limbs[i+7]
		}
	default:
		panic("MixIn: unsupported type")
	}
}

// MixOut subtracts another LtHash from this one (element-wise subtraction mod 2^16)
func (l *LtHash) MixOut(other interface{}) {
	switch v := other.(type) {
	case *LtHash:
		for i := 0; i < LtHashSize; i += 8 {
			l.limbs[i] -= v.limbs[i]
			l.limbs[i+1] -= v.limbs[i+1]
			l.limbs[i+2] -= v.limbs[i+2]
			l.limbs[i+3] -= v.limbs[i+3]
			l.limbs[i+4] -= v.limbs[i+4]
			l.limbs[i+5] -= v.limbs[i+5]
			l.limbs[i+6] -= v.limbs[i+6]
			l.limbs[i+7] -= v.limbs[i+7]
		}
	default:
		panic("MixOut: unsupported type")
	}
}

// Clone creates a deep copy of the LtHash
func (l *LtHash) Clone() *LtHash {
	clone := &LtHash{}
	copy(clone.limbs[:], l.limbs[:])
	return clone
}

// Bytes returns the raw byte representation of the LtHash vector (little-endian)
func (l *LtHash) Bytes() []byte {
	ensureLtHashLittleEndian()
	result := make([]byte, LtHashBytes)
	limbsBytes := (*[LtHashBytes]byte)(unsafe.Pointer(&l.limbs[0]))
	copy(result, limbsBytes[:])
	return result
}

// BytesTo writes the raw byte representation to the provided buffer (must be >= 2048 bytes).
func (l *LtHash) BytesTo(buf []byte) {
	ensureLtHashLittleEndian()
	if len(buf) < LtHashBytes {
		panic("buffer too small")
	}
	limbsBytes := (*[LtHashBytes]byte)(unsafe.Pointer(&l.limbs[0]))
	copy(buf, limbsBytes[:])
}

// FromRaw creates an LtHash from raw bytes (little-endian)
func FromRaw(data []byte) (*LtHash, error) {
	if len(data) != LtHashBytes {
		return nil, fmt.Errorf("invalid LtHash size: got %d, want %d", len(data), LtHashBytes)
	}
	lth := &LtHash{}
	for i := 0; i < LtHashSize; i++ {
		lth.limbs[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	return lth, nil
}

// checksumBufferPool is a pool for checksum computation buffers
var checksumBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, LtHashBytes)
		return &buf
	},
}

// Checksum computes the Blake3 hash of the LtHash vector (32 bytes)
func (l *LtHash) Checksum() [32]byte {
	bufPtr := checksumBufferPool.Get().(*[]byte)
	buf := *bufPtr
	l.BytesTo(buf)
	result := blake3.Sum256(buf)
	checksumBufferPool.Put(bufPtr)
	return result
}

var ltHashPool = sync.Pool{
	New: func() interface{} {
		return NewEmptyLtHash()
	},
}

// FromBytes creates an LtHash from arbitrary data using Blake3 XOF.
func FromBytes(data []byte) *LtHash {
	ensureLtHashLittleEndian()
	if len(data) == 0 {
		return NewEmptyLtHash()
	}

	hasher := blake3HasherPool.Get().(*blake3.Hasher)
	hasher.Reset()
	hasher.Write(data)
	digest := hasher.Digest()

	bufPtr := xofBufferPool.Get().(*[]byte)
	output := *bufPtr
	n, err := digest.Read(output)
	blake3HasherPool.Put(hasher)

	if err != nil || n != LtHashBytes {
		// Fallback (should rarely happen)
		xofBufferPool.Put(bufPtr)
		hash := blake3.Sum256(data)
		output = extendTo2048Bytes(hash[:])

		lth := &LtHash{}
		for i := 0; i < LtHashSize; i++ {
			lth.limbs[i] = binary.LittleEndian.Uint16(output[i*2:])
		}
		return lth
	}

	lth := ltHashPool.Get().(*LtHash)
	limbsBytes := (*[LtHashBytes]byte)(unsafe.Pointer(&lth.limbs[0]))
	copy(limbsBytes[:], output)
	xofBufferPool.Put(bufPtr)
	return lth
}

// extendTo2048Bytes expands a 32-byte seed to LtHashBytes.
func extendTo2048Bytes(seed []byte) []byte {
	result := make([]byte, LtHashBytes)
	copy(result[:32], seed)

	// Extend by hashing previous chunks
	for i := 32; i < LtHashBytes; i += 32 {
		chunk := result[i-32 : i]
		hash := blake3.Sum256(chunk)
		copy(result[i:], hash[:])
		if i+32 > LtHashBytes {
			copy(result[i:], hash[:LtHashBytes-i])
			break
		}
	}
	return result
}

