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
	"sync"

	"google.golang.org/protobuf/encoding/protowire"

	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// Number re-exports protowire.Number so callers can build Schemas without
// also importing google.golang.org/protobuf/encoding/protowire directly.
type Number = protowire.Number

// Schema describes the validation applied to a single proto message type.
// Rules are keyed by proto field number; nesting is expressed by setting
// Rule.Nested to a child schema. Schemas are immutable after construction
// and safe for concurrent use.
type Schema map[Number]Rule

// Rule is the validation applied to one field of a Schema's parent message.
// Nested and MaxCount compose: a field can both descend into a child Schema
// and cap its own occurrence count.
type Rule struct {
	// Nested, if Some, is applied to the contents of this length-delimited
	// field. Use for descending through wrapper layers on the way to a cap.
	Nested utils.Option[reflect.Type]
	// MaxCount, if non-zero, caps how many times this field may appear in the
	// scanned payload. The count is accumulated globally across the whole
	// Scan call — every match of this (Schema, field) pair increments one
	// shared counter, not a fresh counter per parent instance.
	MaxCount int
}

var (
	registryMu sync.RWMutex
	registry   = map[reflect.Type]Schema{}
)

// MustRegister associates T with schema in the global registry used by Scan.
// It panics on duplicate registration or nil schema.
func MustRegister[T any](schema Schema) {
	if schema == nil {
		panic("wireguard: cannot register nil schema")
	}
	t := reflect.TypeFor[T]()
	if t == nil {
		panic("wireguard: cannot register nil type")
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[t]; exists {
		panic(fmt.Sprintf("wireguard: duplicate schema registration for %v", t))
	}
	registry[t] = schema
}

func schemaForType(t reflect.Type) Schema {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[t]
}

// counterKey scopes a MaxCount accumulator by (Schema, field number) so the
// same Schema reached from multiple paths shares one counter, while two
// unrelated Schemas that happen to use the same field number don't collide.
type counterKey struct {
	schema any
	num    Number
}

// Scan walks bz once, applying the schema registered for T. Returns nil on
// success, an error on malformed wire bytes or a rule violation. If T has no
// registered schema, Scan is a no-op.
func Scan[T any](bz []byte) error {
	t := reflect.TypeFor[T]()
	schema := schemaForType(t)
	return schema.scan(bz, t, map[counterKey]int{})
}

// ScanValue walks bz once, applying the schema registered for msg's dynamic
// type. A nil msg or a value with no registered schema is a no-op.
func ScanValue(bz []byte, msg any) error {
	if msg == nil {
		return nil
	}
	t := reflect.TypeOf(msg)
	schema := schemaForType(t)
	return schema.scan(bz, t, map[counterKey]int{})
}

func (s Schema) scan(bz []byte, schemaID any, counts map[counterKey]int) error {
	if s == nil {
		return nil
	}
	for len(bz) > 0 {
		num, typ, tagLen := protowire.ConsumeTag(bz)
		if tagLen < 0 {
			return fmt.Errorf("wireguard: malformed wire tag at field %d: %w", num, protowire.ParseError(tagLen))
		}
		bz = bz[tagLen:]
		rule, hasRule := s[num]
		if typ == protowire.BytesType {
			val, valLen := protowire.ConsumeBytes(bz)
			if valLen < 0 {
				return fmt.Errorf("wireguard: malformed length-delimited field %d: %w", num, protowire.ParseError(valLen))
			}
			if hasRule {
				if rule.MaxCount > 0 {
					key := counterKey{schemaID, num}
					counts[key]++
					if counts[key] > rule.MaxCount {
						return fmt.Errorf("wireguard: field %d exceeds max %d entries", num, rule.MaxCount)
					}
				}
				if nestedType, ok := rule.Nested.Get(); ok {
					nested := schemaForType(nestedType)
					if nested == nil {
						return fmt.Errorf("wireguard: nested schema for %v not registered", nestedType)
					}
					if err := nested.scan(val, nestedType, counts); err != nil {
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
