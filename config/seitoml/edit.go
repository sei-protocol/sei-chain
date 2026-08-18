package seitoml

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/creachadair/tomledit"
	"github.com/creachadair/tomledit/parser"
	"github.com/creachadair/tomledit/scanner"
	"github.com/creachadair/tomledit/transform"
)

// Set writes one key's value, replacing it in place when the key is already present.
//
// Replacing the value on the existing line preserves the comment an operator wrote above or beside
// the key. Rewriting the file from a decoded map drops every comment in it, leaving the operator no
// way to recover the reasoning they recorded.
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
		e.Value = value
		return nil
	}
	f.insert(path, value)
	return nil
}

// insert adds a key the document does not have yet.
//
// A key with no dots belongs at the top level. Otherwise it goes in the table its prefix names,
// which is created when it is absent so writing the first key of a section works without the
// operator having to add the heading by hand.
func (f *File) insert(path parser.Key, value parser.Value) {
	leaf := parser.Key{path[len(path)-1]}
	kv := &parser.KeyValue{Name: leaf, Value: value}

	if len(path) == 1 {
		f.insertGlobal(kv)
		return
	}

	table := path[:len(path)-1]
	if e := transform.FindTable(f.doc, table...); e != nil {
		transform.InsertMapping(e.Section, kv, true)
		return
	}
	f.doc.Sections = append(f.doc.Sections, &tomledit.Section{
		Heading: &parser.Heading{Name: table},
		Items:   []parser.Item{kv},
	})
}

// insertGlobal adds a top-level key, creating the global section when the document has none.
//
// InsertMapping's result is not checked because it only reports a collision it was told not to
// replace, and it is told to replace.
func (f *File) insertGlobal(kv *parser.KeyValue) {
	if f.doc.Global == nil {
		f.doc.Global = &tomledit.Section{}
	}
	transform.InsertMapping(f.doc.Global, kv, true)
}

// SetPreamble puts a comment block at the top of the document, above everything else.
//
// Comments rather than keys, so nothing a reader needs in order to understand the file becomes
// configuration the node has to recognize. This replaces any block it put there before, so
// regenerating does not stack one preamble on the last.
func (f *File) SetPreamble(lines []string) {
	if f.doc.Global == nil {
		f.doc.Global = &tomledit.Section{}
	}
	items := f.doc.Global.Items
	if len(items) > 0 {
		if _, leading := items[0].(parser.Comments); leading {
			items = items[1:]
		}
	}
	if len(lines) == 0 {
		f.doc.Global.Items = items
		return
	}
	f.doc.Global.Items = append([]parser.Item{parser.Comments(lines)}, items...)
}

// Unset removes a key and reports whether the file carried one.
//
// This removes the key rather than writing a zero, because an absent key resolves to the running
// binary's baseline. A key set to its baseline value looks identical in the file but is a commitment
// that survives a release changing that baseline, which is the opposite of what unset means.
func (f *File) Unset(key string) (bool, error) {
	path, err := keyOf(key)
	if err != nil {
		return false, err
	}
	e := f.doc.First(path...)
	if e == nil || e.KeyValue == nil {
		return false, nil
	}
	return e.Remove(), nil
}

// tomlValue renders a Go value as the TOML literal that parses back to it.
//
// One case per type rather than a general formatter, so an unsupported type errors here instead of
// becoming a plausible-looking line in an operator's file. The cases are the widths configuration
// structs in this tree actually declare, which is why a narrower integer is a named refusal rather
// than a case: adding one is what you do when a field needs it.
//
// A duration goes in as its string form, since a bare number of nanoseconds is unreadable and reads
// back as an integer.
func tomlValue(v any) (parser.Value, error) {
	switch x := v.(type) {
	case bool:
		return parser.ParseValue(strconv.FormatBool(x))
	case string:
		return parser.ParseValue(basicString(x))
	case time.Duration:
		return parser.ParseValue(basicString(x.String()))
	case int:
		return parser.ParseValue(quoteInt(int64(x)))
	case int32:
		return parser.ParseValue(quoteInt(int64(x)))
	case int64:
		return parser.ParseValue(quoteInt(x))
	case uint:
		return parser.ParseValue(strconv.FormatUint(uint64(x), 10))
	case uint32:
		return parser.ParseValue(strconv.FormatUint(uint64(x), 10))
	case uint64:
		return parser.ParseValue(strconv.FormatUint(x, 10))
	case float64:
		return floatValue(x)
	case []string:
		return parser.ParseValue("[" + strings.Join(quoteEach(x), ", ") + "]")
	case []any:
		// The shape reading an array back produces. Without this, anything that reads a list and writes
		// it again fails on a value this package handed it. Every element is a value the reader can
		// produce, and every one of those has a case above, so the reader and the writer agree.
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

// floatValue renders a float as a TOML float, which an integral one is not by default.
//
// TOML tells a float from an integer by the fractional part or the exponent, and the shortest form of
// 1.0 is "1", which reads back as an integer. A key declared as a float would then resolve as one type
// from a node's own files and as another from its sei.toml, and which of the two an operator gets
// depends on the value they chose: 0.5 survives and 1.0 does not.
//
// Infinities and NaN are refused, because this file format has no form for either. The alternative is
// a line no reader can load, written into an operator's file with nothing said.
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

// basicString renders a Go string as a quoted TOML basic string.
//
// The escaping is the scanner's own rather than Go's. Go's quoter writes a control character as \x07
// or \a and TOML defines neither, so such a value was refused with a diagnostic naming an offset into
// a string the operator never saw.
func basicString(s string) string {
	return `"` + string(scanner.Escape(s)) + `"`
}

// quoteEach renders every element of a string list.
func quoteEach(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = basicString(s)
	}
	return out
}
