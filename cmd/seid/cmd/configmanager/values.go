package configmanager

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/sei-protocol/sei-chain/config/registry"
)

// whatDecodesToSomethingElse returns, per key, a message for each written value the weakly typed decode
// would silently change: an empty number, a non-boolean switch, a unitless non-zero duration (read as
// nanoseconds), a negative unsigned, a fractional integer, or an out-of-range integer.
func whatDecodesToSomethingElse(fields map[string]reflect.Type, values map[string]any) map[string]string {
	bad := map[string]string{}
	for key, value := range values {
		ft, known := fields[key]
		if !known {
			continue
		}
		if text, isText := value.(string); isText && strings.TrimSpace(text) == "" && holdsANumber(ft) {
			bad[key] = fmt.Sprintf("%s is written with an empty value, which decodes to zero "+
				"rather than leaving the setting as it is; remove the line to keep the declared value",
				key)
			continue
		}
		if text, isText := value.(string); isText && ft.Kind() == reflect.Bool {
			if _, err := strconv.ParseBool(strings.TrimSpace(text)); err != nil {
				bad[key] = fmt.Sprintf("%s = %q is not a value this setting can be switched by, and "+
					"decodes to false rather than being refused; write true or false", key, text)
			}
			continue
		}
		n, numeric := asNumber(value)
		if !numeric {
			continue
		}
		switch {
		case isDuration(ft) && n != 0:
			bad[key] = fmt.Sprintf("%s = %v is a length of time written as a plain number, which "+
				"reads as nanoseconds; write a unit, as %q", key, value, fmt.Sprintf("%vs", value))
		case !holdsAWholeNumber(ft):
		case n < 0 && reflect.New(ft).Elem().CanUint():
			bad[key] = fmt.Sprintf("%s = %v cannot be negative, and decodes to the largest value "+
				"this setting can hold rather than to no limit", key, value)
		case n != math.Trunc(n):
			bad[key] = fmt.Sprintf("%s = %v is a whole-number setting, and the fraction is dropped "+
				"rather than rounded, so it decodes to %v", key, value, math.Trunc(n))
		case !reachesTheFieldAsItself(n, ft):
			bad[key] = fmt.Sprintf("%s = %v is larger than this setting can hold, and decodes to "+
				"its largest value rather than to what is written", key, value)
		}
	}
	return bad
}

// asNumber reports whether a written value is a number, or text parsing as one, and returns it as a float.
func asNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case string:
		// Environment and flag values arrive as text.
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// keyFieldTypes returns every dotted key this type declares, by mapstructure tag, and its field's type.
func keyFieldTypes(t reflect.Type, prefix string) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		tag, ok := f.Tag.Lookup("mapstructure")
		if !ok {
			continue
		}
		name := strings.Split(tag, ",")[0]
		squash := strings.Contains(tag, ",squash")
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		path := name
		if prefix != "" && name != "" {
			path = prefix + "." + name
		}
		switch {
		case squash:
			for key, kt := range keyFieldTypes(ft, prefix) {
				out[key] = kt
			}
		case ft.Kind() == reflect.Struct && !isDuration(ft):
			for key, kt := range keyFieldTypes(ft, path) {
				out[key] = kt
			}
		default:
			out[path] = ft
		}
	}
	return out
}

// isDuration reports whether a field's type is time.Duration or another named int64 convertible to it.
func isDuration(ft reflect.Type) bool {
	return ft.Kind() == reflect.Int64 && ft.ConvertibleTo(reflect.TypeOf(time.Duration(0))) &&
		ft != reflect.TypeOf(int64(0))
}

// holdsANumber reports whether a field holds a number of any kind, whole or fractional.
func holdsANumber(ft reflect.Type) bool {
	v := reflect.New(ft).Elem()
	return v.CanInt() || v.CanUint() || v.CanFloat()
}

// holdsAWholeNumber reports whether a field holds an integer of some width.
func holdsAWholeNumber(ft reflect.Type) bool {
	v := reflect.New(ft).Elem()
	return v.CanInt() || v.CanUint()
}

// reachesTheFieldAsItself reports whether a written number is within the range of a field of this type.
func reachesTheFieldAsItself(n float64, ft reflect.Type) bool {
	v := reflect.New(ft).Elem()
	switch {
	case v.CanInt():
		if n < math.MinInt64 || n > math.MaxInt64 {
			return false
		}
		return !v.OverflowInt(int64(n))
	case v.CanUint():
		if n < 0 || n > math.MaxUint64 {
			return false
		}
		return !v.OverflowUint(uint64(n))
	}
	return true
}

// problemsInOrder renders a key-to-problem map as one line per key, ordered by key.
func problemsInOrder(bad map[string]string) []string {
	out := make([]string, 0, len(bad))
	for _, key := range sortedKeys(bad) {
		out = append(out, bad[key])
	}
	return out
}

// whatEachDeclaredKeyHolds returns the Go type behind every declared key, read from each section's
// defaults for mode. A section whose defaults are not a struct is skipped.
func whatEachDeclaredKeyHolds(mode registry.Mode) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for _, section := range registry.Sections() {
		defaults := section.Defaults(mode)
		if defaults == nil {
			continue
		}
		t := reflect.TypeOf(defaults)
		for t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t.Kind() != reflect.Struct {
			continue
		}
		for key, ft := range keyFieldTypes(t, section.Prefix) {
			out[key] = ft
		}
	}
	return out
}
