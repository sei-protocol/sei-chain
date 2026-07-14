package common

import (
	"encoding/binary"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi"

	storetypes "github.com/sei-protocol/sei-chain/sei-cosmos/store/types"
)

// maxDecodeWalkOps bounds how many type nodes decodeStringCopyBytes visits. For
// the argument shapes precompiles actually use (single-level dynamic arrays and
// flat tuples of leaves) the walk is linear in len(data); this cap is a
// defensive backstop so a hypothetical deeply-nested type cannot turn the cost
// estimate itself into a super-linear computation. Calldata large enough to hit
// it is infeasible under EVM calldata gas costs.
const maxDecodeWalkOps = 1 << 20

// DecodeGasCost returns the gas to charge for ABI-decoding a dynamic precompile
// call's calldata (the full input, including the 4-byte selector) given the
// method's argument list.
//
// The Go ABI decoder's cost is dominated by copying `string` payloads: it
// materializes each string via string(output[begin:end]), and because a single
// string can be referenced by many array/tuple slots, the copied volume can be
// super-linear in len(input) (worst case ~len(input)^2). `bytes` values are
// excluded because the decoder reslices them without copying. The charge is
// therefore a linear pass over the input plus the string-copy volume the
// decoder would produce, priced at the KV read-per-byte rate.
func DecodeGasCost(args abi.Arguments, input []byte) uint64 {
	base := DefaultGasCost(input, false)
	if len(input) < 4 {
		return base
	}
	strBytes, ok := decodeStringCopyBytes(args, input[4:])
	if !ok {
		// Structurally inconsistent, or too deeply nested to price cheaply. The
		// real decoder rejects such input (doing bounded work up to the same
		// point), so the linear base charge is sufficient.
		return base
	}
	return satAdd(base, satMul(storetypes.KVGasConfig().ReadCostPerByte, strBytes))
}

// decodeStringCopyBytes returns the total number of string-payload bytes the ABI
// decoder would copy while unpacking data as args, mirroring go-ethereum's
// unpack traversal. It follows offsets and reads length prefixes but never
// copies payloads, so it runs in time proportional to the number of type nodes
// visited rather than the number of bytes referenced. ok is false when the
// input is structurally inconsistent or hits the traversal cap.
func decodeStringCopyBytes(args abi.Arguments, data []byte) (uint64, bool) {
	w := &decodeWalker{}
	index := 0
	virtualArgs := 0
	for _, arg := range args {
		if arg.Indexed {
			continue
		}
		if abiContainsString(arg.Type) {
			w.walk(data, (index+virtualArgs)*32, arg.Type)
			if w.failed {
				return 0, false
			}
		}
		if (arg.Type.T == abi.ArrayTy || arg.Type.T == abi.TupleTy) && !abiIsDynamic(arg.Type) {
			virtualArgs += abiTypeSize(arg.Type)/32 - 1
		}
		index++
	}
	return w.strBytes, true
}

type decodeWalker struct {
	strBytes uint64
	ops      int
	failed   bool
}

// walk accounts for the string-copy bytes of a value of type t whose head word
// starts at index within output, mirroring go-ethereum abi.toGoType. Only types
// that can contain a string are ever passed in.
func (w *decodeWalker) walk(output []byte, index int, t abi.Type) {
	if w.failed {
		return
	}
	w.ops++
	if w.ops > maxDecodeWalkOps {
		w.failed = true
		return
	}
	if index < 0 || index+32 > len(output) {
		w.failed = true
		return
	}

	switch t.T {
	case abi.StringTy:
		_, length, ok := abiLengthPrefix(index, output)
		if !ok {
			w.failed = true
			return
		}
		w.strBytes += uint64(length)
	case abi.SliceTy:
		begin, length, ok := abiLengthPrefix(index, output)
		if !ok {
			w.failed = true
			return
		}
		w.walkElems(output[begin:], length, *t.Elem)
	case abi.ArrayTy:
		if abiIsDynamic(*t.Elem) {
			offset := int(binary.BigEndian.Uint64(output[index+24 : index+32]))
			if offset < 0 || offset > len(output) {
				w.failed = true
				return
			}
			w.walkElems(output[offset:], t.Size, *t.Elem)
		} else {
			w.walkElems(output[index:], t.Size, *t.Elem)
		}
	case abi.TupleTy:
		base := index
		if abiIsDynamic(t) {
			offset, ok := abiTuplePointsTo(index, output)
			if !ok {
				w.failed = true
				return
			}
			base = offset
		}
		w.walkTuple(output[base:], t)
	default:
		// static scalar (int/uint/bool/address/fixed bytes/...): no string payload
	}
}

func (w *decodeWalker) walkElems(output []byte, size int, elem abi.Type) {
	if w.failed {
		return
	}
	if size < 0 {
		w.failed = true
		return
	}
	// Mirror forEachUnpack's bound (start + 32*size must fit); written as a
	// division to avoid overflowing 32*size for an attacker-supplied size.
	if size != 0 && size > len(output)/32 {
		w.failed = true
		return
	}
	if !abiContainsString(elem) {
		return
	}
	elemSize := abiTypeSize(elem)
	if elemSize <= 0 {
		w.failed = true
		return
	}
	for i, j := 0, 0; j < size; i, j = i+elemSize, j+1 {
		w.walk(output, i, elem)
		if w.failed {
			return
		}
	}
}

func (w *decodeWalker) walkTuple(output []byte, t abi.Type) {
	virtualArgs := 0
	for i, elem := range t.TupleElems {
		if abiContainsString(*elem) {
			w.walk(output, (i+virtualArgs)*32, *elem)
			if w.failed {
				return
			}
		}
		if (elem.T == abi.ArrayTy || elem.T == abi.TupleTy) && !abiIsDynamic(*elem) {
			virtualArgs += abiTypeSize(*elem)/32 - 1
		}
	}
}

// abiContainsString reports whether decoding a value of type t can copy any
// string payload, i.e. whether the type is or transitively contains a string.
// Used to prune subtrees the walk does not need to descend into.
func abiContainsString(t abi.Type) bool {
	switch t.T {
	case abi.StringTy:
		return true
	case abi.SliceTy, abi.ArrayTy:
		return t.Elem != nil && abiContainsString(*t.Elem)
	case abi.TupleTy:
		for _, elem := range t.TupleElems {
			if abiContainsString(*elem) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// abiLengthPrefix mirrors go-ethereum abi.lengthPrefixPointsTo: it interprets
// the word at index as an offset, returns the start of the payload (offset+32)
// and the length read at that offset, with the same bounds checks. ok is false
// if the encoding points outside output.
func abiLengthPrefix(index int, output []byte) (start int, length int, ok bool) {
	if index < 0 || index+32 > len(output) {
		return 0, 0, false
	}
	bigOffsetEnd := new(big.Int).SetBytes(output[index : index+32])
	bigOffsetEnd.Add(bigOffsetEnd, big.NewInt(32))
	outputLength := big.NewInt(int64(len(output)))
	if bigOffsetEnd.Cmp(outputLength) > 0 || bigOffsetEnd.BitLen() > 63 {
		return 0, 0, false
	}
	offsetEnd := int(bigOffsetEnd.Uint64())
	lengthBig := new(big.Int).SetBytes(output[offsetEnd-32 : offsetEnd])
	totalSize := new(big.Int).Add(bigOffsetEnd, lengthBig)
	if totalSize.BitLen() > 63 || totalSize.Cmp(outputLength) > 0 {
		return 0, 0, false
	}
	return offsetEnd, int(lengthBig.Uint64()), true
}

// abiTuplePointsTo mirrors go-ethereum abi.tuplePointsTo for dynamic tuples.
func abiTuplePointsTo(index int, output []byte) (start int, ok bool) {
	if index < 0 || index+32 > len(output) {
		return 0, false
	}
	offset := new(big.Int).SetBytes(output[index : index+32])
	if offset.Cmp(big.NewInt(int64(len(output)))) > 0 || offset.BitLen() > 63 {
		return 0, false
	}
	return int(offset.Uint64()), true
}

// abiIsDynamic mirrors go-ethereum abi.isDynamicType.
func abiIsDynamic(t abi.Type) bool {
	if t.T == abi.TupleTy {
		for _, elem := range t.TupleElems {
			if abiIsDynamic(*elem) {
				return true
			}
		}
		return false
	}
	return t.T == abi.StringTy || t.T == abi.BytesTy || t.T == abi.SliceTy || (t.T == abi.ArrayTy && abiIsDynamic(*t.Elem))
}

// abiTypeSize mirrors go-ethereum abi.getTypeSize: the number of bytes a value
// of type t occupies in the head region of an encoding (32 for dynamic types).
func abiTypeSize(t abi.Type) int {
	if t.T == abi.ArrayTy && !abiIsDynamic(*t.Elem) {
		if t.Elem.T == abi.ArrayTy || t.Elem.T == abi.TupleTy {
			return t.Size * abiTypeSize(*t.Elem)
		}
		return t.Size * 32
	} else if t.T == abi.TupleTy && !abiIsDynamic(t) {
		total := 0
		for _, elem := range t.TupleElems {
			total += abiTypeSize(*elem)
		}
		return total
	}
	return 32
}

func satMul(a, b uint64) uint64 {
	if a == 0 || b == 0 {
		return 0
	}
	if a > math.MaxUint64/b {
		return math.MaxUint64
	}
	return a * b
}

func satAdd(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}
