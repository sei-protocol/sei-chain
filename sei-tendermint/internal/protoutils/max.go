package protoutils

import (
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type Max struct {
	Bytes int
	Repeated int
}

func maxSize(m proto.Message, getMax func(protoreflect.FieldDescriptor) Max) int {
	// TODO: fill m by filling fields with values which will take the largest amount of space
	// for bytes/repeated fields use getMax to determine the size/arity
	// repeated nested messafe fields should be a slice with the same message instance, to minimize the footprint while computing.
	// detect the recursion on T and panic
	// use proto.Size to compute size without serializing.
	panic("TODO")
}

func MaxSize[T Message](getMax func(protoreflect.FieldDescriptor) Max) int {
	return maxSize(New[T](),getMax)	
}
