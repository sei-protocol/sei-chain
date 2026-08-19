package seitoml

import (
	"fmt"
	"math"

	toml "github.com/pelletier/go-toml/v2"
)

// Values returns every key the file writes, as dotted paths to Go values.
//
// This leaves out the schema version and the node mode. Both describe the file rather than configuring
// the node, so a reader checking written keys against the declared set would otherwise report them as
// keys no section owns, on every node, forever.
func (f *File) Values() (map[string]any, error) {
	all, err := f.decoded()
	if err != nil {
		return nil, err
	}
	// Built rather than filtered in place, because decoded hands back the cache itself. Deleting the two
	// describing keys from it made every later Version, Mode and Get read them as absent.
	out := make(map[string]any, len(all))
	for key, v := range all {
		if key == VersionKey || key == ModeKey {
			continue
		}
		out[key] = handedOut(v)
	}
	return out, nil
}

// Get returns one key's written value.
func (f *File) Get(key string) (any, bool, error) {
	path, err := keyOf(key)
	if err != nil {
		return nil, false, err
	}
	all, err := f.decoded()
	if err != nil {
		return nil, false, err
	}
	v, ok := all[path.String()]
	return handedOut(v), ok, nil
}

// handedOut returns a value a caller can change without changing what a later read answers.
//
// A list decodes to a slice the cache holds, so returning that slice shares its backing array, and a
// caller sorting or index-assigning what it was given rewrites the cache. Copied on the way out rather
// than once at decode, because a caller changes what it holds at any point after it holds it.
//
// Only a list needs copying. A scalar is copied by the assignment, and a leaf is never a table: the two
// shapes that would put one here, an inline table and an array of tables, are both refused when the file
// is read and neither can be written, so nothing reaches here as a map.
func handedOut(v any) any {
	list, ok := v.([]any)
	if !ok {
		return v
	}
	out := make([]any, len(list))
	for i, element := range list {
		out[i] = handedOut(element)
	}
	return out
}

// decoded renders the document and reads it back as Go values, keyed by dotted path.
//
// The values come from the decoder a node reads its configuration with, which is what makes "this file
// parses" and "this node can boot from it" the same statement. viper decodes TOML with
// pelletier/go-toml/v2, so this does too, and a shape that library refuses is refused here rather than
// discovered on a node.
//
// The editing parser locates lines and preserves comments, which is why it is also here, and it stops
// short of interpreting a literal. Deciding what "1_000" or a multi-line string means is a second
// implementation of the specification, and a hand-written one went wrong in four places.
//
// Rendering first rather than holding the source means an unsaved edit is read back through the same
// path a later process would use, so a value this package cannot express fails here rather than on a
// node.
//
// The map returned is the cache, and so is every list in it. Every caller here reads them, and one that
// hands either outward passes it through handedOut; writing into this map or a list it holds would
// change what a later read answers.
func (f *File) decoded() (map[string]any, error) {
	if f.values != nil {
		return f.values, nil
	}
	raw, err := f.Bytes()
	if err != nil {
		return nil, err
	}
	out, err := decodeBytes(raw)
	if err != nil {
		return nil, err
	}
	f.values = out
	return out, nil
}

// decodeBytes reads a rendered document as dotted paths to Go values.
//
// Separate from decoded so that a caller holding the rendering already, such as Save, does not render it
// a second time to check it.
func decodeBytes(raw []byte) (map[string]any, error) {
	var nested map[string]any
	if err := toml.Unmarshal(raw, &nested); err != nil {
		return nil, fmt.Errorf("read sei.toml: %w", err)
	}
	out := make(map[string]any, len(nested))
	flatten("", nested, out)
	if err := refuseNonFiniteNumbers(out); err != nil {
		return nil, err
	}
	return out, nil
}

// refuseNonFiniteNumbers rejects an infinity or a NaN a decoder accepted.
//
// TOML spells both as words and a conforming decoder reads them, so a file can hold one. This file
// cannot write one back, because rendering it produces a line no reader loads, so accepting one here
// would mean any later edit of any other key failed on a value this package had handed out.
func refuseNonFiniteNumbers(values map[string]any) error {
	for key, v := range values {
		if err := finite(key, v); err != nil {
			return err
		}
	}
	return nil
}

// finite reports whether a value, or any element of a list, is a number this file can write back.
func finite(key string, v any) error {
	switch x := v.(type) {
	case float64:
		if math.IsInf(x, 0) || math.IsNaN(x) {
			return fmt.Errorf("%s is %v, and a configuration value has to be a finite number", key, x)
		}
	case []any:
		for i, element := range x {
			if err := finite(fmt.Sprintf("%s element %d", key, i), element); err != nil {
				return err
			}
		}
	}
	return nil
}

// flatten expands a decoded table into dotted keys, keeping only the leaves.
//
// A table contributes its name as a prefix and no value of its own, which is what makes the result one
// entry per written key and comparable against a set of declared keys.
func flatten(prefix string, in, out map[string]any) {
	for name, v := range in {
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		if table, ok := v.(map[string]any); ok {
			flatten(key, table, out)
			continue
		}
		out[key] = v
	}
}

// stringValue reads one of the keys that describe the file.
//
// Both are read before anything else, and neither has a sensible reading when it is absent or holds
// something other than a string, so each caller states its own consequence rather than sharing one.
func (f *File) stringValue(key string) (string, bool, error) {
	all, err := f.decoded()
	if err != nil {
		return "", false, err
	}
	v, ok := all[key]
	if !ok {
		return "", false, nil
	}
	s, ok := v.(string)
	if !ok {
		return "", true, fmt.Errorf("%s is %T (%v), want a mode name", key, v, v)
	}
	return s, true, nil
}

// intValue reads one of the keys that describe the file as a whole number.
func (f *File) intValue(key string) (int64, bool, error) {
	all, err := f.decoded()
	if err != nil {
		return 0, false, err
	}
	v, ok := all[key]
	if !ok {
		return 0, false, nil
	}
	n, ok := v.(int64)
	if !ok {
		return 0, true, fmt.Errorf("%s is %T (%v), want an integer", key, v, v)
	}
	return n, true, nil
}
