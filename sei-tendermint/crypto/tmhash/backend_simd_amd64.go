//go:build goexperiment.simd && amd64

package tmhash

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"

	"simd/archsimd"
)

//go:generate go run gen_sha256x16.go

// vzeroupper clears the upper halves of the vector registers. The compiler
// does not emit it after AVX-512 code, and legacy-SSE code that follows (the
// SHA-NI scalar path, memmove) runs several times slower while they are dirty.
//
//go:noescape
func vzeroupper()

const (
	simdBackendName = "simd"
	simdLanes       = 16
	blockSize       = 64
	// simdMaxBlocks bounds the per-lane message length hashed by the SIMD
	// kernel; longer messages go through the scalar path so the block
	// scratch stays small.
	simdMaxBlocks = 64
)

// simdBackend returns the AVX-512 backend when the CPU can run it. AVX512
// covers F/BW/DQ/VL; VBMI provides VPERMB for the prefix shift and VBMI2
// VPSHRDD for the rotates.
func simdBackend() (backend, bool) {
	if !archsimd.X86.AVX512() || !archsimd.X86.AVX512VBMI() || !archsimd.X86.AVX512VBMI2() {
		return backend{}, false
	}
	return backend{
		name:     simdBackendName,
		lanes:    simdLanes,
		sumBatch: sumBatchSIMD,
	}, true
}

// laneScratch is the per-call working set: the transposed message blocks fed
// to the kernel and, per lane, the final padding block's big-endian bit length
// in bytes 56-63 (the other bytes stay zero).
type laneScratch struct {
	blocks []sha256Block16
	lens   [simdLanes][blockSize]byte
	pfx    prefixShift
}

func (sp *laneScratch) setPrefix(prefix []byte) {
	if len(prefix) > 0 {
		sp.pfx.init(prefix)
	}
}

var laneScratchPool = sync.Pool{
	New: func() any { return &laneScratch{blocks: make([]sha256Block16, 4)} },
}

// paddedBlocks returns how many 64-byte blocks SHA-256 processes for a message
// of n bytes: the message, a 0x80 byte and a 64-bit length.
func paddedBlocks(n int) int {
	return (n + 1 + 8 + blockSize - 1) / blockSize
}

// sumBatchSIMD hashes msgs sixteen at a time. Lanes must share a block count,
// so messages are bucketed by padded length; buckets with fewer than sixteen
// messages left over fall back to the scalar backend.
func sumBatchSIMD(prefix []byte, msgs [][]byte, out [][Size]byte) {
	if len(msgs) < simdLanes || len(prefix) >= blockSize {
		sumBatchScalar(prefix, msgs, out)
		return
	}
	sp := laneScratchPool.Get().(*laneScratch)
	sp.setPrefix(prefix)
	// Bucket message indices by block count. Merkle levels are either all
	// inner nodes or leaves of similar size, so most calls stay on the
	// single-bucket path.
	var buckets map[int][]int
	var rest []int
	var lanes [simdLanes]int
	first := paddedBlocks(len(prefix) + len(msgs[0]))
	next := 0
	for i, msg := range msgs {
		nb := paddedBlocks(len(prefix) + len(msg))
		if nb == first && nb <= simdMaxBlocks {
			lanes[next] = i
			if next++; next == simdLanes {
				sha256Lanes(sp, prefix, msgs, out, &lanes, nb)
				next = 0
			}
			continue
		}
		if nb > simdMaxBlocks {
			rest = append(rest, i)
			continue
		}
		if buckets == nil {
			buckets = map[int][]int{}
		}
		buckets[nb] = append(buckets[nb], i)
	}
	rest = append(rest, lanes[:next]...)
	for nb, idx := range buckets {
		for len(idx) >= simdLanes {
			copy(lanes[:], idx[:simdLanes])
			idx = idx[simdLanes:]
			sha256Lanes(sp, prefix, msgs, out, &lanes, nb)
		}
		rest = append(rest, idx...)
	}
	laneScratchPool.Put(sp)
	vzeroupper()
	if len(rest) > 0 {
		h := sha256.New()
		for _, i := range rest {
			h.Reset()
			h.Write(prefix)
			h.Write(msgs[i])
			h.Sum(out[i][:0])
		}
	}
}

// sha256Lanes hashes the sixteen messages selected by lanes, each of nb
// padded blocks, with one kernel call.
func sha256Lanes(sp *laneScratch, prefix []byte, msgs [][]byte, out [][Size]byte, lanes *[simdLanes]int, nb int) {
	if cap(sp.blocks) < nb {
		sp.blocks = make([]sha256Block16, nb)
	}
	blocks := sp.blocks[:nb]
	for lane, i := range lanes {
		n := len(prefix) + len(msgs[i])
		binary.BigEndian.PutUint64(sp.lens[lane][blockSize-8:], uint64(n)*8) //nolint:gosec // G115 n is a slice length
	}
	pfx := &sp.pfx
	for b := range blocks {
		start := b * blockSize
		var rows [simdLanes]archsimd.Uint8x64
		for lane, i := range lanes {
			msg := msgs[i]
			var row archsimd.Uint8x64
			if start >= len(prefix) {
				row, _ = archsimd.LoadUint8x64Part(msg[min(start-len(prefix), len(msg)):])
			} else {
				m, _ := archsimd.LoadUint8x64Part(msg)
				row = m.Permute(pfx.shift).And(pfx.keep).Or(pfx.bytes)
			}
			if p := len(prefix) + len(msg) - start; p >= 0 && p < blockSize {
				row = row.Or(archsimd.LoadUint8x64Array(&pad80[p]))
			}
			if b == nb-1 {
				row = row.Or(archsimd.LoadUint8x64Array(&sp.lens[lane]))
			}
			rows[lane] = row
		}
		transposeBlock(&rows, &blocks[b])
	}
	var st [8][16]uint32
	sha256x16(blocks, &st)
	for lane, i := range lanes {
		d := &out[i]
		for w := range 8 {
			binary.BigEndian.PutUint32(d[4*w:], st[w][lane])
		}
	}
}

// prefixShift assembles the first block of prefix || msg from a raw load of
// msg: shift moves msg byte i to position i+len(prefix), keep zeroes the
// prefix positions and bytes holds the prefix itself. The prefix must be
// shorter than a block.
type prefixShift struct {
	shift, keep, bytes archsimd.Uint8x64
}

func (p *prefixShift) init(prefix []byte) {
	var shift, keep, bytes [blockSize]byte
	for i := range blockSize {
		if i < len(prefix) {
			bytes[i] = prefix[i]
			continue
		}
		shift[i] = byte(i - len(prefix)) //nolint:gosec // G115 0 <= i-len(prefix) < 64
		keep[i] = 0xff
	}
	p.shift = archsimd.LoadUint8x64Array(&shift)
	p.keep = archsimd.LoadUint8x64Array(&keep)
	p.bytes = archsimd.LoadUint8x64Array(&bytes)
}

// pad80[p] is a block with the SHA-256 terminator byte at offset p.
var pad80 = func() [blockSize][blockSize]byte {
	var t [blockSize][blockSize]byte
	for p := range t {
		t[p][p] = 0x80
	}
	return t
}()

// bswap32 reverses the bytes of every 32-bit word (VPSHUFB within 128-bit
// groups); SHA-256 words are big-endian.
var bswap32 = func() [64]int8 {
	var idx [64]int8
	for i := range idx {
		idx[i] = int8((i &^ 3) + (3 - i&3)) //nolint:gosec // G115 0 <= i < 64
	}
	return idx
}()

// transposeIdx holds, per stage k, the ConcatPermute index vectors that swap
// bit k of the row index with bit k of the element index: [k][0] produces the
// row with bit k clear, [k][1] the row with bit k set. Indices 16-31 select
// from the second source.
var transposeIdx = func() [4][2][16]uint32 {
	var idx [4][2][16]uint32
	for k := range 4 {
		bk := uint32(1) << k
		for e := range uint32(16) {
			if e&bk == 0 {
				idx[k][0][e] = e
				idx[k][1][e] = e | bk
			} else {
				idx[k][0][e] = 16 + (e ^ bk)
				idx[k][1][e] = 16 + e
			}
		}
	}
	return idx
}()

// bswapRows reinterprets each 64-byte row as sixteen big-endian words.
func bswapRows(rows *[simdLanes]archsimd.Uint8x64, v *[16]archsimd.Uint32x16) {
	sw := archsimd.LoadInt8x64Array(&bswap32)
	for i := range v {
		v[i] = rows[i].PermuteOrZeroGrouped(sw).ReshapeToUint32s()
	}
}

// transposeBlock converts rows[lane] (64 message bytes of one lane) into
// blk[word][lane] big-endian words with a four-stage in-register transpose.
func transposeBlock(rows *[simdLanes]archsimd.Uint8x64, blk *sha256Block16) {
	var v [16]archsimd.Uint32x16
	bswapRows(rows, &v)
	for k := range 4 {
		lo := archsimd.LoadUint32x16Array(&transposeIdx[k][0])
		hi := archsimd.LoadUint32x16Array(&transposeIdx[k][1])
		bk := 1 << k
		for i := range 16 {
			if i&bk != 0 {
				continue
			}
			j := i | bk
			x, y := v[i], v[j]
			v[i] = x.ConcatPermute(y, lo)
			v[j] = x.ConcatPermute(y, hi)
		}
	}
	for w := range v {
		v[w].StoreArray(&blk[w])
	}
}
