package protoutils

import (
	"fmt"
	"reflect"

	gogoproto "github.com/gogo/protobuf/proto"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

var (
	pointerSize      = int(reflect.TypeFor[*byte]().Size())  // 8 on 64-bit
	sliceHeaderSize  = int(reflect.TypeFor[[]byte]().Size()) // 24 on 64-bit
	stringHeaderSize = int(reflect.TypeFor[string]().Size()) // 16 on 64-bit

	// mapEntryOverhead is added per map entry to account for Go runtime map
	// internals (hmap struct, bucket array slots, tophash bytes) that are not
	// captured by the map-entry message struct size. Adding it per entry
	// over-counts the fixed hmap header for multi-entry maps, which is
	// conservative. 8×pointerSize matches the number of fields in runtime.hmap.
	mapEntryOverhead = 8 * int(reflect.TypeFor[*byte]().Size())
)

// allocEstimate walks raw protobuf wire bytes and returns a conservative
// upper-bound on the heap bytes that proto.Unmarshal would allocate.
//
// The estimate accounts for:
//   - the Go struct for each message occurrence (looked up from protoregistry)
//   - backing arrays for bytes and string fields
//   - pointer elements in repeated-message slices
//
// Unknown fields (field numbers not in the descriptor) are stored verbatim by
// proto.Unmarshal in a single raw []byte blob on the struct without decoding.
// Their allocation cost equals their wire size, so we add the wire bytes for
// each unknown field occurrence. This is exact for bytes-type unknown fields
// and a slight over-count for scalar unknown fields (which store tag+value
// together), but both are correct in the conservative direction.
//
// The function returns an error on corrupt or truncated wire bytes. Truncation
// (n < 0 from a Consume* call) surfaces as protowire.ParseError("unexpected
// end of data") and is treated identically to corruption — the caller receives
// an error and Unmarshal is never called.
func allocEstimate(data []byte, desc protoreflect.MessageDescriptor) (int, error) {
	total := msgStructSize(desc)

	for len(data) > 0 {
		num, typ, tagLen := protowire.ConsumeTag(data)
		if tagLen <= 0 {
			return 0, fmt.Errorf("tag: %w", protowire.ParseError(tagLen))
		}
		if num == 0 {
			// Field number 0 is reserved and illegal in the protobuf spec.
			return 0, fmt.Errorf("invalid field number 0")
		}
		data = data[tagLen:]

		fd := desc.Fields().ByNumber(num)

		switch typ {
		case protowire.BytesType:
			val, n := protowire.ConsumeBytes(data)
			if n <= 0 {
				return 0, fmt.Errorf("field %d bytes: %w", num, protowire.ParseError(n))
			}
			data = data[n:]
			add, err := bytesFieldSize(tagLen, n, val, fd)
			if err != nil {
				return 0, err
			}
			total += add

		case protowire.VarintType:
			_, n := protowire.ConsumeVarint(data)
			if n <= 0 {
				return 0, fmt.Errorf("field %d varint: %w", num, protowire.ParseError(n))
			}
			data = data[n:]
			if fd == nil || !isVarintKind(fd.Kind()) {
				// Unknown field or known field with wrong wire type: proto.Unmarshal
				// stores it verbatim in the unknown fields blob.
				total += tagLen + n
			} else if fd.IsList() {
				total += sliceHeaderSize + scalarElementSize(fd.Kind())
			}

		case protowire.Fixed32Type:
			_, n := protowire.ConsumeFixed32(data)
			if n <= 0 {
				return 0, fmt.Errorf("field %d fixed32: %w", num, protowire.ParseError(n))
			}
			data = data[n:]
			if fd == nil || !isFixed32Kind(fd.Kind()) {
				// Unknown field or known field with wrong wire type: proto.Unmarshal
				// stores it verbatim in the unknown fields blob.
				total += tagLen + n
			} else if fd.IsList() {
				total += sliceHeaderSize + scalarElementSize(fd.Kind())
			}

		case protowire.Fixed64Type:
			_, n := protowire.ConsumeFixed64(data)
			if n <= 0 {
				return 0, fmt.Errorf("field %d fixed64: %w", num, protowire.ParseError(n))
			}
			data = data[n:]
			if fd == nil || !isFixed64Kind(fd.Kind()) {
				// Unknown field or known field with wrong wire type: proto.Unmarshal
				// stores it verbatim in the unknown fields blob.
				total += tagLen + n
			} else if fd.IsList() {
				total += sliceHeaderSize + scalarElementSize(fd.Kind())
			}

		case protowire.StartGroupType:
			val, n := protowire.ConsumeGroup(num, data)
			if n <= 0 {
				return 0, fmt.Errorf("field %d group: %w", num, protowire.ParseError(n))
			}
			data = data[n:]
			if fd != nil && (fd.Kind() == protoreflect.GroupKind) {
				sub, err := allocEstimate(val, fd.Message())
				if err != nil {
					return 0, err
				}
				total += sub
			} else {
				total += tagLen + n
			}

		default:
			return 0, fmt.Errorf("unknown wire type %d at field %d", typ, num)
		}
	}
	return total, nil
}

// bytesFieldSize returns the allocation estimate for one BytesType wire record.
// tagLen+n is the full wire record size (tag + length varint + payload).
// val is the payload slice (without tag or length prefix).
// fd is nil for unknown fields.
func bytesFieldSize(tagLen, n int, val []byte, fd protoreflect.FieldDescriptor) (int, error) {
	if fd == nil {
		// Unknown field: proto.Unmarshal appends the full wire record to the
		// unknown-fields blob. tagLen+n covers tag + length varint + payload.
		return tagLen + n, nil
	}

	total := 0
	if fd.IsMap() {
		// Map fields: Go allocates a runtime map (hmap), not a slice. Add
		// per-entry overhead for hmap fields, bucket slots, and tophash.
		// Over-counts the fixed hmap header across N entries, which is
		// conservative.
		total += mapEntryOverhead
	} else if fd.IsList() {
		// Repeated fields: one slice header per field. Adding it per
		// occurrence over-counts by (N-1)*sliceHeaderSize across N elements
		// — noise near a 1MB limit.
		total += sliceHeaderSize
	}

	switch fd.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		if fd.IsList() {
			total += pointerSize // pointer element in the backing array
		}
		sub, err := allocEstimate(val, fd.Message())
		if err != nil {
			return 0, err
		}
		total += sub
	case protoreflect.BytesKind:
		total += sliceHeaderSize + len(val)
	case protoreflect.StringKind:
		total += stringHeaderSize + len(val)
	default:
		// Packed repeated scalar. Fixed-width kinds have equal wire and Go
		// sizes so len(val) is exact. Varint kinds (bool, int32, int64,
		// uint32, uint64, sint32, sint64, enum) encode small values in 1 byte
		// on the wire while occupying 4 or 8 bytes in the Go slice — up to 8×
		// amplification. We count elements and multiply by Go element size.
		packed, err := packedAllocSize(val, fd.Kind())
		if err != nil {
			return 0, err
		}
		total += packed
	}
	return total, nil
}

// packedAllocSize returns the estimated Go heap bytes for a packed repeated
// scalar field whose raw wire bytes are bz.
//
// Fixed-width wire types (float, double, fixed32, fixed64, sfixed32, sfixed64)
// have the same size on the wire and in Go, so len(bz) is exact.
//
// Varint-encoded types (bool, int32, int64, uint32, uint64, sint32, sint64,
// enum) can encode small values in as little as 1 byte while each element
// occupies 4 or 8 bytes in the Go slice backing array. We walk the payload
// counting elements and multiply by the Go element size.
func packedAllocSize(bz []byte, kind protoreflect.Kind) (int, error) {
	switch kind {
	case protoreflect.Fixed32Kind, protoreflect.Sfixed32Kind, protoreflect.FloatKind:
		return len(bz), nil // 4 bytes wire == 4 bytes Go, exact
	case protoreflect.Fixed64Kind, protoreflect.Sfixed64Kind, protoreflect.DoubleKind:
		return len(bz), nil // 8 bytes wire == 8 bytes Go, exact
	case protoreflect.BoolKind:
		n, err := countVarintsInPacked(bz)
		return n * 1, err // bool = 1 byte in Go
	case protoreflect.Int32Kind, protoreflect.Uint32Kind,
		protoreflect.Sint32Kind, protoreflect.EnumKind:
		n, err := countVarintsInPacked(bz)
		return n * 4, err
	case protoreflect.Int64Kind, protoreflect.Uint64Kind, protoreflect.Sint64Kind:
		n, err := countVarintsInPacked(bz)
		return n * 8, err
	default:
		panic(fmt.Sprintf("packedAllocSize: unexpected kind %v", kind))
	}
}

// scalarElementSize returns the size in bytes of one element in the Go slice
// backing array for a repeated scalar field.
func scalarElementSize(kind protoreflect.Kind) int {
	switch kind {
	case protoreflect.BoolKind:
		return 1
	case protoreflect.Int32Kind, protoreflect.Uint32Kind, protoreflect.Sint32Kind,
		protoreflect.EnumKind, protoreflect.Fixed32Kind, protoreflect.Sfixed32Kind,
		protoreflect.FloatKind:
		return 4
	case protoreflect.Int64Kind, protoreflect.Uint64Kind, protoreflect.Sint64Kind,
		protoreflect.Fixed64Kind, protoreflect.Sfixed64Kind, protoreflect.DoubleKind:
		return 8
	default:
		panic(fmt.Sprintf("scalarElementSize: unexpected kind %v", kind))
	}
}

func isVarintKind(k protoreflect.Kind) bool {
	switch k {
	case protoreflect.BoolKind, protoreflect.EnumKind,
		protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Uint32Kind,
		protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Uint64Kind:
		return true
	}
	return false
}

func isFixed32Kind(k protoreflect.Kind) bool {
	switch k {
	case protoreflect.Fixed32Kind, protoreflect.Sfixed32Kind, protoreflect.FloatKind:
		return true
	}
	return false
}

func isFixed64Kind(k protoreflect.Kind) bool {
	switch k {
	case protoreflect.Fixed64Kind, protoreflect.Sfixed64Kind, protoreflect.DoubleKind:
		return true
	}
	return false
}

// countVarintsInPacked counts the number of varint-encoded elements in a
// packed repeated field payload.
func countVarintsInPacked(bz []byte) (int, error) {
	count := 0
	for len(bz) > 0 {
		_, n := protowire.ConsumeVarint(bz)
		if n <= 0 {
			return 0, fmt.Errorf("packed varint: %w", protowire.ParseError(n))
		}
		bz = bz[n:]
		count++
	}
	return count, nil
}

// msgStructSize returns the size of the Go struct backing desc.
// It tries the google protobuf v2 registry first (for protoc-gen-go types),
// then falls back to the gogoproto registry (for protoc-gen-gogofaster types
// used by Tendermint P2P). Panics if the type is not registered in either
// registry, since that indicates a programming error.
func msgStructSize(desc protoreflect.MessageDescriptor) int {
	if desc.IsMapEntry() {
		// Synthetic map-entry types have no standalone Go struct; the runtime map
		// stores keys and values in bucket arrays. Return 0 here; mapEntryOverhead
		// is added per entry at the call site to account for runtime overhead.
		return 0
	}
	if mt, err := protoregistry.GlobalTypes.FindMessageByName(desc.FullName()); err == nil {
		return int(reflect.TypeOf(mt.Zero().Interface()).Elem().Size())
	}
	if t := gogoproto.MessageType(string(desc.FullName())); t != nil {
		return int(t.Elem().Size())
	}
	panic(fmt.Sprintf("protoutils: message type not registered: %s", desc.FullName()))
}
