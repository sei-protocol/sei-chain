package seitoml

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/creachadair/tomledit"
	"github.com/creachadair/tomledit/parser"
	"github.com/creachadair/tomledit/scanner"
)

// Values returns every key the file writes, as dotted paths to Go values.
//
// This leaves out the schema version and the node mode. Both describe the file rather than configuring
// the node, so a reader checking written keys against the declared set would otherwise report them as
// keys no section owns, on every node, forever.
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

// goValue converts a parsed value to the Go value a reader sees.
//
// Integers arrive as int64 and floats as float64, matching what a TOML decoder produces, so a
// comparison against a default does not have to know which parser read the file. A date or time comes
// back as its text: nothing configures a node with one, and the text keeps such a key visible to a
// check rather than dropping it.
//
// There is no case for an inline table, because Parse refuses one. Every value a reader sees is
// therefore one this package can also write back.
func goValue(v parser.Value) (any, error) {
	switch d := v.X.(type) {
	case parser.Token:
		return tokenValue(d)
	case parser.Array:
		return arrayValue(d)
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
		if math.IsInf(x, 0) || math.IsNaN(x) {
			// TOML spells these as words, and ParseFloat accepts them, so a file can hold one. Refused
			// here because writing one is refused: read but not writable, a rename or an edit of any
			// other key in the file would fail on a value this package handed back.
			return nil, fmt.Errorf("%q is not a finite number, and a configuration value has to be one",
				text)
		}
		return x, nil
	case scanner.DateTime, scanner.LocalDate, scanner.LocalTime, scanner.LocalDateTime:
		return text, nil
	default:
		return nil, fmt.Errorf("%q is not a value (%v)", text, t.Type)
	}
}

// unquote strips a string literal's quoting and decodes its escapes.
//
// A basic string carries escapes and needs them decoded; a literal string reads exactly as written,
// which is the whole reason TOML has both. Getting this backwards turns a Windows path's
// backslashes into control characters.
//
// The decoding is the scanner's own rather than Go's. The two grammars differ in three places that
// each reach an operator's file: TOML has no \x escape, Go has no line-ending continuation, and Go's
// decoder rejects the literal newline that makes a multi-line string multi-line.
func unquote(kind scanner.Token, text string) (string, error) {
	switch kind {
	case scanner.String:
		return unescape(text, strings.TrimSuffix(strings.TrimPrefix(text, `"`), `"`))
	case scanner.MString:
		inner := unixNewlines(strings.TrimSuffix(strings.TrimPrefix(text, `"""`), `"""`))
		return unescape(text, foldContinuations(trimOpeningNewline(inner)))
	case scanner.LString:
		return strings.TrimSuffix(strings.TrimPrefix(text, `'`), `'`), nil
	case scanner.MLString:
		inner := unixNewlines(strings.TrimSuffix(strings.TrimPrefix(text, `'''`), `'''`))
		return trimOpeningNewline(inner), nil
	default:
		return "", fmt.Errorf("%v is not a string", kind)
	}
}

// unescape decodes a basic string's escapes, refusing one TOML does not define.
//
// The scanner substitutes a replacement rune for an undefined escape rather than failing, so a typo
// such as \q would otherwise reach a node as U+FFFD inside its configuration. text is the literal as
// written, so a refusal quotes what the operator typed rather than the decoded form.
func unescape(text, inner string) (string, error) {
	out, err := scanner.Unescape([]byte(inner))
	if err != nil {
		return "", fmt.Errorf("%s is not a well-formed string: %w", text, err)
	}
	if bytes.ContainsRune(out, utf8.RuneError) && !strings.ContainsRune(inner, utf8.RuneError) {
		return "", fmt.Errorf("%s carries an escape TOML does not define", text)
	}
	return string(out), nil
}

// unixNewlines rewrites a carriage return and newline pair as a newline alone.
//
// The line ending an editor chose is not part of the value. A default in the binary carries a bare
// newline, so a file saved on Windows would differ from a default it matches, on every line, and a diff
// would report a change nobody can see. Rendering already writes bare newlines, so this is what makes
// reading agree with writing.
//
// A carriage return the operator wrote as an escape is two characters here and survives, since escapes
// are decoded after this runs.
func unixNewlines(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// trimOpeningNewline drops the newline TOML allows immediately after a multi-line delimiter, so the
// value starts at the operator's first line of content.
func trimOpeningNewline(s string) string { return strings.TrimPrefix(s, "\n") }

// foldContinuations removes a backslash that ends a line together with the whitespace following it.
//
// This is how TOML lets one value span several lines without carrying the newlines into it. An escape
// consumes the character after it, so a doubled backslash is written through rather than read as a
// continuation; otherwise a value ending in a path separator would swallow the next line.
func foldContinuations(s string) string {
	var out strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '\\' {
			out.WriteByte(s[i])
			i++
			continue
		}
		if end, folded := continuationEnd(s, i); folded {
			i = end
			continue
		}
		out.WriteByte(s[i])
		i++
		if i < len(s) {
			out.WriteByte(s[i])
			i++
		}
	}
	return out.String()
}

// continuationEnd reports where a continuation starting at the backslash in s[i] ends, if it is one.
func continuationEnd(s string, i int) (int, bool) {
	j := i + 1
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	if j >= len(s) || (s[j] != '\n' && s[j] != '\r') {
		return 0, false
	}
	for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\r' || s[j] == '\n') {
		j++
	}
	return j, true
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
