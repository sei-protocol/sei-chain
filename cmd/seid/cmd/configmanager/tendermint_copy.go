package configmanager

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"

	tmcfg "github.com/sei-protocol/sei-chain/sei-tendermint/config"
)

// detachSections makes a copy hold what the original holds without sharing anything a decode can write
// through.
//
// A copy of the struct alone shares every section, every list and every map it points at. A decoder writes
// a list into the array its target already holds, so a shared one means the rehearsal edits the original
// and a refused value leaves exactly the half-written configuration the copy exists to prevent.
//
// Walked over the type rather than field by field, so a section or a list added to the node's configuration
// is detached without this changing. A field it cannot detach is an error rather than a silent share, and
// the test beside this holds every reference in the type against that promise.
func detachSections(out, from *tmcfg.Config) error {
	if out == nil || from == nil {
		return fmt.Errorf("no configuration to detach")
	}
	return detachValue(reflect.ValueOf(out).Elem(), "")
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

// describe reads the value the node's configuration currently holds for each key, as text.
//
// Read through the same tags the decode writes through, so a key names the same field in both directions.
// Held as text because what a report needs is whether two values differ and what they are, and comparing
// the shapes a decode produced against the shapes a struct holds would answer a different question.
func describe(cfg *tmcfg.Config, keys []string) (map[string]string, error) {
	out := map[string]string{}
	if cfg == nil {
		return out, fmt.Errorf("no configuration to read")
	}
	var nested map[string]any
	if err := mapstructure.Decode(cfg, &nested); err != nil {
		return out, err
	}
	flat := map[string]any{}
	flatten("", nested, flat)
	for _, key := range keys {
		if v, ok := flat[key]; ok {
			out[key] = fmt.Sprint(v)
		}
	}
	return out, nil
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
// Exported for the test that measures the two generators against each other, which lives beside the boot
// because only a boot produces a generated file.
func DescribeForTest(cfg *tmcfg.Config, keys []string) map[string]string {
	out, _ := describe(cfg, keys)
	return out
}

// refuseWhatDecodesToSomethingElse reports written values the decoder accepts and turns into something the
// operator did not mean, with what they should have written.
//
// Two shapes, and both decode cleanly, which is why nothing later objects.
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
// This is the one place either can be caught. The resolution sees a number and a key; only the struct says
// what the key is.
func refuseWhatDecodesToSomethingElse(cfg *tmcfg.Config, values map[string]any) []string {
	t := reflect.TypeOf(*cfg)
	durations := durationKeys(t, "")
	unsigned := unsignedKeys(t, "")

	var bad []string
	for key, value := range values {
		n, numeric := asNumber(value)
		if !numeric {
			continue
		}
		switch {
		case durations[key] && n != 0:
			bad = append(bad, fmt.Sprintf("%s = %v is a length of time, so write a unit, as %q",
				key, value, fmt.Sprintf("%vs", value)))
		case unsigned[key] && n < 0:
			bad = append(bad, fmt.Sprintf("%s = %v cannot be negative, and decodes to the largest value "+
				"this setting can hold rather than to no limit", key, value))
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

// unsignedKeys returns the dotted keys whose field cannot hold a negative number.
func unsignedKeys(t reflect.Type, prefix string) map[string]bool {
	return keysWhoseFieldIs(t, prefix, func(ft reflect.Type) bool {
		switch ft.Kind() {
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return true
		}
		return false
	})
}

// durationKeys returns the dotted keys whose field is a length of time.
//
// Matched by conversion rather than by identity, so a named type over the same underlying number is a length
// of time too.
func durationKeys(t reflect.Type, prefix string) map[string]bool {
	durationType := reflect.TypeOf(time.Duration(0))
	return keysWhoseFieldIs(t, prefix, func(ft reflect.Type) bool {
		return ft.Kind() == reflect.Int64 && ft.ConvertibleTo(durationType) && ft != reflect.TypeOf(int64(0))
	})
}

// keysWhoseFieldIs returns the dotted keys whose field answers a question about its type.
//
// One walk for every such question, over the same tag rules the declaration derives keys by, so a key found
// here is a key that can be written.
func keysWhoseFieldIs(t reflect.Type, prefix string, is func(reflect.Type) bool) map[string]bool {
	out := map[string]bool{}
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
		if squash {
			for key := range keysWhoseFieldIs(ft, prefix, is) {
				out[key] = true
			}
			continue
		}
		if is(ft) {
			out[path] = true
			continue
		}
		if ft.Kind() == reflect.Struct {
			for key := range keysWhoseFieldIs(ft, path, is) {
				out[key] = true
			}
		}
	}
	return out
}

// referencePathsIn returns every path in a type that a copy has to detach, for the test that holds
// detachSections to the type it copies.
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

// samePath reports whether two paths name the same field, ignoring repeats a pointer produces.
func samePath(a, b string) bool { return strings.TrimSuffix(a, ".") == strings.TrimSuffix(b, ".") }
