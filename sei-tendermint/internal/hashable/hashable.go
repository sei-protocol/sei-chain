package hashable

import (
	"cmp"
	"crypto/sha256"
	"fmt"
	"slices"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type Hashable interface {
	proto.Message
	IsHashable()
}

// Hash is a SHA-256 hash.
type Hash[T Hashable] [sha256.Size]byte

// ParseHash parses a Hash from bytes.
func ParseHash[T Hashable](raw []byte) (Hash[T], error) {
	if got, want := len(raw), sha256.Size; got != want {
		return Hash[T]{}, fmt.Errorf("hash size = %v, want %v", got, want)
	}
	return Hash[T](raw), nil
}

// ToHash hashes a Hashable proto object.
func ToHash[T Hashable](a T) Hash[T] {
	return sha256.Sum256(MarshalCanonical(a))
}

// MarshalCanonical returns the canonical protobuf encoding of msg according to
// the custom Tendermint hashing/signing rules described in canonical.go.
// The output is deterministic and suitable for hashing and signing.
func MarshalCanonical[T Hashable](msg T) []byte {
	return builder{}.Message(msg.ProtoReflect())
}

type builder []byte

func (b builder) Tag(num protowire.Number, typ protowire.Type) builder {
	return protowire.AppendTag(b, num, typ)
}
func (b builder) Varint(v uint64) builder    { return protowire.AppendVarint(b, v) }
func (b builder) Fixed32(v uint32) builder   { return protowire.AppendFixed32(b, v) }
func (b builder) Fixed64(v uint64) builder   { return protowire.AppendFixed64(b, v) }
func (b builder) Bytes(bytes []byte) builder { return protowire.AppendBytes(b, bytes) }
func (b builder) String(s string) builder    { return protowire.AppendString(b, s) }

func (b builder) Message(msg protoreflect.Message) builder {
	// NOTE: we ignore unknown fields - we are unable to encode them canonically.
	// NOTE: we can sort fields on init if needed (in the generated files).
	for _, fd := range sortedFields(msg.Descriptor().Fields()) {
		if fd.IsList() {
			b = b.List(fd.Number(), fd.Kind(), msg.Get(fd).List())
		} else if msg.Has(fd) {
			b = b.Singular(fd.Number(), fd.Kind(), msg.Get(fd))
		}
	}
	return b
}

func (b builder) List(num protoreflect.FieldNumber, kind protoreflect.Kind, list protoreflect.List) builder {
	if list.Len() == 0 {
		return b
	}
	// We pack only lists longer than 1 for backward compatibility of optional -> repeated changes.
	if isPackable(kind) && list.Len() > 1 {
		var packed builder
		for i := range list.Len() {
			packed = packed.Value(kind, list.Get(i))
		}
		return b.Tag(num, protowire.BytesType).Bytes(packed)
	}

	for i := range list.Len() {
		b = b.Singular(num, kind, list.Get(i))
	}
	return b
}

func (b builder) Singular(num protoreflect.FieldNumber, kind protoreflect.Kind, value protoreflect.Value) builder {
	switch kind {
	case protoreflect.BoolKind,
		protoreflect.EnumKind,
		protoreflect.Int32Kind,
		protoreflect.Int64Kind,
		protoreflect.Sint32Kind,
		protoreflect.Sint64Kind,
		protoreflect.Uint32Kind,
		protoreflect.Uint64Kind:
		b = b.Tag(num, protowire.VarintType)
	case protoreflect.Fixed32Kind, protoreflect.Sfixed32Kind:
		b = b.Tag(num, protowire.Fixed32Type)
	case protoreflect.Fixed64Kind, protoreflect.Sfixed64Kind:
		b = b.Tag(num, protowire.Fixed64Type)
	case protoreflect.BytesKind, protoreflect.StringKind, protoreflect.MessageKind:
		b = b.Tag(num, protowire.BytesType)
	default:
		panic(fmt.Errorf("unsupported field kind %s", kind))
	}
	return b.Value(kind, value)
}

func (b builder) Value(kind protoreflect.Kind, value protoreflect.Value) builder {
	switch kind {
	case protoreflect.BoolKind:
		var v uint64
		if value.Bool() {
			v = 1
		}
		return b.Varint(v)
	case protoreflect.EnumKind:
		return b.Varint(uint64(value.Enum())) //nolint:gosec // protobuf enum values fit in uint64
	case protoreflect.Int32Kind:
		return b.Varint(uint64(uint32(value.Int()))) //nolint:gosec // intentional truncation to 32-bit per protobuf wire format
	case protoreflect.Int64Kind:
		return b.Varint(uint64(value.Int())) //nolint:gosec // reinterpret signed as unsigned per protobuf varint encoding
	case protoreflect.Sint32Kind:
		return b.Varint(protowire.EncodeZigZag(int64(int32(value.Int())))) //nolint:gosec // intentional truncation to 32-bit per protobuf zigzag encoding
	case protoreflect.Sint64Kind:
		return b.Varint(protowire.EncodeZigZag(value.Int()))
	case protoreflect.Uint32Kind:
		return b.Varint(uint64(uint32(value.Uint()))) //nolint:gosec // intentional truncation to 32-bit per protobuf wire format
	case protoreflect.Uint64Kind:
		return b.Varint(value.Uint())
	case protoreflect.Fixed32Kind:
		return b.Fixed32(uint32(value.Uint())) //nolint:gosec // intentional truncation to 32-bit per protobuf fixed32
	case protoreflect.Fixed64Kind:
		return b.Fixed64(value.Uint())
	case protoreflect.Sfixed32Kind:
		return b.Fixed32(uint32(int32(value.Int()))) //nolint:gosec // intentional truncation to 32-bit per protobuf sfixed32
	case protoreflect.Sfixed64Kind:
		return b.Fixed64(uint64(value.Int())) //nolint:gosec // reinterpret signed as unsigned per protobuf fixed64 encoding
	case protoreflect.BytesKind:
		return b.Bytes(value.Bytes())
	case protoreflect.StringKind:
		return b.String(value.String())
	case protoreflect.MessageKind:
		return b.Bytes(builder{}.Message(value.Message()))
	default:
		panic(fmt.Errorf("unsupported kind %s", kind))
	}
}

func isPackable(kind protoreflect.Kind) bool {
	switch kind {
	case protoreflect.BoolKind,
		protoreflect.EnumKind,
		protoreflect.Int32Kind,
		protoreflect.Int64Kind,
		protoreflect.Sint32Kind,
		protoreflect.Sint64Kind,
		protoreflect.Uint32Kind,
		protoreflect.Uint64Kind,
		protoreflect.Fixed32Kind,
		protoreflect.Fixed64Kind,
		protoreflect.Sfixed32Kind,
		protoreflect.Sfixed64Kind:
		return true
	default:
		return false
	}
}

func sortedFields(fields protoreflect.FieldDescriptors) []protoreflect.FieldDescriptor {
	result := make([]protoreflect.FieldDescriptor, fields.Len())
	for i := range fields.Len() {
		result[i] = fields.Get(i)
	}
	slices.SortFunc(result, func(a, b protoreflect.FieldDescriptor) int {
		return cmp.Compare(a.Number(), b.Number())
	})
	return result
}
