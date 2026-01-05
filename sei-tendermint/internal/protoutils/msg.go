package protoutils

import (
	"google.golang.org/protobuf/proto"
)

// Message is comparable proto.Message.
type Message interface {
	comparable
	proto.Message
}

// Clone clones a proto.Message object.
func Clone[T Message](item T) T { return proto.Clone(item).(T) }

// Equal compares two Message objects.
func Equal[T Message](a, b T) bool { return proto.Equal(a, b) }
