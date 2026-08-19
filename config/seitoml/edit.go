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
		f.changed()
		e.Value = value
		return nil
	}
	f.changed()

	// A key the document does not hold yet lands in a namespace that may already use its name for a
	// table, or use a table's name for it. Rather than enumerate the shapes that collide, insert and then
	// ask the decoder, which is the same one the node reads with: if the document no longer decodes, the
	// insert is undone and the key is named. Enumerating them by hand missed three.
	undo, inserted := f.insert(path, value)
	if !inserted {
		// Unreachable: the lookup above established the key is absent, and the dotted name insert builds
		// addresses the same place. Reported rather than returned as success, because a caller told a
		// write landed when nothing was written has no way to find out.
		return fmt.Errorf("%s: the document already holds this key", key)
	}
	if err := f.decodable(); err != nil {
		// Nothing to drop: the decode that just failed left no cache behind, which is why the undo needs
		// no invalidation of its own.
		undo()
		return fmt.Errorf("%s: %w", key, err)
	}
	return nil
}

// decodable reports whether the document still renders to something the node's decoder can read.
//
// The one check every edit passes through, so a shape no caller anticipated is refused as surely as one
// somebody did. Rendering is what a later process reads, so this asks the question in the form the
// answer matters in.
func (f *File) decodable() error {
	_, err := f.decoded()
	return err
}

// insert adds a key the document does not have yet.
//
// A key with no dots belongs at the top level. Otherwise it goes in the table its prefix names,
// which is created when it is absent so writing the first key of a section works without the
// operator having to add the heading by hand.
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
	// No section carries this name. It may still be a table the document created by writing a dotted key,
	// and a heading for one of those defines it twice, so the leaf joins that dotted name instead. Where
	// nothing has created it, the heading is new and correct, which is the form an operator expects to
	// read.
	if owner, under := f.ancestorOf(table); owner != nil {
		return appendItem(owner, &parser.KeyValue{Name: dottedName(under, leaf), Value: value})
	}
	before := len(f.doc.Sections)
	f.doc.Sections = append(f.doc.Sections, &tomledit.Section{
		Heading: &parser.Heading{Name: copyKey(table)},
		Items:   []parser.Item{kv},
	})
	return func() { f.doc.Sections = f.doc.Sections[:before] }, true
}

// ancestorOf returns the section a table's keys belong in when the table has no heading of its own, and
// the path from that section down to the table.
//
// A section whose heading is a prefix of the table owns it, and the longest such heading is the nearest
// ancestor. The global section owns it when a top-level dotted key has already created it: that section
// has no heading, so no prefix can find it, and a heading written for the table would be the second
// definition the decoder refuses.
func (f *File) ancestorOf(table parser.Key) (*tomledit.Section, parser.Key) {
	// At most one section can qualify, so the first match is the only match. Two would need a section
	// [a] holding a dotted key beginning b. alongside a section [a.b], and that names the table b twice,
	// which the decoder refuses at the door.
	for _, s := range f.doc.Sections {
		if s.Heading == nil || !s.Name.IsPrefixOf(table) {
			continue
		}
		if below := table[len(s.Name):]; createsTable(s, below) {
			return s, copyKey(below)
		}
	}
	if f.doc.Global != nil && createsTable(f.doc.Global, table) {
		return f.doc.Global, copyKey(table)
	}
	return nil, nil
}

// createsTable reports whether a dotted key in this section already names the given table.
//
// Every proper prefix of a dotted key names a table, so flatkv.enable creates flatkv without giving it a
// heading. Only such a table is joined by extending a dotted name; one nothing has created gets a
// heading of its own, so a table is spelled the same way whatever order its keys were written in.
func createsTable(s *tomledit.Section, table parser.Key) bool {
	for _, item := range s.Items {
		kv, ok := item.(*parser.KeyValue)
		if !ok {
			continue
		}
		for i := 1; i < len(kv.Name); i++ {
			if kv.Name[:i].Equals(table) {
				return true
			}
		}
	}
	return false
}

// copyKey returns a key that shares no storage with its argument.
//
// The paths here are slices of one another, so appending to a shorter one would write into the longer
// one's storage.
func copyKey(k parser.Key) parser.Key { return append(parser.Key(nil), k...) }

// dottedName joins the path down to a table with the key inside it.
func dottedName(under parser.Key, leaf parser.Key) parser.Key {
	return append(copyKey(under), leaf...)
}

// appendItem adds an item to a section, and reports how to remove it again and whether it went in.
//
// Told not to replace, so a key already present is reported rather than overwritten. The distinction
// matters twice: replacing would leave the undo deleting an entry that predated the edit, and a caller
// needs to know a write did not happen rather than being told it did.
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

// Unset removes a key and reports whether the file carried one.
//
// This removes the key rather than writing a zero, because an absent key resolves to the running
// binary's default. A key set to its default value looks identical in the file but is a commitment
// that survives a release changing that default, which is the opposite of what unset means.
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
		// Reported rather than returned as an absent key, which is what the file carrying one and the
		// removal doing nothing would otherwise look like to a caller.
		return false, fmt.Errorf("%s: the file carries this key and it could not be removed", key)
	}
	return true, nil
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

// unsignedValue renders an unsigned integer, refusing one no reader can hand back.
//
// A TOML integer is signed and decodes into an int64, so a value above its maximum renders as a line
// that reads back as an error rather than a number. Refused here for the same reason an infinity is:
// this package does not write what it cannot read.
func unsignedValue(x uint64) (parser.Value, error) {
	if x > math.MaxInt64 {
		return parser.Value{}, fmt.Errorf("%d is larger than a configuration file's integers go, which "+
			"reach %d", x, int64(math.MaxInt64))
	}
	return parser.ParseValue(strconv.FormatUint(x, 10))
}

// floatValue renders a float as a TOML float, which the shortest form of an integral one is not.
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
func basicString(s string) (string, error) {
	if !utf8.ValidString(s) {
		// The escaper substitutes a replacement rune for a byte that is not valid UTF-8, so writing one
		// would store a different value than the caller passed and no error would say so.
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
