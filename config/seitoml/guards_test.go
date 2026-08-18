package seitoml

import (
	"strings"
	"testing"

	"github.com/creachadair/tomledit"
	"github.com/creachadair/tomledit/parser"
	"github.com/creachadair/tomledit/scanner"
)

// The guards below cannot be reached by parsing a file, because the parser refuses the input that
// would reach them: a bare word, a malformed escape, an unterminated table. They exist because the
// parser is a dependency, and a version of it that accepted more would otherwise turn an unknown token
// into a plausible Go value rather than a refusal. Driving them here is what keeps that refusal real
// instead of assumed, so this test is in the package rather than beside it.

// TestAnUnknownTokenIsRefusedRatherThanGuessed holds the value decoder's own vocabulary.
func TestAnUnknownTokenIsRefusedRatherThanGuessed(t *testing.T) {
	for _, tc := range []struct {
		name string
		tok  parser.Token
		want string
	}{
		{"a word that is neither true nor false", retyped(t, "3", scanner.Word),
			"not a value TOML recognizes"},
		{"a token type that is not a value", retyped(t, "3", scanner.LBracket),
			"is not a value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tokenValue(tc.tok)
			if err == nil {
				t.Fatalf("%s decoded to %#v; an unknown token becoming a value is how a node runs a "+
					"setting nobody wrote", tc.name, got)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal reads %q, which does not mention %q", err, tc.want)
			}
		})
	}
}

// TestAValueShapeWithNoDecoderIsRefused covers the outer switch over a parsed value.
func TestAValueShapeWithNoDecoderIsRefused(t *testing.T) {
	if _, err := goValue(parser.Value{}); err == nil {
		t.Error("a value carrying no shape this package knows decoded without complaint")
	}
}

// TestAnUndefinedEscapeIsRefusedRatherThanReplaced covers the scanner's substitution behaviour.
//
// The scanner writes a replacement rune for an escape TOML does not define rather than failing, so
// without this check a typo such as \q would reach a node as U+FFFD inside a configuration value. The
// parser rejects the escape before a file can carry one, which is why this drives the decoder directly.
func TestAnUndefinedEscapeIsRefusedRatherThanReplaced(t *testing.T) {
	if _, err := unescape(`"a\qc"`, `a\qc`); err == nil {
		t.Error("an escape TOML does not define decoded without complaint")
	}
	// A replacement rune the operator actually wrote is theirs to keep.
	got, err := unescape("\"a\ufffdc\"", "a\ufffdc")
	if err != nil || got != "a\ufffdc" {
		t.Errorf("a written replacement rune came back as (%q, %v), want it preserved", got, err)
	}
}

// TestUnquoteRefusesAKindThatIsNotAString covers the string decoder's own guard.
//
// unquote picks the escaping rules from the token kind, so a kind it does not know has no rules to
// apply. Returning the text as written would decode a basic string's escapes as literal backslashes.
func TestUnquoteRefusesAKindThatIsNotAString(t *testing.T) {
	if _, err := unquote(scanner.Integer, "3"); err == nil {
		t.Error("unquote accepted a kind that is not a string")
	}
}

// TestATopLevelKeyReachesADocumentWithNoGlobalSection covers a document built rather than parsed.
//
// Parsing always produces a global section, even for a file whose first line is a table heading, so
// this shape only arises from a document assembled in code. Writing the schema version into one has to
// create the space rather than panic.
func TestATopLevelKeyReachesADocumentWithNoGlobalSection(t *testing.T) {
	f := &File{doc: &tomledit.Document{}}
	if err := f.Set(ModeKey, "seed"); err != nil {
		t.Fatalf("Set on a document with no global section: %v", err)
	}
	mode, err := f.Mode()
	if err != nil || mode != "seed" {
		t.Errorf("Mode = (%q, %v), want seed", mode, err)
	}
}

// TestAPreambleReachesADocumentWithNoGlobalSection is the preamble's half of the same shape.
func TestAPreambleReachesADocumentWithNoGlobalSection(t *testing.T) {
	f := &File{doc: &tomledit.Document{}}
	f.SetPreamble([]string{" a header"})

	raw, err := f.Bytes()
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !strings.Contains(string(raw), "# a header") {
		t.Errorf("the preamble is not in the rendered document: %q", raw)
	}
}

// retyped parses a literal and relabels the token's type, which is how a decoder is driven over a
// token the parser will not produce from any file. A token's text is not settable from outside the
// parser, so the text stays whatever the literal scanned as and only the label changes.
func retyped(t *testing.T, literal string, kind scanner.Token) parser.Token {
	t.Helper()
	v, err := parser.ParseValue(literal)
	if err != nil {
		t.Fatalf("ParseValue(%q): %v", literal, err)
	}
	tok, ok := v.X.(parser.Token)
	if !ok {
		t.Fatalf("ParseValue(%q) is a %T, want a token", literal, v.X)
	}
	tok.Type = kind
	return tok
}
