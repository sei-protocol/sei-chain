package lthash

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/zeebo/blake3"
)

const (
	// LtHashSize is the number of uint16 limbs (1024).
	LtHashSize = 1024
	// LtHashBytes is the byte size of an LtHash (2048).
	LtHashBytes = LtHashSize * 2
)

// LtHash is a 1024-element uint16 vector supporting homomorphic updates.
type LtHash struct {
	limbs [LtHashSize]uint16
}

// New creates a zero-initialized LtHash.
func New() *LtHash {
	return &LtHash{}
}

// Reset sets all elements to zero.
func (l *LtHash) Reset() {
	for i := range l.limbs {
		l.limbs[i] = 0
	}
}

// IsZero returns true if all elements are zero.
func (l *LtHash) IsZero() bool {
	for i := range l.limbs {
		if l.limbs[i] != 0 {
			return false
		}
	}
	return true
}

// MixIn adds other to this LtHash (element-wise mod 2^16). Nil is a no-op.
func (l *LtHash) MixIn(other *LtHash) {
	if other == nil {
		return
	}
	for i := 0; i < LtHashSize; i += 8 {
		l.limbs[i] += other.limbs[i]
		l.limbs[i+1] += other.limbs[i+1]
		l.limbs[i+2] += other.limbs[i+2]
		l.limbs[i+3] += other.limbs[i+3]
		l.limbs[i+4] += other.limbs[i+4]
		l.limbs[i+5] += other.limbs[i+5]
		l.limbs[i+6] += other.limbs[i+6]
		l.limbs[i+7] += other.limbs[i+7]
	}
}

// MixOut subtracts other from this LtHash (element-wise mod 2^16). Nil is a no-op.
func (l *LtHash) MixOut(other *LtHash) {
	if other == nil {
		return
	}
	for i := 0; i < LtHashSize; i += 8 {
		l.limbs[i] -= other.limbs[i]
		l.limbs[i+1] -= other.limbs[i+1]
		l.limbs[i+2] -= other.limbs[i+2]
		l.limbs[i+3] -= other.limbs[i+3]
		l.limbs[i+4] -= other.limbs[i+4]
		l.limbs[i+5] -= other.limbs[i+5]
		l.limbs[i+6] -= other.limbs[i+6]
		l.limbs[i+7] -= other.limbs[i+7]
	}
}

// Clone returns a deep copy.
func (l *LtHash) Clone() *LtHash {
	clone := &LtHash{}
	copy(clone.limbs[:], l.limbs[:])
	return clone
}

// Marshal returns the 2048-byte little-endian serialization.
func (l *LtHash) Marshal() []byte {
	result := make([]byte, LtHashBytes)
	l.MarshalTo(result)
	return result
}

// MarshalTo writes the serialization to buf (must be >= 2048 bytes).
func (l *LtHash) MarshalTo(buf []byte) {
	if len(buf) < LtHashBytes {
		panic("buffer too small")
	}
	for i := 0; i < LtHashSize; i++ {
		binary.LittleEndian.PutUint16(buf[i*2:(i+1)*2], l.limbs[i])
	}
}

// Unmarshal deserializes 2048 bytes into an LtHash.
func Unmarshal(data []byte) (*LtHash, error) {
	if len(data) != LtHashBytes {
		return nil, fmt.Errorf("invalid LtHash size: got %d, want %d", len(data), LtHashBytes)
	}
	lth := &LtHash{}
	for i := 0; i < LtHashSize; i++ {
		lth.limbs[i] = binary.LittleEndian.Uint16(data[i*2:])
	}
	return lth, nil
}

// Checksum returns the Blake3-256 hash of the serialized vector (32 bytes).
func (l *LtHash) Checksum() [32]byte {
	bufPtr := checksumBufferPool.Get().(*[]byte)
	buf := *bufPtr
	l.MarshalTo(buf)
	result := blake3.Sum256(buf)
	checksumBufferPool.Put(bufPtr)
	return result
}

// --- internal hash functions ---

// hash creates an LtHash from arbitrary data using Blake3 XOF.
func hash(data []byte) *LtHash {
	if len(data) == 0 {
		return New()
	}

	hasher := blake3HasherPool.Get().(*blake3.Hasher)
	hasher.Reset()
	_, _ = hasher.Write(data)
	digest := hasher.Digest()

	bufPtr := xofBufferPool.Get().(*[]byte)
	output := *bufPtr
	_, _ = digest.Read(output) // Blake3 XOF never errors and always fills buffer
	blake3HasherPool.Put(hasher)

	lth := ltHashPool.Get().(*LtHash)
	for i := 0; i < LtHashSize; i++ {
		lth.limbs[i] = binary.LittleEndian.Uint16(output[i*2 : (i+1)*2])
	}
	xofBufferPool.Put(bufPtr)
	return lth
}

// serializeKV encodes a KV pair with length-prefixed fields.
// Format: keyLen[4] || key || valueLen[4] || value
func serializeKV(key, value []byte) []byte {
	if len(key) == 0 || len(value) == 0 {
		return nil
	}
	keyLen := len(key)
	valueLen := len(value)

	if keyLen > 0xFFFFFFFF || valueLen > 0xFFFFFFFF {
		panic("serializeKV: length overflow")
	}

	buf := make([]byte, 4+keyLen+4+valueLen)
	off := 0
	binary.LittleEndian.PutUint32(buf[off:], uint32(keyLen))
	off += 4
	copy(buf[off:], key)
	off += keyLen
	binary.LittleEndian.PutUint32(buf[off:], uint32(valueLen))
	off += 4
	copy(buf[off:], value)
	return buf
}

// --- internal pools ---

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

var checksumBufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, LtHashBytes)
		return &buf
	},
}

var ltHashPool = sync.Pool{
	New: func() interface{} {
		return New()
	},
}

func getLtHashFromPool() *LtHash {
	lth := ltHashPool.Get().(*LtHash)
	lth.Reset()
	return lth
}

func putLtHashToPool(lth *LtHash) {
	if lth != nil {
		ltHashPool.Put(lth)
	}
}
