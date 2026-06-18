// Package wireguard runs bounded checks on raw protobuf wire bytes before
// the message is handed to Unmarshal. A caller registers a Schema describing
// which fields to descend into and which repeated fields to cap; Scan then
// walks the bytes once and rejects any payload that violates the rules.
//
// The intended use is as a channel/stream PreDecode hook: any size or shape
// invariant that must be enforced before decoding goes here, expressed
// declaratively as a schema next to the channel definition.
package wireguard

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// Scanner is implemented by proto types whose generated *.wireguard.go adds a
// WireguardScan method. protoutils.Unmarshal asserts this interface and calls
// it automatically before proto.Unmarshal.
type Scanner interface {
	WireguardScan([]byte) error
}

// Number re-exports protowire.Number so callers can build Schemas without
// also importing google.golang.org/protobuf/encoding/protowire directly.
type Number = protowire.Number

// Schema describes the validation applied to a single proto message type.
// Rules are keyed by proto field number; nesting is expressed by setting
// Rule.Nested to a child Schema. Schemas are immutable after construction
// and safe for concurrent use.
type Schema struct {
	Rules map[Number]Rule
}

// Rule is the validation applied to one field of a Schema's parent message.
// Nested and MaxCount compose: a field can both descend into a child Schema
// and cap its own occurrence count.
type Rule struct {
	// Nested, if Some, is applied to the contents of this length-delimited
	// field. Use for descending through wrapper layers on the way to a cap.
	Nested utils.Option[*Schema]
	// MaxCount, if non-zero, caps how many times this field may appear within
	// one instance of the parent message. The cap is per-instance: each
	// occurrence of the outer message gets its own independent counter for
	// this field. Total memory at any nesting depth is therefore bounded by
	// the product of the caps along the path, not by a single global sum.
	MaxCount int
}

// Scan walks bz once, applying schema. Returns nil on success, an error on
// malformed wire bytes or a rule violation. A nil schema is a no-op.
func Scan(bz []byte, schema *Schema) error {
	if schema == nil {
		return nil
	}
	return scan(bz, schema, map[counterKey]int{})
}

// Scan is the method form of the package-level Scan. It's the shape a
// ChannelDescriptor's PreDecode hook expects, so the generated SchemaForX
// values can be wired in directly without a wrapping closure.
func (s *Schema) Scan(bz []byte) error {
	return Scan(bz, s)
}

// counterKey scopes a MaxCount accumulator to a (Schema, field number) pair
// within one scan of a single message instance.
type counterKey struct {
	schema *Schema
	num    Number
}

func scan(bz []byte, schema *Schema, counts map[counterKey]int) error {
	for len(bz) > 0 {
		num, typ, tagLen := protowire.ConsumeTag(bz)
		if tagLen < 0 {
			return fmt.Errorf("wireguard: malformed wire tag at field %d: %w", num, protowire.ParseError(tagLen))
		}
		bz = bz[tagLen:]
		rule, hasRule := schema.Rules[num]
		if typ == protowire.BytesType {
			val, valLen := protowire.ConsumeBytes(bz)
			if valLen < 0 {
				return fmt.Errorf("wireguard: malformed length-delimited field %d: %w", num, protowire.ParseError(valLen))
			}
			if hasRule {
				if rule.MaxCount > 0 {
					key := counterKey{schema, num}
					counts[key]++
					if counts[key] > rule.MaxCount {
						return fmt.Errorf("wireguard: field %d exceeds max %d entries", num, rule.MaxCount)
					}
				}
				if nested, ok := rule.Nested.Get(); ok {
					// Fresh counts for each nested-message occurrence so that
					// MaxCount is checked per instance rather than summed
					// globally. This keeps the semantics intuitive: a cap of N
					// on an inner field means "at most N per outer element",
					// which is easy to reason about and still bounds total
					// memory: outer_cap × inner_cap × … at every level.
					if err := scan(val, nested, map[counterKey]int{}); err != nil {
						return err
					}
				}
			}
			bz = bz[valLen:]
			continue
		}
		valLen := protowire.ConsumeFieldValue(num, typ, bz)
		if valLen < 0 {
			return fmt.Errorf("wireguard: malformed field %d value: %w", num, protowire.ParseError(valLen))
		}
		bz = bz[valLen:]
	}
	return nil
}

// MustFieldNum reads the protobuf field number declared on T's field whose
// proto `name=` tag matches protoName. It panics if the field is missing or
// the tag is malformed, since both indicate a divergence between caller code
// and the regenerated proto bindings — a silent miscompare is worse than a
// loud startup failure for code that wires up Schemas at init.
//
// Reflection runs against the *struct type* via reflect.TypeFor[T](); no
// runtime instance is examined. Repeated fields, optional fields, and oneof
// variants all generate a struct field in the proto bindings regardless of
// whether any message instance populates them, so an empty / nil value at
// runtime is irrelevant here.
//
// To remove a proto field that a Schema currently references: first delete
// the MustFieldNum call and the Schema Rule that uses it, then regenerate
// proto with the field gone. Doing it in the other order panics at init.
func MustFieldNum[T any](protoName string) Number {
	t := reflect.TypeFor[T]()
	for i := range t.NumField() {
		tag := t.Field(i).Tag.Get("protobuf")
		if tag == "" {
			continue
		}
		parts := strings.Split(tag, ",")
		var name, numStr string
		for j, p := range parts {
			if j == 1 {
				numStr = p
			}
			if strings.HasPrefix(p, "name=") {
				name = strings.TrimPrefix(p, "name=")
			}
		}
		if name != protoName {
			continue
		}
		num, err := strconv.ParseInt(numStr, 10, 32)
		if err != nil {
			panic(fmt.Sprintf("wireguard: bad protobuf tag on %s.%s: %v", t.Name(), t.Field(i).Name, err))
		}
		return Number(num) //nolint:gosec // ParseInt with bitSize=32 bounds num to int32 range
	}
	panic(fmt.Sprintf("wireguard: proto field %q not found on %s", protoName, t.Name()))
}
