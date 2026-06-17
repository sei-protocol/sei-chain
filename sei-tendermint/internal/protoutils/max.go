package protoutils

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type Max struct {
	Size  int
	Count int
}

type maxState struct {
	getMax func(protoreflect.FieldDescriptor) Max
	stack  map[protoreflect.FullName]struct{}
	cache  map[protoreflect.FullName]int
}

// MaxSize returns an upper bound on the wire size of T.
//
// The estimate intentionally counts some suboptimal-but-valid encodings, such as
// singular fields set to their default values, so the result may be larger than
// the size of a concretely marshaled message.
func MaxSize[T Message](getMax func(protoreflect.FieldDescriptor) Max) int {
	return (&maxState{
		getMax: getMax,
		stack:  map[protoreflect.FullName]struct{}{},
		cache:  map[protoreflect.FullName]int{},
	}).messageSize(New[T]().ProtoReflect().Descriptor())
}

func (s *maxState) messageSize(desc protoreflect.MessageDescriptor) int {
	name := desc.FullName()
	if _, ok := s.stack[name]; ok {
		panic(fmt.Sprintf("recursive message %s", name))
	}
	if size, ok := s.cache[name]; ok {
		return size
	}
	s.stack[name] = struct{}{}
	defer delete(s.stack, name)

	size := 0
	fields := desc.Fields()
	for i := range fields.Len() {
		fd := fields.Get(i)
		if oo := fd.ContainingOneof(); oo != nil && !oo.IsSynthetic() {
			// The descriptor iteration visits every oneof member. Only account for each
			// real oneof once, when we reach its first field, and let oneofSize pick the
			// largest member.
			if oo.Fields().Get(0) == fd {
				oneofSize := 0
				for j := range oo.Fields().Len() {
					oneofSize = max(oneofSize, s.singularFieldSize(oo.Fields().Get(j)))
				}
				size += oneofSize
			}
		} else {
			size += s.fieldSize(fd)
		}
	}

	s.cache[name] = size
	return size
}

func (s *maxState) fieldSize(fd protoreflect.FieldDescriptor) int {
	switch {
	case fd.IsList():
		return s.getMax(fd).Count * (tagSize(fd) + s.valueSize(fd))
	case fd.IsMap():
		payload := s.singularFieldSize(fd.MapKey()) + s.singularFieldSize(fd.MapValue())
		return s.getMax(fd).Count * (tagSize(fd) + protowire.SizeBytes(payload))
	default:
		return s.singularFieldSize(fd)
	}
}

func (s *maxState) singularFieldSize(fd protoreflect.FieldDescriptor) int {
	return tagSize(fd) + s.valueSize(fd)
}

func (s *maxState) valueSize(fd protoreflect.FieldDescriptor) int {
	switch fd.Kind() {
	case protoreflect.BoolKind:
		return protowire.SizeVarint(1)
	case protoreflect.EnumKind:
		size := 0
		values := fd.Enum().Values()
		for i := range values.Len() {
			size = max(size, protowire.SizeVarint(uint64(values.Get(i).Number())))
		}
		return size
	case protoreflect.Int32Kind, protoreflect.Int64Kind:
		return protowire.SizeVarint(^uint64(0))
	case protoreflect.Sint32Kind:
		return protowire.SizeVarint(protowire.EncodeZigZag(-1 << 31))
	case protoreflect.Sint64Kind:
		return protowire.SizeVarint(protowire.EncodeZigZag(-1 << 63))
	case protoreflect.Uint32Kind:
		return protowire.SizeVarint(^uint64(uint32(0)))
	case protoreflect.Uint64Kind:
		return protowire.SizeVarint(^uint64(0))
	case protoreflect.Sfixed32Kind, protoreflect.Fixed32Kind, protoreflect.FloatKind:
		return protowire.SizeFixed32()
	case protoreflect.Sfixed64Kind, protoreflect.Fixed64Kind, protoreflect.DoubleKind:
		return protowire.SizeFixed64()
	case protoreflect.StringKind, protoreflect.BytesKind:
		return protowire.SizeBytes(s.getMax(fd).Size)
	case protoreflect.MessageKind:
		return protowire.SizeBytes(s.messageSize(fd.Message()))
	case protoreflect.GroupKind:
		panic(fmt.Sprintf("unsupported field kind %s", fd.Kind()))
	default:
		panic(fmt.Sprintf("unsupported field kind %s", fd.Kind()))
	}
}

func tagSize(fd protoreflect.FieldDescriptor) int {
	return protowire.SizeTag(protowire.Number(fd.Number()))
}
