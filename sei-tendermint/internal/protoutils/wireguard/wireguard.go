// Package wireguard runs bounded checks on raw protobuf wire bytes before
// the message is handed to Unmarshal. A caller registers a Schema describing
// which fields to descend into and which repeated fields to cap; Scan then
// walks the bytes once and rejects any payload that violates the rules.
//
// The intended use is as a channel/stream PreDecode hook: any size or shape
// invariant that must be enforced before the decoder allocates per-entry
// structs goes here, expressed declaratively as a schema next to the
// channel definition.
package wireguard

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"google.golang.org/protobuf/encoding/protowire"
)

// Number re-exports protowire.Number so callers can build Schemas without
// also importing google.golang.org/protobuf/encoding/protowire directly.
type Number = protowire.Number

// Schema describes the validation applied to a single proto message type.
// Rules are keyed by proto field number and address the fields of the parent
// message; nesting is expressed by pointing Rule.Nested at a child Schema.
// Schemas are immutable after construction and safe for concurrent use.
type Schema struct {
	Name  string // optional, used in error messages
	Rules map[Number]Rule
}

// Rule is the validation applied to one field of a Schema's parent message.
// Nested and MaxCount compose: a repeated message field may both descend
// into the child Schema and cap its own occurrence count.
type Rule struct {
	// Nested, if non-nil, is applied to the contents of this length-delimited
	// field. Use for descending through wrapper layers on the way to a cap.
	Nested *Schema
	// MaxCount, if non-zero, caps how many times this field may appear in
	// the parent message. Exceeding the cap returns an error.
	MaxCount int
}

// Scan walks bz once, applying schema. Returns nil on success, an error on
// malformed wire bytes or a rule violation. A nil schema is a no-op.
func Scan(bz []byte, schema *Schema) error {
	if schema == nil {
		return nil
	}
	return scan(bz, schema)
}

func scan(bz []byte, schema *Schema) error {
	var counts map[Number]int
	for len(bz) > 0 {
		num, typ, tagLen := protowire.ConsumeTag(bz)
		if tagLen < 0 {
			return wrap(schema, "malformed wire tag", protowire.ParseError(tagLen))
		}
		bz = bz[tagLen:]
		rule, hasRule := schema.Rules[num]
		if typ == protowire.BytesType {
			val, valLen := protowire.ConsumeBytes(bz)
			if valLen < 0 {
				return wrap(schema, "malformed length-delimited field", protowire.ParseError(valLen))
			}
			if hasRule {
				if rule.MaxCount > 0 {
					if counts == nil {
						counts = make(map[Number]int)
					}
					counts[num]++
					if counts[num] > rule.MaxCount {
						return fmt.Errorf("wireguard: %s field %d exceeds max %d entries",
							schemaName(schema), num, rule.MaxCount)
					}
				}
				if rule.Nested != nil {
					if err := scan(val, rule.Nested); err != nil {
						return err
					}
				}
			}
			bz = bz[valLen:]
			continue
		}
		valLen := protowire.ConsumeFieldValue(num, typ, bz)
		if valLen < 0 {
			return wrap(schema, "malformed field value", protowire.ParseError(valLen))
		}
		bz = bz[valLen:]
	}
	return nil
}

func schemaName(s *Schema) string {
	if s == nil || s.Name == "" {
		return ""
	}
	return s.Name + ":"
}

func wrap(s *Schema, what string, err error) error {
	if name := schemaName(s); name != "" {
		return fmt.Errorf("wireguard: %s %s: %w", name, what, err)
	}
	return fmt.Errorf("wireguard: %s: %w", what, err)
}

// MustFieldNum reads the protobuf field number declared on msg's field whose
// proto `name=` tag matches protoName. It panics if the field is missing or
// the tag is malformed, since both indicate a divergence between caller code
// and the regenerated proto bindings — a silent miscompare is worse than a
// loud startup failure for code that wires up Schemas at init.
//
// Reflection runs against the *struct type*, not a value. The msg argument
// is conventionally a typed nil pointer (e.g. (*tmproto.Commit)(nil)) used
// only to identify the type; no runtime instance is examined. Repeated
// fields, optional fields, and oneof variants all generate a struct field
// in the proto bindings regardless of whether any message instance
// populates them, so an empty / nil value at runtime is irrelevant here.
//
// To remove a proto field that a Schema currently references: first delete
// the MustFieldNum call and the Schema Rule that uses it, then regenerate
// proto with the field gone. Doing it in the other order panics at init.
func MustFieldNum(msg interface{}, protoName string) Number {
	t := reflect.TypeOf(msg).Elem()
	for i := 0; i < t.NumField(); i++ {
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
