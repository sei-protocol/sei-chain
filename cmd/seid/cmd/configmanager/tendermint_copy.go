package configmanager

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-viper/mapstructure/v2"

	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// detachReferences replaces every reference under cfg with one nothing else holds, so a decode into cfg
// cannot write through to whatever cfg was copied from.
//
// A copy of the struct alone shares every section, every list and every map it points at. A decoder writes
// a list into the array its target already holds, so a shared one means the rehearsal edits the original
// and a refused value leaves exactly the half-written configuration the copy exists to prevent.
//
// Walked over the type rather than field by field, so a section or a list added to the node's configuration
// is detached without this changing. Every exported reference gets one of its own and one that cannot is an
// error; an unexported field keeps what it was copied with, which is safe only because the decoder this
// guards against cannot write to one either. The test beside this walks the same type and holds each
// reference it finds to having been detached.
func detachReferences(cfg *tmcfg.Config) error {
	if cfg == nil {
		return fmt.Errorf("no configuration to detach")
	}
	return detachValue(reflect.ValueOf(cfg).Elem(), "")
}

// detachValue replaces every reference under v with one nothing else holds.
//
// An unexported field is skipped rather than refused. The copy this walks was made by assigning the struct,
// which copies unexported fields by value, and a decoder cannot write to one either.
func detachValue(v reflect.Value, path string) error {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() || !v.CanSet() {
			return nil
		}
		fresh := reflect.New(v.Type().Elem())
		fresh.Elem().Set(v.Elem())
		if err := detachValue(fresh.Elem(), path); err != nil {
			return err
		}
		v.Set(fresh)

	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			f := v.Type().Field(i)
			if !v.Field(i).CanSet() {
				continue
			}
			if err := detachValue(v.Field(i), join(path, f.Name)); err != nil {
				return err
			}
		}

	case reflect.Slice:
		if v.IsNil() || !v.CanSet() {
			return nil
		}
		fresh := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		reflect.Copy(fresh, v)
		for i := 0; i < fresh.Len(); i++ {
			if err := detachValue(fresh.Index(i), path); err != nil {
				return err
			}
		}
		v.Set(fresh)

	case reflect.Map:
		if v.IsNil() || !v.CanSet() {
			return nil
		}
		fresh := reflect.MakeMapWithSize(v.Type(), v.Len())
		for _, key := range v.MapKeys() {
			elem := reflect.New(v.Type().Elem()).Elem()
			elem.Set(v.MapIndex(key))
			if err := detachValue(elem, path); err != nil {
				return err
			}
			fresh.SetMapIndex(key, elem)
		}
		v.Set(fresh)

	case reflect.Interface:
		if v.IsNil() || !v.CanSet() {
			return nil
		}
		inner := v.Elem()
		fresh := reflect.New(inner.Type()).Elem()
		fresh.Set(inner)
		if err := detachValue(fresh, path); err != nil {
			return err
		}
		v.Set(fresh)

	case reflect.Array:
		// An array holds its elements rather than pointing at them, so the copy already has its own. Each
		// element still needs detaching, because what an element holds can be a reference.
		if !v.CanSet() {
			return nil
		}
		for i := 0; i < v.Len(); i++ {
			if err := detachValue(v.Index(i), path); err != nil {
				return err
			}
		}

	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return fmt.Errorf("%s is a %s, which cannot be copied", path, v.Kind())
	}
	return nil
}

// join builds a field path for a message.
func join(path, field string) string {
	if path == "" {
		return field
	}
	return path + "." + field
}

// describe reads the value the node's configuration currently holds for each key, as text, and names the
// keys it could not read.
//
// Read through the same tags the decode writes through, so a key names the same field in both directions.
// Held as text because what a report needs is whether two values differ and what they are, and comparing
// the shapes a decode produced against the shapes a struct holds would answer a different question.
//
// The unread keys are returned rather than left out of the answer. A key missing from a map reads as an
// empty value, so a caller comparing two answers finds an unread key equal on both sides and reports that
// it did not move. That is the same statement as a key an operator wrote and got, produced by having read
// nothing.
func describe(cfg *tmcfg.Config, keys []string) (values map[string]string, unread []string, err error) {
	values = map[string]string{}
	if cfg == nil {
		return values, keys, fmt.Errorf("no configuration to read")
	}
	var nested map[string]any
	if err := mapstructure.Decode(cfg, &nested); err != nil {
		return values, keys, err
	}
	flat := map[string]any{}
	flatten("", nested, flat)
	for _, key := range keys {
		v, ok := flat[key]
		if !ok {
			unread = append(unread, key)
			continue
		}
		values[key] = fmt.Sprint(v)
	}
	sort.Strings(unread)
	return values, unread, nil
}

// flatten turns a nested map into one keyed by dotted path.
func flatten(prefix string, in map[string]any, out map[string]any) {
	for name, value := range in {
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}
		if inner, nested := value.(map[string]any); nested {
			flatten(path, inner, out)
			continue
		}
		out[path] = value
	}
}

// DescribeForTest reads what a node's configuration holds for each key, as text.
//
// Exported for the tests that measure a booted node's configuration, which live beside the boot because
// only a boot produces one.
//
// It fails the test rather than answering partially. A caller comparing two of these answers over a hundred
// keys finds every value equal when both are empty, so an answer produced by reading nothing is
// indistinguishable from a node where nothing moved.
func DescribeForTest(t *testing.T, cfg *tmcfg.Config, keys []string) map[string]string {
	t.Helper()
	values, unread, err := describe(cfg, keys)
	if err != nil {
		t.Fatalf("reading %d keys off the node's configuration: %v", len(keys), err)
	}
	if len(unread) > 0 {
		t.Fatalf("%d of %d keys are not present in the node's configuration, so a comparison over them "+
			"would find every one unchanged: %v", len(unread), len(keys), unread)
	}
	return values
}

// refuseWhatDecodesToSomethingElse reports written values the decoder accepts and turns into something the
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
func whatDecodesToSomethingElse(cfg *tmcfg.Config, values map[string]any) []string {
	fields := keyFieldTypes(reflect.TypeOf(*cfg), "")

	var bad []string
	for key, value := range values {
		ft, known := fields[key]
		if !known {
			continue
		}
		n, numeric := asNumber(value)
		if !numeric {
			continue
		}
		switch {
		case isDuration(ft) && n != 0:
			bad = append(bad, fmt.Sprintf("%s = %v is a length of time written as a plain number, which "+
				"reads as nanoseconds; write a unit, as %q", key, value, fmt.Sprintf("%vs", value)))
		case !holdsAWholeNumber(ft):
		case n < 0 && reflect.New(ft).Elem().CanUint():
			bad = append(bad, fmt.Sprintf("%s = %v cannot be negative, and decodes to the largest value "+
				"this setting can hold rather than to no limit", key, value))
		case n != math.Trunc(n):
			bad = append(bad, fmt.Sprintf("%s = %v is a whole-number setting, and the fraction is dropped "+
				"rather than rounded, so it decodes to %v", key, value, math.Trunc(n)))
		case !reachesTheFieldAsItself(n, ft):
			bad = append(bad, fmt.Sprintf("%s = %v is larger than this setting can hold, and decodes to "+
				"its largest value rather than to what is written", key, value))
		}
	}
	sort.Strings(bad)
	return bad
}

// asNumber reports whether a written value arrived as a number, and what it was.
//
// Held as a float because what the checks above ask is whether it is zero and whether it is negative, and
// every numeric shape a file, a variable or a flag can carry answers both.
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

// referencePathsIn returns every path in a type that a copy has to detach, for the test that holds the
// detach to the type it walks.
func referencePathsIn(t reflect.Type, path string, seen map[reflect.Type]bool) []string {
	if seen[t] {
		return nil
	}
	seen[t] = true
	defer delete(seen, t)

	var out []string
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
		if path != "" {
			out = append(out, path)
		}
		if t.Kind() != reflect.Interface {
			out = append(out, referencePathsIn(t.Elem(), path, seen)...)
		}
	case reflect.Array:
		// The array itself is not a reference, so it is not a path of its own. What it holds can be, and
		// leaving this case out gives this walk the same blind spot as the copy it holds to account.
		out = append(out, referencePathsIn(t.Elem(), path, seen)...)
	case reflect.Struct:
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue
			}
			out = append(out, referencePathsIn(f.Type, join(path, f.Name), seen)...)
		}
	}
	sort.Strings(out)
	return out
}

// publishNodeConfig makes a node's configuration hold what the candidate holds, without replacing it.
//
// Field by field rather than by assigning the whole struct. The configuration is one struct behind one
// pointer, and every section under it is a pointer of its own that components take and keep: constructors
// throughout the node ask for a section rather than for the configuration holding it. Assigning the whole
// struct swaps every one of those pointers for a fresh one, so anything already holding a section goes on
// reading the values that section had before the delivery, and nothing says so.
//
// Assigning through each pointer instead leaves every pointer identity as it was, so a component reads the
// delivered values whether it took its pointer before this ran or after. That removes the ordering the
// delivery would otherwise depend on, which is an ordering nothing states and no test holds.
func publishNodeConfig(target, candidate *tmcfg.Config) error {
	if target == nil || candidate == nil {
		return fmt.Errorf("no configuration to publish into")
	}
	return publishValue(reflect.ValueOf(target).Elem(), reflect.ValueOf(candidate).Elem(), "")
}

// publishValue assigns candidate into target, following a pointer rather than replacing it.
//
// A pointer to a struct is followed and assigned through, which is what keeps the identity whatever holds it
// depends on. Everything else is assigned, and that is what carries the values. A pointer the target does
// not have yet is assigned rather than followed, because there is nothing to assign through.
//
// An unexported field is skipped, for the reason the detach skips one: the candidate was made by assigning
// the struct, so it already holds the same value, and a decoder cannot write to one either.
func publishValue(target, candidate reflect.Value, path string) error {
	if target.Kind() != reflect.Struct {
		target.Set(candidate)
		return nil
	}
	for i := 0; i < target.NumField(); i++ {
		f := target.Type().Field(i)
		tf, cf := target.Field(i), candidate.Field(i)
		if !tf.CanSet() {
			continue
		}
		at := join(path, f.Name)
		followable := f.Type.Kind() == reflect.Pointer && f.Type.Elem().Kind() == reflect.Struct
		if followable && !tf.IsNil() && !cf.IsNil() {
			if err := publishValue(tf.Elem(), cf.Elem(), at); err != nil {
				return err
			}
			continue
		}
		if f.Type.Kind() == reflect.Struct {
			if err := publishValue(tf, cf, at); err != nil {
				return err
			}
			continue
		}
		tf.Set(cf)
	}
	return nil
}
