package protoutils

import (
	"reflect"
	
	"google.golang.org/protobuf/proto"
	gogoproto "github.com/gogo/protobuf/proto"
	
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/runtime"
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

// Computes the size of the message encoding.
func Size[T Message](t T) int {
	return proto.Size(t)
}

func Marshal[T Message](t T) []byte {
	// Marshalling messages is always expected to succeed.
	return utils.OrPanic1(proto.Marshal(t))
}

func Unmarshal[T Message](bytes []byte) (T, error) {
	t := New[T]()
	if err := Scan[T](bytes); err != nil {
		return utils.Zero[T](), err
	}
	err := proto.Unmarshal(bytes, t)
	return t, err
}

// Scan walks bz once, applying the schema registered for T. Returns nil on
// success, an error on malformed wire bytes or a rule violation. If T has no
// registered schema, Scan is a no-op.
func Scan[T any](bz []byte) error {
	return runtime.Scan(reflect.TypeFor[T](),bz)
}

// ScanAny walks bz once, applying the schema registered for msg's dynamic
// type. A nil msg or a value with no registered schema is a no-op.
func ScanAny(bz []byte, msg gogoproto.Message) error {
	return runtime.Scan(reflect.TypeOf(msg),bz)
}

// Clone clones a proto.Message object.
func Clone[T Message](item T) T { return proto.Clone(item).(T) }

// Equal compares two Message objects.
func Equal[T Message](a, b T) bool { return proto.Equal(a, b) }
