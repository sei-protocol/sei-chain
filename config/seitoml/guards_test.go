package seitoml

import (
	"reflect"
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

// TestAReadReusesItsDecodeAndNeverAStaleOne drives the cache itself, which only this package can see.
//
// A read renders the document and decodes it, so a caller walking every key would pay that per key. Two
// properties make the saving safe, and neither is visible from outside: a read with no edit before it
// reuses the last decode, and no edit ever leaves a decode behind that describes the document as it was.
// The second is the one that would be a correctness bug rather than a lost saving.
func TestAReadReusesItsDecodeAndNeverAStaleOne(t *testing.T) {
	newFile := func(t *testing.T) *File {
		t.Helper()
		f, err := Parse(strings.NewReader("schema_version = 1\nnode_mode = \"validator\"\n\n[probe]\nn = 1\n"))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		return f
	}

	t.Run("a second read reuses the first", func(t *testing.T) {
		f := newFile(t)
		first, err := f.decoded()
		if err != nil {
			t.Fatalf("decoded: %v", err)
		}
		// Written into the map the first read returned. A second read that decoded again would hand back
		// a map without it.
		first["probe.sentinel"] = true
		second, err := f.decoded()
		if err != nil {
			t.Fatalf("decoded: %v", err)
		}
		if _, reused := second["probe.sentinel"]; !reused {
			t.Error("a read with no edit before it decoded the document again")
		}
	})

	for _, tc := range []struct {
		name string
		edit func(*testing.T, *File)
	}{
		{"Set replacing a value", func(t *testing.T, f *File) {
			if err := f.Set("probe.n", 2); err != nil {
				t.Fatalf("Set: %v", err)
			}
		}},
		{"Set adding a key", func(t *testing.T, f *File) {
			if err := f.Set("probe.m", 3); err != nil {
				t.Fatalf("Set: %v", err)
			}
		}},
		{"Set adding a section", func(t *testing.T, f *File) {
			if err := f.Set("p2p.laddr", "x"); err != nil {
				t.Fatalf("Set: %v", err)
			}
		}},
		{"Unset", func(t *testing.T, f *File) {
			if _, err := f.Unset("probe.n"); err != nil {
				t.Fatalf("Unset: %v", err)
			}
		}},
		{"a Set the decoder refused", func(t *testing.T, f *File) {
			// Refused after the write, so the document changed and changed back. A decode of either state
			// in between describes neither.
			if err := f.Set("probe.n.deeper", 4); err == nil {
				t.Fatal("writing a table over a value was accepted")
			}
		}},
	} {
		t.Run("after "+tc.name, func(t *testing.T) {
			f := newFile(t)
			if _, err := f.decoded(); err != nil {
				t.Fatalf("decoded: %v", err)
			}
			tc.edit(t, f)

			held := f.values
			if held == nil {
				return // nothing cached, so nothing can be stale
			}
			raw, err := f.Bytes()
			if err != nil {
				t.Fatalf("Bytes: %v", err)
			}
			fresh, err := decodeBytes(raw)
			if err != nil {
				t.Fatalf("decodeBytes: %v", err)
			}
			if !reflect.DeepEqual(held, fresh) {
				t.Errorf("%s left a decode describing another document:\n held %v\nfresh %v",
					tc.name, held, fresh)
			}
		})
	}
}
