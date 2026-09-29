package seitoml

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/creachadair/tomledit"
	"github.com/creachadair/tomledit/parser"
	"github.com/creachadair/tomledit/scanner"
	"github.com/creachadair/tomledit/transform"
)

// Set writes one key's value, replacing it in place when the key is already present so its comments
// survive.
func (f *File) Set(key string, v any) error {
	path, err := keyOf(key)
	if err != nil {
		return err
	}
	value, err := tomlValue(v)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}

	if e := f.doc.First(path...); e != nil && e.KeyValue != nil {
		f.changed()
		// The trailing comment belongs to the value, so carry it across.
		value.Trailer = e.Value.Trailer
		e.Value = value
		return nil
	}
	f.changed()

	// A new key can collide with a table name. Rather than enumerate collisions, insert and undo if the
	// document no longer decodes.
	undo, inserted := f.insert(path, value)
	if !inserted {
		// Unreachable, since the lookup above found no such key.
		return fmt.Errorf("%s: the document already holds this key", key)
	}
	if err := f.decodable(); err != nil {
		// The failed decode cached nothing, so the undo needs no invalidation.
		undo()
		return fmt.Errorf("%s: %w", key, err)
	}
	return nil
}

// decodable reports whether the document still renders to something the node's decoder can read.
func (f *File) decodable() error {
	_, err := f.decoded()
	return err
}

// insert adds a key the document does not have yet, creating its table when absent, and returns how to
// undo it.
func (f *File) insert(path parser.Key, value parser.Value) (func(), bool) {
	leaf := parser.Key{path[len(path)-1]}
	kv := &parser.KeyValue{Name: leaf, Value: value}

	if len(path) == 1 {
		return f.insertGlobal(kv)
	}

	table := path[:len(path)-1]
	if e := transform.FindTable(f.doc, table...); e != nil {
		return appendItem(e.Section, kv)
	}
	before := len(f.doc.Sections)
	f.doc.Sections = append(f.doc.Sections, &tomledit.Section{
		Heading: &parser.Heading{Name: copyKey(table)},
		Items:   []parser.Item{kv},
	})
	return func() { f.doc.Sections = f.doc.Sections[:before] }, true
}

// copyKey returns a key that shares no storage with its argument.
func copyKey(k parser.Key) parser.Key { return append(parser.Key(nil), k...) }

// appendItem adds an item to a section without replacing an existing one, and reports how to remove it
// again and whether it went in.
func appendItem(s *tomledit.Section, kv *parser.KeyValue) (func(), bool) {
	if !transform.InsertMapping(s, kv, false) {
		return nil, false
	}
	return func() {
		for i, item := range s.Items {
			if item == parser.Item(kv) {
				s.Items = append(s.Items[:i], s.Items[i+1:]...)
				return
			}
		}
	}, true
}

// insertGlobal adds a top-level key, creating the global section when the document has none.
func (f *File) insertGlobal(kv *parser.KeyValue) (func(), bool) {
	if f.doc.Global == nil {
		f.doc.Global = &tomledit.Section{}
	}
	return appendItem(f.doc.Global, kv)
}

// Unset removes a key, so it resolves to the binary's default, and reports whether the file carried one.
func (f *File) Unset(key string) (bool, error) {
	path, err := keyOf(key)
	if err != nil {
		return false, err
	}
	e := f.doc.First(path...)
	if e == nil || e.KeyValue == nil {
		return false, nil
	}
	f.changed()
	if !e.Remove() {
		return false, fmt.Errorf("%s: the file carries this key and it could not be removed", key)
	}
	return true, nil
}

// tomlValue renders a Go value as the TOML literal that parses back to it. It supports the types
// configuration structs here declare and refuses the rest. A duration is written as its string form.
func tomlValue(v any) (parser.Value, error) {
	switch x := v.(type) {
	case bool:
		return parser.ParseValue(strconv.FormatBool(x))
	case string:
		text, err := basicString(x)
		if err != nil {
			return parser.Value{}, err
		}
		return parser.ParseValue(text)
	case time.Duration:
		text, err := basicString(x.String())
		if err != nil {
			return parser.Value{}, err
		}
		return parser.ParseValue(text)
	case int:
		return parser.ParseValue(quoteInt(int64(x)))
	case int32:
		return parser.ParseValue(quoteInt(int64(x)))
	case int64:
		return parser.ParseValue(quoteInt(x))
	case uint:
		return unsignedValue(uint64(x))
	case uint32:
		return unsignedValue(uint64(x))
	case uint64:
		return unsignedValue(x)
	case float64:
		return floatValue(x)
	case []string:
		quoted, err := quoteEach(x)
		if err != nil {
			return parser.Value{}, err
		}
		return parser.ParseValue("[" + strings.Join(quoted, ", ") + "]")
	case []any:
		// The shape a decoded list has, so a value read can be written back.
		rendered := make([]string, 0, len(x))
		for i, item := range x {
			element, err := tomlValue(item)
			if err != nil {
				return parser.Value{}, fmt.Errorf("element %d: %w", i, err)
			}
			rendered = append(rendered, element.String())
		}
		return parser.ParseValue("[" + strings.Join(rendered, ", ") + "]")
	default:
		return parser.Value{}, fmt.Errorf("cannot write a %T to a configuration file", v)
	}
}

// unsignedValue renders an unsigned integer, refusing one above math.MaxInt64 since TOML integers are
// signed 64-bit.
func unsignedValue(x uint64) (parser.Value, error) {
	if x > math.MaxInt64 {
		return parser.Value{}, fmt.Errorf("%d is larger than a configuration file's integers go, which "+
			"reach %d", x, int64(math.MaxInt64))
	}
	return parser.ParseValue(strconv.FormatUint(x, 10))
}

// floatValue renders a finite float as a TOML float, adding ".0" to an integral one so it does not read
// back as an integer.
func floatValue(x float64) (parser.Value, error) {
	if math.IsInf(x, 0) || math.IsNaN(x) {
		return parser.Value{}, fmt.Errorf("%v cannot be written to a configuration file, which holds "+
			"finite numbers", x)
	}
	text := strconv.FormatFloat(x, 'g', -1, 64)
	if !strings.ContainsAny(text, ".eE") {
		text += ".0"
	}
	return parser.ParseValue(text)
}

// basicString renders a Go string as a quoted TOML basic string, using TOML's escapes rather than Go's.
func basicString(s string) (string, error) {
	if !utf8.ValidString(s) {
		// The escaper would silently substitute a replacement rune.
		return "", fmt.Errorf("the value is not valid UTF-8, and a configuration file holds text")
	}
	return `"` + string(scanner.Escape(s)) + `"`, nil
}

// quoteEach renders every element of a string list.
func quoteEach(ss []string) ([]string, error) {
	out := make([]string, len(ss))
	for i, s := range ss {
		text, err := basicString(s)
		if err != nil {
			return nil, fmt.Errorf("element %d: %w", i, err)
		}
		out[i] = text
	}
	return out, nil
}
