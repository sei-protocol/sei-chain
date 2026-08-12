package seitoml

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/creachadair/tomledit"
	"github.com/creachadair/tomledit/parser"
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
	return f.insert(path, value)
}

// insert adds a key the document does not have yet.
//
// A key with no dots belongs at the top level. Otherwise it goes in the table its prefix names,
// which is created when it is absent so writing the first key of a section works without the
// operator having to add the heading by hand.
func (f *File) insert(path parser.Key, value parser.Value) error {
	leaf := parser.Key{path[len(path)-1]}
	kv := &parser.KeyValue{Name: leaf, Value: value}

	if len(path) == 1 {
		return f.insertGlobal(kv)
	}

	table := path[:len(path)-1]
	if e := transform.FindTable(f.doc, table...); e != nil {
		if !transform.InsertMapping(e.Section, kv, true) {
			return fmt.Errorf("could not write %s into the existing [%s] table", leaf, table.String())
		}
		return nil
	}
	f.doc.Sections = append(f.doc.Sections, &tomledit.Section{
		Heading: &parser.Heading{Name: table},
		Items:   []parser.Item{kv},
	})
	return nil
}

// insertGlobal adds a top-level key, creating the global section when the document has none.
func (f *File) insertGlobal(kv *parser.KeyValue) error {
	if f.doc.Global == nil {
		f.doc.Global = &tomledit.Section{}
	}
	if !transform.InsertMapping(f.doc.Global, kv, true) {
		return fmt.Errorf("could not write %s at the top level", kv.Name.String())
	}
	return nil
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
// becoming a plausible-looking line in an operator's file. A duration goes in as its string form,
// since a bare number of nanoseconds is unreadable and reads back as an integer.
func tomlValue(v any) (parser.Value, error) {
	switch x := v.(type) {
	case bool:
		return parser.ParseValue(strconv.FormatBool(x))
	case string:
		return parser.ParseValue(strconv.Quote(x))
	case time.Duration:
		return parser.ParseValue(strconv.Quote(x.String()))
	case int:
		return parser.ParseValue(quoteInt(int64(x)))
	case int8:
		return parser.ParseValue(quoteInt(int64(x)))
	case int16:
		return parser.ParseValue(quoteInt(int64(x)))
	case int32:
		return parser.ParseValue(quoteInt(int64(x)))
	case int64:
		return parser.ParseValue(quoteInt(x))
	case uint:
		return parser.ParseValue(strconv.FormatUint(uint64(x), 10))
	case uint8:
		return parser.ParseValue(strconv.FormatUint(uint64(x), 10))
	case uint16:
		return parser.ParseValue(strconv.FormatUint(uint64(x), 10))
	case uint32:
		return parser.ParseValue(strconv.FormatUint(uint64(x), 10))
	case uint64:
		return parser.ParseValue(strconv.FormatUint(x, 10))
	case float32:
		return parser.ParseValue(strconv.FormatFloat(float64(x), 'g', -1, 32))
	case float64:
		return parser.ParseValue(strconv.FormatFloat(x, 'g', -1, 64))
	case []string:
		return parser.ParseValue("[" + strings.Join(quoteEach(x), ", ") + "]")
	default:
		return parser.Value{}, fmt.Errorf("cannot write a %T to a configuration file", v)
	}
}

// quoteEach quotes every element of a string list.
func quoteEach(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strconv.Quote(s)
	}
	return out
}
