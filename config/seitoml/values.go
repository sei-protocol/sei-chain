package seitoml

import (
	"fmt"
	"math"

	"github.com/BurntSushi/toml"
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
	delete(all, VersionKey)
	delete(all, ModeKey)
	return all, nil
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
	return v, ok, nil
}

// decoded renders the document and reads it back as Go values, keyed by dotted path.
//
// The values come from a TOML decoder rather than from the editing parser's tokens. The editing parser
// locates lines and preserves comments, which is why it is here, and it deliberately stops short of
// interpreting a literal. Deciding what "1_000" or a multi-line string means is a second implementation
// of the TOML specification, and the difference between Go's string grammar and TOML's is where a
// hand-written one goes wrong.
//
// Rendering first rather than holding the source means an unsaved edit is read back through the same
// path a later process would use, so a value this package cannot express fails here rather than on a
// node.
func (f *File) decoded() (map[string]any, error) {
	raw, err := f.Bytes()
	if err != nil {
		return nil, err
	}
	var nested map[string]any
	if _, err := toml.Decode(string(raw), &nested); err != nil {
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
