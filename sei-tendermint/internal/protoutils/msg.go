package protoutils

import (
	"fmt"
	"reflect"

	gogoproto "github.com/gogo/protobuf/proto"
	golangproto "github.com/golang/protobuf/proto" //nolint:staticcheck // MessageReflect is the only bridge from gogoproto to protoreflect.Message
	"google.golang.org/protobuf/proto"

	"github.com/sei-protocol/sei-chain/sei-tendermint/internal/protoutils/runtime"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// Message is comparable proto.Message.
type Message interface {
	comparable
	proto.Message
}

type Sized interface {
	Message
	MaxSize() int
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

// UnmarshalWithLimit estimates the heap allocation that proto.Unmarshal would
// make for bytes and returns an error if the estimate exceeds limitBytes.
// This bounds allocation amplification where a small wire payload encodes many
// empty repeated-field entries, each causing a Go heap allocation. The estimate
// is conservative (may over-count) so legitimate messages must stay well within
// the limit.
func UnmarshalWithLimit[T Message](bytes []byte, limitBytes int) (T, error) {
	if limitBytes <= 0 {
		panic(fmt.Sprintf("protoutils: limitBytes must be positive, got %d", limitBytes))
	}
	if err := Scan[T](bytes); err != nil {
		return utils.Zero[T](), err
	}
	desc := New[T]().ProtoReflect().Descriptor()
	est, err := allocEstimate(bytes, desc)
	if err != nil {
		return utils.Zero[T](), fmt.Errorf("protoutils: alloc scan: %w", err)
	}
	if est > limitBytes {
		return utils.Zero[T](), fmt.Errorf("protoutils: message would allocate ~%d bytes, limit is %d", est, limitBytes)
	}
	t := New[T]()
	if err := proto.Unmarshal(bytes, t); err != nil {
		return utils.Zero[T](), err
	}
	return t, nil
}

// UnmarshalGogoWithLimit is the gogoproto variant of UnmarshalWithLimit.
// It uses github.com/golang/protobuf's reflection bridge to obtain the
// protoreflect.MessageDescriptor from a gogoproto-generated type (which does
// not implement google.golang.org/protobuf/proto.Message directly), allowing
// the same allocEstimate walk to protect Tendermint P2P messages.
func UnmarshalGogoWithLimit(bz []byte, msg gogoproto.Message, limitBytes int) error {
	if limitBytes <= 0 {
		panic(fmt.Sprintf("protoutils: limitBytes must be positive, got %d", limitBytes))
	}
	if msg == nil {
		return fmt.Errorf("protoutils: nil message")
	}
	if err := ScanAny(bz, msg); err != nil {
		return err
	}
	desc := golangproto.MessageReflect(msg).Descriptor() //nolint:staticcheck
	est, err := allocEstimate(bz, desc)
	if err != nil {
		return fmt.Errorf("protoutils: alloc scan: %w", err)
	}
	if est > limitBytes {
		return fmt.Errorf("protoutils: message would allocate ~%d bytes, limit is %d", est, limitBytes)
	}
	return gogoproto.Unmarshal(bz, msg)
}

// Scan walks bz once, applying the schema registered for T. Returns nil on
// success, an error on malformed wire bytes or a rule violation. If T has no
// registered schema, Scan is a no-op.
func Scan[T any](bz []byte) error {
	return runtime.Scan(reflect.TypeFor[T](), bz)
}

// ScanAny walks bz once, applying the schema registered for msg's dynamic
// type. A nil msg or a value with no registered schema is a no-op.
func ScanAny(bz []byte, msg gogoproto.Message) error {
	return runtime.Scan(reflect.TypeOf(msg), bz)
}

// Clone clones a proto.Message object.
func Clone[T Message](item T) T { return proto.Clone(item).(T) }

// Equal compares two Message objects.
func Equal[T Message](a, b T) bool { return proto.Equal(a, b) }
