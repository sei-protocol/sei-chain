package protoutils

import (
	"google.golang.org/protobuf/proto"
	"github.com/tendermint/tendermint/libs/utils"
)

// Message is comparable proto.Message.
type Message interface {
	comparable
	proto.Message
}

// Constructs an empty message.
func New[T Message]() T {
	return utils.Zero[T]().ProtoReflect().New().Interface().(T)
}

func Marshal[T Message](t T) []byte {
	// Marshalling messages is always expected to succeed.
	return utils.OrPanic1(proto.Marshal(t))
}

func Unmarshal[T Message](bytes []byte) (T,error) {
	t := New[T]()
	err := proto.Unmarshal(bytes,t)
	return t,err
}

// Clone clones a proto.Message object.
func Clone[T Message](item T) T { return proto.Clone(item).(T) }

// Equal compares two Message objects.
func Equal[T Message](a, b T) bool { return proto.Equal(a, b) }
