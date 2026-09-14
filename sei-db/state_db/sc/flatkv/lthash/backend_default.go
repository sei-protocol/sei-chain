package lthash

import (
	"encoding/binary"
	"sync"

	"github.com/zeebo/blake3"
)

// defaultBackend is the portable implementation: zeebo/blake3 for the XOF and
// plain Go loops for the limb arithmetic. It is always compiled in.
var defaultBackend = backend{
	name:   "default",
	expand: expandBlake3,
	add:    addScalar,
	sub:    subScalar,
}

func expandBlake3(data []byte, dst *LtHash) {
	hasher := blake3HasherPool.Get().(*blake3.Hasher)
	hasher.Reset()
	_, _ = hasher.Write(data)
	digest := hasher.Digest()

	bufPtr := xofBufferPool.Get().(*[]byte)
	output := *bufPtr
	_, _ = digest.Read(output) // Blake3 XOF never errors and always fills buffer
	blake3HasherPool.Put(hasher)

	for i := 0; i < LtHashSize; i++ {
		dst.limbs[i] = binary.LittleEndian.Uint16(output[i*2 : (i+1)*2])
	}
	xofBufferPool.Put(bufPtr)
}

func addScalar(dst, src *LtHash) {
	for i := 0; i < LtHashSize; i += 8 {
		dst.limbs[i] += src.limbs[i]
		dst.limbs[i+1] += src.limbs[i+1]
		dst.limbs[i+2] += src.limbs[i+2]
		dst.limbs[i+3] += src.limbs[i+3]
		dst.limbs[i+4] += src.limbs[i+4]
		dst.limbs[i+5] += src.limbs[i+5]
		dst.limbs[i+6] += src.limbs[i+6]
		dst.limbs[i+7] += src.limbs[i+7]
	}
}

func subScalar(dst, src *LtHash) {
	for i := 0; i < LtHashSize; i += 8 {
		dst.limbs[i] -= src.limbs[i]
		dst.limbs[i+1] -= src.limbs[i+1]
		dst.limbs[i+2] -= src.limbs[i+2]
		dst.limbs[i+3] -= src.limbs[i+3]
		dst.limbs[i+4] -= src.limbs[i+4]
		dst.limbs[i+5] -= src.limbs[i+5]
		dst.limbs[i+6] -= src.limbs[i+6]
		dst.limbs[i+7] -= src.limbs[i+7]
	}
}

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
