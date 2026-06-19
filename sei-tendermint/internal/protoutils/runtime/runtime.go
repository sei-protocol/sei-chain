package runtime

import (
	"fmt"
	"reflect"

	"google.golang.org/protobuf/encoding/protowire"
	"github.com/sei-protocol/sei-chain/sei-tendermint/libs/utils"
)

// Number re-exports protowire.Number so generated schemas do not need to
// import protowire directly.
type Number = protowire.Number

// Schema describes the validation applied to a single proto message type.
// Rules are keyed by proto field number; nesting is expressed by setting
// Rule.Nested to a child schema. Schemas are immutable after construction
// and safe for concurrent use.
type Schema map[Number]Rule

// Rule is the validation applied to one field of a Schema's parent message.
// Nested, MaxCount, MaxSize, and MaxTotalSize compose.
type Rule struct {
	// Nested, if Some, is applied to the contents of this length-delimited
	// field. Use for descending through wrapper layers on the way to a cap.
	Nested utils.Option[reflect.Type]
	// MaxCount, if non-zero, caps how many times this field may appear in the
	// current message instance.
	MaxCount int
	// MaxSize, if non-zero, caps the raw byte length of each individual
	// length-delimited field instance. This applies to strings, bytes, and
	// nested messages before any nested scan runs.
	MaxSize int
	// MaxTotalSize, if non-zero, caps the sum of raw byte lengths across all
	// instances of this length-delimited field within the current message
	// instance.
	MaxTotalSize int
}

var registry = map[reflect.Type]Schema{}

// MustRegister associates T with schema in the global registry used by Scan.
// Can be called ONLY from generated init() functions.
// It panics on duplicate registration or nil schema.
func MustRegister[T any](schema Schema) {
	t := reflect.TypeFor[T]()
	if _, exists := registry[t]; exists {
		panic(fmt.Sprintf("wireguard: duplicate schema registration for %v", t))
	}
	registry[t] = schema
}

func Scan(t reflect.Type, bz []byte) error { return registry[t].scan(bz) }

func (s Schema) scan(bz []byte) error {
	if len(s) == 0 {
		return nil
	}
	counts := map[Number]int{}
	totalSizes := map[Number]int{}
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
					counts[num]++
					if counts[num] > rule.MaxCount {
						return fmt.Errorf("wireguard: field %d exceeds max %d entries", num, rule.MaxCount)
					}
				}
				if rule.MaxSize > 0 && len(val) > rule.MaxSize {
					return fmt.Errorf("wireguard: field %d exceeds max size %d bytes", num, rule.MaxSize)
				}
				if rule.MaxTotalSize > 0 {
					totalSizes[num] += len(val)
					if totalSizes[num] > rule.MaxTotalSize {
						return fmt.Errorf("wireguard: field %d exceeds max total size %d bytes", num, rule.MaxTotalSize)
					}
				}
				if nestedType, ok := rule.Nested.Get(); ok {
					if err := registry[nestedType].scan(val); err != nil {
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
