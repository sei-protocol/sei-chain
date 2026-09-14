//go:build goexperiment.simd && amd64

package lthash

import (
	"encoding/binary"
	"math/bits"
	"unsafe"

	"simd/archsimd"
)

//go:generate go run gen_blake3_xof16.go

// simdBackendName is the name reported by ActiveBackend for the AVX-512 path.
const simdBackendName = "simd"

// simdBackend returns the AVX-512 backend when the CPU can run it. The XOF
// kernel needs AVX-512F (Uint32x16) and VBMI2 (VPSHRDD rotates); the limb
// arithmetic needs AVX-512BW (Uint16x32).
func simdBackend() (backend, bool) {
	if !archsimd.X86.AVX512() || !archsimd.X86.AVX512VBMI2() {
		return backend{}, false
	}
	return backend{
		name:   simdBackendName,
		expand: expandSIMD,
		add:    addSIMD,
		sub:    subSIMD,
	}, true
}

const (
	blake3BlockLen = 64
	blake3ChunkLen = 1024

	blake3ChunkStart = 1 << 0
	blake3ChunkEnd   = 1 << 1
	blake3Root       = 1 << 3
)

var blake3IV = [8]uint32{
	0x6A09E667, 0xBB67AE85, 0x3C6EF372, 0xA54FF53A,
	0x510E527F, 0x9B05688C, 0x1F83D9AB, 0x5BE0CD19,
}

// expandSIMD computes the 2048-byte Blake3 XOF of data as two 16-lane root
// compressions. Inputs longer than one chunk need the Blake3 tree, which the
// default backend already implements.
func expandSIMD(data []byte, dst *LtHash) {
	if len(data) > blake3ChunkLen {
		expandBlake3(data, dst)
		return
	}
	var in xof16Inputs
	singleChunkRoot(data, &in)
	out := (*[2][16][16]uint32)(unsafe.Pointer(&dst.limbs)) //nolint:gosec // G103: same size, little-endian limb layout
	xof16(&in, &out[0])
	for lane := range in[12] {
		in[12][lane] = 16
	}
	xof16(&in, &out[1])
}

// singleChunkRoot compresses all but the last block of a one-chunk message
// into a chaining value and broadcasts the root compression inputs into in.
func singleChunkRoot(data []byte, in *xof16Inputs) {
	cv := blake3IV
	flags := uint32(blake3ChunkStart)
	var block [16]uint32
	for len(data) > blake3BlockLen {
		loadBlock(&block, data[:blake3BlockLen])
		out := blake3Compress(&cv, &block, 0, blake3BlockLen, flags)
		copy(cv[:], out[:8])
		flags = 0
		data = data[blake3BlockLen:]
	}
	var last [blake3BlockLen]byte
	copy(last[:], data)
	loadBlock(&block, last[:])
	flags |= blake3ChunkEnd | blake3Root

	for lane := 0; lane < 16; lane++ {
		for i := 0; i < 8; i++ {
			in[i][lane] = cv[i]
			in[8+i][lane] = blake3IV[i]
		}
		in[12][lane] = 0
		in[13][lane] = 0
		in[14][lane] = uint32(len(data)) //nolint:gosec // G115: len(data) <= blake3BlockLen
		in[15][lane] = flags
		for i := 0; i < 16; i++ {
			in[16+i][lane] = block[i]
		}
	}
}

func loadBlock(block *[16]uint32, b []byte) {
	for i := range block {
		block[i] = binary.LittleEndian.Uint32(b[4*i:])
	}
}

// blake3Compress is the scalar Blake3 compression function, returning the
// full 16-word state (only the first 8 words are the chaining value).
func blake3Compress(cv *[8]uint32, block *[16]uint32, counter uint64, blockLen, flags uint32) [16]uint32 {
	s := [16]uint32{
		cv[0], cv[1], cv[2], cv[3], cv[4], cv[5], cv[6], cv[7],
		blake3IV[0], blake3IV[1], blake3IV[2], blake3IV[3],
		uint32(counter), uint32(counter >> 32), blockLen, flags, //nolint:gosec // G115: counter is split into its two 32-bit halves
	}
	m := *block
	for r := 0; r < 7; r++ {
		blake3G(&s, 0, 4, 8, 12, m[0], m[1])
		blake3G(&s, 1, 5, 9, 13, m[2], m[3])
		blake3G(&s, 2, 6, 10, 14, m[4], m[5])
		blake3G(&s, 3, 7, 11, 15, m[6], m[7])
		blake3G(&s, 0, 5, 10, 15, m[8], m[9])
		blake3G(&s, 1, 6, 11, 12, m[10], m[11])
		blake3G(&s, 2, 7, 8, 13, m[12], m[13])
		blake3G(&s, 3, 4, 9, 14, m[14], m[15])
		m = [16]uint32{
			m[2], m[6], m[3], m[10], m[7], m[0], m[4], m[13],
			m[1], m[11], m[12], m[5], m[9], m[14], m[15], m[8],
		}
	}
	for i := 0; i < 8; i++ {
		s[i] ^= s[i+8]
		s[i+8] ^= cv[i]
	}
	return s
}

func blake3G(s *[16]uint32, a, b, c, d int, mx, my uint32) {
	s[a] += s[b] + mx
	s[d] = bits.RotateLeft32(s[d]^s[a], -16)
	s[c] += s[d]
	s[b] = bits.RotateLeft32(s[b]^s[c], -12)
	s[a] += s[b] + my
	s[d] = bits.RotateLeft32(s[d]^s[a], -8)
	s[c] += s[d]
	s[b] = bits.RotateLeft32(s[b]^s[c], -7)
}

const simdLimbVectors = LtHashSize / 32

// limbVectors views the limbs as 32-lane vectors; the sizes are identical.
func limbVectors(l *LtHash) *[simdLimbVectors][32]uint16 {
	return (*[simdLimbVectors][32]uint16)(unsafe.Pointer(&l.limbs)) //nolint:gosec // G103
}

func addSIMD(dst, src *LtHash) {
	a, b := limbVectors(dst), limbVectors(src)
	for i := range a {
		archsimd.LoadUint16x32Array(&a[i]).Add(archsimd.LoadUint16x32Array(&b[i])).StoreArray(&a[i])
	}
}

func subSIMD(dst, src *LtHash) {
	a, b := limbVectors(dst), limbVectors(src)
	for i := range a {
		archsimd.LoadUint16x32Array(&a[i]).Sub(archsimd.LoadUint16x32Array(&b[i])).StoreArray(&a[i])
	}
}
