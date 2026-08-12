package seitoml

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/creachadair/tomledit"
	"github.com/creachadair/tomledit/parser"
	"github.com/creachadair/tomledit/scanner"
)

// Values returns every key the file writes, as dotted paths to Go values.
//
// This leaves out the schema version and the node mode. Both describe the file rather than
// configuring the node, so a reader checking written keys against the declared set would otherwise
// report them as keys no section owns, on every node, forever.
func (f *File) Values() (map[string]any, error) {
	out := map[string]any{}
	var bad error

	f.doc.Scan(func(full parser.Key, e *tomledit.Entry) bool {
		if e.KeyValue == nil {
			return true // a table heading carries no value of its own
		}
		key := strings.ToLower(full.String())
		if key == VersionKey || key == ModeKey {
			return true
		}
		v, err := goValue(e.Value)
		if err != nil {
			bad = fmt.Errorf("%s: %w", key, err)
			return false
		}
		// An inline table is one written value holding several keys, so it flattens into the same
		// dotted space as a table would. Left nested, its leaves would be invisible to any check
		// that walks declared keys.
		if inline, ok := v.(map[string]any); ok {
			for sub, sv := range flatten(key, inline) {
				out[sub] = sv
			}
			return true
		}
		out[key] = v
		return true
	})
	if bad != nil {
		return nil, bad
	}
	return out, nil
}

// Get returns one key's written value.
func (f *File) Get(key string) (any, bool, error) {
	path, err := keyOf(key)
	if err != nil {
		return nil, false, err
	}
	e := f.doc.First(path...)
	if e == nil || e.KeyValue == nil {
		return nil, false, nil
	}
	v, err := goValue(e.Value)
	if err != nil {
		return nil, false, fmt.Errorf("%s: %w", key, err)
	}
	return v, true, nil
}

// flatten expands an inline table into dotted keys under prefix.
func flatten(prefix string, m map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		key := prefix + "." + k
		if nested, ok := v.(map[string]any); ok {
			for sub, sv := range flatten(key, nested) {
				out[sub] = sv
			}
			continue
		}
		out[key] = v
	}
	return out
}

// goValue converts a parsed value to the Go value a reader sees.
//
// Integers arrive as int64 and floats as float64, matching what a TOML decoder produces, so a
// comparison against a baseline does not have to know which parser read the file. A date or time
// comes back as its text: nothing configures a node with one, and the text keeps such a key visible
// to a check rather than dropping it.
func goValue(v parser.Value) (any, error) {
	switch d := v.X.(type) {
	case parser.Token:
		return tokenValue(d)
	case parser.Array:
		return arrayValue(d)
	case parser.Inline:
		return inlineValue(d)
	default:
		return nil, fmt.Errorf("unsupported value %T", v.X)
	}
}

// tokenValue converts a single literal.
func tokenValue(t parser.Token) (any, error) {
	text := t.String()
	switch t.Type {
	case scanner.Word:
		switch text {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
		return nil, fmt.Errorf("%q is not a value TOML recognizes", text)
	case scanner.String, scanner.MString, scanner.LString, scanner.MLString:
		return unquote(t.Type, text)
	case scanner.Integer:
		n, err := strconv.ParseInt(strings.ReplaceAll(text, "_", ""), 0, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not an integer: %w", text, err)
		}
		return n, nil
	case scanner.Float:
		x, err := strconv.ParseFloat(strings.ReplaceAll(text, "_", ""), 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number: %w", text, err)
		}
		return x, nil
	case scanner.DateTime, scanner.LocalDate, scanner.LocalTime, scanner.LocalDateTime:
		return text, nil
	default:
		return nil, fmt.Errorf("%q is not a value (%v)", text, t.Type)
	}
}

// unquote strips a string literal's quoting.
//
// A basic string carries escapes and needs them decoded; a literal string reads exactly as written,
// which is the whole reason TOML has both. Getting this backwards turns a Windows path's
// backslashes into control characters.
func unquote(kind scanner.Token, text string) (string, error) {
	switch kind {
	case scanner.String:
		s, err := strconv.Unquote(text)
		if err != nil {
			return "", fmt.Errorf("%s is not a well-formed string: %w", text, err)
		}
		return s, nil
	case scanner.MString:
		inner := strings.TrimSuffix(strings.TrimPrefix(text, `"""`), `"""`)
		s, err := strconv.Unquote(`"` + strings.ReplaceAll(inner, `"`, `\"`) + `"`)
		if err != nil {
			return "", fmt.Errorf("%s is not a well-formed string: %w", text, err)
		}
		return strings.TrimPrefix(s, "\n"), nil
	case scanner.LString:
		return strings.TrimSuffix(strings.TrimPrefix(text, `'`), `'`), nil
	case scanner.MLString:
		inner := strings.TrimSuffix(strings.TrimPrefix(text, `'''`), `'''`)
		return strings.TrimPrefix(inner, "\n"), nil
	default:
		return "", fmt.Errorf("%v is not a string", kind)
	}
}

// arrayValue converts an array, skipping the comment lines written between its items.
func arrayValue(a parser.Array) (any, error) {
	out := make([]any, 0, len(a))
	for _, item := range a {
		v, ok := item.(parser.Value)
		if !ok {
			continue // a comment between items
		}
		gv, err := goValue(v)
		if err != nil {
			return nil, err
		}
		out = append(out, gv)
	}
	return out, nil
}

// inlineValue converts an inline table to a map its caller flattens.
func inlineValue(in parser.Inline) (any, error) {
	out := map[string]any{}
	for _, kv := range in {
		if kv == nil {
			continue
		}
		v, err := goValue(kv.Value)
		if err != nil {
			return nil, err
		}
		out[strings.ToLower(kv.Name.String())] = v
	}
	return out, nil
}
