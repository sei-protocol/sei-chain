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
		name:           simdBackendName,
		expand:         expandSIMD,
		add:            addSIMD,
		sub:            subSIMD,
		newAccumulator: newSIMDAccumulator,
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
	var xof xofAccumulator
	xof.fold(data, false)
	xof.reshape(dst)
}

// xofAccumulator sums root-XOF outputs in the order xof16Add produces them:
// rows[half][word] carries the limb pair that state word contributes in each of
// that half's sixteen output blocks.
type xofAccumulator struct {
	in   xof16Inputs
	rows [2][16][32]uint16
}

// fold runs both halves of the root XOF over data and adds them into the rows,
// subtracting instead when subtract is true. data is at most one Blake3 chunk.
func (x *xofAccumulator) fold(data []byte, subtract bool) {
	singleChunkRoot(data, &x.in)
	if subtract {
		xof16Sub(&x.in, &x.rows[0])
		x.in.setCounterBase(16)
		xof16Sub(&x.in, &x.rows[1])
		return
	}
	xof16Add(&x.in, &x.rows[0])
	x.in.setCounterBase(16)
	xof16Add(&x.in, &x.rows[1])
}

// reshape writes the accumulated rows to dst in limb order, replacing its limbs.
func (x *xofAccumulator) reshape(dst *LtHash) {
	for half := range x.rows {
		for word := range x.rows[half] {
			row := &x.rows[half][word]
			for block := 0; block < 16; block++ {
				limb := 32*(16*half+block) + 2*word
				dst.limbs[limb] = row[2*block]
				dst.limbs[limb+1] = row[2*block+1]
			}
		}
	}
}

// setCounterBase broadcasts the block counter the next compression starts at.
func (in *xof16Inputs) setCounterBase(base uint32) {
	in[12][0] = base
	lane0 := archsimd.LoadUint32x16Array(&xof16WordIndex[0])
	archsimd.LoadUint32x16Array(&in[12]).Permute(lane0).StoreArray(&in[12])
}

var _ accumulator = (*simdAccumulator)(nil)

// simdAccumulator folds one-chunk inputs straight from the XOF vectors, so the
// reshape into limb order is paid once per chunk of pairs rather than per hash.
type simdAccumulator struct {
	xof xofAccumulator
	// spill takes inputs longer than one Blake3 chunk. Those fall back to the
	// portable expansion, which produces limbs already in order.
	spill LtHash
}

func newSIMDAccumulator() accumulator {
	return &simdAccumulator{}
}

func (a *simdAccumulator) fold(data []byte, subtract bool) {
	if len(data) > blake3ChunkLen {
		var fresh LtHash
		expandBlake3(data, &fresh)
		if subtract {
			subSIMD(&a.spill, &fresh)
			return
		}
		addSIMD(&a.spill, &fresh)
		return
	}
	a.xof.fold(data, subtract)
}

func (a *simdAccumulator) finish(dst *LtHash) {
	a.xof.reshape(dst)
	addSIMD(dst, &a.spill)
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
	flags |= blake3ChunkEnd | blake3Root

	// The first sixteen rows are one scalar each, in state-word order, so
	// staging them contiguously lets a single vector load feed every splat.
	var state [16]uint32
	copy(state[0:8], cv[:])
	copy(state[8:12], blake3IV[0:4])
	state[14] = uint32(len(data)) //nolint:gosec // G115: len(data) <= blake3BlockLen
	state[15] = flags
	splat((*[16][16]uint32)(in[0:16]), archsimd.LoadUint32x16Array(&state))

	// x86 is little-endian, so the padded block is already its sixteen
	// message words.
	words := (*[16]uint32)(unsafe.Pointer(&last)) //nolint:gosec // G103
	splat((*[16][16]uint32)(in[16:32]), archsimd.LoadUint32x16Array(words))
}

// xof16WordIndex[i] is the VPERMD index vector selecting word i into every lane.
var xof16WordIndex = newWordIndex()

func newWordIndex() [16][16]uint32 {
	var table [16][16]uint32
	for word := range table {
		for lane := range table[word] {
			table[word][lane] = uint32(word) //nolint:gosec // G115: word < 16
		}
	}
	return table
}

// splat writes sixteen compression-input rows, row i holding word i of src in
// every lane.
func splat(rows *[16][16]uint32, src archsimd.Uint32x16) {
	for i := range xof16WordIndex {
		src.Permute(archsimd.LoadUint32x16Array(&xof16WordIndex[i])).StoreArray(&rows[i])
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
