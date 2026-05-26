package protoutils

import (
	"fmt"
	"iter"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// Test tests whether reencoding a value is an identity operation.
func (c *Conv[T, P]) Test(want T) error {
	p := c.Encode(want)
	raw, err := proto.Marshal(p)
	if err != nil {
		return fmt.Errorf("Marshal(): %w", err)
	}
	if err := proto.Unmarshal(raw, p); err != nil {
		return fmt.Errorf("Unmarshal(): %w", err)
	}
	got, err := c.Decode(p)
	if err != nil {
		return fmt.Errorf("Decode(Encode()): %w", err)
	}
	// Check that Decode does not panic on any malformed version of p.
	// The malformed values might or might not be parseable - here we
	// only check that Decode does not panic.
	for m := range iterMalformed(p) {
		_, _ = c.Decode(m)
	}
	return utils.TestDiff(want, got)
}

// Iterates over copies of msg with exactly one transitive field set to nil.
// This simulates situations in which a malicious proto value has been received.
// Note that setting to nil a single entry of a slice representing a repeated field
// is NOT a feasible malformed message (proto.Unmarshal would never return such a result).
func iterMalformed[M Message](msg M) iter.Seq[M] {
	return func(yield func(M) bool) {
		var walk func(current protoreflect.Message) bool
		walk = func(current protoreflect.Message) bool {
			clone := Clone(current.Interface()).ProtoReflect()
			fields := current.Descriptor().Fields()
			for i := range fields.Len() {
				field := fields.Get(i)
				if !current.Has(field) {
					continue
				}

				// Clear the field, then yield a clone of the top level message, then set the field to a clone we did beforehand.
				// We cannot v := Get -> Clear -> Set(v), because Get for repeated values returns a reference (i.e. Clear would destroy it).
				current.Clear(field)
				msgClone := Clone(msg)
				current.Set(field, clone.Get(field))
				if !yield(msgClone) {
					return false
				}

				// Iterate recursively in case the field was a message/contained messages.
				switch {
				case field.IsList():
					if field.Kind() == protoreflect.MessageKind {
						list := current.Mutable(field).List()
						for i := range list.Len() {
							if !walk(list.Get(i).Message()) {
								return false
							}
						}
					}
				case field.IsMap():
					if field.MapValue().Kind() == protoreflect.MessageKind {
						for _, value := range current.Mutable(field).Map().Range {
							if !walk(value.Message()) {
								return false
							}
						}
					}
				case field.Kind() == protoreflect.MessageKind:
					if !walk(current.Mutable(field).Message()) {
						return false
					}
				}
			}
			return true
		}
		walk(msg.ProtoReflect())
	}
}
