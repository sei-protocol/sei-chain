package utils

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"

	"google.golang.org/protobuf/proto"
)

// Hash is a SHA-256 hash.
type Hash [sha256.Size]byte

// GetHash computes a hash of the given data.
func GetHash(data []byte) Hash {
	return sha256.Sum256(data)
}

// ParseHash parses a Hash from bytes.
func ParseHash(raw []byte) (Hash, error) {
	if got, want := len(raw), sha256.Size; got != want {
		return Hash{}, fmt.Errorf("hash size = %v, want %v", got, want)
	}
	return Hash(raw), nil
}

// ProtoClone clones a proto.Message object.
func ProtoClone[T proto.Message](item T) T {
	return proto.Clone(item).(T)
}

// ProtoEqual compares two proto.Message objects.
func ProtoEqual[T proto.Message](a, b T) bool {
	return proto.Equal(a, b)
}

// ProtoHash hashes a proto.Message object.
// TODO(gprusak): make it deterministic.
func ProtoHash(a proto.Message) Hash {
	raw, err := proto.Marshal(a)
	if err != nil {
		panic(err)
	}
	return sha256.Sum256(raw)
}

// ProtoMessage is comparable proto.Message.
type ProtoMessage interface {
	comparable
	proto.Message
}

// ProtoConv is a pair of functions to encode and decode between a type and a ProtoMessage.
type ProtoConv[T any, P ProtoMessage] struct {
	Encode func(T) P
	Decode func(P) (T, error)
}

// EncodeSlice encodes a slice of T into a slice of P.
func (c ProtoConv[T, P]) EncodeSlice(t []T) []P {
	p := make([]P, len(t))
	for i := range t {
		p[i] = c.Encode(t[i])
	}
	return p
}

// DecodeSlice decodes a slice of P into a slice of T.
func (c ProtoConv[T, P]) DecodeSlice(p []P) ([]T, error) {
	t := make([]T, len(p))
	var err error
	for i := range p {
		if t[i], err = c.Decode(p[i]); err != nil {
			return nil, fmt.Errorf("[%d]: %w", i, err)
		}
	}
	return t, nil
}

// Slice constructs a slice.
// It is a syntax sugar for `[]T{v...}`, which avoids
// spelling out T. Not very useful if you need to spell
// out T to construct the elements: in that case
// you might prefer the []T{{...},{...}} syntax instead.
func Slice[T any](v ...T) []T { return v }

// Alloc moves value to heap.
func Alloc[T any](v T) *T { return &v }

// Zero returns a zero value of type T.
func Zero[T any]() (zero T) { return }

// NoCopy may be added to structs which must not be copied
// after the first use.
//
// See https://golang.org/issues/8005#issuecomment-190753527
// for details.
//
// Note that it must not be embedded, otherwise Lock and Unlock methods
// will be exported.
type NoCopy struct{}

// Lock implements sync.Locker.
func (*NoCopy) Lock() {}

// Unlock implements sync.Locker.
func (*NoCopy) Unlock() {}

var _ sync.Locker = (*NoCopy)(nil)

// NoCompare may be added to structs which must not be used as
// map keys.
type NoCompare [0]func()

// EncodeOpt encodes Option[T], mapping None to Zero[P]().
func (c ProtoConv[T, P]) EncodeOpt(mv Option[T]) P {
	v, ok := mv.Get()
	if !ok {
		return Zero[P]()
	}
	return c.Encode(v)
}

// DecodeReq decodes a ProtoMessage into a T, returning an error if p is nil.
func (c ProtoConv[T, P]) DecodeReq(p P) (T, error) {
	if p == Zero[P]() {
		return Zero[T](), errors.New("missing")
	}
	return c.Decode(p)
}

// DecodeOpt decodes a ProtoMessage into a T, returning nil if p is nil.
func (c ProtoConv[T, P]) DecodeOpt(p P) (Option[T], error) {
	if p == Zero[P]() {
		return None[T](), nil
	}
	t, err := c.DecodeReq(p)
	if err != nil {
		return None[T](), err
	}
	return Some(t), nil
}
