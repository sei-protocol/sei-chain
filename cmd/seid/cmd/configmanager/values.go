// Value shapes a decoder accepts and turns into something the operator did not mean.
//
// Both deliveries ask this. One puts a value into the source a reader looks keys up in, the other decodes
// values into the struct that holds them, and neither reader objects to any of the shapes below. So the
// check belongs beside the declaration of what a key's field can hold rather than beside either delivery.

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

// whatDecodesToSomethingElse reports written values the decoder accepts and turns into something the
// operator did not mean, with what they should have written.
//
// Four shapes, and every one of them decodes cleanly, which is why nothing later objects.
//
// A length of time has no form of its own in the file, so it is written as text with a unit. A plain number
// is read as nanoseconds, the shortest unit there is, so sixty means sixty billionths of a second. Zero is
// the exception and is allowed: nanoseconds and seconds are the same at zero, and zero is the documented way
// to turn several of these settings off.
//
// A negative number written where the field cannot hold one wraps to the largest value that field has. So
// minus one, which is how an operator says "no limit" in most software they have used, becomes a limit of
// eighteen million million million: the ceiling on connected peers stops bounding anything, and a window
// measured in seconds becomes six centuries.
//
// A number too large for the field reaches the same place from the other direction. It saturates rather
// than being refused, so the largest value the field holds is what the setting means, and a ceiling written
// far too high stops being a ceiling at all.
//
// A fraction written where the field holds whole numbers is truncated rather than rounded, so a size
// written as one and a half decodes to one. That is a mempool of a single transaction where the operator
// wrote something between one and two.
//
// This is the one place any of them can be caught. The resolution sees a number and a key; only the struct
// says what the key is, and the range and the whole-number rule are both facts about the field.
func whatDecodesToSomethingElse(fields map[string]reflect.Type, values map[string]any) map[string]string {
	bad := map[string]string{}
	for key, value := range values {
		ft, known := fields[key]
		if !known {
			continue
		}
		// Before the numeric checks, because an empty value is not a number and reached none of them. The
		// decode is weakly typed, so it turns an empty value into zero. A line with nothing after the
		// equals sign turns the setting off instead of leaving it as it is.
		if text, isText := value.(string); isText && strings.TrimSpace(text) == "" && holdsANumber(ft) {
			bad[key] = fmt.Sprintf("%s is written with an empty value, which decodes to zero "+
				"rather than leaving the setting as it is; remove the line to keep the declared value",
				key)
			continue
		}
		// A switch written as prose. The decode reads a word it does not recognise as off, so a setting an
		// operator wrote in order to turn something on arrives off. Nothing else objects: the reader asks
		// for a bool and gets one.
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

// asNumber reports whether a written value arrived as a number, and what it was.
//
// Held as a float. The checks above ask whether a value is zero and whether it is negative. Every numeric
// shape a file, a variable or a flag carries answers both.
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
		// Only the file carries a typed number. An environment variable and a flag both arrive as text, so
		// without this the checks below never run for either.
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}

// keyFieldTypes returns every dotted key this type declares and the type of the field it names.
//
// One walk, over the same tag rules the declaration derives keys by, so a key found here is a key that can
// be written. It answers with the field's type rather than with a yes or no, because the questions asked of
// it differ: one is whether a length of time was written as a bare number, another is whether a written
// number is one the field can hold at all.
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

// isDuration reports whether a field holds a length of time.
//
// Matched by conversion rather than by identity, so a named type over the same underlying number is a
// length of time too. A plain int64 is excluded, because every length of time is one and it is not.
func isDuration(ft reflect.Type) bool {
	return ft.Kind() == reflect.Int64 && ft.ConvertibleTo(reflect.TypeOf(time.Duration(0))) &&
		ft != reflect.TypeOf(int64(0))
}

// holdsAWholeNumber reports whether a field holds an integer of some width.
// holdsANumber reports whether a field holds a number of any kind, whole or fractional.
func holdsANumber(ft reflect.Type) bool {
	v := reflect.New(ft).Elem()
	return v.CanInt() || v.CanUint() || v.CanFloat()
}

func holdsAWholeNumber(ft reflect.Type) bool {
	v := reflect.New(ft).Elem()
	return v.CanInt() || v.CanUint()
}

// reachesTheFieldAsItself reports whether a written number arrives at a field of this type unchanged.
//
// A number outside the range a field holds is not refused by the decoder. It saturates, so the largest
// value the field has is what the setting ends up meaning, which for a ceiling is no ceiling at all.
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

// whatEachDeclaredKeyHolds returns the Go type behind every declared key, for one kind of node.
//
// Read from each section's own defaults, which is a value of the type that section registered, so the
// answer comes from the same struct the declaration derives its keys from. A section is skipped rather
// than refused when its defaults are not a struct, because a section that cannot answer for its own
// shape is a defect the registry already reports.
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
