package configtest

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

// maxDumpDepth bounds recursion. Config structs are shallow, but a fuzzed TOML
// document can nest tables arbitrarily and reaches these functions as
// map[string]any, so the walk needs a floor it cannot fall through.
const maxDumpDepth = 24

// durationType lets the walk render a time.Duration readably instead of as the
// int64 nanosecond count its Kind reports.
var durationType = reflect.TypeOf(time.Duration(0))

// mapKeyPath renders a map key for the emitted path. It uses %#v so a key's type survives
// into the path, keeping the promise the rest of this file makes: the string "1" and the int
// 1 are different keys and must not both render as path[1]. The ordering key above is
// type-qualified for the same reason, and the two have to agree or a dump could sort by one
// distinction and print another.
func mapKeyPath(k reflect.Value) string {
	if k.Kind() == reflect.Interface && !k.IsNil() {
		k = k.Elem()
	}
	return fmt.Sprintf("%#v", k.Interface())
}

// mapKeyOrder renders a map key for ordering, qualified by its dynamic type so two keys
// with the same text but different types cannot tie.
func mapKeyOrder(k reflect.Value) string {
	if k.Kind() == reflect.Interface && !k.IsNil() {
		k = k.Elem()
	}
	return fmt.Sprintf("%s\x00%v", k.Type().String(), k.Interface())
}

// Dump renders a resolved configuration view as deterministic, diff-friendly
// text: one `path = type(value)` line per leaf, struct fields in declaration
// order, map keys sorted.
//
// Carrying the concrete Go type on every leaf is the point rather than noise.
// The legacy path resolves values through spf13/cast, so whether a key arrived as
// the string "5" or the int 5 decides what cast.ToBoolE does with it, whether a
// downstream cast.To* silently yields a zero, and whether two managers that
// "agree on the value" actually agree. A dump that prints both as `5` would let
// exactly the divergence class this suite exists to catch pass unnoticed.
func Dump(v any) string {
	var lines []string
	dumpInto("", reflect.ValueOf(v), &lines, 0)
	return strings.Join(lines, "\n")
}

// DumpAt renders v the way Dump does, with every line prefixed by path. Use it to
// build one comparable document out of several views.
func DumpAt(path string, v any) string {
	var lines []string
	dumpInto(path, reflect.ValueOf(v), &lines, 0)
	return strings.Join(lines, "\n")
}

func dumpInto(path string, v reflect.Value, out *[]string, depth int) {
	if depth > maxDumpDepth {
		*out = append(*out, path+" = <max-depth>")
		return
	}
	if !v.IsValid() {
		*out = append(*out, path+" = <nil>")
		return
	}

	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			*out = append(*out, path+" = <nil>")
			return
		}
		dumpInto(path, v.Elem(), out, depth+1)

	case reflect.Struct:
		// time.Time and anything else with unexported-only innards would render
		// as an empty subtree, so fall back to a formatted leaf.
		if v.NumField() == 0 || !hasExportedField(v.Type()) {
			*out = append(*out, leaf(path, v))
			return
		}
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			// An embedded struct is flattened into its parent's path. Config types
			// use embedding to mean "these are the parent's own fields" —
			// tmcfg.Config embeds BaseConfig with mapstructure:",squash", and the
			// keys it carries live at TOML root scope — so a path of
			// BaseConfig.Moniker would name a nesting level that exists in Go and
			// nowhere in the file the value came from.
			if f.Anonymous && f.Type.Kind() == reflect.Struct {
				dumpInto(path, v.Field(i), out, depth+1)
				continue
			}
			dumpInto(join(path, f.Name), v.Field(i), out, depth+1)
		}

	case reflect.Map:
		if v.IsNil() {
			*out = append(*out, path+" = <nil-map>")
			return
		}
		keys := v.MapKeys()
		// SliceStable with a type-qualified key, because this dump is the harness's definition
		// of "the same result". %v alone renders the int 1 and the string "1" identically, so a
		// map holding both would tie, and an unstable sort would break that tie by whatever
		// order the map happened to yield. It would surface as a CheckDeterministic failure in
		// the one component whose job is determinism. Config maps are map[string]any in
		// practice, so this is structural rather than a fix for something observed.
		sort.SliceStable(keys, func(i, j int) bool {
			return mapKeyOrder(keys[i]) < mapKeyOrder(keys[j])
		})
		if len(keys) == 0 {
			*out = append(*out, path+" = <empty-map>")
			return
		}
		for _, k := range keys {
			dumpInto(path+"["+mapKeyPath(k)+"]", v.MapIndex(k), out, depth+1)
		}

	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice && v.IsNil() {
			*out = append(*out, path+" = <nil-slice>")
			return
		}
		// Byte slices are values, not sequences.
		if v.Type().Elem().Kind() == reflect.Uint8 {
			*out = append(*out, leaf(path, v))
			return
		}
		if v.Len() == 0 {
			*out = append(*out, path+" = <empty-slice>")
			return
		}
		for i := range v.Len() {
			dumpInto(fmt.Sprintf("%s[%d]", path, i), v.Index(i), out, depth+1)
		}

	default:
		*out = append(*out, leaf(path, v))
	}
}

func leaf(path string, v reflect.Value) string {
	if v.Type() == durationType {
		return fmt.Sprintf("%s = time.Duration(%s)", path, time.Duration(v.Int()))
	}
	// %q on strings so leading/trailing space and embedded newlines survive the
	// round trip into a golden file.
	if v.Kind() == reflect.String {
		return fmt.Sprintf("%s = %s(%q)", path, v.Type(), v.String())
	}
	return fmt.Sprintf("%s = %s(%v)", path, v.Type(), v.Interface())
}

func hasExportedField(t reflect.Type) bool {
	for i := range t.NumField() {
		if t.Field(i).IsExported() {
			return true
		}
	}
	return false
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

// LeafAt pulls the lines describing one field out of a rendered view, so a test
// can assert on a single resolved value instead of a whole document. It returns
// false when the path names nothing in the view. A composite field renders as
// several indexed lines, which are rejoined.
func LeafAt(dump, path string) (string, bool) {
	var matched []string
	for _, line := range strings.Split(dump, "\n") {
		if isLeafLine(line, path) {
			matched = append(matched, line)
		}
	}
	if len(matched) == 0 {
		return "", false
	}
	return strings.Join(matched, "\n"), true
}

// isLeafLine reports whether a rendered line describes path.
//
// Three shapes count as the field: its own "path = value" line, the indexed lines a slice
// or map renders as, and the dotted lines a struct-valued field renders as. The prefix is
// path+"." rather than path, so a sibling named FooBar is not swallowed by a path of Foo.
func isLeafLine(line, path string) bool {
	return line == path+" = <nil>" || strings.HasPrefix(line, path+" = ") ||
		strings.HasPrefix(line, path+"[") || strings.HasPrefix(line, path+".")
}
