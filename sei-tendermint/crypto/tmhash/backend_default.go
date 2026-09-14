package tmhash

import (
	"crypto/sha256"
	"hash"
)

// defaultBackend hashes one message at a time with crypto/sha256, which uses
// the SHA-NI single-lane instructions where the CPU has them.
var defaultBackend = backend{
	name:     "default",
	lanes:    1,
	sumBatch: sumBatchScalar,
}

func sumBatchScalar(prefix []byte, msgs [][]byte, out [][Size]byte) {
	h := sha256.New()
	for i, msg := range msgs {
		sumOne(h, prefix, msg, &out[i])
	}
}

// sumScalarAt hashes msgs[i] into out[i] for every i in idx.
func sumScalarAt(prefix []byte, msgs [][]byte, out [][Size]byte, idx []int) {
	if len(idx) == 0 {
		return
	}
	h := sha256.New()
	for _, i := range idx {
		sumOne(h, prefix, msgs[i], &out[i])
	}
}

func sumOne(h hash.Hash, prefix, msg []byte, out *[Size]byte) {
	h.Reset()
	h.Write(prefix)
	h.Write(msg)
	h.Sum(out[:0])
}
