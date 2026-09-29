package seitoml

import (
	"fmt"
	"math"
	"sort"

	toml "github.com/pelletier/go-toml/v2"
)

// Values returns every key the file writes, as dotted paths to Go values, except VersionKey and ModeKey.
func (f *File) Values() (map[string]any, error) {
	all, err := f.decoded()
	if err != nil {
		return nil, err
	}
	// Copied, because decoded returns the cache.
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

// handedOut returns a copy of a cached value that shares no storage with the cache. Only lists need
// copying: a leaf is never a table, since the file refuses inline tables and arrays of tables.
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

// decoded renders the document and decodes it with the node's own TOML decoder, keyed by dotted path.
// The result is cached; callers must not modify it and pass anything they return through handedOut.
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

// refuseNonFiniteNumbers rejects an infinity or a NaN, which the file cannot write back.
func refuseNonFiniteNumbers(values map[string]any) error {
	// Sorted so the first refusal reported is stable.
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := finite(key, values[key]); err != nil {
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

// stringValue reads a top-level string key, reporting whether it is present.
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
