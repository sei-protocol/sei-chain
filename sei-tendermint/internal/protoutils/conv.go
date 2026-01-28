package protoutils

import (
	"errors"
	"fmt"
	"github.com/tendermint/tendermint/libs/utils"
)

func EncodeOpt[T any](v utils.Option[T]) *T {
	if v, ok := v.Get(); ok {
		return &v
	}
	return nil
}

func DecodeOpt[T any](v *T) utils.Option[T] {
	if v != nil {
		return utils.Some(*v)
	}
	return utils.None[T]()
}

// Conv is a pair of functions to encode and decode between a type and a Message.
type Conv[T any, P Message] struct {
	Encode func(T) P
	Decode func(P) (T, error)
}

func (c Conv[T, P]) Marshal(t T) []byte {
	return Marshal(c.Encode(t))
}

func (c Conv[T, P]) Unmarshal(bytes []byte) (T, error) {
	p, err := Unmarshal[P](bytes)
	if err != nil {
		return utils.Zero[T](), err
	}
	return c.Decode(p)
}

// EncodeSlice encodes a slice of T into a slice of P.
func (c Conv[T, P]) EncodeSlice(t []T) []P {
	p := make([]P, len(t))
	for i := range t {
		p[i] = c.Encode(t[i])
	}
	return p
}

// DecodeSlice decodes a slice of P into a slice of T.
func (c Conv[T, P]) DecodeSlice(p []P) ([]T, error) {
	t := make([]T, len(p))
	var err error
	for i := range p {
		if t[i], err = c.Decode(p[i]); err != nil {
			return nil, fmt.Errorf("[%d]: %w", i, err)
		}
	}
	return t, nil
}

// EncodeOpt encodes utils.Option[T], mapping utils.None to utils.Zero[P]().
func (c Conv[T, P]) EncodeOpt(mv utils.Option[T]) P {
	v, ok := mv.Get()
	if !ok {
		return utils.Zero[P]()
	}
	return c.Encode(v)
}

// DecodeReq decodes a ProtoMessage into a T, returning an error if p is nil.
func (c Conv[T, P]) DecodeReq(p P) (T, error) {
	if p == utils.Zero[P]() {
		return utils.Zero[T](), errors.New("missing")
	}
	return c.Decode(p)
}

// DecodeOpt decodes a ProtoMessage into a T, returning nil if p is nil.
func (c Conv[T, P]) DecodeOpt(p P) (utils.Option[T], error) {
	if p == utils.Zero[P]() {
		return utils.None[T](), nil
	}
	t, err := c.DecodeReq(p)
	if err != nil {
		return utils.None[T](), err
	}
	return utils.Some(t), nil
}
