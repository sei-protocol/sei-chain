package tmhash

import "crypto/sha256"

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
		h.Reset()
		h.Write(prefix)
		h.Write(msg)
		h.Sum(out[i][:0])
	}
}
