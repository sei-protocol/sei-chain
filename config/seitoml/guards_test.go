package seitoml

import (
	"strings"
	"testing"

	"github.com/creachadair/tomledit"
)

// The shapes below arise only from a document assembled in code rather than parsed from a file, so
// they are driven from inside the package.

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
